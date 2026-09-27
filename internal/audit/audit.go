// Package audit keeps a per-user, append-only trail of key operations such as
// creating, editing and deleting diary entries.
//
// Entries are JSON lines stored under <data>/logs/audit/<userID>/YYYY-MM-DD.log,
// one file per day (server local time). The trail is deliberately kept out of
// the database: it has to stay readable when the database is the thing being
// investigated, and writing it must never be able to fail a business request.
// Every exported method is safe on a nil *Logger, which simply does nothing.
package audit

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/songtianlun/diarum/internal/logger"
)

// Setting keys and bounds for how long each user's trail is kept.
const (
	SettingRetentionDays = "audit.retention_days"
	DefaultRetentionDays = 7
	MinRetentionDays     = 1
	MaxRetentionDays     = 365
)

// Actions recorded in the trail.
const (
	ActionDiaryCreate    = "diary.create"
	ActionDiaryUpdate    = "diary.update"
	ActionDiaryDelete    = "diary.delete"
	ActionDiaryRestore   = "diary.restore"
	ActionDiaryView      = "diary.view"
	ActionDiarySearch    = "diary.search"
	ActionConvDelete     = "conversation.delete"
	ActionMediaUpload    = "media.upload"
	ActionMediaDelete    = "media.delete"
	ActionDataImport     = "data.import"
	ActionDataExport     = "data.export"
	ActionSettingsUpdate = "settings.update"
	ActionTokenUpdate    = "token.update"
	ActionAuthLogin      = "auth.login"
	ActionAuthLoginFail  = "auth.login_failed"
	ActionAuthRegister   = "auth.register"
)

// Sources describe which entry point performed an action.
const (
	SourceWeb    = "web"
	SourceMCP    = "mcp"
	SourceMemos  = "memos"
	SourceSystem = "system"
)

const (
	// DirName is the audit subdirectory inside the log directory.
	DirName = "audit"
	// LogDirName is the log directory inside the data directory.
	LogDirName = "logs"

	fileDateLayout = "2006-01-02"
	fileSuffix     = ".log"
	maxUALength    = 256
	maxLineBytes   = 1 << 20

	// Autosave coalescing: the editor saves about a second after every pause
	// in typing, which would bury everything else under "updated" lines. The
	// first save of an editing session is written at once; later saves of the
	// same entry are merged and written after the session has been idle for
	// coalesceIdle, or at the latest every coalesceMaxSpan. Repeated reads of
	// the same thing from the same place within coalesceIdle are logged once.
	defaultCoalesceIdle    = 2 * time.Minute
	defaultCoalesceMaxSpan = 10 * time.Minute

	// queueSize bounds memory if the disk stalls; Record never blocks on it.
	queueSize       = 4096
	flushInterval   = 15 * time.Second
	cleanupInterval = 6 * time.Hour
	// Readers wait at most this long for queued entries to reach disk.
	flushWait = 3 * time.Second
)

// Entry is one line in the trail. Diary contents are never logged; Detail
// only carries metadata such as word counts, mood and weather.
type Entry struct {
	Time   time.Time      `json:"time"`
	User   string         `json:"user"`
	Actor  string         `json:"actor,omitempty"`
	Action string         `json:"action"`
	Target string         `json:"target,omitempty"`
	Source string         `json:"source,omitempty"`
	IP     string         `json:"ip,omitempty"`
	UA     string         `json:"ua,omitempty"`
	Detail map[string]any `json:"detail,omitempty"`
}

// Options configures a Logger.
type Options struct {
	// Retention returns how many days of logs to keep for a user. Nil means
	// DefaultRetentionDays for everyone.
	Retention func(userID string) int
	// Location names the daily files; defaults to time.Local.
	Location *time.Location
	// Now overrides the clock, for tests.
	Now func() time.Time
	// CoalesceIdle and CoalesceMaxSpan override the autosave merge window.
	CoalesceIdle    time.Duration
	CoalesceMaxSpan time.Duration
	// FlushInterval is how often idle merged entries are written out.
	FlushInterval time.Duration
}

// Logger writes and reads audit trails. Recording is asynchronous: Record
// only queues the entry, and a single background goroutine owns every write,
// so a slow or failing disk never delays the request being audited. Any
// failure is reported loudly in the system log.
type Logger struct {
	root      string
	retention func(string) int
	loc       *time.Location
	now       func() time.Time
	idle      time.Duration
	maxSpan   time.Duration
	flushTick time.Duration

	ops  chan op
	stop chan struct{}
	done chan struct{}

	closeMu   sync.RWMutex
	closed    bool
	closeOnce sync.Once
	dropped   atomic.Int64

	// Owned by the writer goroutine.
	pending   map[string]*pendingEntry
	lastWrite map[string]time.Time
}

type op struct {
	entry *Entry
	// flush, when set, asks for merged entries of flushUser (all users when
	// empty) to be written; done is closed once they are on disk.
	flush     bool
	flushUser string
	done      chan struct{}
}

type pendingEntry struct {
	entry Entry
	first time.Time
	last  time.Time
	saves int
}

var userIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Dir returns the audit directory for a data directory.
func Dir(dataDir string) string {
	return filepath.Join(dataDir, LogDirName, DirName)
}

// New creates the audit directory and starts the background writer, which
// also prunes expired files. Stop it with Close.
func New(dataDir string, opts Options) (*Logger, error) {
	root := Dir(dataDir)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	l := &Logger{
		root:      root,
		retention: opts.Retention,
		loc:       opts.Location,
		now:       opts.Now,
		idle:      opts.CoalesceIdle,
		maxSpan:   opts.CoalesceMaxSpan,
		flushTick: opts.FlushInterval,
		ops:       make(chan op, queueSize),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
		pending:   make(map[string]*pendingEntry),
		lastWrite: make(map[string]time.Time),
	}
	if l.loc == nil {
		l.loc = time.Local
	}
	if l.now == nil {
		l.now = time.Now
	}
	if l.idle <= 0 {
		l.idle = defaultCoalesceIdle
	}
	if l.maxSpan <= 0 {
		l.maxSpan = defaultCoalesceMaxSpan
	}
	if l.flushTick <= 0 {
		l.flushTick = flushInterval
	}
	go l.run()
	return l, nil
}

func (l *Logger) run() {
	defer close(l.done)
	l.CleanupAll()
	flush := time.NewTicker(l.flushTick)
	cleanup := time.NewTicker(cleanupInterval)
	defer flush.Stop()
	defer cleanup.Stop()
	for {
		select {
		case o := <-l.ops:
			l.handle(o)
		case <-flush.C:
			l.safely("flush", func() { l.flushPending("", false) })
		case <-cleanup.C:
			l.CleanupAll()
		case <-l.stop:
			// Record refuses new entries once closed; write what is queued.
			for {
				select {
				case o := <-l.ops:
					l.handle(o)
				default:
					l.safely("flush", func() { l.flushPending("", true) })
					return
				}
			}
		}
	}
}

func (l *Logger) handle(o op) {
	l.safely("write", func() {
		if o.entry != nil {
			l.apply(*o.entry)
		}
		if o.flush {
			l.flushPending(o.flushUser, true)
		}
	})
	if o.done != nil {
		close(o.done)
	}
}

// safely keeps one bad entry from taking the writer goroutine down with it.
func (l *Logger) safely(what string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("[AUDIT] !!! audit %s panicked, entry lost: %v", what, r)
		}
	}()
	fn()
}

// Close stops accepting entries, writes everything queued or merged, and
// waits for the writer to finish.
func (l *Logger) Close() {
	if l == nil {
		return
	}
	l.closeOnce.Do(func() {
		l.closeMu.Lock()
		l.closed = true
		l.closeMu.Unlock()
		close(l.stop)
		<-l.done
		if n := l.dropped.Load(); n > 0 {
			logger.Error("[AUDIT] !!! %d audit entries were dropped during this run", n)
		}
	})
}

// Record queues an entry for its user's trail and returns immediately. It
// never blocks, fails or panics: a broken audit trail must not break the
// operation audited. Entries that cannot be queued are reported in the
// system log.
func (l *Logger) Record(e Entry) {
	if l == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			logger.Error("[AUDIT] !!! record panicked, entry lost: %v", r)
		}
	}()
	if !validUserID(e.User) || e.Action == "" {
		logger.Error("[AUDIT] !!! rejected malformed audit entry: user=%q action=%q", e.User, e.Action)
		return
	}
	if e.Time.IsZero() {
		e.Time = l.now()
	}
	e.Time = e.Time.UTC()
	if len(e.UA) > maxUALength {
		e.UA = e.UA[:maxUALength]
	}

	l.closeMu.RLock()
	defer l.closeMu.RUnlock()
	if l.closed {
		l.dropped.Add(1)
		logger.Error("[AUDIT] !!! audit logger closed, dropped %s by %s on %q", e.Action, e.User, e.Target)
		return
	}
	select {
	case l.ops <- op{entry: &e}:
	default:
		l.dropped.Add(1)
		logger.Error("[AUDIT] !!! audit queue full (%d), dropped %s by %s on %q", queueSize, e.Action, e.User, e.Target)
	}
}

// Flush waits until everything queued so far for a user is on disk, so a
// read right after an action sees it.
func (l *Logger) Flush(userID string) {
	if l == nil {
		return
	}
	done := make(chan struct{})
	l.closeMu.RLock()
	if l.closed {
		l.closeMu.RUnlock()
		return
	}
	timer := time.NewTimer(flushWait)
	defer timer.Stop()
	select {
	case l.ops <- op{flush: true, flushUser: userID, done: done}:
	case <-timer.C:
		l.closeMu.RUnlock()
		logger.Warn("[AUDIT] flush request timed out; the log view may miss the newest entries")
		return
	}
	l.closeMu.RUnlock()
	select {
	case <-done:
	case <-timer.C:
		logger.Warn("[AUDIT] flush timed out; the log view may miss the newest entries")
	}
}

// apply runs on the writer goroutine.
func (l *Logger) apply(e Entry) {
	key := e.User + "\x00" + e.Target
	switch e.Action {
	case ActionDiaryUpdate:
		l.applyUpdate(key, e)
		return
	case ActionDiaryView, ActionDiarySearch:
		// Reads are frequent and repetitive (reopening a page, prefetching);
		// the same read from the same place is logged once per window.
		readKey := strings.Join([]string{e.User, e.Action, e.Target, e.Source, e.IP}, "\x00")
		if last, ok := l.lastWrite[readKey]; ok && e.Time.Sub(last) < l.idle {
			return
		}
		l.write(e)
		l.lastWrite[readKey] = e.Time
		return
	}
	// Anything else that touches the entry (delete, restore) must land after
	// the edits that came before it.
	if p, ok := l.pending[key]; ok {
		l.writePending(key, p)
	}
	l.write(e)
}

func (l *Logger) applyUpdate(key string, e Entry) {
	if p, ok := l.pending[key]; ok {
		p.saves++
		p.last = e.Time
		before := p.entry.Detail["words_before"]
		p.entry = e
		if p.entry.Detail == nil {
			p.entry.Detail = map[string]any{}
		}
		if before != nil {
			p.entry.Detail["words_before"] = before
		}
		return
	}
	if last, ok := l.lastWrite[key]; ok && e.Time.Sub(last) < l.idle {
		detail := make(map[string]any, len(e.Detail)+2)
		for k, v := range e.Detail {
			detail[k] = v
		}
		e.Detail = detail
		l.pending[key] = &pendingEntry{entry: e, first: e.Time, last: e.Time, saves: 1}
		return
	}
	l.write(e)
	l.lastWrite[key] = e.Time
}

func (l *Logger) writePending(key string, p *pendingEntry) {
	delete(l.pending, key)
	e := p.entry
	e.Time = p.last
	if e.Detail == nil {
		e.Detail = map[string]any{}
	}
	e.Detail["saves"] = p.saves
	e.Detail["since"] = p.first.UTC().Format(time.RFC3339)
	l.write(e)
	l.lastWrite[key] = p.last
}

// flushPending writes merged entries for userID (every user when empty):
// all of them when force is set, otherwise only sessions that went idle.
func (l *Logger) flushPending(userID string, force bool) {
	now := l.now()
	prefix := userID + "\x00"
	for key, p := range l.pending {
		if userID != "" && !strings.HasPrefix(key, prefix) {
			continue
		}
		if force || now.Sub(p.last) >= l.idle || now.Sub(p.first) >= l.maxSpan {
			l.writePending(key, p)
		}
	}
	if userID != "" {
		return
	}
	// Forget sessions that can no longer be merged into.
	for key, last := range l.lastWrite {
		if _, busy := l.pending[key]; !busy && now.Sub(last) >= l.idle {
			delete(l.lastWrite, key)
		}
	}
}

func (l *Logger) write(e Entry) {
	line, err := json.Marshal(e)
	if err != nil {
		logger.Error("[AUDIT] !!! encode %s by %s failed, entry lost: %v", e.Action, e.User, err)
		return
	}
	line = append(line, '\n')
	dir := l.userDir(e.User)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		logger.Error("[AUDIT] !!! create %s failed, %s by %s lost: %v", dir, e.Action, e.User, err)
		return
	}
	path := filepath.Join(dir, e.Time.In(l.loc).Format(fileDateLayout)+fileSuffix)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		logger.Error("[AUDIT] !!! open %s failed, %s by %s lost: %v", path, e.Action, e.User, err)
		return
	}
	// One write per line keeps appends whole even if the process dies.
	if _, err := f.Write(line); err != nil {
		logger.Error("[AUDIT] !!! write %s failed, %s by %s lost: %v", path, e.Action, e.User, err)
	}
	if err := f.Close(); err != nil {
		logger.Error("[AUDIT] !!! close %s failed: %v", path, err)
	}
}

func (l *Logger) userDir(userID string) string {
	return filepath.Join(l.root, userID)
}

func validUserID(userID string) bool {
	return userIDPattern.MatchString(userID)
}

// RetentionDays returns the user's retention, clamped to the allowed range.
func (l *Logger) RetentionDays(userID string) int {
	if l == nil || l.retention == nil {
		return DefaultRetentionDays
	}
	return ClampRetention(l.retention(userID))
}

// ClampRetention keeps a retention setting within the supported range.
func ClampRetention(days int) int {
	if days < MinRetentionDays {
		return MinRetentionDays
	}
	if days > MaxRetentionDays {
		return MaxRetentionDays
	}
	return days
}

// Cleanup deletes a user's files older than their retention window. Today
// counts as one of the kept days, so today's file is never touched.
func (l *Logger) Cleanup(userID string) int {
	if l == nil || !validUserID(userID) {
		return 0
	}
	days := l.RetentionDays(userID)
	cutoff := l.today().AddDate(0, 0, -(days - 1)).Format(fileDateLayout)
	files, err := l.listFiles(userID)
	if err != nil {
		logger.Error("[AUDIT] !!! list logs of %s for cleanup failed: %v", userID, err)
		return 0
	}
	removed := 0
	for _, file := range files {
		if file.Date >= cutoff {
			continue
		}
		if err := os.Remove(filepath.Join(l.userDir(userID), file.Date+fileSuffix)); err != nil && !errors.Is(err, os.ErrNotExist) {
			logger.Error("[AUDIT] !!! remove expired log %s/%s failed: %v", userID, file.Date, err)
			continue
		}
		removed++
	}
	return removed
}

// CleanupAll applies retention to every user with a trail.
func (l *Logger) CleanupAll() {
	if l == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			logger.Error("[AUDIT] !!! cleanup panicked: %v", r)
		}
	}()
	entries, err := os.ReadDir(l.root)
	if err != nil {
		logger.Error("[AUDIT] !!! read %s failed: %v", l.root, err)
		return
	}
	total := 0
	for _, entry := range entries {
		if entry.IsDir() && validUserID(entry.Name()) {
			total += l.Cleanup(entry.Name())
		}
	}
	if total > 0 {
		logger.Info("[Audit] removed %d expired log file(s)", total)
	}
}

func (l *Logger) today() time.Time {
	now := l.now().In(l.loc)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, l.loc)
}
