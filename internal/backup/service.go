package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/songtianlun/diarum/internal/archive"
	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/logger"
	"github.com/songtianlun/diarum/internal/store"
)

const (
	KindBackup  = "backup"
	KindRestore = "restore"

	TriggerManual    = "manual"
	TriggerScheduled = "scheduled"

	StatusRunning = "running"
	StatusSuccess = "success"
	StatusFailed  = "failed"
	// StatusMissing marks a backup whose log says it succeeded but whose
	// archive is no longer in the bucket.
	StatusMissing = "missing"

	// KeyLastRun persists the outcome of the latest job, so an error stays
	// visible after a restart even when it never reached the bucket.
	KeyLastRun = "backup.last_run"

	archiveSuffix = ".zip"
	logSuffix     = ".log.json"
	idLayout      = "20060102T150405Z"
	probeName     = ".diarum-write-test"

	jobTimeout     = 12 * time.Hour
	logPutTimeout  = 2 * time.Minute
	maxFailedLines = 20
	// At most this many jobs run at once across all users; the rest wait.
	maxParallelJobs = 2
)

var (
	ErrBusy       = errors.New("another backup or restore is already running")
	ErrDisabled   = errors.New("backups are not enabled")
	ErrIncomplete = errors.New("bucket, region, access key and secret are required for S3")
	ErrInvalidID  = errors.New("invalid backup id")
	ErrNotFound   = errors.New("backup not found")

	idPattern = regexp.MustCompile(`^\d{8}T\d{6}Z-[0-9a-f]{6}$`)
)

// LogEntry is one line of a backup log.
type LogEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

// Log is stored next to the archive as <id>.log.json.
type Log struct {
	ID         string               `json:"id"`
	Trigger    string               `json:"trigger"`
	Status     string               `json:"status"`
	StartedAt  string               `json:"started_at"`
	FinishedAt string               `json:"finished_at"`
	DurationMs int64                `json:"duration_ms"`
	Archive    string               `json:"archive,omitempty"`
	Size       int64                `json:"size"`
	SHA256     string               `json:"sha256,omitempty"`
	AppVersion string               `json:"app_version,omitempty"`
	Stats      *archive.ExportStats `json:"stats,omitempty"`
	Error      string               `json:"error,omitempty"`
	Entries    []LogEntry           `json:"entries"`
}

// Entry is one backup in a listing.
type Entry struct {
	ID            string `json:"id"`
	CreatedAt     string `json:"created_at"`
	Status        string `json:"status"`
	Trigger       string `json:"trigger,omitempty"`
	Size          int64  `json:"size"`
	HasArchive    bool   `json:"has_archive"`
	HasLog        bool   `json:"has_log"`
	DurationMs    int64  `json:"duration_ms"`
	Error         string `json:"error,omitempty"`
	Diaries       int    `json:"diaries"`
	Media         int    `json:"media"`
	Conversations int    `json:"conversations"`
}

// Job is the state of a user's current or latest backup or restore.
type Job struct {
	Kind        string               `json:"kind"`
	BackupID    string               `json:"backup_id"`
	Trigger     string               `json:"trigger,omitempty"`
	Status      string               `json:"status"`
	Stage       string               `json:"stage"`
	StartedAt   string               `json:"started_at"`
	FinishedAt  string               `json:"finished_at,omitempty"`
	Error       string               `json:"error,omitempty"`
	ImportStats *archive.ImportStats `json:"import_stats,omitempty"`
}

// Service runs backups and restores. Each user has at most one job at a
// time.
type Service struct {
	store   *store.Store
	config  *config.ConfigService
	tempDir string
	version string

	// OpenStore connects to a destination; tests replace it.
	OpenStore func(S3Config) (ObjectStore, error)
	// OnRestored runs after a successful restore.
	OnRestored func(userID string)

	now   func() time.Time
	slots chan struct{}

	mu   sync.Mutex
	jobs map[string]*Job
	wg   sync.WaitGroup
}

// NewService creates the backup service. Temporary files left behind by a
// run interrupted by a crash are removed.
func NewService(s *store.Store, version string) *Service {
	tempDir := filepath.Join(s.DataDir, "tmp", "backup")
	_ = os.RemoveAll(tempDir)
	return &Service{
		store:     s,
		config:    config.NewConfigService(s),
		tempDir:   tempDir,
		version:   version,
		OpenStore: NewS3Store,
		now:       time.Now,
		slots:     make(chan struct{}, maxParallelJobs),
		jobs:      make(map[string]*Job),
	}
}

// Wait blocks until every job started so far has finished.
func (s *Service) Wait() {
	s.wg.Wait()
}

// UserPrefix is the key prefix holding a user's backups.
func UserPrefix(cfg S3Config, userID string) string {
	if cfg.Prefix == "" {
		return userID + "/"
	}
	return cfg.Prefix + "/" + userID + "/"
}

// Status returns the user's current or latest job, or nil.
func (s *Service) Status(userID string) *Job {
	s.mu.Lock()
	job := s.jobs[userID]
	s.mu.Unlock()
	if job != nil {
		return s.snapshot(job)
	}
	raw, err := s.store.GetSetting(userID, KeyLastRun)
	if err != nil || raw == nil {
		return nil
	}
	data, _ := json.Marshal(raw)
	var last Job
	if json.Unmarshal(data, &last) != nil || last.Kind == "" {
		return nil
	}
	if last.Status == StatusRunning {
		// The process stopped while this job ran.
		last.Status = StatusFailed
		last.Error = "interrupted by a server restart"
	}
	return &last
}

func (s *Service) snapshot(job *Job) *Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	copied := *job
	if job.ImportStats != nil {
		stats := *job.ImportStats
		copied.ImportStats = &stats
	}
	return &copied
}

// claim registers job as the user's running job unless one is running.
func (s *Service) claim(userID string, job *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current := s.jobs[userID]; current != nil && current.Status == StatusRunning {
		return ErrBusy
	}
	s.jobs[userID] = job
	return nil
}

func (s *Service) update(userID string, fn func(*Job)) {
	s.mu.Lock()
	if job := s.jobs[userID]; job != nil {
		fn(job)
	}
	s.mu.Unlock()
}

func (s *Service) finishJob(userID string, err error, fn func(*Job)) {
	s.update(userID, func(job *Job) {
		job.FinishedAt = s.now().UTC().Format(time.RFC3339)
		job.Stage = ""
		if err != nil {
			job.Status = StatusFailed
			job.Error = err.Error()
		} else {
			job.Status = StatusSuccess
		}
		if fn != nil {
			fn(job)
		}
	})
	if job := s.Status(userID); job != nil {
		if perr := s.store.SetSetting(userID, KeyLastRun, job, false); perr != nil {
			logger.Warn("[Backup] saving last run for %s: %v", userID, perr)
		}
	}
}

// destination loads the user's settings and connects to the destination.
func (s *Service) destination(userID string) (Settings, ObjectStore, error) {
	settings, err := LoadSettings(s.config, userID)
	if err != nil {
		return Settings{}, nil, err
	}
	if !settings.Enabled {
		return settings, nil, ErrDisabled
	}
	if !settings.S3.Complete() {
		return settings, nil, ErrIncomplete
	}
	dest, err := s.OpenStore(settings.S3)
	if err != nil {
		return settings, nil, fmt.Errorf("connect to S3: %w", err)
	}
	return settings, dest, nil
}

func (s *Service) newID() (string, error) {
	suffix := make([]byte, 3)
	if _, err := rand.Read(suffix); err != nil {
		return "", err
	}
	return s.now().UTC().Format(idLayout) + "-" + hex.EncodeToString(suffix), nil
}

// acquire waits for a free job slot.
func (s *Service) acquire(ctx context.Context) (func(), error) {
	select {
	case s.slots <- struct{}{}:
		return func() { <-s.slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// StartBackup starts a backup in the background.
func (s *Service) StartBackup(userID, trigger string) (*Job, error) {
	settings, dest, err := s.destination(userID)
	if err != nil {
		return nil, err
	}
	id, err := s.newID()
	if err != nil {
		return nil, err
	}
	job := &Job{Kind: KindBackup, BackupID: id, Trigger: trigger, Status: StatusRunning, Stage: "queued", StartedAt: s.now().UTC().Format(time.RFC3339)}
	if err := s.claim(userID, job); err != nil {
		return nil, err
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runBackup(userID, id, trigger, settings, dest)
	}()
	return s.snapshot(job), nil
}

// run collects the log of one backup.
type run struct {
	svc    *Service
	userID string
	log    *Log
}

func (r *run) add(level, format string, args ...any) {
	r.log.Entries = append(r.log.Entries, LogEntry{Time: r.svc.now().UTC().Format(time.RFC3339), Level: level, Message: fmt.Sprintf(format, args...)})
}

func (r *run) info(format string, args ...any)  { r.add("info", format, args...) }
func (r *run) warn(format string, args ...any)  { r.add("warn", format, args...) }
func (r *run) error(format string, args ...any) { r.add("error", format, args...) }

func (r *run) stage(name string) {
	r.svc.update(r.userID, func(job *Job) { job.Stage = name })
}

func (s *Service) runBackup(userID, id, trigger string, settings Settings, dest ObjectStore) {
	started := s.now()
	r := &run{svc: s, userID: userID, log: &Log{ID: id, Trigger: trigger, StartedAt: started.UTC().Format(time.RFC3339), AppVersion: s.version, Entries: make([]LogEntry, 0)}}
	r.info("Backup %s started (%s)", id, trigger)
	logger.Info("[Backup] %s started for user %s (%s)", id, userID, trigger)

	ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
	defer cancel()
	err := func() error {
		r.stage("waiting")
		release, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer release()
		return s.backup(ctx, r, settings, dest)
	}()

	finished := s.now()
	r.log.FinishedAt = finished.UTC().Format(time.RFC3339)
	r.log.DurationMs = finished.Sub(started).Milliseconds()
	if err != nil {
		r.log.Status = StatusFailed
		r.log.Error = err.Error()
		r.error("Backup failed: %v", err)
		logger.Error("[Backup] %s failed for user %s: %v", id, userID, err)
	} else {
		r.log.Status = StatusSuccess
		r.info("Backup finished in %s", finished.Sub(started).Round(time.Second))
		logger.Info("[Backup] %s finished for user %s", id, userID)
	}

	// The log is written even when the backup failed.
	logErr := s.putLog(dest, UserPrefix(settings.S3, userID)+id+logSuffix, r.log)
	if logErr != nil {
		logger.Error("[Backup] %s: writing log failed: %v", id, logErr)
		if err == nil {
			err = fmt.Errorf("backup uploaded, but writing its log failed: %w", logErr)
		} else {
			err = fmt.Errorf("%v (writing the log also failed: %v)", err, logErr)
		}
	}
	s.finishJob(userID, err, nil)
}

func (s *Service) putLog(dest ObjectStore, key string, log *Log) error {
	data, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), logPutTimeout)
	defer cancel()
	return dest.Put(ctx, key, bytes.NewReader(data), "application/json")
}

func (s *Service) backup(ctx context.Context, r *run, settings Settings, dest ObjectStore) error {
	prefix := UserPrefix(settings.S3, r.userID)
	archiveKey := prefix + r.log.ID + archiveSuffix
	r.info("Destination: s3://%s/%s", settings.S3.Bucket, prefix)

	if err := os.MkdirAll(s.tempDir, 0o700); err != nil {
		return fmt.Errorf("create temp directory: %w", err)
	}
	tmp, err := os.CreateTemp(s.tempDir, r.log.ID+"-*.zip")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	r.stage("exporting")
	r.info("Exporting diaries, media and AI conversations")
	opts := archive.AllTime(s.now())
	opts.TempDir = s.tempDir
	opts.Now = s.now
	hash := sha256.New()
	stats, err := archive.Export(ctx, s.store, r.userID, opts, io.MultiWriter(tmp, hash))
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}
	stats.DateRangeType = "all"
	r.log.Stats = stats
	r.info("Exported %d diaries, %d of %d media files, %d conversations (%d messages)",
		stats.Diaries.ActualExported, stats.Media.ActualExported, stats.Media.ShouldExport, stats.Conversations.ActualExported, stats.Messages)
	for i, item := range stats.FailedItems {
		if i == maxFailedLines {
			r.warn("... and %d more items could not be exported", len(stats.FailedItems)-maxFailedLines)
			break
		}
		r.warn("Skipped %s %s: %s", item.Type, item.ID, item.Reason)
	}

	size, err := tmp.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("read archive: %w", err)
	}
	// Check the archive is readable before it can push out an older backup.
	if _, err := zip.NewReader(tmp, size); err != nil {
		return fmt.Errorf("verify archive: %w", err)
	}
	r.log.Size = size
	r.log.SHA256 = hex.EncodeToString(hash.Sum(nil))
	r.info("Archive verified: %s, sha256 %s", FormatBytes(size), r.log.SHA256)
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind archive: %w", err)
	}

	r.stage("uploading")
	r.info("Uploading %s", archiveKey)
	uploadStart := s.now()
	if err := dest.Put(ctx, archiveKey, tmp, "application/zip"); err != nil {
		return fmt.Errorf("upload archive: %w", err)
	}
	r.log.Archive = archiveKey
	r.info("Upload finished in %s", s.now().Sub(uploadStart).Round(time.Millisecond))

	r.stage("cleaning up")
	s.applyRetention(ctx, dest, prefix, settings.Keep, r)
	return nil
}

// group is one backup in the bucket: its archive, its log, or both.
type group struct {
	id      string
	archive *Object
	log     *Object
}

// listGroups lists the backups under prefix, newest first. Objects that do
// not look like backups are ignored, so they are never touched.
func listGroups(ctx context.Context, dest ObjectStore, prefix string) ([]*group, error) {
	objects, err := dest.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*group)
	for i := range objects {
		obj := &objects[i]
		name := strings.TrimPrefix(obj.Key, prefix)
		var id string
		isLog := strings.HasSuffix(name, logSuffix)
		if isLog {
			id = strings.TrimSuffix(name, logSuffix)
		} else {
			id = strings.TrimSuffix(name, archiveSuffix)
		}
		if id == name || !idPattern.MatchString(id) {
			continue
		}
		g := byID[id]
		if g == nil {
			g = &group{id: id}
			byID[id] = g
		}
		if isLog {
			g.log = obj
		} else {
			g.archive = obj
		}
	}
	groups := make([]*group, 0, len(byID))
	for _, g := range byID {
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].id > groups[j].id })
	return groups, nil
}

// applyRetention keeps the newest keep backups and the newest keep failed
// attempts, deleting everything older. Problems are logged, not returned:
// the new backup is already safe.
func (s *Service) applyRetention(ctx context.Context, dest ObjectStore, prefix string, keep int, r *run) {
	groups, err := listGroups(ctx, dest, prefix)
	if err != nil {
		r.warn("Could not list backups for cleanup: %v", err)
		return
	}
	var complete, failed []*group
	for _, g := range groups {
		if g.archive != nil {
			complete = append(complete, g)
		} else {
			failed = append(failed, g)
		}
	}
	var expired []*group
	if len(complete) > keep {
		expired = append(expired, complete[keep:]...)
	}
	if len(failed) > keep {
		expired = append(expired, failed[keep:]...)
	}
	if len(expired) == 0 {
		r.info("Retention: %d backups stored, keeping up to %d", len(complete), keep)
		return
	}
	removed := 0
	for _, g := range expired {
		if err := s.deleteGroup(ctx, dest, prefix, g.id); err != nil {
			r.warn("Could not remove old backup %s: %v", g.id, err)
			continue
		}
		removed++
		r.info("Removed old backup %s", g.id)
	}
	r.info("Retention: removed %d of %d expired backups, keeping up to %d", removed, len(expired), keep)
}

// deleteGroup removes a backup's archive first, then its log, so a partial
// failure never leaves an archive without the log that explains it.
func (s *Service) deleteGroup(ctx context.Context, dest ObjectStore, prefix, id string) error {
	if err := dest.Delete(ctx, prefix+id+archiveSuffix); err != nil {
		return err
	}
	return dest.Delete(ctx, prefix+id+logSuffix)
}

// List returns the user's backups, newest first.
func (s *Service) List(ctx context.Context, userID string) ([]Entry, error) {
	settings, dest, err := s.destination(userID)
	if err != nil {
		return nil, err
	}
	prefix := UserPrefix(settings.S3, userID)
	groups, err := listGroups(ctx, dest, prefix)
	if err != nil {
		return nil, fmt.Errorf("list backups: %w", err)
	}

	entries := make([]Entry, len(groups))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, g := range groups {
		entry := &entries[i]
		entry.ID = g.id
		if created, err := time.Parse(idLayout, g.id[:len(idLayout)]); err == nil {
			entry.CreatedAt = created.Format(time.RFC3339)
		}
		entry.HasArchive = g.archive != nil
		entry.HasLog = g.log != nil
		if g.archive != nil {
			entry.Size = g.archive.Size
			entry.Status = StatusSuccess
		} else {
			entry.Status = StatusFailed
		}
		if g.log == nil {
			continue
		}
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			log, err := readLog(ctx, dest, key)
			if err != nil {
				return
			}
			entry.Trigger = log.Trigger
			entry.DurationMs = log.DurationMs
			entry.Error = log.Error
			if log.Stats != nil {
				entry.Diaries = log.Stats.Diaries.ActualExported
				entry.Media = log.Stats.Media.ActualExported
				entry.Conversations = log.Stats.Conversations.ActualExported
			}
			if log.Status == StatusSuccess && !entry.HasArchive {
				entry.Status = StatusMissing
			}
		}(g.log.Key)
	}
	wg.Wait()
	return entries, nil
}

func readLog(ctx context.Context, dest ObjectStore, key string) (*Log, error) {
	rc, err := dest.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var log Log
	if err := json.NewDecoder(io.LimitReader(rc, 16<<20)).Decode(&log); err != nil {
		return nil, fmt.Errorf("read log: %w", err)
	}
	return &log, nil
}

// Log returns the log of one backup.
func (s *Service) Log(ctx context.Context, userID, id string) (*Log, error) {
	if !idPattern.MatchString(id) {
		return nil, ErrInvalidID
	}
	settings, dest, err := s.destination(userID)
	if err != nil {
		return nil, err
	}
	log, err := readLog(ctx, dest, UserPrefix(settings.S3, userID)+id+logSuffix)
	if errors.Is(err, ErrObjectNotFound) {
		return nil, ErrNotFound
	}
	return log, err
}

// Open streams a backup archive.
func (s *Service) Open(ctx context.Context, userID, id string) (io.ReadCloser, error) {
	if !idPattern.MatchString(id) {
		return nil, ErrInvalidID
	}
	settings, dest, err := s.destination(userID)
	if err != nil {
		return nil, err
	}
	rc, err := dest.Get(ctx, UserPrefix(settings.S3, userID)+id+archiveSuffix)
	if errors.Is(err, ErrObjectNotFound) {
		return nil, ErrNotFound
	}
	return rc, err
}

// Delete removes a backup's archive and log.
func (s *Service) Delete(ctx context.Context, userID, id string) error {
	if !idPattern.MatchString(id) {
		return ErrInvalidID
	}
	if job := s.Status(userID); job != nil && job.Status == StatusRunning && job.BackupID == id {
		return ErrBusy
	}
	settings, dest, err := s.destination(userID)
	if err != nil {
		return err
	}
	return s.deleteGroup(ctx, dest, UserPrefix(settings.S3, userID), id)
}

// StartRestore imports a backup in the background. Like any import it only
// adds what is missing: diaries on dates that already have one are skipped.
func (s *Service) StartRestore(userID, id string) (*Job, error) {
	if !idPattern.MatchString(id) {
		return nil, ErrInvalidID
	}
	settings, dest, err := s.destination(userID)
	if err != nil {
		return nil, err
	}
	job := &Job{Kind: KindRestore, BackupID: id, Status: StatusRunning, Stage: "queued", StartedAt: s.now().UTC().Format(time.RFC3339)}
	if err := s.claim(userID, job); err != nil {
		return nil, err
	}
	key := UserPrefix(settings.S3, userID) + id + archiveSuffix
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		logger.Info("[Backup] restoring %s for user %s", id, userID)
		stats, err := s.restore(userID, key, dest)
		if err != nil {
			logger.Error("[Backup] restoring %s for user %s failed: %v", id, userID, err)
		} else if s.OnRestored != nil {
			s.OnRestored(userID)
		}
		s.finishJob(userID, err, func(job *Job) { job.ImportStats = stats })
	}()
	return s.snapshot(job), nil
}

func (s *Service) restore(userID, key string, dest ObjectStore) (*archive.ImportStats, error) {
	ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
	defer cancel()
	stage := func(name string) { s.update(userID, func(job *Job) { job.Stage = name }) }

	stage("waiting")
	release, err := s.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	stage("downloading")
	if err := os.MkdirAll(s.tempDir, 0o700); err != nil {
		return nil, fmt.Errorf("create temp directory: %w", err)
	}
	tmp, err := os.CreateTemp(s.tempDir, "restore-*.zip")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	rc, err := dest.Get(ctx, key)
	if err != nil {
		_ = tmp.Close()
		if errors.Is(err, ErrObjectNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("download: %w", err)
	}
	_, err = io.Copy(tmp, rc)
	_ = rc.Close()
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}

	stage("importing")
	zr, err := zip.OpenReader(tmp.Name())
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer zr.Close()
	return archive.Import(ctx, s.store, userID, &zr.Reader)
}

// Test checks that the destination accepts writes, reads, listings and
// deletes, which is everything backups, restores and cleanup rely on.
func (s *Service) Test(ctx context.Context, userID string, cfg S3Config) error {
	cfg = cfg.Normalized()
	if !cfg.Complete() {
		return ErrIncomplete
	}
	dest, err := s.OpenStore(cfg)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	key := UserPrefix(cfg, userID) + probeName
	payload := "diarum backup write test " + s.now().UTC().Format(time.RFC3339)
	if err := dest.Put(ctx, key, strings.NewReader(payload), "text/plain"); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}
	defer func() { _ = dest.Delete(context.Background(), key) }()
	rc, err := dest.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("read failed: %w", err)
	}
	got, err := io.ReadAll(io.LimitReader(rc, 1024))
	_ = rc.Close()
	if err != nil || string(got) != payload {
		return fmt.Errorf("read failed: object content did not match")
	}
	if _, err := dest.List(ctx, UserPrefix(cfg, userID)); err != nil {
		return fmt.Errorf("list failed: %w", err)
	}
	if err := dest.Delete(ctx, key); err != nil {
		return fmt.Errorf("delete failed: %w", err)
	}
	return nil
}

// FormatBytes renders a size for logs.
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
