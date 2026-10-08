// Package visits records who reads each diary entry, so owners can audit
// access to their own writing: every read through the API (web, API token,
// MCP) and every refused attempt, signed in or not.
//
// Records live in their own SQLite file, <data>/visits/visits.db, rather than
// in the main database: a flood of reads can never bloat the database that
// holds the diaries, and the main database's backups stay small. Writing is
// asynchronous and bounded, so recording never slows down or fails a read.
//
// Several guards keep the table from growing out of hand:
//   - repeated reads of the same entry by the same visitor and device within
//     the dedupe window (one minute by default) count once;
//   - refused and anonymous attempts are rate limited per IP and globally,
//     and a single request records at most MaxPerRequest entries;
//   - records older than the retention period are deleted (archived to S3
//     first when the archive is enabled), and a hard row cap trims the
//     oldest records if the table still grows past it.
//
// Every exported method is safe on a nil *Tracker, which records nothing.
package visits

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/songtianlun/diarum/internal/logger"

	_ "modernc.org/sqlite"
)

const (
	// DirName is the directory inside the data directory.
	DirName = "visits"
	// DatabaseName is the SQLite file inside DirName.
	DatabaseName = "visits.db"

	// MaxPerRequest bounds how many entries one request may record (a range
	// read through the API returns many entries at once).
	MaxPerRequest = 500

	dayLayout = "2006-01-02"

	maxUALength     = 256
	maxIPLength     = 64
	maxRouteLength  = 96
	maxDeviceLength = 48
	maxIDLength     = 64
	maxNameLength   = 64
	maxReasonLength = 64

	queueSize     = 8192
	batchSize     = 500
	flushInterval = time.Second
	flushWait     = 3 * time.Second
	// dedupeSweep is how often expired dedupe and rate limit state is freed;
	// dedupeMaxKeys caps that state if a flood outpaces the sweep.
	dedupeSweep   = time.Minute
	dedupeMaxKeys = 200_000
)

// Sources: which entry point performed the read.
const (
	SourceWeb = "web"
	SourceAPI = "api"
	SourceMCP = "mcp"
)

// Kinds: what was read.
const (
	KindView     = "view"     // one entry opened
	KindRevision = "revision" // a saved version of an entry
	KindRange    = "range"    // part of a date-range read (API, MCP)
)

// Reasons a read was refused.
const (
	ReasonUnauthorized = "unauthorized"
	ReasonForbidden    = "forbidden"
	ReasonNotFound     = "not_found"
	ReasonBadRequest   = "bad_request"
	ReasonError        = "error"
	ReasonOther        = "other"
)

// Visit is one read of (or attempt to read) a diary entry.
type Visit struct {
	ID   string    `json:"id"`
	Time time.Time `json:"time"`
	// OwnerID owns the entry; empty when the attempt could not be tied to an
	// account (for example an anonymous request for a date).
	OwnerID   string `json:"owner_id,omitempty"`
	DiaryID   string `json:"diary_id,omitempty"`
	DiaryDate string `json:"diary_date,omitempty"`
	// VisitorID is the account that read; empty for anonymous visitors.
	VisitorID string `json:"visitor_id,omitempty"`
	Visitor   string `json:"visitor,omitempty"`
	Self      bool   `json:"self"`
	Source    string `json:"source,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Route     string `json:"route,omitempty"`
	Status    int    `json:"status"`
	Success   bool   `json:"success"`
	Reason    string `json:"reason,omitempty"`
	IP        string `json:"ip,omitempty"`
	UA        string `json:"ua,omitempty"`
	// Device identifies the browser or client: the device ID the web app
	// sends, or else a hash of IP and user agent.
	Device string `json:"device,omitempty"`
	// Pulled marks records brought back from the S3 archive.
	Pulled bool `json:"pulled,omitempty"`
}

// Options configures a Tracker.
type Options struct {
	// Settings persists the settings; nil keeps them in memory.
	Settings SettingsStore
	// SharedS3 returns the S3 connection configured for the system audit
	// archive, which the visit archive can reuse.
	SharedS3 SharedS3Func
	// NewObjectStore overrides how S3 is reached, for tests.
	NewObjectStore ObjectStoreFactory
	// Location names the archive days; defaults to time.Local.
	Location *time.Location
	// Now overrides the clock, for tests.
	Now func() time.Time
	// SchedulerDelay is how long after StartScheduler the first cleanup
	// runs; defaults to one minute.
	SchedulerDelay time.Duration
}

// Tracker records and reads visits.
type Tracker struct {
	db   *sql.DB
	path string
	loc  *time.Location
	now  func() time.Time

	ops  chan op
	stop chan struct{}
	done chan struct{}

	closeMu   sync.RWMutex
	closed    bool
	closeOnce sync.Once

	written    atomic.Int64
	dropped    atomic.Int64
	deduped    atomic.Int64
	suppressed atomic.Int64

	limiter *limiter

	archiver       *archiver
	schedulerDelay time.Duration
}

type op struct {
	visit *Visit
	done  chan struct{}
}

// Dir returns the visits directory for a data directory.
func Dir(dataDir string) string {
	return filepath.Join(dataDir, DirName)
}

// New opens (creating if needed) the visits database and starts the writer.
// StartScheduler starts the daily cleanup; Close stops both.
func New(dataDir string, opts Options) (*Tracker, error) {
	dir := Dir(dataDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, DatabaseName)
	db, err := openDB(path)
	if err != nil {
		return nil, err
	}
	t := &Tracker{
		db:             db,
		path:           path,
		loc:            opts.Location,
		now:            opts.Now,
		ops:            make(chan op, queueSize),
		stop:           make(chan struct{}),
		done:           make(chan struct{}),
		schedulerDelay: opts.SchedulerDelay,
	}
	if t.loc == nil {
		t.loc = time.Local
	}
	if t.now == nil {
		t.now = time.Now
	}
	if t.schedulerDelay <= 0 {
		t.schedulerDelay = startupDelay
	}
	t.archiver = newArchiver(t, opts.Settings, opts.SharedS3, opts.NewObjectStore)
	t.limiter = newLimiter()
	go t.run()
	return t, nil
}

func openDB(path string) (*sql.DB, error) {
	// auto_vacuum must be chosen before the first table exists; incremental
	// mode lets cleanup hand freed pages back to the file system.
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=auto_vacuum(INCREMENTAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS visits (
			id TEXT PRIMARY KEY NOT NULL,
			ts INTEGER NOT NULL,
			day TEXT NOT NULL,
			owner TEXT NOT NULL DEFAULT '',
			diary_id TEXT NOT NULL DEFAULT '',
			diary_date TEXT NOT NULL DEFAULT '',
			visitor_id TEXT NOT NULL DEFAULT '',
			visitor TEXT NOT NULL DEFAULT '',
			vkey TEXT NOT NULL DEFAULT '',
			self INTEGER NOT NULL DEFAULT 0,
			source TEXT NOT NULL DEFAULT '',
			kind TEXT NOT NULL DEFAULT '',
			route TEXT NOT NULL DEFAULT '',
			status INTEGER NOT NULL DEFAULT 0,
			success INTEGER NOT NULL DEFAULT 0,
			reason TEXT NOT NULL DEFAULT '',
			ip TEXT NOT NULL DEFAULT '',
			ua TEXT NOT NULL DEFAULT '',
			device TEXT NOT NULL DEFAULT '',
			pulled INTEGER NOT NULL DEFAULT 0,
			pulled_at INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_visits_owner_ts ON visits(owner, ts)`,
		`CREATE INDEX IF NOT EXISTS idx_visits_owner_date ON visits(owner, diary_date, ts)`,
		`CREATE INDEX IF NOT EXISTS idx_visits_day ON visits(pulled, day)`,
		`CREATE INDEX IF NOT EXISTS idx_visits_ts ON visits(ts)`,
		`CREATE TABLE IF NOT EXISTS suppressed (
			day TEXT NOT NULL,
			reason TEXT NOT NULL,
			count INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (day, reason)
		)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}

// Path is the database file, for size reporting.
func (t *Tracker) Path() string {
	if t == nil {
		return ""
	}
	return t.path
}

// Enabled reports whether visits are being recorded.
func (t *Tracker) Enabled() bool {
	return t != nil && t.archiver.current().Enabled
}

// Record queues a visit and returns immediately. It applies the dedupe
// window and rate limits, fills in derived fields and never blocks or fails.
// It reports whether the visit was queued.
func (t *Tracker) Record(v Visit) (queued bool) {
	if t == nil {
		return false
	}
	defer func() {
		if r := recover(); r != nil {
			logger.Error("[Visits] record panicked, visit lost: %v", r)
			queued = false
		}
	}()
	settings := t.archiver.current()
	if !settings.Enabled {
		return false
	}
	if v.Time.IsZero() {
		v.Time = t.now()
	}
	normalize(&v)
	if v.ID == "" {
		v.ID = newID()
	}

	switch t.limiter.allow(&v, v.Time, time.Duration(settings.DedupeSeconds)*time.Second, settings) {
	case verdictDuplicate:
		t.deduped.Add(1)
		return false
	case verdictLimited:
		t.suppressed.Add(1)
		t.limiter.noteSuppressed(v.Time.In(t.loc).Format(dayLayout), suppressReason(&v))
		return false
	}

	t.closeMu.RLock()
	defer t.closeMu.RUnlock()
	if t.closed {
		t.dropped.Add(1)
		return false
	}
	select {
	case t.ops <- op{visit: &v}:
		return true
	default:
		if t.dropped.Add(1)%1000 == 1 {
			logger.Error("[Visits] queue full (%d), dropping visits (%d so far)", queueSize, t.dropped.Load())
		}
		return false
	}
}

func suppressReason(v *Visit) string {
	if !v.Success {
		return "failed"
	}
	return "flood"
}

// normalize trims and bounds every field and derives the computed ones. It is
// applied to fresh visits and to records pulled back from the archive alike,
// so both read the same.
func normalize(v *Visit) {
	v.Time = v.Time.UTC().Truncate(time.Millisecond)
	v.ID = clip(cleanToken(v.ID), maxIDLength)
	v.OwnerID = clip(cleanToken(v.OwnerID), maxIDLength)
	v.DiaryID = clip(cleanToken(v.DiaryID), maxIDLength)
	v.VisitorID = clip(cleanToken(v.VisitorID), maxIDLength)
	v.Visitor = clip(strings.TrimSpace(v.Visitor), maxNameLength)
	v.DiaryDate = clip(strings.TrimSpace(v.DiaryDate), 32)
	if len(v.DiaryDate) > 10 && (v.DiaryDate[10] == ' ' || v.DiaryDate[10] == 'T') && validDay(v.DiaryDate[:10]) {
		v.DiaryDate = v.DiaryDate[:10]
	}
	v.Source = oneOf(strings.ToLower(strings.TrimSpace(v.Source)), SourceWeb, SourceWeb, SourceAPI, SourceMCP)
	v.Kind = oneOf(strings.ToLower(strings.TrimSpace(v.Kind)), KindView, KindView, KindRevision, KindRange)
	v.Route = clip(strings.TrimSpace(v.Route), maxRouteLength)
	v.IP = clip(strings.TrimSpace(v.IP), maxIPLength)
	v.UA = clip(strings.TrimSpace(v.UA), maxUALength)
	v.Device = clip(strings.TrimSpace(v.Device), maxDeviceLength)
	if v.Status < 0 || v.Status > 999 {
		v.Status = 0
	}
	if v.Status != 0 {
		v.Success = v.Status >= 200 && v.Status < 300
	}
	if v.Success {
		v.Reason = ""
	} else {
		v.Reason = clip(strings.ToLower(strings.TrimSpace(v.Reason)), maxReasonLength)
		if v.Reason == "" {
			v.Reason = ReasonForStatus(v.Status)
		}
	}
	if v.Device == "" {
		v.Device = HashDevice(v.IP, v.UA)
	}
	v.Self = v.OwnerID != "" && v.VisitorID == v.OwnerID
}

// ReasonForStatus names why a read with this HTTP status was refused.
func ReasonForStatus(status int) string {
	switch {
	case status == 401:
		return ReasonUnauthorized
	case status == 403:
		return ReasonForbidden
	case status == 404:
		return ReasonNotFound
	case status >= 500:
		return ReasonError
	case status >= 400:
		return ReasonBadRequest
	default:
		return ReasonOther
	}
}

// HashDevice identifies a client that sent no device ID.
func HashDevice(ip, ua string) string {
	sum := sha256.Sum256([]byte(ip + "\x00" + ua))
	return "h:" + hex.EncodeToString(sum[:8])
}

// CleanDeviceID accepts a client-chosen device ID made of letters, digits,
// '-' and '_' (at most 40 characters); anything else is rejected.
func CleanDeviceID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 40 {
		return ""
	}
	for _, r := range raw {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return ""
		}
	}
	return "d:" + raw
}

// visitorKey identifies a distinct visitor: the account, or for anonymous
// visitors the device.
func visitorKey(v *Visit) string {
	if v.VisitorID != "" {
		return "u:" + v.VisitorID
	}
	return "a:" + v.Device
}

func cleanToken(s string) string {
	s = strings.TrimSpace(s)
	if strings.ContainsAny(s, "\x00\n\r") {
		return strings.Map(func(r rune) rune {
			if r == 0 || r == '\n' || r == '\r' {
				return -1
			}
			return r
		}, s)
	}
	return s
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	// Do not leave half a UTF-8 sequence behind.
	for len(s) > 0 && s[len(s)-1]&0xC0 == 0x80 {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && s[len(s)-1] >= 0xC0 {
		s = s[:len(s)-1]
	}
	return s
}

func oneOf(value, fallback string, allowed ...string) string {
	for _, a := range allowed {
		if value == a {
			return value
		}
	}
	return fallback
}

func validDay(day string) bool {
	if len(day) != len(dayLayout) {
		return false
	}
	_, err := time.Parse(dayLayout, day)
	return err == nil
}

func newID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("150405.000000000")))
	}
	return hex.EncodeToString(b[:])
}

// ----- Writer -----

func (t *Tracker) run() {
	defer close(t.done)
	tick := time.NewTicker(flushInterval)
	defer tick.Stop()
	sweep := time.NewTicker(dedupeSweep)
	defer sweep.Stop()
	batch := make([]*Visit, 0, batchSize)
	flush := func() {
		if len(batch) > 0 {
			t.safely(func() { t.insert(batch, false, 0) })
			batch = batch[:0]
		}
		t.safely(t.flushSuppressed)
	}
	for {
		select {
		case o := <-t.ops:
			if o.visit != nil {
				batch = append(batch, o.visit)
				if len(batch) >= batchSize {
					flush()
				}
			}
			if o.done != nil {
				flush()
				close(o.done)
			}
		case <-tick.C:
			flush()
		case <-sweep.C:
			t.limiter.sweep(t.now())
		case <-t.stop:
			for {
				select {
				case o := <-t.ops:
					if o.visit != nil {
						batch = append(batch, o.visit)
					}
					if o.done != nil {
						close(o.done)
					}
				default:
					flush()
					return
				}
			}
		}
	}
}

func (t *Tracker) safely(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("[Visits] writer panicked: %v", r)
		}
	}()
	fn()
}

const insertColumns = `id, ts, day, owner, diary_id, diary_date, visitor_id, visitor, vkey, self, source, kind, route, status, success, reason, ip, ua, device, pulled, pulled_at`

// insert writes visits in one transaction; existing IDs are left alone, so
// pulling an archive twice never duplicates records. It returns how many
// rows were added.
func (t *Tracker) insert(batch []*Visit, pulled bool, pulledAt int64) int64 {
	tx, err := t.db.Begin()
	if err != nil {
		t.dropped.Add(int64(len(batch)))
		logger.Error("[Visits] begin failed, %d visits lost: %v", len(batch), err)
		return 0
	}
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO visits (` + insertColumns + `) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		tx.Rollback()
		t.dropped.Add(int64(len(batch)))
		logger.Error("[Visits] prepare failed, %d visits lost: %v", len(batch), err)
		return 0
	}
	defer stmt.Close()
	var added int64
	for _, v := range batch {
		res, err := stmt.Exec(v.ID, v.Time.UnixMilli(), v.Time.In(t.loc).Format(dayLayout), v.OwnerID, v.DiaryID, v.DiaryDate, v.VisitorID, v.Visitor, visitorKey(v), boolInt(v.Self), v.Source, v.Kind, v.Route, v.Status, boolInt(v.Success), v.Reason, v.IP, v.UA, v.Device, boolInt(pulled), pulledAt)
		if err != nil {
			logger.Error("[Visits] insert failed: %v", err)
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added += n
		}
	}
	if err := tx.Commit(); err != nil {
		t.dropped.Add(int64(len(batch)))
		logger.Error("[Visits] commit failed, %d visits lost: %v", len(batch), err)
		return 0
	}
	if !pulled {
		t.written.Add(added)
	}
	return added
}

func (t *Tracker) flushSuppressed() {
	counts := t.limiter.takeSuppressed()
	if len(counts) == 0 {
		return
	}
	for key, count := range counts {
		_, err := t.db.Exec(`INSERT INTO suppressed (day, reason, count) VALUES (?, ?, ?)
			ON CONFLICT(day, reason) DO UPDATE SET count = count + excluded.count`, key.day, key.reason, count)
		if err != nil {
			logger.Warn("[Visits] save suppressed count failed: %v", err)
		}
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Flush waits until everything queued so far is in the database.
func (t *Tracker) Flush() {
	if t == nil {
		return
	}
	done := make(chan struct{})
	t.closeMu.RLock()
	if t.closed {
		t.closeMu.RUnlock()
		return
	}
	timer := time.NewTimer(flushWait)
	defer timer.Stop()
	select {
	case t.ops <- op{done: done}:
	case <-timer.C:
		t.closeMu.RUnlock()
		return
	}
	t.closeMu.RUnlock()
	select {
	case <-done:
	case <-timer.C:
	}
}

// Close stops the scheduler and the writer, writing what is queued.
func (t *Tracker) Close() {
	if t == nil {
		return
	}
	t.closeOnce.Do(func() {
		t.archiver.stop()
		t.closeMu.Lock()
		t.closed = true
		t.closeMu.Unlock()
		close(t.stop)
		<-t.done
		if err := t.db.Close(); err != nil {
			logger.Warn("[Visits] close database: %v", err)
		}
	})
}

// WriterStats describes the writer since the server started.
type WriterStats struct {
	Written    int64 `json:"written"`
	Dropped    int64 `json:"dropped"`
	Deduped    int64 `json:"deduped"`
	Suppressed int64 `json:"suppressed"`
	Queued     int   `json:"queued"`
}

// WriterStats reports the writer counters.
func (t *Tracker) WriterStats() WriterStats {
	if t == nil {
		return WriterStats{}
	}
	return WriterStats{Written: t.written.Load(), Dropped: t.dropped.Load(), Deduped: t.deduped.Load(), Suppressed: t.suppressed.Load(), Queued: len(t.ops)}
}

// ----- Dedupe and rate limits -----

type verdict int

const (
	verdictAccept verdict = iota
	verdictDuplicate
	verdictLimited
)

type suppressKey struct{ day, reason string }

type window struct {
	start time.Time
	count int
}

// limiter holds the dedupe window and the per-minute counters. All of it is
// memory only: losing it on restart costs at most one extra record.
type limiter struct {
	mu         sync.Mutex
	seen       map[string]time.Time
	perIP      map[string]*window
	perOwner   map[string]*window
	global     window
	suppressed map[suppressKey]int64
}

func newLimiter() *limiter {
	return &limiter{seen: map[string]time.Time{}, perIP: map[string]*window{}, perOwner: map[string]*window{}, suppressed: map[suppressKey]int64{}}
}

func bump(w *window, now time.Time, limit int) bool {
	if now.Sub(w.start) >= time.Minute || now.Before(w.start) {
		w.start, w.count = now, 0
	}
	if limit > 0 && w.count >= limit {
		return false
	}
	w.count++
	return true
}

func (l *limiter) allow(v *Visit, now time.Time, dedupe time.Duration, s Settings) verdict {
	l.mu.Lock()
	defer l.mu.Unlock()
	target := v.DiaryDate
	if v.DiaryID != "" {
		target = v.DiaryID
	}
	key := strings.Join([]string{v.OwnerID, target, v.Source, v.Kind, visitorKey(v), v.Device, boolStr(v.Success)}, "|")
	if dedupe > 0 {
		if last, ok := l.seen[key]; ok && now.Sub(last) < dedupe && !now.Before(last) {
			return verdictDuplicate
		}
	}
	// Refused and anonymous attempts are what an attacker can generate at
	// will; they are limited per IP and in total.
	if !v.Success || v.VisitorID == "" {
		w := l.perIP[v.IP]
		if w == nil {
			w = &window{}
			l.perIP[v.IP] = w
		}
		if !bump(w, now, s.IPLimitPerMinute) || !bump(&l.global, now, s.GlobalFailedPerMinute) {
			return verdictLimited
		}
	} else if v.OwnerID != "" {
		w := l.perOwner[v.OwnerID]
		if w == nil {
			w = &window{}
			l.perOwner[v.OwnerID] = w
		}
		if !bump(w, now, s.OwnerLimitPerMinute) {
			return verdictLimited
		}
	}
	if dedupe > 0 {
		if len(l.seen) >= dedupeMaxKeys {
			l.sweepLocked(now, dedupe)
			if len(l.seen) >= dedupeMaxKeys {
				l.seen = map[string]time.Time{}
			}
		}
		l.seen[key] = now
	}
	return verdictAccept
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func (l *limiter) noteSuppressed(day, reason string) {
	l.mu.Lock()
	l.suppressed[suppressKey{day, reason}]++
	l.mu.Unlock()
}

func (l *limiter) takeSuppressed() map[suppressKey]int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.suppressed) == 0 {
		return nil
	}
	out := l.suppressed
	l.suppressed = map[suppressKey]int64{}
	return out
}

func (l *limiter) sweep(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepLocked(now, maxDedupeWindow)
}

func (l *limiter) sweepLocked(now time.Time, keep time.Duration) {
	for key, last := range l.seen {
		if now.Sub(last) >= keep {
			delete(l.seen, key)
		}
	}
	for key, w := range l.perIP {
		if now.Sub(w.start) >= time.Minute {
			delete(l.perIP, key)
		}
	}
	for key, w := range l.perOwner {
		if now.Sub(w.start) >= time.Minute {
			delete(l.perOwner, key)
		}
	}
}

// errClosed is returned by operations on a closed tracker.
var errClosed = errors.New("visit tracking is not available")
