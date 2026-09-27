package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/smithy-go"

	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/store"
)

type fixture struct {
	t     *testing.T
	store *store.Store
	user  *store.User
	svc   *Service
	mem   *MemoryStore
	clock *fakeClock
}

// fakeClock advances a minute on every read, so each backup gets its own
// timestamped id.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(time.Minute)
	return c.t
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	id, _ := store.GenerateID()
	user, err := s.CreateUser("user_"+id, id+"@example.com", "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	svc := NewService(s, "test")
	mem := NewMemoryStore()
	svc.OpenStore = func(S3Config) (ObjectStore, error) { return mem, nil }
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	svc.now = clock.now
	t.Cleanup(svc.Wait)
	return &fixture{t: t, store: s, user: user, svc: svc, mem: mem, clock: clock}
}

func testS3() S3Config {
	return S3Config{Bucket: "bucket", Region: "us-east-1", AccessKey: "key", Secret: "secret", Prefix: "backups"}
}

func (f *fixture) configure(settings Settings) {
	f.t.Helper()
	if settings.Schedule == "" {
		settings.Schedule = DefaultSchedule
	}
	if settings.Keep == 0 {
		settings.Keep = DefaultKeep
	}
	if err := config.NewConfigService(f.store).SetBatch(f.user.ID, settings.Values()); err != nil {
		f.t.Fatalf("SetBatch: %v", err)
	}
}

func (f *fixture) enable(keep int) {
	f.configure(Settings{Enabled: true, S3: testS3(), Keep: keep})
}

func (f *fixture) prefix() string {
	return UserPrefix(testS3(), f.user.ID)
}

// backup runs one backup to completion and returns the finished job.
func (f *fixture) backup() *Job {
	f.t.Helper()
	if _, err := f.svc.StartBackup(f.user.ID, TriggerManual); err != nil {
		f.t.Fatalf("StartBackup: %v", err)
	}
	f.svc.Wait()
	return f.svc.Status(f.user.ID)
}

func (f *fixture) countKeys(suffix string) int {
	n := 0
	for _, key := range f.mem.Keys() {
		if strings.HasPrefix(key, f.prefix()) && strings.HasSuffix(key, suffix) {
			n++
		}
	}
	return n
}

func TestBackupWritesArchiveLogAndAppliesRetention(t *testing.T) {
	f := newFixture(t)
	f.enable(2)
	if _, err := f.store.InsertImportedDiary(f.user.ID, "", "2025-05-01", "hello", "", ""); err != nil {
		t.Fatalf("InsertImportedDiary: %v", err)
	}
	// Objects that are not backups are never touched by cleanup.
	foreign := []string{f.prefix() + "notes.txt", f.prefix() + "20200101T000000Z-zzzzzz.zip", f.prefix() + "nested/20200101T000000Z-aaaaaa.zip"}
	for _, key := range foreign {
		_ = f.mem.Put(context.Background(), key, strings.NewReader("x"), "")
	}

	var ids []string
	for i := 0; i < 4; i++ {
		job := f.backup()
		if job.Status != StatusSuccess || job.Kind != KindBackup || job.Error != "" {
			t.Fatalf("job %d = %#v", i, job)
		}
		ids = append(ids, job.BackupID)
	}
	if f.countKeys(archiveSuffix) != 4 || f.countKeys(logSuffix) != 2 { // 2 archives + 2 foreign zips
		t.Fatalf("keys after retention = %v", f.mem.Keys())
	}
	for _, key := range foreign {
		if _, ok := f.mem.Object(key); !ok {
			t.Fatalf("foreign object %s was removed", key)
		}
	}

	entries, err := f.svc.List(context.Background(), f.user.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 || entries[0].ID != ids[3] || entries[1].ID != ids[2] {
		t.Fatalf("entries = %#v, want newest two of %v", entries, ids)
	}
	e := entries[0]
	if e.Status != StatusSuccess || e.Trigger != TriggerManual || e.Diaries != 1 || !e.HasArchive || !e.HasLog || e.Size == 0 || e.CreatedAt == "" {
		t.Fatalf("entry = %#v", e)
	}

	log, err := f.svc.Log(context.Background(), f.user.ID, ids[3])
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if log.Status != StatusSuccess || log.SHA256 == "" || log.Stats == nil || log.AppVersion != "test" {
		t.Fatalf("log = %#v", log)
	}
	joined := ""
	for _, entry := range log.Entries {
		joined += entry.Message + "\n"
	}
	for _, want := range []string{"Exported 1 diaries", "Archive verified", "Removed old backup " + ids[1], "Backup finished"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("log lacks %q:\n%s", want, joined)
		}
	}

	// The archive is a regular export.
	rc, err := f.svc.Open(context.Background(), f.user.ID, ids[3])
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	data, _ := io.ReadAll(rc)
	rc.Close()
	if _, err := zip.NewReader(bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("archive is not a zip: %v", err)
	}
}

func TestFailedBackupStillWritesLog(t *testing.T) {
	f := newFixture(t)
	f.enable(2)
	f.mem.FailPut = func(key string) error {
		if strings.HasSuffix(key, archiveSuffix) {
			return errors.New("access denied")
		}
		return nil
	}
	var last *Job
	for i := 0; i < 3; i++ {
		last = f.backup()
		if last.Status != StatusFailed || !strings.Contains(last.Error, "upload archive: access denied") {
			t.Fatalf("job = %#v", last)
		}
	}
	// Failed attempts are pruned too once the next backup succeeds.
	f.mem.FailPut = nil
	ok := f.backup()
	if f.countKeys(archiveSuffix) != 1 || f.countKeys(logSuffix) != 3 {
		t.Fatalf("keys = %v", f.mem.Keys())
	}
	entries, err := f.svc.List(context.Background(), f.user.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 3 || entries[0].ID != ok.BackupID || entries[1].ID != last.BackupID {
		t.Fatalf("entries = %#v", entries)
	}
	failed := entries[1]
	if failed.Status != StatusFailed || failed.HasArchive || !failed.HasLog || !strings.Contains(failed.Error, "access denied") {
		t.Fatalf("failed entry = %#v", failed)
	}
	log, err := f.svc.Log(context.Background(), f.user.ID, last.BackupID)
	if err != nil || log.Status != StatusFailed || !strings.Contains(log.Entries[len(log.Entries)-1].Message, "Backup failed") {
		t.Fatalf("failed log = %#v, %v", log, err)
	}
}

func TestBackupLogUploadFailures(t *testing.T) {
	f := newFixture(t)
	f.enable(3)
	f.mem.FailPut = func(key string) error {
		if strings.HasSuffix(key, logSuffix) {
			return errors.New("log denied")
		}
		return nil
	}
	job := f.backup()
	if job.Status != StatusFailed || !strings.Contains(job.Error, "backup uploaded, but writing its log failed") {
		t.Fatalf("job = %#v", job)
	}
	f.mem.FailPut = func(string) error { return errors.New("denied") }
	job = f.backup()
	if job.Status != StatusFailed || !strings.Contains(job.Error, "writing the log also failed") {
		t.Fatalf("job = %#v", job)
	}
}

func TestBackupListingMarksMissingArchives(t *testing.T) {
	f := newFixture(t)
	f.enable(3)
	job := f.backup()
	_ = f.mem.Delete(context.Background(), f.prefix()+job.BackupID+archiveSuffix)
	_ = f.mem.Put(context.Background(), f.prefix()+"20250101T000000Z-abcdef"+logSuffix, strings.NewReader("not json"), "")
	_ = f.mem.Put(context.Background(), f.prefix()+"20250101T000000Z-abcdee"+archiveSuffix, strings.NewReader("zip"), "")
	entries, err := f.svc.List(context.Background(), f.user.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	statuses := map[string]string{}
	for _, e := range entries {
		statuses[e.ID] = e.Status
	}
	if statuses[job.BackupID] != StatusMissing || statuses["20250101T000000Z-abcdef"] != StatusFailed || statuses["20250101T000000Z-abcdee"] != StatusSuccess {
		t.Fatalf("statuses = %v", statuses)
	}
	if _, err := f.svc.Log(context.Background(), f.user.ID, "20250101T000000Z-abcdef"); err == nil {
		t.Fatal("reading a corrupt log should fail")
	}
}

func TestRetentionReportsProblems(t *testing.T) {
	f := newFixture(t)
	r := &run{svc: f.svc, userID: f.user.ID, log: &Log{}}
	f.mem.FailList = func(string) error { return errors.New("list denied") }
	f.svc.applyRetention(context.Background(), f.mem, f.prefix(), 1, r)
	f.mem.FailList = nil
	for _, id := range []string{"20250101T000000Z-aaaaaa", "20250102T000000Z-aaaaaa"} {
		_ = f.mem.Put(context.Background(), f.prefix()+id+archiveSuffix, strings.NewReader("x"), "")
	}
	f.mem.FailDelete = func(key string) error { return errors.New("delete denied") }
	f.svc.applyRetention(context.Background(), f.mem, f.prefix(), 1, r)
	text := ""
	for _, e := range r.log.Entries {
		text += e.Level + ":" + e.Message + "\n"
	}
	if !strings.Contains(text, "warn:Could not list backups for cleanup: list denied") || !strings.Contains(text, "warn:Could not remove old backup 20250101T000000Z-aaaaaa") {
		t.Fatalf("retention log:\n%s", text)
	}
}

func TestServiceRejectsWhenNotReady(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.StartBackup(f.user.ID, TriggerManual); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled err = %v", err)
	}
	f.configure(Settings{Enabled: true, S3: S3Config{Bucket: "b"}})
	if _, err := f.svc.List(context.Background(), f.user.ID); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("incomplete err = %v", err)
	}
	f.enable(3)
	f.svc.OpenStore = func(S3Config) (ObjectStore, error) { return nil, errors.New("bad endpoint") }
	if _, err := f.svc.StartRestore(f.user.ID, "20250101T000000Z-aaaaaa"); err == nil || !strings.Contains(err.Error(), "connect to S3: bad endpoint") {
		t.Fatalf("open store err = %v", err)
	}
	for name, call := range map[string]func() error{
		"log":     func() error { _, err := f.svc.Log(context.Background(), f.user.ID, "x"); return err },
		"open":    func() error { _, err := f.svc.Open(context.Background(), f.user.ID, "../x"); return err },
		"delete":  func() error { return f.svc.Delete(context.Background(), f.user.ID, "") },
		"restore": func() error { _, err := f.svc.StartRestore(f.user.ID, "nope"); return err },
	} {
		if err := call(); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("%s with a bad id err = %v", name, err)
		}
	}
	for name, call := range map[string]func() error{
		"log": func() error {
			_, err := f.svc.Log(context.Background(), f.user.ID, "20250101T000000Z-aaaaaa")
			return err
		},
		"open": func() error {
			_, err := f.svc.Open(context.Background(), f.user.ID, "20250101T000000Z-aaaaaa")
			return err
		},
		"delete": func() error { return f.svc.Delete(context.Background(), f.user.ID, "20250101T000000Z-aaaaaa") },
	} {
		if err := call(); err == nil {
			t.Fatalf("%s without a destination should fail", name)
		}
	}
	f.svc.OpenStore = func(S3Config) (ObjectStore, error) { return f.mem, nil }
	if _, err := f.svc.Log(context.Background(), f.user.ID, "20250101T000000Z-aaaaaa"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing log err = %v", err)
	}
	if _, err := f.svc.Open(context.Background(), f.user.ID, "20250101T000000Z-aaaaaa"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing archive err = %v", err)
	}
	f.mem.FailList = func(string) error { return errors.New("list denied") }
	if _, err := f.svc.List(context.Background(), f.user.ID); err == nil {
		t.Fatal("List should report listing errors")
	}
}

func TestOneJobPerUser(t *testing.T) {
	f := newFixture(t)
	f.enable(3)
	running := &Job{Kind: KindRestore, BackupID: "20250101T000000Z-aaaaaa", Status: StatusRunning}
	if err := f.svc.claim(f.user.ID, running); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := f.svc.StartBackup(f.user.ID, TriggerManual); !errors.Is(err, ErrBusy) {
		t.Fatalf("StartBackup while busy err = %v", err)
	}
	if _, err := f.svc.StartRestore(f.user.ID, "20250101T000000Z-aaaaaa"); !errors.Is(err, ErrBusy) {
		t.Fatalf("StartRestore while busy err = %v", err)
	}
	if err := f.svc.Delete(context.Background(), f.user.ID, "20250101T000000Z-aaaaaa"); !errors.Is(err, ErrBusy) {
		t.Fatalf("Delete of the backup being restored err = %v", err)
	}
	running.Status = StatusSuccess
	if err := f.svc.Delete(context.Background(), f.user.ID, "20250101T000000Z-aaaaaa"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestJobsWaitForAFreeSlot(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < maxParallelJobs; i++ {
		f.svc.slots <- struct{}{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.svc.acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("acquire err = %v", err)
	}
	f.enable(3)
	if _, err := f.svc.StartBackup(f.user.ID, TriggerManual); err != nil {
		t.Fatalf("StartBackup: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if job := f.svc.Status(f.user.ID); job.Status != StatusRunning || job.Stage != "waiting" {
		t.Fatalf("queued job = %#v", job)
	}
	for i := 0; i < maxParallelJobs; i++ {
		<-f.svc.slots
	}
	f.svc.Wait()
	if job := f.svc.Status(f.user.ID); job.Status != StatusSuccess {
		t.Fatalf("job = %#v", job)
	}
}

func TestRestoreBringsBackDeletedData(t *testing.T) {
	f := newFixture(t)
	f.enable(3)
	diary, err := f.store.InsertImportedDiary(f.user.ID, "", "2025-05-01", "precious", "", "")
	if err != nil {
		t.Fatalf("InsertImportedDiary: %v", err)
	}
	job := f.backup()
	if err := f.svc.Delete(context.Background(), f.user.ID, "20990101T000000Z-aaaaaa"); err != nil {
		t.Fatalf("deleting an absent backup should succeed: %v", err)
	}
	if _, err := f.store.DB.Exec(`DELETE FROM diaries WHERE id = ?`, diary.ID); err != nil {
		t.Fatalf("delete diary: %v", err)
	}

	restored := ""
	f.svc.OnRestored = func(userID string) { restored = userID }
	started, err := f.svc.StartRestore(f.user.ID, job.BackupID)
	if err != nil || started.Kind != KindRestore || started.Status != StatusRunning {
		t.Fatalf("StartRestore = %#v, %v", started, err)
	}
	f.svc.Wait()
	done := f.svc.Status(f.user.ID)
	if done.Status != StatusSuccess || done.ImportStats == nil || done.ImportStats.Diaries.Imported != 1 {
		t.Fatalf("restore job = %#v", done)
	}
	if restored != f.user.ID || f.store.CountDiaries(f.user.ID) != 1 {
		t.Fatalf("restored=%q diaries=%d", restored, f.store.CountDiaries(f.user.ID))
	}

	if err := f.svc.Delete(context.Background(), f.user.ID, job.BackupID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(f.mem.Keys()) != 0 {
		t.Fatalf("Delete left %v", f.mem.Keys())
	}
}

func TestRestoreFailures(t *testing.T) {
	f := newFixture(t)
	f.enable(3)
	id := "20250101T000000Z-aaaaaa"
	restore := func() *Job {
		t.Helper()
		if _, err := f.svc.StartRestore(f.user.ID, id); err != nil {
			t.Fatalf("StartRestore: %v", err)
		}
		f.svc.Wait()
		return f.svc.Status(f.user.ID)
	}
	if job := restore(); job.Status != StatusFailed || job.Error != ErrNotFound.Error() {
		t.Fatalf("missing archive job = %#v", job)
	}
	_ = f.mem.Put(context.Background(), f.prefix()+id+archiveSuffix, strings.NewReader("not a zip"), "")
	if job := restore(); job.Status != StatusFailed || !strings.Contains(job.Error, "open archive") {
		t.Fatalf("corrupt archive job = %#v", job)
	}
	f.mem.FailGet = func(string) error { return errors.New("read denied") }
	if job := restore(); !strings.Contains(job.Error, "download: read denied") {
		t.Fatalf("download failure job = %#v", job)
	}
	f.mem.FailGet = nil
	f.svc.tempDir = "/dev/null/impossible"
	if job := restore(); !strings.Contains(job.Error, "temp directory") {
		t.Fatalf("temp dir failure job = %#v", job)
	}
	if job := f.backup(); !strings.Contains(job.Error, "temp directory") {
		t.Fatalf("backup temp dir failure job = %#v", job)
	}
}

func TestStatusSurvivesRestart(t *testing.T) {
	f := newFixture(t)
	if f.svc.Status(f.user.ID) != nil {
		t.Fatal("no job yet")
	}
	f.enable(3)
	job := f.backup()

	restarted := NewService(f.store, "test")
	last := restarted.Status(f.user.ID)
	if last == nil || last.BackupID != job.BackupID || last.Status != StatusSuccess {
		t.Fatalf("persisted job = %#v", last)
	}
	running := *job
	running.Status = StatusRunning
	if err := f.store.SetSetting(f.user.ID, KeyLastRun, running, false); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if last := restarted.Status(f.user.ID); last.Status != StatusFailed || !strings.Contains(last.Error, "restart") {
		t.Fatalf("interrupted job = %#v", last)
	}
	if err := f.store.SetSetting(f.user.ID, KeyLastRun, "garbage", false); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if restarted.Status(f.user.ID) != nil {
		t.Fatal("garbage last run should be ignored")
	}
}

type mismatchStore struct{ *MemoryStore }

func (m mismatchStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("something else")), nil
}

func TestConnectionTest(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.svc.Test(ctx, f.user.ID, testS3()); err != nil {
		t.Fatalf("Test: %v", err)
	}
	if len(f.mem.Keys()) != 0 {
		t.Fatalf("probe left behind: %v", f.mem.Keys())
	}
	if err := f.svc.Test(ctx, f.user.ID, S3Config{Bucket: " "}); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("incomplete err = %v", err)
	}
	cases := map[string]func(){
		"write failed": func() { f.mem.FailPut = func(string) error { return errors.New("x") } },
		"read failed":  func() { f.mem.FailGet = func(string) error { return errors.New("x") } },
		"list failed":  func() { f.mem.FailList = func(string) error { return errors.New("x") } },
		"delete failed": func() {
			f.mem.FailDelete = func(string) error { return errors.New("x") }
		},
	}
	for want, setup := range cases {
		f.mem.FailPut, f.mem.FailGet, f.mem.FailList, f.mem.FailDelete = nil, nil, nil, nil
		setup()
		if err := f.svc.Test(ctx, f.user.ID, testS3()); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("want %q, got %v", want, err)
		}
	}
	f.mem.FailDelete = nil
	f.svc.OpenStore = func(S3Config) (ObjectStore, error) { return mismatchStore{f.mem}, nil }
	if err := f.svc.Test(ctx, f.user.ID, testS3()); err == nil || !strings.Contains(err.Error(), "did not match") {
		t.Fatalf("mismatch err = %v", err)
	}
	f.svc.OpenStore = func(S3Config) (ObjectStore, error) { return nil, errors.New("nope") }
	if err := f.svc.Test(ctx, f.user.ID, testS3()); err == nil || !strings.Contains(err.Error(), "connect: nope") {
		t.Fatalf("connect err = %v", err)
	}
}

func TestSettings(t *testing.T) {
	f := newFixture(t)
	cfg := config.NewConfigService(f.store)
	defaults, err := LoadSettings(cfg, f.user.ID)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if defaults.Enabled || defaults.Schedule != DefaultSchedule || defaults.Keep != DefaultKeep || defaults.S3.Prefix != DefaultPrefix {
		t.Fatalf("defaults = %#v", defaults)
	}
	if err := f.store.SetSetting(f.user.ID, KeySchedule, "  ", false); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if got, _ := LoadSettings(cfg, f.user.ID); got.Schedule != DefaultSchedule {
		t.Fatalf("blank schedule = %q", got.Schedule)
	}

	in := S3Config{Bucket: " b ", Region: " r ", Endpoint: " e ", AccessKey: " a ", Secret: " s ", Prefix: " /x//./../y\\z/ "}
	if got := in.Normalized(); got.Bucket != "b" || got.Region != "r" || got.Endpoint != "e" || got.AccessKey != "a" || got.Secret != "s" || got.Prefix != "x/y/z" {
		t.Fatalf("Normalized = %#v", got)
	}
	if !testS3().Complete() || (S3Config{Bucket: "b"}).Complete() {
		t.Fatal("Complete")
	}
	if UserPrefix(S3Config{}, "u") != "u/" || UserPrefix(S3Config{Prefix: "p"}, "u") != "p/u/" {
		t.Fatal("UserPrefix")
	}
	for value, want := range map[any]int{float64(7): 7, 9: 9, float64(0): MinKeep, float64(1000): MaxKeep, "x": DefaultKeep, nil: DefaultKeep, 2.7: 2} {
		if got := ClampKeep(value); got != want {
			t.Fatalf("ClampKeep(%v) = %d, want %d", value, got, want)
		}
	}
	if got := ClampKeep(nanValue()); got != DefaultKeep {
		t.Fatalf("ClampKeep(NaN) = %d", got)
	}
	for n, want := range map[int64]string{10: "10 B", 2048: "2.0 KiB", 5 << 30: "5.0 GiB"} {
		if got := FormatBytes(n); got != want {
			t.Fatalf("FormatBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func nanValue() float64 {
	zero := 0.0
	return zero / zero
}

func TestParseSchedule(t *testing.T) {
	sched, err := ParseSchedule("  0   2 * * * ", "Asia/Shanghai")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	if sched.Expr != "0 2 * * *" || sched.Loc.String() != "Asia/Shanghai" {
		t.Fatalf("schedule = %#v", sched)
	}
	from := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	runs := sched.NextRuns(from, 2)
	if len(runs) != 2 || runs[0].UTC() != time.Date(2026, 3, 1, 18, 0, 0, 0, time.UTC) || runs[1].Sub(runs[0]) != 24*time.Hour {
		t.Fatalf("runs = %v", runs)
	}
	for _, expr := range []string{"@daily", "@weekly", "0 */6 * * *", "30 3 * * 0", "0 3 1 * *"} {
		if _, err := ParseSchedule(expr, ""); err != nil {
			t.Fatalf("ParseSchedule(%q): %v", expr, err)
		}
	}
	bad := map[string]string{
		"":                   "required",
		"TZ=UTC 0 2 * * *":   "time zone separately",
		"@every 1h":          "@every",
		"* * * *":            "invalid cron",
		"61 * * * *":         "invalid cron",
		"*/5 * * * *":        "15 minutes apart",
		"0 0 30 2 *":         "never runs",
		"CRON_TZ=UTC @daily": "time zone separately",
	}
	for expr, want := range bad {
		if _, err := ParseSchedule(expr, ""); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("ParseSchedule(%q) err = %v, want %q", expr, err, want)
		}
	}
	if _, err := ParseSchedule("@daily", "Mars/Olympus"); err == nil || !strings.Contains(err.Error(), "unknown time zone") {
		t.Fatalf("bad zone err = %v", err)
	}
	if loc, err := LoadLocation(" "); err != nil || loc != time.Local {
		t.Fatalf("LoadLocation blank = %v, %v", loc, err)
	}
}

func TestS3StoreAgainstFakeServer(t *testing.T) {
	fake := newFakeS3(t)
	dest, err := NewS3Store(fake.config())
	if err != nil {
		t.Fatalf("NewS3Store: %v", err)
	}
	ctx := context.Background()
	if err := dest.Put(ctx, "backups/u/small.log.json", strings.NewReader(`{"ok":true}`), "application/json"); err != nil {
		t.Fatalf("Put small: %v", err)
	}
	// Larger than one part: goes through multipart upload.
	big := bytes.Repeat([]byte("0123456789abcdef"), (uploadPartSize+1024)/16)
	if err := dest.Put(ctx, "backups/u/big.zip", bytes.NewReader(big), "application/zip"); err != nil {
		t.Fatalf("Put big: %v", err)
	}
	if got := fake.objects["backups/u/big.zip"]; !bytes.Equal(got, big) {
		t.Fatalf("multipart upload stored %d bytes, want %d", len(got), len(big))
	}
	rc, err := dest.Get(ctx, "backups/u/small.log.json")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	body, _ := io.ReadAll(rc)
	rc.Close()
	if string(body) != `{"ok":true}` {
		t.Fatalf("Get body = %q", body)
	}
	if _, err := dest.Get(ctx, "backups/u/missing"); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("Get missing err = %v", err)
	}
	objects, err := dest.List(ctx, "backups/u/")
	if err != nil || len(objects) != 2 || objects[0].Key != "backups/u/big.zip" || objects[0].Size != int64(len(big)) {
		t.Fatalf("List = %#v, %v", objects, err)
	}
	if err := dest.Delete(ctx, "backups/u/big.zip"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := dest.Delete(ctx, "backups/u/never-existed"); err != nil {
		t.Fatalf("Delete missing: %v", err)
	}

	fake.failAll = 403
	if _, err := dest.List(ctx, "backups/"); err == nil {
		t.Fatal("List should fail")
	}
	if _, err := dest.Get(ctx, "backups/u/small.log.json"); err == nil || errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("Get forbidden err = %v", err)
	}
	if err := dest.Delete(ctx, "backups/u/small.log.json"); err == nil {
		t.Fatal("Delete should fail")
	}

	// End to end: the service against the fake server.
	fake.failAll = 0
	f := newFixture(t)
	f.svc.OpenStore = NewS3Store
	f.configure(Settings{Enabled: true, S3: fake.config(), Keep: 1})
	if err := f.svc.Test(ctx, f.user.ID, fake.config()); err != nil {
		t.Fatalf("Test against fake S3: %v", err)
	}
	if job := f.backup(); job.Status != StatusSuccess {
		t.Fatalf("backup against fake S3 = %#v", job)
	}
}

func TestIsNotFound(t *testing.T) {
	if !isNotFound(&smithy.GenericAPIError{Code: "NotFound"}) || isNotFound(&smithy.GenericAPIError{Code: "AccessDenied"}) || isNotFound(errors.New("x")) {
		t.Fatal("isNotFound")
	}
	if _, err := NewS3Store(S3Config{Region: "us-east-1", Endpoint: "://bad"}); err != nil {
		// Endpoint problems surface on first use, not here.
		t.Fatalf("NewS3Store: %v", err)
	}
}

func TestMemoryStoreBodyErrors(t *testing.T) {
	m := NewMemoryStore()
	if err := m.Put(context.Background(), "k", errReader{}, ""); err == nil {
		t.Fatal("Put should report read errors")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Put(ctx, "k", strings.NewReader("x"), ""); err == nil {
		t.Fatal("Put should honour the context")
	}
	data, _ := json.Marshal(m.Keys())
	if string(data) != "[]" {
		t.Fatalf("keys = %s", data)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("broken body") }
