package audit

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/songtianlun/diarum/internal/backup"
	"github.com/songtianlun/diarum/internal/logger"
)

// Settings bounds and defaults. Local files are the working set that can be
// searched at any time; the S3 archive keeps finished days for longer.
const (
	SettingsKey = "audit.settings"

	DefaultRetentionDays = 3
	MinRetentionDays     = 1
	MaxRetentionDays     = 90

	DefaultArchiveRetentionDays = 30
	MinArchiveRetentionDays     = 1
	MaxArchiveRetentionDays     = 3650

	DefaultArchivePrefix = "diarum-audit"

	archiveSuffix = ".log.gz"
	stateFile     = "state.json"

	// Days that failed to reach the archive are kept locally for at most
	// this long past the retention window before being deleted anyway.
	unarchivedGraceDays = 7
	// Pulled files larger than this are refused (a guard against gzip bombs).
	maxPulledBytes = 2 << 30

	// Cleanup runs daily at this local time, and once shortly after start.
	cleanupHour   = 0
	cleanupMinute = 10
	startupDelay  = 30 * time.Second
	retryInterval = time.Hour
	runTimeout    = 30 * time.Minute
)

// ErrAlreadyLocal is returned when pulling a day that is still on disk.
var ErrAlreadyLocal = errors.New("this day is still available locally")

// ErrArchiveDisabled is returned when an archive operation needs S3 but none
// is configured.
var ErrArchiveDisabled = errors.New("S3 archive is not enabled or not fully configured")

// ErrArchiveNotFound is returned when a day has no archive object.
var ErrArchiveNotFound = errors.New("no archive for this day")

// SettingsStore persists the audit settings.
type SettingsStore interface {
	GetSystemSetting(key string) (string, bool, error)
	SetSystemSetting(key, value string) error
}

// ObjectStoreFactory connects to an S3 destination.
type ObjectStoreFactory func(cfg backup.S3Config) (backup.ObjectStore, error)

// Settings configures retention and archiving.
type Settings struct {
	// RetentionDays is how many days (today included) stay on local disk.
	RetentionDays int             `json:"retention_days"`
	Archive       ArchiveSettings `json:"archive"`
}

// ArchiveSettings configures the S3 archive of finished days.
type ArchiveSettings struct {
	Enabled bool            `json:"enabled"`
	S3      backup.S3Config `json:"s3"`
	// RetentionDays is how many days of archives are kept in the bucket.
	RetentionDays int `json:"retention_days"`
}

// DefaultSettings returns the settings used until an admin changes them.
func DefaultSettings() Settings {
	return Settings{
		RetentionDays: DefaultRetentionDays,
		Archive: ArchiveSettings{
			S3:            backup.S3Config{Prefix: DefaultArchivePrefix},
			RetentionDays: DefaultArchiveRetentionDays,
		},
	}
}

// Validate checks the bounds and normalizes the S3 fields.
func (s Settings) Validate() (Settings, error) {
	if s.RetentionDays < MinRetentionDays || s.RetentionDays > MaxRetentionDays {
		return s, fmt.Errorf("retention_days must be between %d and %d", MinRetentionDays, MaxRetentionDays)
	}
	if s.Archive.RetentionDays < MinArchiveRetentionDays || s.Archive.RetentionDays > MaxArchiveRetentionDays {
		return s, fmt.Errorf("archive retention_days must be between %d and %d", MinArchiveRetentionDays, MaxArchiveRetentionDays)
	}
	s.Archive.S3 = s.Archive.S3.Normalized()
	if s.Archive.S3.Prefix == "" {
		s.Archive.S3.Prefix = DefaultArchivePrefix
	}
	if s.Archive.Enabled && !s.Archive.S3.Complete() {
		return s, errors.New("bucket, region, access key and secret are required to enable the archive")
	}
	return s, nil
}

// archiveReady reports whether finished days should go to S3.
func (s Settings) archiveReady() bool {
	return s.Archive.Enabled && s.Archive.S3.Complete()
}

// RunReport describes one archive or cleanup run.
type RunReport struct {
	Kind          string    `json:"kind"`
	Trigger       string    `json:"trigger"`
	Started       time.Time `json:"started"`
	Finished      time.Time `json:"finished"`
	Archived      []string  `json:"archived"`
	RemovedLocal  []string  `json:"removed_local"`
	RemovedRemote []string  `json:"removed_remote"`
	RemovedPulled []string  `json:"removed_pulled"`
	// Kept lists days past retention kept because they are not archived yet.
	Kept  []string `json:"kept"`
	Error string   `json:"error,omitempty"`
}

// State is what the archiver last did and will do next.
type State struct {
	LastCleanup *RunReport `json:"last_cleanup,omitempty"`
	LastArchive *RunReport `json:"last_archive,omitempty"`
	NextCleanup time.Time  `json:"next_cleanup"`
	NextRetry   *time.Time `json:"next_retry,omitempty"`
	Running     bool       `json:"running"`
}

// ArchiveInfo is one archived day in the bucket.
type ArchiveInfo struct {
	Date   string `json:"date"`
	Key    string `json:"key"`
	Size   int64  `json:"size"`
	Local  bool   `json:"local"`
	Pulled bool   `json:"pulled"`
}

type archiver struct {
	l       *Logger
	store   SettingsStore
	factory ObjectStoreFactory

	mu       sync.Mutex
	settings Settings
	state    State

	// runMu serializes everything that touches files or the bucket.
	runMu sync.Mutex

	started  atomic.Bool
	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

func newArchiver(l *Logger, store SettingsStore, factory ObjectStoreFactory) *archiver {
	if factory == nil {
		factory = backup.NewS3Store
	}
	a := &archiver{l: l, store: store, factory: factory, settings: DefaultSettings(), stopCh: make(chan struct{}), doneCh: make(chan struct{})}
	a.loadSettings()
	a.loadState()
	return a
}

func (a *archiver) loadSettings() {
	if a.store == nil {
		return
	}
	raw, ok, err := a.store.GetSystemSetting(SettingsKey)
	if err != nil {
		logger.Error("[AUDIT] !!! load audit settings failed, using defaults: %v", err)
		return
	}
	if !ok {
		return
	}
	settings := DefaultSettings()
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		logger.Error("[AUDIT] !!! audit settings are unreadable, using defaults: %v", err)
		return
	}
	if settings.RetentionDays == 0 {
		settings.RetentionDays = DefaultRetentionDays
	}
	if settings.Archive.RetentionDays == 0 {
		settings.Archive.RetentionDays = DefaultArchiveRetentionDays
	}
	settings.RetentionDays = min(max(settings.RetentionDays, MinRetentionDays), MaxRetentionDays)
	settings.Archive.RetentionDays = min(max(settings.Archive.RetentionDays, MinArchiveRetentionDays), MaxArchiveRetentionDays)
	settings.Archive.S3 = settings.Archive.S3.Normalized()
	if settings.Archive.S3.Prefix == "" {
		settings.Archive.S3.Prefix = DefaultArchivePrefix
	}
	a.settings = settings
}

func (a *archiver) loadState() {
	raw, err := os.ReadFile(filepath.Join(a.l.root, stateFile))
	if err != nil {
		return
	}
	var state State
	if json.Unmarshal(raw, &state) == nil {
		state.Running = false
		state.NextRetry = nil
		a.state = state
	}
}

func (a *archiver) saveState() {
	a.mu.Lock()
	raw, err := json.MarshalIndent(a.state, "", "  ")
	a.mu.Unlock()
	if err != nil {
		return
	}
	path := filepath.Join(a.l.root, stateFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		logger.Warn("[Audit] save archive state failed: %v", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		logger.Warn("[Audit] save archive state failed: %v", err)
	}
}

func (a *archiver) current() Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings
}

// Settings returns the current audit settings.
func (l *Logger) Settings() Settings {
	if l == nil {
		return DefaultSettings()
	}
	return l.archiver.current()
}

// UpdateSettings validates, stores and applies new settings. An empty S3
// secret keeps the stored one, so clients never need to see it.
func (l *Logger) UpdateSettings(next Settings) (Settings, error) {
	if l == nil {
		return next, errors.New("audit logging is disabled")
	}
	a := l.archiver
	previous := a.current()
	if next.Archive.S3.Secret == "" {
		next.Archive.S3.Secret = previous.Archive.S3.Secret
	}
	next, err := next.Validate()
	if err != nil {
		return previous, err
	}
	if a.store != nil {
		raw, err := json.Marshal(next)
		if err != nil {
			return previous, err
		}
		if err := a.store.SetSystemSetting(SettingsKey, string(raw)); err != nil {
			return previous, err
		}
	}
	a.mu.Lock()
	a.settings = next
	a.mu.Unlock()
	return next, nil
}

// ArchiveState reports the last runs and the next scheduled cleanup.
func (l *Logger) ArchiveState() State {
	if l == nil {
		return State{}
	}
	a := l.archiver
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

// StartScheduler begins the daily cleanup (and archive) schedule; the first
// run happens shortly after start. Close stops it.
func (l *Logger) StartScheduler() {
	if l == nil {
		return
	}
	if l.archiver.started.CompareAndSwap(false, true) {
		go l.archiver.loop(l.schedulerDelay)
	}
}

// stop ends the schedule and waits for any run in progress, so no upload
// is cut short.
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
	next := a.l.now().Add(delay)
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
		a.saveState()

		wake := next
		if !retry.IsZero() && retry.Before(wake) {
			wake = retry
		}
		timer := time.NewTimer(max(wake.Sub(a.l.now()), 0))
		select {
		case <-a.stopCh:
			timer.Stop()
			return
		case <-timer.C:
		}

		now := a.l.now()
		var report *RunReport
		if !now.Before(next) {
			report = a.run(true, "schedule")
			next = nextCleanupAfter(now, a.l.loc)
		} else {
			report = a.run(false, "retry")
		}
		retry = time.Time{}
		if report.Error != "" && a.current().archiveReady() {
			retry = now.Add(retryInterval)
		}
	}
}

// Cleanup archives finished days, applies local and archive retention and
// removes pulled days. It is what the daily schedule runs.
func (l *Logger) Cleanup(trigger string) *RunReport {
	if l == nil {
		return &RunReport{}
	}
	return l.archiver.run(true, trigger)
}

// ArchiveNow uploads finished days that are not in the bucket yet.
func (l *Logger) ArchiveNow(trigger string) (*RunReport, error) {
	if l == nil || !l.archiver.current().archiveReady() {
		return nil, ErrArchiveDisabled
	}
	report := l.archiver.run(false, trigger)
	if report.Error != "" {
		return report, errors.New(report.Error)
	}
	return report, nil
}

func (a *archiver) run(full bool, trigger string) (report *RunReport) {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	kind := "archive"
	if full {
		kind = "cleanup"
	}
	report = &RunReport{Kind: kind, Trigger: trigger, Started: a.l.now().UTC(), Archived: []string{}, RemovedLocal: []string{}, RemovedRemote: []string{}, RemovedPulled: []string{}, Kept: []string{}}
	a.mu.Lock()
	a.state.Running = true
	a.mu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			report.Error = fmt.Sprintf("panic: %v", r)
		}
		report.Finished = a.l.now().UTC()
		a.mu.Lock()
		a.state.Running = false
		if full {
			a.state.LastCleanup = report
		}
		if a.settings.archiveReady() || !full {
			a.state.LastArchive = report
		}
		a.mu.Unlock()
		a.saveState()
		if report.Error != "" {
			logger.Error("[AUDIT] !!! audit %s failed: %s", kind, report.Error)
		} else if len(report.Archived)+len(report.RemovedLocal)+len(report.RemovedRemote)+len(report.RemovedPulled) > 0 {
			logger.Info("[Audit] %s: archived %d, removed %d local, %d archived, %d pulled day(s)", kind, len(report.Archived), len(report.RemovedLocal), len(report.RemovedRemote), len(report.RemovedPulled))
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	settings := a.current()
	a.l.Flush()
	today := a.l.today()
	todayDate := today.Format(fileDateLayout)

	local, err := a.l.listDir(a.l.root, false)
	if err != nil {
		report.Error = err.Error()
		return report
	}
	sort.Slice(local, func(i, j int) bool { return local[i].Date < local[j].Date })

	archived := map[string]bool{}
	var errs []string
	if settings.archiveReady() {
		objects, store, err := a.listArchive(ctx, settings)
		if err != nil {
			errs = append(errs, "list archive: "+err.Error())
		} else {
			for _, obj := range objects {
				archived[obj.Date] = true
			}
			for _, file := range local {
				if file.Date >= todayDate || archived[file.Date] {
					continue
				}
				if err := a.upload(ctx, store, settings, file.Date); err != nil {
					errs = append(errs, file.Date+": "+err.Error())
					continue
				}
				archived[file.Date] = true
				report.Archived = append(report.Archived, file.Date)
			}
			if full {
				cutoff := today.AddDate(0, 0, -(settings.Archive.RetentionDays - 1)).Format(fileDateLayout)
				for _, obj := range objects {
					if obj.Date >= cutoff {
						continue
					}
					if err := store.Delete(ctx, obj.Key); err != nil {
						errs = append(errs, "delete "+obj.Key+": "+err.Error())
						continue
					}
					report.RemovedRemote = append(report.RemovedRemote, obj.Date)
				}
			}
		}
	}

	if full {
		cutoff := today.AddDate(0, 0, -(settings.RetentionDays - 1)).Format(fileDateLayout)
		hardCutoff := today.AddDate(0, 0, -(settings.RetentionDays - 1 + unarchivedGraceDays)).Format(fileDateLayout)
		for _, file := range local {
			if file.Date >= cutoff {
				continue
			}
			if settings.archiveReady() && !archived[file.Date] && file.Date >= hardCutoff {
				report.Kept = append(report.Kept, file.Date)
				continue
			}
			if err := os.Remove(a.l.filePath(file.Date, false)); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, "remove "+file.Date+": "+err.Error())
				continue
			}
			report.RemovedLocal = append(report.RemovedLocal, file.Date)
		}
		pulled, err := a.l.listDir(filepath.Join(a.l.root, PulledDirName), true)
		if err != nil {
			errs = append(errs, "list pulled: "+err.Error())
		}
		for _, file := range pulled {
			if err := os.Remove(a.l.filePath(file.Date, true)); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, "remove pulled "+file.Date+": "+err.Error())
				continue
			}
			report.RemovedPulled = append(report.RemovedPulled, file.Date)
		}
	}
	report.Error = strings.Join(errs, "; ")
	return report
}

func archivePrefix(settings Settings) string {
	prefix := backup.NormalizePrefix(settings.Archive.S3.Prefix)
	if prefix == "" {
		prefix = DefaultArchivePrefix
	}
	return prefix + "/"
}

func archiveKey(settings Settings, date string) string {
	return archivePrefix(settings) + date + archiveSuffix
}

func (a *archiver) listArchive(ctx context.Context, settings Settings) ([]ArchiveInfo, backup.ObjectStore, error) {
	store, err := a.factory(settings.Archive.S3)
	if err != nil {
		return nil, nil, err
	}
	prefix := archivePrefix(settings)
	objects, err := store.List(ctx, prefix)
	if err != nil {
		return nil, nil, err
	}
	out := make([]ArchiveInfo, 0, len(objects))
	for _, obj := range objects {
		name := strings.TrimPrefix(obj.Key, prefix)
		if !strings.HasSuffix(name, archiveSuffix) {
			continue
		}
		date := strings.TrimSuffix(name, archiveSuffix)
		if !validDate(date) {
			continue
		}
		out = append(out, ArchiveInfo{Date: date, Key: obj.Key, Size: obj.Size})
	}
	return out, store, nil
}

func (a *archiver) upload(ctx context.Context, store backup.ObjectStore, settings Settings, date string) error {
	f, err := os.Open(a.l.filePath(date, false))
	if err != nil {
		return err
	}
	defer f.Close()
	reader, writer := io.Pipe()
	go func() {
		gz := gzip.NewWriter(writer)
		_, err := io.Copy(gz, f)
		if closeErr := gz.Close(); err == nil {
			err = closeErr
		}
		writer.CloseWithError(err)
	}()
	err = store.Put(ctx, archiveKey(settings, date), reader, "application/gzip")
	reader.CloseWithError(err)
	return err
}

// ListArchives lists the archived days in the bucket, newest first, marking
// the ones also available on disk.
func (l *Logger) ListArchives(ctx context.Context) ([]ArchiveInfo, error) {
	if l == nil {
		return nil, ErrArchiveDisabled
	}
	settings := l.archiver.current()
	if !settings.archiveReady() {
		return nil, ErrArchiveDisabled
	}
	archives, _, err := l.archiver.listArchive(ctx, settings)
	if err != nil {
		return nil, err
	}
	for i := range archives {
		archives[i].Local = fileExists(l.filePath(archives[i].Date, false))
		archives[i].Pulled = fileExists(l.filePath(archives[i].Date, true))
	}
	sort.Slice(archives, func(i, j int) bool { return archives[i].Date > archives[j].Date })
	return archives, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Pull downloads one archived day into the pulled directory, where searches
// that include pulled days can read it until the next cleanup.
func (l *Logger) Pull(ctx context.Context, date string) (*FileInfo, error) {
	if l == nil {
		return nil, ErrArchiveDisabled
	}
	if !validDate(date) {
		return nil, ErrInvalidDate
	}
	a := l.archiver
	settings := a.current()
	if !settings.archiveReady() {
		return nil, ErrArchiveDisabled
	}
	if fileExists(l.filePath(date, false)) {
		return nil, ErrAlreadyLocal
	}
	a.runMu.Lock()
	defer a.runMu.Unlock()
	store, err := a.factory(settings.Archive.S3)
	if err != nil {
		return nil, err
	}
	body, err := store.Get(ctx, archiveKey(settings, date))
	if errors.Is(err, backup.ErrObjectNotFound) {
		return nil, ErrArchiveNotFound
	}
	if err != nil {
		return nil, err
	}
	defer body.Close()
	gz, err := gzip.NewReader(body)
	if err != nil {
		return nil, fmt.Errorf("archive is not valid gzip: %w", err)
	}
	defer gz.Close()

	dir := filepath.Join(l.root, PulledDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	final := l.filePath(date, true)
	tmp, err := os.CreateTemp(dir, date+".*.tmp")
	if err != nil {
		return nil, err
	}
	written, err := io.Copy(tmp, io.LimitReader(gz, maxPulledBytes+1))
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil && written > maxPulledBytes {
		err = errors.New("archive is larger than the 2 GB pull limit")
	}
	if err != nil {
		os.Remove(tmp.Name())
		return nil, err
	}
	if err := os.Rename(tmp.Name(), final); err != nil {
		os.Remove(tmp.Name())
		return nil, err
	}
	now := l.now()
	_ = os.Chtimes(final, now, now)
	return &FileInfo{Date: date, Size: written, Pulled: true, PulledAt: &now}, nil
}

// RemovePulled deletes one pulled day ahead of the next cleanup.
func (l *Logger) RemovePulled(date string) error {
	if l == nil {
		return os.ErrNotExist
	}
	if !validDate(date) {
		return ErrInvalidDate
	}
	return os.Remove(l.filePath(date, true))
}

// TestArchive checks that the bucket can be listed, written and cleaned up.
func (l *Logger) TestArchive(ctx context.Context, cfg backup.S3Config) error {
	if l == nil {
		return errors.New("audit logging is disabled")
	}
	if cfg.Secret == "" {
		cfg.Secret = l.archiver.current().Archive.S3.Secret
	}
	cfg = cfg.Normalized()
	if !cfg.Complete() {
		return errors.New("bucket, region, access key and secret are required")
	}
	store, err := l.archiver.factory(cfg)
	if err != nil {
		return err
	}
	settings := Settings{Archive: ArchiveSettings{S3: cfg}}
	prefix := archivePrefix(settings)
	if _, err := store.List(ctx, prefix); err != nil {
		return fmt.Errorf("list: %w", err)
	}
	key := prefix + ".diarum-probe"
	if err := store.Put(ctx, key, strings.NewReader("ok"), "text/plain"); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err := store.Delete(ctx, key); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	return nil
}
