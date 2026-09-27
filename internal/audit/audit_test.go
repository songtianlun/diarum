package audit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// clock is a settable time source shared by a test and its logger.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Set(t time.Time) {
	c.mu.Lock()
	c.now = t
	c.mu.Unlock()
}

func newTestLogger(t *testing.T, opts Options) (*Logger, string, *clock) {
	t.Helper()
	dir := t.TempDir()
	clk := &clock{now: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}
	if opts.Location == nil {
		opts.Location = time.UTC
	}
	if opts.Now == nil {
		opts.Now = clk.Now
	}
	l, err := New(dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Close)
	return l, dir, clk
}

func readDay(t *testing.T, dir, date string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(Dir(dir), date+fileSuffix))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestRecordWritesDailyFiles(t *testing.T) {
	l, dir, _ := newTestLogger(t, Options{})
	day1 := time.Date(2026, 9, 27, 23, 59, 0, 0, time.UTC)
	day2 := day1.Add(2 * time.Minute)
	l.Record(Entry{Time: day1, UserID: "u1", Action: ActionDiaryCreate, UA: strings.Repeat("x", 400), Error: strings.Repeat("e", 400)})
	l.Record(Entry{Time: day2, UserID: "u1", Method: "GET", Path: "/api/v1/diaries"})
	l.Record(Entry{UserID: "u2", Action: ActionAuthLogin}) // time filled in
	l.Flush()

	first := readDay(t, dir, "2026-09-27")
	if strings.Count(first, "\n") != 1 || !strings.Contains(first, `"action":"diary.create"`) {
		t.Fatalf("day 1 = %q", first)
	}
	if strings.Contains(first, strings.Repeat("x", maxUALength+1)) || strings.Contains(first, strings.Repeat("e", maxErrorLength+1)) {
		t.Fatal("long fields were not truncated")
	}
	if second := readDay(t, dir, "2026-09-28"); strings.Count(second, "\n") != 2 {
		t.Fatalf("day 2 = %q", second)
	}
	if stats := l.WriterStats(); stats.Written != 3 || stats.Dropped != 0 {
		t.Fatalf("writer stats = %+v", stats)
	}

	l.Close()
	l.Record(Entry{UserID: "u1", Action: ActionDiaryView})
	l.Flush() // no-op once closed
	if l.WriterStats().Dropped != 1 {
		t.Fatalf("entries after close should be dropped: %+v", l.WriterStats())
	}
}

func TestFlushTickerWritesAndReleasesFiles(t *testing.T) {
	l, dir, _ := newTestLogger(t, Options{FlushInterval: 5 * time.Millisecond})
	l.Record(Entry{UserID: "u1", Action: ActionDiaryView})
	deadline := time.Now().Add(2 * time.Second)
	for {
		if raw, err := os.ReadFile(filepath.Join(Dir(dir), "2026-09-28.log")); err == nil && len(raw) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ticker never flushed")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestIdleFlushClosesYesterdaysFile(t *testing.T) {
	l, _, clk := newTestLogger(t, Options{FlushInterval: time.Hour})
	l.Record(Entry{UserID: "u1", Action: ActionDiaryView})
	l.Flush()
	// Flush is a barrier: the writer finished before it returned.
	if l.file == nil {
		t.Fatal("file should stay open for more writes today")
	}
	clk.Set(clk.Now().AddDate(0, 0, 1))
	l.Flush()
	if l.file != nil {
		t.Fatal("yesterday's file should be closed once nothing more is headed for it")
	}
}

func TestWriteFailuresAreCounted(t *testing.T) {
	l, dir, _ := newTestLogger(t, Options{})
	// Replace the directory with a file so the day file cannot be created.
	root := Dir(dir)
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	l.Record(Entry{UserID: "u1", Action: ActionDiaryView})
	l.Flush()
	if l.WriterStats().Dropped != 1 {
		t.Fatalf("stats = %+v", l.WriterStats())
	}
}

func TestNilLoggerIsSafe(t *testing.T) {
	var l *Logger
	l.Record(Entry{})
	l.Flush()
	l.Close()
	l.StartScheduler()
	if l.WriterStats() != (WriterStats{}) {
		t.Fatal("nil writer stats")
	}
	if files, err := l.Files(true); err != nil || len(files) != 0 {
		t.Fatal("nil files")
	}
	if _, err := l.OpenFile("2026-01-01", false); err == nil {
		t.Fatal("nil open")
	}
	if r, err := l.Search(Query{}); err != nil || len(r.Entries) != 0 {
		t.Fatal("nil search")
	}
	if l.Settings() != DefaultSettings() {
		t.Fatal("nil settings")
	}
	if _, err := l.UpdateSettings(DefaultSettings()); err == nil {
		t.Fatal("nil update")
	}
	if (l.ArchiveState() != State{}) {
		t.Fatal("nil state")
	}
	if r := l.Cleanup("x"); r == nil {
		t.Fatal("nil cleanup")
	}
	if _, err := l.ArchiveNow("x"); err == nil {
		t.Fatal("nil archive")
	}
	if _, err := l.ListArchives(context.Background()); err == nil {
		t.Fatal("nil list")
	}
	if _, err := l.Pull(context.Background(), "2026-01-01"); err == nil {
		t.Fatal("nil pull")
	}
	if err := l.RemovePulled("2026-01-01"); err == nil {
		t.Fatal("nil remove")
	}
	if err := l.TestArchive(context.Background(), DefaultSettings().Archive.S3); err == nil {
		t.Fatal("nil test")
	}
}

func TestNewFailsWhenDirectoryCannotBeCreated(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, LogDirName), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(dir, Options{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestDefaultsAndAppendDirect(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if l.loc != time.Local || l.flushTick != defaultFlushInterval || l.schedulerDelay != startupDelay {
		t.Fatal("defaults not applied")
	}
	l.Close()
	l.Close() // idempotent

	if err := AppendDirect(dir, Entry{Source: SourceCLI, Action: ActionAdminRole, Target: "bob"}); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(Dir(dir))
	found := false
	for _, f := range files {
		if strings.HasSuffix(f.Name(), fileSuffix) {
			found = true
		}
	}
	if !found {
		t.Fatal("AppendDirect wrote nothing")
	}

	blocked := t.TempDir()
	if err := os.WriteFile(filepath.Join(blocked, LogDirName), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AppendDirect(blocked, Entry{Action: "x"}); err == nil {
		t.Fatal("expected mkdir error")
	}
	if err := AppendDirect(dir, Entry{Action: "x", Detail: map[string]any{"bad": func() {}}}); err == nil {
		t.Fatal("expected encode error")
	}
	if err := os.Chmod(Dir(dir), 0o500); err == nil {
		defer os.Chmod(Dir(dir), 0o700)
		if err := AppendDirect(dir, Entry{Action: "x", Time: time.Date(2001, 1, 1, 0, 0, 0, 0, time.Local)}); err == nil && os.Geteuid() != 0 {
			t.Fatal("expected open error")
		}
	}
}

func TestRecordRecoversAndEncodeErrors(t *testing.T) {
	l, _, _ := newTestLogger(t, Options{})
	// Unencodable detail is reported and skipped, not fatal.
	l.Record(Entry{Action: "x", Detail: map[string]any{"bad": make(chan int)}})
	l.Flush()
	if l.WriterStats().Written != 0 {
		t.Fatal("bad entry written")
	}
	// A panicking writer step is contained.
	l.safely("test", func() { panic("boom") })
}

func TestRemoveLegacy(t *testing.T) {
	dir := t.TempDir()
	RemoveLegacy(dir) // nothing there
	legacy := filepath.Join(dir, LogDirName, LegacyDirName, "user1")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	RemoveLegacy(dir)
	if _, err := os.Stat(filepath.Dir(legacy)); !os.IsNotExist(err) {
		t.Fatal("legacy dir kept")
	}
}

func TestCountNewlines(t *testing.T) {
	if countNewlines([]byte("a\nb\n")) != 2 {
		t.Fatal("count")
	}
}
