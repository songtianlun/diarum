package audit

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/songtianlun/diarum/internal/backup"
)

type memSettings struct {
	mu     sync.Mutex
	values map[string]string
	getErr error
	setErr error
}

func (m *memSettings) GetSystemSetting(key string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return "", false, m.getErr
	}
	v, ok := m.values[key]
	return v, ok, nil
}

func (m *memSettings) SetSystemSetting(key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setErr != nil {
		return m.setErr
	}
	if m.values == nil {
		m.values = map[string]string{}
	}
	m.values[key] = value
	return nil
}

func s3Config() backup.S3Config {
	return backup.S3Config{Bucket: "b", Region: "r", AccessKey: "ak", Secret: "sk", Prefix: "/audit//logs/"}
}

// archiveLogger is a logger whose archive goes to an in-memory bucket.
func archiveLogger(t *testing.T) (*Logger, string, *clock, *backup.MemoryStore, *memSettings) {
	t.Helper()
	bucket := backup.NewMemoryStore()
	settings := &memSettings{}
	var factoryErr error
	l, dir, clk := newTestLogger(t, Options{
		Settings: settings,
		NewObjectStore: func(cfg backup.S3Config) (backup.ObjectStore, error) {
			if factoryErr != nil {
				return nil, factoryErr
			}
			return bucket, nil
		},
	})
	return l, dir, clk, bucket, settings
}

func enableArchive(t *testing.T, l *Logger) Settings {
	t.Helper()
	s := DefaultSettings()
	s.Archive.Enabled = true
	s.Archive.S3 = s3Config()
	got, err := l.UpdateSettings(s)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func writeDays(t *testing.T, dir string, pulled bool, dates ...string) {
	t.Helper()
	root := Dir(dir)
	if pulled {
		root = filepath.Join(root, PulledDirName)
	}
	for _, date := range dates {
		line := `{"time":"` + date + `T01:00:00Z","action":"diary.view"}` + "\n"
		if err := os.WriteFile(filepath.Join(root, date+fileSuffix), []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func exists(dir, date string, pulled bool) bool {
	root := Dir(dir)
	if pulled {
		root = filepath.Join(root, PulledDirName)
	}
	_, err := os.Stat(filepath.Join(root, date+fileSuffix))
	return err == nil
}

func gz(t *testing.T, s string) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write([]byte(s))
	w.Close()
	return buf.Bytes()
}

func TestSettingsValidateAndPersist(t *testing.T) {
	l, _, _, _, store := archiveLogger(t)
	if got := l.Settings(); got.RetentionDays != 3 || got.Archive.RetentionDays != 30 || got.Archive.S3.Prefix != DefaultArchivePrefix {
		t.Fatalf("defaults = %+v", got)
	}
	bad := []func(*Settings){
		func(s *Settings) { s.RetentionDays = 0 },
		func(s *Settings) { s.RetentionDays = MaxRetentionDays + 1 },
		func(s *Settings) { s.Archive.RetentionDays = 0 },
		func(s *Settings) { s.Archive.Enabled = true }, // no bucket
	}
	for i, mutate := range bad {
		s := DefaultSettings()
		mutate(&s)
		if _, err := l.UpdateSettings(s); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}

	saved := enableArchive(t, l)
	if saved.Archive.S3.Prefix != "audit/logs" {
		t.Fatalf("prefix = %q", saved.Archive.S3.Prefix)
	}
	if !strings.Contains(store.values[SettingsKey], `"secret":"sk"`) {
		t.Fatalf("stored = %s", store.values[SettingsKey])
	}
	// An empty secret keeps the stored one; an empty prefix gets the default.
	next := saved
	next.Archive.S3.Secret = ""
	next.Archive.S3.Prefix = ""
	next.RetentionDays = 5
	got, err := l.UpdateSettings(next)
	if err != nil || got.Archive.S3.Secret != "sk" || got.RetentionDays != 5 || got.Archive.S3.Prefix != DefaultArchivePrefix {
		t.Fatalf("update = %+v (%v)", got, err)
	}

	store.setErr = errors.New("disk full")
	if _, err := l.UpdateSettings(next); err == nil {
		t.Fatal("store error ignored")
	}

	// A fresh logger on the same store loads what was saved.
	store.setErr = nil
	l2, err := New(t.TempDir(), Options{Settings: store})
	if err != nil {
		t.Fatal(err)
	}
	defer l2.Close()
	if l2.Settings().RetentionDays != 5 || !l2.Settings().Archive.Enabled {
		t.Fatalf("reloaded = %+v", l2.Settings())
	}
}

func TestLoadSettingsFallbacks(t *testing.T) {
	for _, store := range []*memSettings{
		{getErr: errors.New("db down")},
		{values: map[string]string{SettingsKey: "{broken"}},
		{values: map[string]string{SettingsKey: `{"retention_days":0,"archive":{"retention_days":99999,"s3":{"prefix":""}}}`}},
	} {
		l, err := New(t.TempDir(), Options{Settings: store})
		if err != nil {
			t.Fatal(err)
		}
		got := l.Settings()
		l.Close()
		if got.RetentionDays != DefaultRetentionDays || got.Archive.S3.Prefix != DefaultArchivePrefix {
			t.Fatalf("settings = %+v", got)
		}
		if got.Archive.RetentionDays != DefaultArchiveRetentionDays && got.Archive.RetentionDays != MaxArchiveRetentionDays {
			t.Fatalf("archive retention = %d", got.Archive.RetentionDays)
		}
	}
	// No store: settings live in memory only.
	l, _ := New(t.TempDir(), Options{})
	defer l.Close()
	s := DefaultSettings()
	s.RetentionDays = 7
	if got, err := l.UpdateSettings(s); err != nil || got.RetentionDays != 7 {
		t.Fatalf("in-memory update = %+v %v", got, err)
	}
}

func TestCleanupWithoutArchive(t *testing.T) {
	l, dir, _, _, _ := archiveLogger(t)
	// Today is 2026-09-28; retention 3 keeps 26..28.
	writeDays(t, dir, false, "2026-09-20", "2026-09-25", "2026-09-26", "2026-09-27", "2026-09-28")
	writeDays(t, dir, true, "2026-08-01")
	report := l.Cleanup("test")
	if report.Error != "" || strings.Join(report.RemovedLocal, ",") != "2026-09-20,2026-09-25" || len(report.RemovedPulled) != 1 {
		t.Fatalf("report = %+v", report)
	}
	for _, date := range []string{"2026-09-26", "2026-09-27", "2026-09-28"} {
		if !exists(dir, date, false) {
			t.Fatalf("%s removed", date)
		}
	}
	state := l.ArchiveState()
	if state.LastCleanup == nil || state.LastArchive != nil || state.Running {
		t.Fatalf("state = %+v", state)
	}
	// The state survives a restart.
	raw, err := os.ReadFile(filepath.Join(Dir(dir), stateFile))
	if err != nil || !strings.Contains(string(raw), "2026-09-25") {
		t.Fatalf("state file = %s (%v)", raw, err)
	}
	l2, _ := New(dir, Options{})
	defer l2.Close()
	if l2.ArchiveState().LastCleanup == nil {
		t.Fatal("state not reloaded")
	}
}

func TestArchiveUploadsFinishedDaysAndAppliesRetention(t *testing.T) {
	l, dir, clk, bucket, _ := archiveLogger(t)
	settings := enableArchive(t, l)
	writeDays(t, dir, false, "2026-09-20", "2026-09-27", "2026-09-28")
	// Archives older than 30 days are removed from the bucket; junk is ignored.
	bucket.Put(context.Background(), "audit/logs/2026-08-01.log.gz", bytes.NewReader(gz(t, "old")), "")
	bucket.Put(context.Background(), "audit/logs/readme.txt", strings.NewReader("x"), "")
	bucket.Put(context.Background(), "audit/logs/nodate.log.gz", strings.NewReader("x"), "")

	report, err := l.ArchiveNow("test")
	if err != nil || strings.Join(report.Archived, ",") != "2026-09-20,2026-09-27" {
		t.Fatalf("archive = %+v (%v)", report, err)
	}
	obj, ok := bucket.Object(archiveKey(settings, "2026-09-27"))
	if !ok {
		t.Fatal("not uploaded")
	}
	r, _ := gzip.NewReader(bytes.NewReader(obj))
	raw, _ := io.ReadAll(r)
	if !strings.Contains(string(raw), "diary.view") {
		t.Fatalf("archive content = %q", raw)
	}
	// Nothing new: a second run uploads nothing.
	again, _ := l.ArchiveNow("test")
	if len(again.Archived) != 0 {
		t.Fatalf("re-uploaded %v", again.Archived)
	}

	clk.Set(at(28, 12, 0))
	cleanup := l.Cleanup("test")
	if cleanup.Error != "" || strings.Join(cleanup.RemovedLocal, ",") != "2026-09-20" || strings.Join(cleanup.RemovedRemote, ",") != "2026-08-01" {
		t.Fatalf("cleanup = %+v", cleanup)
	}

	archives, err := l.ListArchives(context.Background())
	if err != nil || len(archives) != 2 || archives[0].Date != "2026-09-27" || !archives[0].Local || archives[1].Local {
		t.Fatalf("archives = %+v (%v)", archives, err)
	}
}

func TestUnarchivedDaysAreKeptUntilGraceRunsOut(t *testing.T) {
	l, dir, _, bucket, _ := archiveLogger(t)
	enableArchive(t, l)
	bucket.FailPut = func(string) error { return errors.New("bucket full") }
	writeDays(t, dir, false, "2026-09-10", "2026-09-24")
	report := l.Cleanup("test")
	if report.Error == "" || strings.Join(report.Kept, ",") != "2026-09-24" || strings.Join(report.RemovedLocal, ",") != "2026-09-10" {
		t.Fatalf("report = %+v", report)
	}
	if l.ArchiveState().LastArchive == nil {
		t.Fatal("archive outcome not recorded")
	}
	if _, err := l.ArchiveNow("test"); err == nil {
		t.Fatal("upload failure not reported")
	}

	bucket.FailPut = nil
	bucket.FailList = func(string) error { return errors.New("denied") }
	if r := l.Cleanup("test"); !strings.Contains(r.Error, "list archive") {
		t.Fatalf("list failure = %+v", r)
	}
	bucket.FailList = nil
	bucket.Put(context.Background(), "audit/logs/2026-01-01.log.gz", strings.NewReader("x"), "")
	bucket.FailDelete = func(string) error { return errors.New("denied") }
	if r := l.Cleanup("test"); !strings.Contains(r.Error, "delete") {
		t.Fatalf("delete failure = %+v", r)
	}
}

func TestPullAndRemovePulled(t *testing.T) {
	l, dir, _, bucket, _ := archiveLogger(t)
	ctx := context.Background()
	if _, err := l.Pull(ctx, "2026-09-01"); !errors.Is(err, ErrArchiveDisabled) {
		t.Fatalf("disabled err = %v", err)
	}
	settings := enableArchive(t, l)
	line := `{"time":"2026-09-01T08:00:00Z","user_id":"u1","action":"diary.delete"}` + "\n"
	bucket.Put(ctx, archiveKey(settings, "2026-09-01"), bytes.NewReader(gz(t, line)), "")
	bucket.Put(ctx, archiveKey(settings, "2026-09-02"), strings.NewReader("not gzip"), "")
	writeDays(t, dir, false, "2026-09-28")

	if _, err := l.Pull(ctx, "bad"); !errors.Is(err, ErrInvalidDate) {
		t.Fatalf("invalid err = %v", err)
	}
	if _, err := l.Pull(ctx, "2026-09-28"); !errors.Is(err, ErrAlreadyLocal) {
		t.Fatalf("local err = %v", err)
	}
	if _, err := l.Pull(ctx, "2026-09-03"); !errors.Is(err, ErrArchiveNotFound) {
		t.Fatalf("missing err = %v", err)
	}
	if _, err := l.Pull(ctx, "2026-09-02"); err == nil || !strings.Contains(err.Error(), "gzip") {
		t.Fatalf("gzip err = %v", err)
	}
	bucket.FailGet = func(string) error { return errors.New("boom") }
	if _, err := l.Pull(ctx, "2026-09-01"); err == nil {
		t.Fatal("get error ignored")
	}
	bucket.FailGet = nil

	file, err := l.Pull(ctx, "2026-09-01")
	if err != nil || !file.Pulled || file.Size != int64(len(line)) {
		t.Fatalf("pull = %+v (%v)", file, err)
	}
	result, _ := l.Search(Query{IncludePulled: true, Actions: []string{ActionDiaryDelete}})
	if result.Total != 1 {
		t.Fatalf("pulled entries not searchable: %d", result.Total)
	}
	archives, _ := l.ListArchives(ctx)
	if len(archives) != 2 || !archives[1].Pulled {
		t.Fatalf("archives = %+v", archives)
	}

	if err := l.RemovePulled("nope"); !errors.Is(err, ErrInvalidDate) {
		t.Fatalf("remove invalid = %v", err)
	}
	if err := l.RemovePulled("2026-09-01"); err != nil {
		t.Fatal(err)
	}
	if err := l.RemovePulled("2026-09-01"); !os.IsNotExist(err) {
		t.Fatalf("remove twice = %v", err)
	}
}

func TestArchiveFactoryErrors(t *testing.T) {
	boom := errors.New("no route to bucket")
	l, _, _ := newTestLogger(t, Options{NewObjectStore: func(backup.S3Config) (backup.ObjectStore, error) { return nil, boom }})
	enableArchive(t, l)
	ctx := context.Background()
	if _, err := l.ListArchives(ctx); !errors.Is(err, boom) {
		t.Fatalf("list = %v", err)
	}
	if _, err := l.Pull(ctx, "2026-09-01"); !errors.Is(err, boom) {
		t.Fatalf("pull = %v", err)
	}
	if err := l.TestArchive(ctx, s3Config()); !errors.Is(err, boom) {
		t.Fatalf("test = %v", err)
	}
}

func TestTestArchive(t *testing.T) {
	l, _, _, bucket, _ := archiveLogger(t)
	ctx := context.Background()
	if err := l.TestArchive(ctx, backup.S3Config{Bucket: "b"}); err == nil {
		t.Fatal("incomplete config accepted")
	}
	if err := l.TestArchive(ctx, s3Config()); err != nil {
		t.Fatal(err)
	}
	if len(bucket.Keys()) != 0 {
		t.Fatalf("probe left behind: %v", bucket.Keys())
	}
	noPrefix := s3Config()
	noPrefix.Prefix = " / "
	if err := l.TestArchive(ctx, noPrefix); err != nil {
		t.Fatal(err)
	}
	// The stored secret is used when the form leaves it blank.
	enableArchive(t, l)
	cfg := s3Config()
	cfg.Secret = ""
	if err := l.TestArchive(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	for name, fail := range map[string]func(){
		"list":   func() { bucket.FailList = func(string) error { return errors.New("x") } },
		"write":  func() { bucket.FailPut = func(string) error { return errors.New("x") } },
		"delete": func() { bucket.FailDelete = func(string) error { return errors.New("x") } },
	} {
		bucket.FailList, bucket.FailPut, bucket.FailDelete = nil, nil, nil
		fail()
		if err := l.TestArchive(ctx, s3Config()); err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("%s failure = %v", name, err)
		}
	}
}

func TestArchiveNowDisabled(t *testing.T) {
	l, _, _, _, _ := archiveLogger(t)
	if _, err := l.ArchiveNow("x"); !errors.Is(err, ErrArchiveDisabled) {
		t.Fatalf("err = %v", err)
	}
	if _, err := l.ListArchives(context.Background()); !errors.Is(err, ErrArchiveDisabled) {
		t.Fatalf("list err = %v", err)
	}
}

func TestCleanupReportsFileErrors(t *testing.T) {
	l, dir, _, _, _ := archiveLogger(t)
	root := Dir(dir)
	os.RemoveAll(root)
	os.WriteFile(root, []byte("x"), 0o600)
	if r := l.Cleanup("test"); r.Error == "" {
		t.Fatal("unreadable root not reported")
	}
}

func TestCleanupRecoversFromPanic(t *testing.T) {
	l, _, _ := newTestLogger(t, Options{NewObjectStore: func(backup.S3Config) (backup.ObjectStore, error) { panic("sdk bug") }})
	enableArchive(t, l)
	if r := l.Cleanup("test"); !strings.Contains(r.Error, "panic") {
		t.Fatalf("report = %+v", r)
	}
}

func TestNextCleanupAfter(t *testing.T) {
	before := time.Date(2026, 9, 28, 0, 5, 0, 0, time.UTC)
	after := time.Date(2026, 9, 28, 0, 10, 0, 0, time.UTC)
	if got := nextCleanupAfter(before, time.UTC); !got.Equal(after) {
		t.Fatalf("before = %v", got)
	}
	if got := nextCleanupAfter(after, time.UTC); !got.Equal(after.AddDate(0, 0, 1)) {
		t.Fatalf("at = %v", got)
	}
}

func TestSchedulerRunsCleanupAndRetries(t *testing.T) {
	bucket := backup.NewMemoryStore()
	bucket.FailPut = func(string) error { return errors.New("down") }
	dir := t.TempDir()
	l, err := New(dir, Options{
		Location:       time.UTC,
		SchedulerDelay: time.Millisecond,
		NewObjectStore: func(backup.S3Config) (backup.ObjectStore, error) { return bucket, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	writeDays(t, dir, false, "2000-01-01")
	s := DefaultSettings()
	s.Archive.Enabled = true
	s.Archive.S3 = s3Config()
	if _, err := l.UpdateSettings(s); err != nil {
		t.Fatal(err)
	}
	l.StartScheduler()
	l.StartScheduler() // second call is a no-op
	deadline := time.Now().Add(5 * time.Second)
	for {
		state := l.ArchiveState()
		if state.LastCleanup != nil && state.NextRetry != nil {
			if !state.NextCleanup.After(time.Now()) {
				t.Fatalf("next cleanup = %v", state.NextCleanup)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("scheduler never ran: %+v", state)
		}
		time.Sleep(5 * time.Millisecond)
	}
	l.Close()
}
