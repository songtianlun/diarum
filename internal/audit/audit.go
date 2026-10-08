// Package audit keeps the system-wide audit trail: every API call (who, what,
// from where, with which result) plus the business meaning handlers attach to
// it, such as "edited the diary entry for 2026-09-28".
//
// Entries are JSON lines in one file per server-local day under
// <data>/logs/system-audit/YYYY-MM-DD.log. The trail is deliberately kept out
// of the database: it must stay readable when the database is the thing being
// investigated, and writing it must never be able to slow down or fail a
// request. Finished days can be archived to S3 (see archive.go) and pulled
// back into <data>/logs/system-audit/pulled/ for analysis.
//
// Every exported method is safe on a nil *Logger, which does nothing.
package audit

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/songtianlun/diarum/internal/logger"
)

// Actions handlers attach to requests. Requests without one are still
// recorded, identified by method and route.
const (
	ActionDiaryCreate  = "diary.create"
	ActionDiaryUpdate  = "diary.update"
	ActionDiaryDelete  = "diary.delete"
	ActionDiaryRestore = "diary.restore"
	ActionDiaryView    = "diary.view"
	ActionDiarySearch  = "diary.search"
	ActionConvDelete   = "conversation.delete"
	ActionMediaUpload  = "media.upload"
	// ActionMediaDelete is what deleting an image was logged as before the
	// trash existed; deleting now moves the image to the trash.
	ActionMediaDelete    = "media.delete"
	ActionMediaTrash     = "media.trash"
	ActionMediaRestore   = "media.restore"
	ActionMediaPurge     = "media.purge"
	ActionDataImport     = "data.import"
	ActionDataExport     = "data.export"
	ActionSettingsUpdate = "settings.update"
	ActionTokenUpdate    = "token.update"
	ActionAuthLogin      = "auth.login"
	ActionAuthLoginFail  = "auth.login_failed"
	ActionAuthLogout     = "auth.logout"
	ActionAuthRegister   = "auth.register"
	ActionAuthDenied     = "auth.denied"
	ActionAuthForbidden  = "auth.forbidden"
	ActionAdminRole      = "admin.role_change"
	ActionAdminAudit     = "admin.audit_settings"
	ActionAdminPull      = "admin.audit_pull"
	ActionAdminVisits    = "admin.visits_settings"
	ActionAdminVisitPull = "admin.visits_pull"
	ActionAdminVisitDrop = "admin.visits_unload"
)

// Sources describe which entry point performed an action.
const (
	SourceWeb    = "web"
	SourceAPI    = "api"
	SourceMCP    = "mcp"
	SourceMemos  = "memos"
	SourceCLI    = "cli"
	SourceSystem = "system"
)

const (
	// LogDirName is the log directory inside the data directory.
	LogDirName = "logs"
	// DirName is the system audit directory inside the log directory.
	DirName = "system-audit"
	// PulledDirName holds archived days pulled back from S3.
	PulledDirName = "pulled"
	// LegacyDirName is where the former per-user audit trail lived.
	LegacyDirName = "audit"

	fileDateLayout = "2006-01-02"
	fileSuffix     = ".log"
	maxUALength    = 256
	maxErrorLength = 300
	maxLineBytes   = 1 << 20

	// queueSize bounds memory if the disk stalls; Record never blocks on it.
	queueSize = 16384
	// Lines are batched in memory and written at most this far apart, or
	// sooner once the batch reaches bufferLimit.
	defaultFlushInterval = time.Second
	bufferLimit          = 256 << 10
	// Readers wait at most this long for queued entries to reach disk.
	flushWait = 3 * time.Second
)

// Entry is one line in the trail. Diary contents are never logged; Detail
// only carries metadata such as word counts, mood and weather.
type Entry struct {
	Time time.Time `json:"time"`
	// UserID and User identify the account acting; empty for anonymous
	// requests. User (the username) may be missing when only the ID was known.
	UserID string `json:"user_id,omitempty"`
	User   string `json:"user,omitempty"`
	Source string `json:"source,omitempty"`
	Action string `json:"action,omitempty"`
	Target string `json:"target,omitempty"`
	// Request facts, filled in by the HTTP middleware.
	Method   string         `json:"method,omitempty"`
	Route    string         `json:"route,omitempty"`
	Path     string         `json:"path,omitempty"`
	Status   int            `json:"status,omitempty"`
	Duration int64          `json:"ms,omitempty"`
	IP       string         `json:"ip,omitempty"`
	UA       string         `json:"ua,omitempty"`
	Error    string         `json:"error,omitempty"`
	Detail   map[string]any `json:"detail,omitempty"`
}

// Options configures a Logger.
type Options struct {
	// Settings persists the audit settings; nil keeps them in memory.
	Settings SettingsStore
	// Location names the daily files; defaults to time.Local.
	Location *time.Location
	// Now overrides the clock, for tests.
	Now func() time.Time
	// FlushInterval is how often batched lines are written out.
	FlushInterval time.Duration
	// NewObjectStore overrides how the S3 archive is reached, for tests.
	NewObjectStore ObjectStoreFactory
	// SchedulerDelay is how long after StartScheduler the first cleanup
	// runs; defaults to 30 seconds.
	SchedulerDelay time.Duration
}

// Logger writes and reads the trail. Recording is asynchronous: Record only
// queues the entry, and one background goroutine owns every write, so a slow
// or failing disk never delays a request. Failures are logged loudly.
type Logger struct {
	root      string
	loc       *time.Location
	now       func() time.Time
	flushTick time.Duration
	// schedulerDelay postpones the first scheduled cleanup after start.
	schedulerDelay time.Duration

	ops  chan op
	stop chan struct{}
	done chan struct{}

	closeMu   sync.RWMutex
	closed    bool
	closeOnce sync.Once
	dropped   atomic.Int64
	written   atomic.Int64

	// Owned by the writer goroutine.
	buf      []byte
	bufDate  string
	file     *os.File
	fileDate string

	archiver *archiver
}

type op struct {
	entry *Entry
	// done, when set, is closed once everything queued before it is on disk.
	done chan struct{}
}

// Dir returns the system audit directory for a data directory.
func Dir(dataDir string) string {
	return filepath.Join(dataDir, LogDirName, DirName)
}

// New creates the audit directory and starts the background writer and the
// archive/cleanup scheduler. Stop both with Close.
func New(dataDir string, opts Options) (*Logger, error) {
	root := Dir(dataDir)
	if err := os.MkdirAll(filepath.Join(root, PulledDirName), 0o700); err != nil {
		return nil, err
	}
	l := &Logger{
		root:      root,
		loc:       opts.Location,
		now:       opts.Now,
		flushTick: opts.FlushInterval,
		ops:       make(chan op, queueSize),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	if l.loc == nil {
		l.loc = time.Local
	}
	if l.now == nil {
		l.now = time.Now
	}
	if l.flushTick <= 0 {
		l.flushTick = defaultFlushInterval
	}
	l.schedulerDelay = opts.SchedulerDelay
	if l.schedulerDelay <= 0 {
		l.schedulerDelay = startupDelay
	}
	l.archiver = newArchiver(l, opts.Settings, opts.NewObjectStore)
	go l.run()
	return l, nil
}

// RemoveLegacy deletes the directory of the former per-user audit trail,
// which the system-wide trail replaces.
func RemoveLegacy(dataDir string) {
	dir := filepath.Join(dataDir, LogDirName, LegacyDirName)
	if _, err := os.Stat(dir); err != nil {
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		logger.Warn("[Audit] could not remove legacy per-user audit logs at %s: %v", dir, err)
		return
	}
	logger.Info("[Audit] removed legacy per-user audit logs at %s", dir)
}

func (l *Logger) run() {
	defer close(l.done)
	flush := time.NewTicker(l.flushTick)
	defer flush.Stop()
	for {
		select {
		case o := <-l.ops:
			l.handle(o)
		case <-flush.C:
			l.safely("flush", l.flushBuffer)
		case <-l.stop:
			// Record refuses new entries once closed; write what is queued.
			for {
				select {
				case o := <-l.ops:
					l.handle(o)
				default:
					l.safely("flush", l.flushBuffer)
					l.closeFile()
					return
				}
			}
		}
	}
}

func (l *Logger) handle(o op) {
	if o.entry != nil {
		l.safely("write", func() { l.append(o.entry) })
	}
	if o.done != nil {
		l.safely("flush", l.flushBuffer)
		close(o.done)
	}
}

// safely keeps one bad entry from taking the writer goroutine down with it.
func (l *Logger) safely(what string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("[AUDIT] !!! audit %s panicked, entries lost: %v", what, r)
		}
	}()
	fn()
}

// Close stops the scheduler, stops accepting entries, writes everything
// queued and waits for the writer to finish.
func (l *Logger) Close() {
	if l == nil {
		return
	}
	l.closeOnce.Do(func() {
		l.archiver.stop()
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

// Record queues an entry and returns immediately. It never blocks, fails or
// panics: a broken audit trail must not break the operation audited. Entries
// that cannot be queued are counted and reported in the system log.
func (l *Logger) Record(e Entry) {
	if l == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			logger.Error("[AUDIT] !!! record panicked, entry lost: %v", r)
		}
	}()
	if e.Time.IsZero() {
		e.Time = l.now()
	}
	normalize(&e)

	l.closeMu.RLock()
	defer l.closeMu.RUnlock()
	if l.closed {
		l.dropped.Add(1)
		logger.Error("[AUDIT] !!! audit logger closed, dropped %s %s by %q", e.Method, e.Path+e.Action, e.UserID)
		return
	}
	select {
	case l.ops <- op{entry: &e}:
	default:
		if l.dropped.Add(1)%1000 == 1 {
			logger.Error("[AUDIT] !!! audit queue full (%d), dropping entries (%d so far)", queueSize, l.dropped.Load())
		}
	}
}

func normalize(e *Entry) {
	e.Time = e.Time.UTC()
	if len(e.UA) > maxUALength {
		e.UA = e.UA[:maxUALength]
	}
	if len(e.Error) > maxErrorLength {
		e.Error = e.Error[:maxErrorLength]
	}
}

// Flush waits until everything queued so far is on disk, so a read right
// after an action sees it.
func (l *Logger) Flush() {
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
	case l.ops <- op{done: done}:
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

// Stats about the writer itself.
type WriterStats struct {
	Written int64 `json:"written"`
	Dropped int64 `json:"dropped"`
	Queued  int   `json:"queued"`
}

// WriterStats reports how many entries were written and dropped this run.
func (l *Logger) WriterStats() WriterStats {
	if l == nil {
		return WriterStats{}
	}
	return WriterStats{Written: l.written.Load(), Dropped: l.dropped.Load(), Queued: len(l.ops)}
}

// append runs on the writer goroutine. Lines are batched per day file;
// a batch always holds whole lines so every write appends complete lines.
func (l *Logger) append(e *Entry) {
	line, err := json.Marshal(e)
	if err != nil {
		logger.Error("[AUDIT] !!! encode %s %s failed, entry lost: %v", e.Method, e.Path, err)
		return
	}
	date := e.Time.In(l.loc).Format(fileDateLayout)
	if date != l.bufDate && len(l.buf) > 0 {
		l.flushBuffer()
	}
	l.bufDate = date
	l.buf = append(l.buf, line...)
	l.buf = append(l.buf, '\n')
	if len(l.buf) >= bufferLimit {
		l.flushBuffer()
	}
}

func (l *Logger) flushBuffer() {
	if len(l.buf) == 0 {
		// Let go of yesterday's file once nothing more is headed for it.
		if l.file != nil && l.fileDate != l.now().In(l.loc).Format(fileDateLayout) {
			l.closeFile()
		}
		return
	}
	lines := int64(countNewlines(l.buf))
	defer func() {
		l.buf = l.buf[:0]
		if cap(l.buf) > 4*bufferLimit {
			l.buf = nil
		}
	}()
	if l.file == nil || l.fileDate != l.bufDate {
		l.closeFile()
		path := filepath.Join(l.root, l.bufDate+fileSuffix)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			l.dropped.Add(lines)
			logger.Error("[AUDIT] !!! open %s failed, %d entries lost: %v", path, lines, err)
			return
		}
		l.file, l.fileDate = f, l.bufDate
	}
	if _, err := l.file.Write(l.buf); err != nil {
		l.dropped.Add(lines)
		logger.Error("[AUDIT] !!! write %s failed, %d entries lost: %v", l.file.Name(), lines, err)
		// Reopen on the next batch in case the file was removed or rotated.
		l.closeFile()
		return
	}
	l.written.Add(lines)
}

func (l *Logger) closeFile() {
	if l.file == nil {
		return
	}
	if err := l.file.Close(); err != nil {
		logger.Error("[AUDIT] !!! close %s failed: %v", l.file.Name(), err)
	}
	l.file, l.fileDate = nil, ""
}

func countNewlines(b []byte) int {
	n := 0
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}

// AppendDirect writes one entry synchronously to the trail of dataDir. It is
// meant for short-lived processes such as CLI commands; a running server
// appends whole lines too, so the two never interleave within a line.
func AppendDirect(dataDir string, e Entry) error {
	root := Dir(dataDir)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	normalize(&e)
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	path := filepath.Join(root, e.Time.In(time.Local).Format(fileDateLayout)+fileSuffix)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	return errors.Join(err, f.Close())
}

func (l *Logger) today() time.Time {
	now := l.now().In(l.loc)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, l.loc)
}
