package visits

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/songtianlun/diarum/internal/backup"
	"github.com/songtianlun/diarum/internal/logger"
)

const (
	archiveSuffix = ".jsonl.gz"
	// archiveFormat heads every archive so a pull can recognise it.
	archiveFormat  = "diarum-visits"
	archiveVersion = 1

	// Days that failed to reach the archive are kept locally for at most
	// this long past the retention window before being deleted anyway.
	unarchivedGraceDays = 7
	// Pulled records stay this long before cleanup removes them again.
	PulledKeepDays = 7
	// A pulled archive may not decompress to more than this.
	maxPulledBytes = 1 << 30
	maxLineBytes   = 64 << 10
	// deleteChunk bounds each DELETE so cleanup never holds a long lock.
	deleteChunk = 5000
	// trimTarget: when the row cap is exceeded, trim down to this share of it.
	trimTarget = 0.95

	cleanupHour   = 0
	cleanupMinute = 20
	startupDelay  = time.Minute
	retryInterval = time.Hour
	runTimeout    = time.Hour
	pullTimeout   = 30 * time.Minute
)

// Errors returned by archive operations.
var (
	ErrArchiveDisabled = errors.New("the visit archive is not enabled or has no complete S3 connection")
	ErrArchiveNotFound = errors.New("no archive for this day")
	ErrInvalidDay      = errors.New("invalid day, expected YYYY-MM-DD")
	ErrCapacity        = errors.New("pulling these archives would exceed the record limit; unload pulled records or raise the limit first")
	ErrBusy            = errors.New("another archive operation is running")
)

// RunReport describes one archive or cleanup run.
type RunReport struct {
	Kind          string    `json:"kind"`
	Trigger       string    `json:"trigger"`
	Started       time.Time `json:"started"`
	Finished      time.Time `json:"finished"`
	Archived      []string  `json:"archived"`
	RemovedRemote []string  `json:"removed_remote"`
	// Deleted counts records removed by retention; Trimmed those removed by
	// the row cap; Unloaded pulled records that expired.
	Deleted  int64 `json:"deleted"`
	Trimmed  int64 `json:"trimmed"`
	Unloaded int64 `json:"unloaded"`
	// Kept lists days past retention kept because they are not archived yet.
	Kept  []string `json:"kept"`
	Error string   `json:"error,omitempty"`
}

// PullReport describes a pull from the archive.
type PullReport struct {
	Days    []string `json:"days"`
	Skipped []string `json:"skipped"`
	Records int64    `json:"records"`
	Invalid int64    `json:"invalid"`
	Errors  []string `json:"errors"`
}

// State is what the archiver last did and will do next.
type State struct {
	LastCleanup *RunReport  `json:"last_cleanup,omitempty"`
	LastArchive *RunReport  `json:"last_archive,omitempty"`
	LastPull    *PullReport `json:"last_pull,omitempty"`
	NextCleanup time.Time   `json:"next_cleanup"`
	NextRetry   *time.Time  `json:"next_retry,omitempty"`
	Running     bool        `json:"running"`
}

// ArchiveInfo is one archived day in the bucket.
type ArchiveInfo struct {
	Day  string `json:"day"`
	Key  string `json:"key"`
	Size int64  `json:"size"`
	// Local is true while the day's records are still in the database;
	// Pulled when they were brought back from this archive.
	Local  bool `json:"local"`
	Pulled bool `json:"pulled"`
}

type archiver struct {
	t       *Tracker
	store   SettingsStore
	shared  SharedS3Func
	factory ObjectStoreFactory

	mu       sync.Mutex
	settings Settings
	state    State

	// runMu serializes everything that touches the bucket or deletes rows.
	runMu sync.Mutex

	started  atomic.Bool
	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

func newArchiver(t *Tracker, store SettingsStore, shared SharedS3Func, factory ObjectStoreFactory) *archiver {
	if factory == nil {
		factory = backup.NewS3Store
	}
	a := &archiver{t: t, store: store, shared: shared, factory: factory, settings: DefaultSettings(), stopCh: make(chan struct{}), doneCh: make(chan struct{})}
	a.loadSettings()
	return a
}

// State reports the last runs and the next scheduled cleanup.
func (t *Tracker) State() State {
	if t == nil {
		return State{}
	}
	t.archiver.mu.Lock()
	defer t.archiver.mu.Unlock()
	return t.archiver.state
}

// StartScheduler begins the daily cleanup; the first run happens shortly
// after start. Close stops it.
func (t *Tracker) StartScheduler() {
	if t == nil {
		return
	}
	if t.archiver.started.CompareAndSwap(false, true) {
		go t.archiver.loop(t.schedulerDelay)
	}
}

func (a *archiver) stop() {
	a.stopOnce.Do(func() { close(a.stopCh) })
	if a.started.Load() {
		<-a.doneCh
	}
	a.runMu.Lock()
	defer a.runMu.Unlock()
}

func nextCleanupAfter(now time.Time, loc *time.Location) time.Time {
	now = now.In(loc)
	next := time.Date(now.Year(), now.Month(), now.Day(), cleanupHour, cleanupMinute, 0, 0, loc)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func (a *archiver) loop(delay time.Duration) {
	defer close(a.doneCh)
	next := a.t.now().Add(delay)
	var retry time.Time
	for {
		a.mu.Lock()
		a.state.NextCleanup = next
		if retry.IsZero() {
			a.state.NextRetry = nil
		} else {
			r := retry
			a.state.NextRetry = &r
		}
		a.mu.Unlock()

		wake := next
		if !retry.IsZero() && retry.Before(wake) {
			wake = retry
		}
		timer := time.NewTimer(max(wake.Sub(a.t.now()), 0))
		select {
		case <-a.stopCh:
			timer.Stop()
			return
		case <-timer.C:
		}

		now := a.t.now()
		var report *RunReport
		if !now.Before(next) {
			report = a.run(true, "schedule")
			next = nextCleanupAfter(now, a.t.loc)
		} else {
			report = a.run(false, "retry")
		}
		retry = time.Time{}
		if report.Error != "" && a.archiveReady(a.current()) {
			retry = now.Add(retryInterval)
		}
	}
}

// Cleanup archives finished days and applies retention and the row cap. It
// is what the daily schedule runs.
func (t *Tracker) Cleanup(trigger string) *RunReport {
	if t == nil {
		return &RunReport{}
	}
	return t.archiver.run(true, trigger)
}

// ArchiveNow uploads finished days that are not in the bucket yet.
func (t *Tracker) ArchiveNow(trigger string) (*RunReport, error) {
	if t == nil || !t.archiver.archiveReady(t.archiver.current()) {
		return nil, ErrArchiveDisabled
	}
	report := t.archiver.run(false, trigger)
	if report.Error != "" {
		return report, errors.New(report.Error)
	}
	return report, nil
}

func (a *archiver) today() string {
	return a.t.now().In(a.t.loc).Format(dayLayout)
}

func (a *archiver) dayOffset(days int) string {
	now := a.t.now().In(a.t.loc)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, a.t.loc)
	return day.AddDate(0, 0, days).Format(dayLayout)
}

func (a *archiver) run(full bool, trigger string) (report *RunReport) {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	kind := "archive"
	if full {
		kind = "cleanup"
	}
	report = &RunReport{Kind: kind, Trigger: trigger, Started: a.t.now().UTC(), Archived: []string{}, RemovedRemote: []string{}, Kept: []string{}}
	a.mu.Lock()
	a.state.Running = true
	a.mu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			report.Error = fmt.Sprintf("panic: %v", r)
		}
		report.Finished = a.t.now().UTC()
		ready := a.archiveReady(a.current())
		a.mu.Lock()
		a.state.Running = false
		if full {
			a.state.LastCleanup = report
		}
		if ready || !full {
			a.state.LastArchive = report
		}
		a.mu.Unlock()
		if report.Error != "" {
			logger.Error("[Visits] %s failed: %s", kind, report.Error)
		} else if len(report.Archived)+len(report.RemovedRemote) > 0 || report.Deleted+report.Trimmed+report.Unloaded > 0 {
			logger.Info("[Visits] %s: archived %d day(s), removed %d archive(s), deleted %d, trimmed %d, unloaded %d record(s)", kind, len(report.Archived), len(report.RemovedRemote), report.Deleted, report.Trimmed, report.Unloaded)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	settings := a.current()
	a.t.Flush()
	today := a.today()

	var errs []string
	archived := map[string]bool{}
	ready := a.archiveReady(settings)
	if ready {
		objects, store, err := a.listArchive(ctx, settings)
		if err != nil {
			errs = append(errs, "list archive: "+err.Error())
		} else {
			for _, obj := range objects {
				archived[obj.Day] = true
			}
			days, err := a.t.localDays(today)
			if err != nil {
				errs = append(errs, "list days: "+err.Error())
			}
			for _, day := range days {
				if archived[day] {
					continue
				}
				if err := a.upload(ctx, store, settings, day); err != nil {
					errs = append(errs, day+": "+err.Error())
					continue
				}
				archived[day] = true
				report.Archived = append(report.Archived, day)
			}
			if full && settings.Archive.RetentionDays > 0 {
				cutoff := a.dayOffset(-(settings.Archive.RetentionDays - 1))
				for _, obj := range objects {
					if obj.Day >= cutoff {
						continue
					}
					if err := store.Delete(ctx, obj.Key); err != nil {
						errs = append(errs, "delete "+obj.Key+": "+err.Error())
						continue
					}
					report.RemovedRemote = append(report.RemovedRemote, obj.Day)
				}
			}
		}
	}

	if full {
		if settings.RetentionDays > 0 {
			cutoff := a.dayOffset(-(settings.RetentionDays - 1))
			hardCutoff := a.dayOffset(-(settings.RetentionDays - 1 + unarchivedGraceDays))
			days, err := a.t.localDaysBefore(cutoff)
			if err != nil {
				errs = append(errs, "list expired days: "+err.Error())
			}
			for _, day := range days {
				if ready && !archived[day] && day >= hardCutoff {
					report.Kept = append(report.Kept, day)
					continue
				}
				n, err := a.t.deleteWhere(`pulled = 0 AND day = ?`, day)
				report.Deleted += n
				if err != nil {
					errs = append(errs, "delete "+day+": "+err.Error())
				}
			}
			if _, err := a.t.db.Exec(`DELETE FROM suppressed WHERE day < ?`, cutoff); err != nil {
				errs = append(errs, "delete suppressed: "+err.Error())
			}
		}
		expired := a.t.now().AddDate(0, 0, -PulledKeepDays).UnixMilli()
		n, err := a.t.deleteWhere(`pulled = 1 AND pulled_at < ?`, expired)
		report.Unloaded += n
		if err != nil {
			errs = append(errs, "unload pulled: "+err.Error())
		}
		trimmed, err := a.t.trimToCap(settings.MaxRecords)
		report.Trimmed += trimmed
		if err != nil {
			errs = append(errs, "trim: "+err.Error())
		}
		if report.Deleted+report.Trimmed+report.Unloaded > 0 {
			if _, err := a.t.db.Exec(`PRAGMA incremental_vacuum`); err != nil {
				logger.Warn("[Visits] incremental vacuum: %v", err)
			}
		}
	}
	report.Error = strings.Join(errs, "; ")
	return report
}

// localDays lists the finished days (before today) that have own records.
func (t *Tracker) localDays(today string) ([]string, error) {
	return t.queryDays(`SELECT DISTINCT day FROM visits WHERE pulled = 0 AND day < ? ORDER BY day`, today)
}

func (t *Tracker) localDaysBefore(cutoff string) ([]string, error) {
	return t.queryDays(`SELECT DISTINCT day FROM visits WHERE pulled = 0 AND day < ? ORDER BY day`, cutoff)
}

func (t *Tracker) queryDays(query string, args ...any) ([]string, error) {
	rows, err := t.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var days []string
	for rows.Next() {
		var day string
		if err := rows.Scan(&day); err != nil {
			return nil, err
		}
		days = append(days, day)
	}
	return days, rows.Err()
}

// deleteWhere deletes matching rows in chunks and reports how many went.
func (t *Tracker) deleteWhere(where string, args ...any) (int64, error) {
	var total int64
	for {
		res, err := t.db.Exec(`DELETE FROM visits WHERE rowid IN (SELECT rowid FROM visits WHERE `+where+` LIMIT ?)`, append(args, deleteChunk)...)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		if n < deleteChunk {
			return total, nil
		}
	}
}

// trimToCap removes the oldest records once the table holds more than limit:
// pulled records first, then the oldest of the rest.
func (t *Tracker) trimToCap(limit int) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}
	var count int64
	if err := t.db.QueryRow(`SELECT COUNT(*) FROM visits`).Scan(&count); err != nil {
		return 0, err
	}
	if count <= int64(limit) {
		return 0, nil
	}
	excess := count - int64(float64(limit)*trimTarget)
	var trimmed int64
	for _, pulled := range []int{1, 0} {
		for excess > 0 {
			chunk := min(excess, deleteChunk)
			res, err := t.db.Exec(`DELETE FROM visits WHERE rowid IN (SELECT rowid FROM visits WHERE pulled = ? ORDER BY ts LIMIT ?)`, pulled, chunk)
			if err != nil {
				return trimmed, err
			}
			n, _ := res.RowsAffected()
			trimmed += n
			excess -= n
			if n < chunk {
				break
			}
		}
	}
	if trimmed > 0 {
		logger.Warn("[Visits] record limit %d exceeded, trimmed the %d oldest record(s)", limit, trimmed)
	}
	return trimmed, nil
}

func archivePrefix(s Settings) string {
	prefix := backup.NormalizePrefix(s.Archive.Prefix)
	if prefix == "" {
		prefix = DefaultArchivePrefix
	}
	return prefix + "/"
}

func archiveKey(s Settings, day string) string {
	return archivePrefix(s) + day[:4] + "/" + day + archiveSuffix
}

func (a *archiver) connect(s Settings) (backup.ObjectStore, error) {
	cfg, ok := a.archiveS3(s)
	if !ok {
		return nil, ErrArchiveDisabled
	}
	return a.factory(cfg)
}

func (a *archiver) listArchive(ctx context.Context, s Settings) ([]ArchiveInfo, backup.ObjectStore, error) {
	store, err := a.connect(s)
	if err != nil {
		return nil, nil, err
	}
	prefix := archivePrefix(s)
	objects, err := store.List(ctx, prefix)
	if err != nil {
		return nil, nil, err
	}
	out := make([]ArchiveInfo, 0, len(objects))
	for _, obj := range objects {
		name := strings.TrimPrefix(obj.Key, prefix)
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		if !strings.HasSuffix(name, archiveSuffix) {
			continue
		}
		day := strings.TrimSuffix(name, archiveSuffix)
		if !validDay(day) || obj.Key != archiveKey(s, day) {
			continue
		}
		out = append(out, ArchiveInfo{Day: day, Key: obj.Key, Size: obj.Size})
	}
	return out, store, nil
}

type archiveHeader struct {
	Format  string `json:"_format"`
	Version int    `json:"_version"`
	Day     string `json:"day"`
}

// upload writes one day's own records as gzipped JSON lines.
func (a *archiver) upload(ctx context.Context, store backup.ObjectStore, s Settings, day string) error {
	rows, err := a.t.db.QueryContext(ctx, `SELECT `+selectColumns+` FROM visits WHERE pulled = 0 AND day = ? ORDER BY ts`, day)
	if err != nil {
		return err
	}
	reader, writer := io.Pipe()
	go func() {
		defer rows.Close()
		gz := gzip.NewWriter(writer)
		enc := json.NewEncoder(gz)
		err := enc.Encode(archiveHeader{Format: archiveFormat, Version: archiveVersion, Day: day})
		for err == nil && rows.Next() {
			var v *Visit
			if v, err = scanVisit(rows); err == nil {
				v.Pulled = false
				err = enc.Encode(v)
			}
		}
		if err == nil {
			err = rows.Err()
		}
		if closeErr := gz.Close(); err == nil {
			err = closeErr
		}
		writer.CloseWithError(err)
	}()
	err = store.Put(ctx, archiveKey(s, day), reader, "application/gzip")
	reader.CloseWithError(err)
	return err
}

// ListArchives lists the archived days, newest first, marking those whose
// records are in the database.
func (t *Tracker) ListArchives(ctx context.Context) ([]ArchiveInfo, error) {
	if t == nil {
		return nil, ErrArchiveDisabled
	}
	s := t.archiver.current()
	if !t.archiver.archiveReady(s) {
		return nil, ErrArchiveDisabled
	}
	archives, _, err := t.archiver.listArchive(ctx, s)
	if err != nil {
		return nil, err
	}
	local, pulled, err := t.dayPresence()
	if err != nil {
		return nil, err
	}
	for i := range archives {
		archives[i].Local = local[archives[i].Day]
		archives[i].Pulled = pulled[archives[i].Day]
	}
	sort.Slice(archives, func(i, j int) bool { return archives[i].Day > archives[j].Day })
	return archives, nil
}

func (t *Tracker) dayPresence() (local, pulled map[string]bool, err error) {
	rows, err := t.db.Query(`SELECT DISTINCT day, pulled FROM visits`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	local, pulled = map[string]bool{}, map[string]bool{}
	for rows.Next() {
		var day string
		var p int
		if err := rows.Scan(&day, &p); err != nil {
			return nil, nil, err
		}
		if p == 1 {
			pulled[day] = true
		} else {
			local[day] = true
		}
	}
	return local, pulled, rows.Err()
}

// Pull brings archived days back into the database, where every statistics
// view includes them until they are unloaded or expire after PulledKeepDays.
// With no days given, every archived day between start and end (inclusive,
// either may be empty) that is not in the database yet is pulled.
func (t *Tracker) Pull(ctx context.Context, days []string, start, end string) (*PullReport, error) {
	if t == nil {
		return nil, ErrArchiveDisabled
	}
	for _, day := range append(append([]string{}, days...), start, end) {
		if day != "" && !validDay(day) {
			return nil, ErrInvalidDay
		}
	}
	a := t.archiver
	s := a.current()
	if !a.archiveReady(s) {
		return nil, ErrArchiveDisabled
	}
	if !a.runMu.TryLock() {
		return nil, ErrBusy
	}
	defer a.runMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, pullTimeout)
	defer cancel()

	archives, store, err := a.listArchive(ctx, s)
	if err != nil {
		return nil, err
	}
	local, pulled, err := t.dayPresence()
	if err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	for _, day := range days {
		wanted[day] = true
	}
	report := &PullReport{Days: []string{}, Skipped: []string{}, Errors: []string{}}
	var selected []ArchiveInfo
	for _, archive := range archives {
		if len(days) > 0 && !wanted[archive.Day] {
			continue
		}
		if len(days) == 0 && ((start != "" && archive.Day < start) || (end != "" && archive.Day > end)) {
			continue
		}
		if local[archive.Day] || pulled[archive.Day] {
			report.Skipped = append(report.Skipped, archive.Day)
			continue
		}
		selected = append(selected, archive)
	}
	if len(days) > 0 && len(selected)+len(report.Skipped) == 0 {
		return nil, ErrArchiveNotFound
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Day > selected[j].Day })

	var count int64
	if err := t.db.QueryRow(`SELECT COUNT(*) FROM visits`).Scan(&count); err != nil {
		return nil, err
	}
	budget := int64(s.MaxRecords) - count
	pulledAt := t.now().UnixMilli()
	for _, archive := range selected {
		if budget <= 0 {
			if len(report.Days) == 0 {
				return report, ErrCapacity
			}
			report.Errors = append(report.Errors, ErrCapacity.Error())
			break
		}
		added, invalid, err := t.pullDay(ctx, store, archive.Key, pulledAt, budget)
		report.Records += added
		report.Invalid += invalid
		budget -= added
		if err != nil {
			report.Errors = append(report.Errors, archive.Day+": "+err.Error())
			if errors.Is(err, ErrCapacity) {
				break
			}
			continue
		}
		report.Days = append(report.Days, archive.Day)
	}
	a.mu.Lock()
	a.state.LastPull = report
	a.mu.Unlock()
	return report, nil
}

// pullDay downloads one archive and inserts its records as pulled.
func (t *Tracker) pullDay(ctx context.Context, store backup.ObjectStore, key string, pulledAt, budget int64) (added, invalid int64, err error) {
	body, err := store.Get(ctx, key)
	if errors.Is(err, backup.ErrObjectNotFound) {
		return 0, 0, ErrArchiveNotFound
	}
	if err != nil {
		return 0, 0, err
	}
	defer body.Close()
	gz, err := gzip.NewReader(body)
	if err != nil {
		return 0, 0, fmt.Errorf("archive is not valid gzip: %w", err)
	}
	defer gz.Close()
	limited := &io.LimitedReader{R: gz, N: maxPulledBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 0, 64<<10), maxLineBytes)
	batch := make([]*Visit, 0, batchSize)
	flush := func() {
		if len(batch) > 0 {
			added += t.insert(batch, true, pulledAt)
			batch = batch[:0]
		}
	}
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		v, ok := parseArchivedVisit(line)
		if !ok {
			invalid++
			continue
		}
		if added+int64(len(batch)) >= budget {
			flush()
			return added, invalid, ErrCapacity
		}
		batch = append(batch, v)
		if len(batch) >= batchSize {
			flush()
		}
	}
	flush()
	if limited.N <= 0 {
		return added, invalid, errors.New("archive is larger than the 1 GB pull limit")
	}
	if err := scanner.Err(); err != nil {
		return added, invalid, err
	}
	return added, invalid, nil
}

// parseArchivedVisit reads one archive line into a normalized visit. Header
// lines and records without a time are rejected; missing IDs are derived
// from the record so pulling twice never duplicates it.
func parseArchivedVisit(line []byte) (*Visit, bool) {
	var probe struct {
		Format string `json:"_format"`
	}
	if json.Unmarshal(line, &probe) == nil && probe.Format != "" {
		return nil, false
	}
	var v Visit
	if err := json.Unmarshal(line, &v); err != nil || v.Time.IsZero() {
		return nil, false
	}
	normalize(&v)
	if v.ID == "" {
		sum := HashDevice(fmt.Sprint(v.Time.UnixMilli(), v.OwnerID, v.DiaryID, v.DiaryDate), v.VisitorID+v.IP+v.UA)
		v.ID = "p" + strings.TrimPrefix(sum, "h:")
	}
	v.Pulled = true
	return &v, true
}

// Unload removes pulled records ahead of their expiry: those of one day, or
// all of them when day is empty.
func (t *Tracker) Unload(day string) (int64, error) {
	if t == nil {
		return 0, errClosed
	}
	if day != "" && !validDay(day) {
		return 0, ErrInvalidDay
	}
	t.archiver.runMu.Lock()
	defer t.archiver.runMu.Unlock()
	var n int64
	var err error
	if day == "" {
		n, err = t.deleteWhere(`pulled = 1`)
	} else {
		n, err = t.deleteWhere(`pulled = 1 AND day = ?`, day)
	}
	if n > 0 {
		_, _ = t.db.Exec(`PRAGMA incremental_vacuum`)
	}
	return n, err
}

// TestArchive checks that the bucket can be listed, written and cleaned up.
// cfg is used for the custom source; the shared source uses the audit
// archive's connection.
func (t *Tracker) TestArchive(ctx context.Context, source string, cfg backup.S3Config, prefix string) error {
	if t == nil {
		return errClosed
	}
	current := t.archiver.current()
	s := Settings{Archive: ArchiveSettings{Source: source, S3: cfg.Normalized(), Prefix: prefix}}
	if s.Archive.Source != S3SourceCustom {
		s.Archive.Source = S3SourceShared
	}
	if s.Archive.S3.Secret == "" {
		s.Archive.S3.Secret = current.Archive.S3.Secret
	}
	if s.Archive.Prefix = backup.NormalizePrefix(prefix); s.Archive.Prefix == "" {
		s.Archive.Prefix = DefaultArchivePrefix
	}
	store, err := t.archiver.connect(s)
	if err != nil {
		if errors.Is(err, ErrArchiveDisabled) {
			if s.Archive.Source == S3SourceShared {
				return errors.New("the audit archive has no complete S3 connection to reuse")
			}
			return errors.New("bucket, region, access key and secret are required")
		}
		return err
	}
	p := archivePrefix(s)
	if _, err := store.List(ctx, p); err != nil {
		return fmt.Errorf("list: %w", err)
	}
	key := p + ".diarum-probe"
	if err := store.Put(ctx, key, strings.NewReader("ok"), "text/plain"); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err := store.Delete(ctx, key); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	return nil
}
