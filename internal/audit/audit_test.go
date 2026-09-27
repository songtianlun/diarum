package audit

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func newTestLogger(t *testing.T, retention int) (*Logger, *fakeClock, string) {
	t.Helper()
	dir := t.TempDir()
	clock := &fakeClock{now: time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)}
	l, err := New(dir, Options{
		Location:  time.UTC,
		Now:       clock.Now,
		Retention: func(string) int { return retention },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Close)
	return l, clock, dir
}

func search(t *testing.T, l *Logger, user string, q Query) []Entry {
	t.Helper()
	result, err := l.Search(user, q)
	if err != nil {
		t.Fatal(err)
	}
	return result.Entries
}

func TestRecordWritesDailyFilePerUser(t *testing.T) {
	l, clock, dir := newTestLogger(t, 7)
	l.Record(Entry{User: "u1", Action: ActionDiaryCreate, Target: "2026-09-28", Detail: map[string]any{"words": 3}})
	l.Record(Entry{User: "u2", Action: ActionDiaryDelete, Target: "2026-09-27"})
	clock.Advance(24 * time.Hour)
	l.Record(Entry{User: "u1", Action: ActionDiaryDelete, Target: "2026-09-28"})
	l.Flush("u1")
	l.Flush("u2")

	for _, path := range []string{
		filepath.Join(dir, "logs", "audit", "u1", "2026-09-28.log"),
		filepath.Join(dir, "logs", "audit", "u1", "2026-09-29.log"),
		filepath.Join(dir, "logs", "audit", "u2", "2026-09-28.log"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s: %v", path, err)
		}
	}

	entries := search(t, l, "u1", Query{})
	if len(entries) != 2 || entries[0].Action != ActionDiaryDelete || entries[1].Action != ActionDiaryCreate {
		t.Fatalf("u1 entries newest first, got %+v", entries)
	}
	if other := search(t, l, "u2", Query{}); len(other) != 1 {
		t.Fatalf("users must not see each other's entries, got %+v", other)
	}
}

func TestRecordRejectsUnsafeUserIDs(t *testing.T) {
	l, _, dir := newTestLogger(t, 7)
	l.Record(Entry{User: "../escape", Action: ActionDiaryCreate})
	l.Record(Entry{User: "", Action: ActionDiaryCreate})
	l.Flush("")
	if _, err := os.Stat(filepath.Join(dir, "logs", "escape")); !os.IsNotExist(err) {
		t.Fatalf("path traversal must be refused: %v", err)
	}
	if entries, _ := os.ReadDir(Dir(dir)); len(entries) != 0 {
		t.Fatalf("nothing should be written, got %d entries", len(entries))
	}
}

func TestAutosavesAreCoalesced(t *testing.T) {
	l, clock, _ := newTestLogger(t, 7)
	save := func(before, after int) {
		l.Record(Entry{User: "u1", Action: ActionDiaryUpdate, Target: "2026-09-28", Detail: map[string]any{"words_before": before, "words": after}})
		clock.Advance(5 * time.Second)
	}
	save(10, 12) // written at once
	save(12, 15) // merged
	save(15, 20) // merged
	save(20, 30) // merged

	l.Flush("u1")
	entries := search(t, l, "u1", Query{})
	if len(entries) != 2 {
		t.Fatalf("expected first save plus one merged entry, got %d: %+v", len(entries), entries)
	}
	merged := entries[0]
	if merged.Detail["saves"] != float64(3) || merged.Detail["words_before"] != float64(12) || merged.Detail["words"] != float64(30) {
		t.Fatalf("merged entry should span the session, got %+v", merged.Detail)
	}

	// After an idle gap a new session starts with an immediate entry.
	clock.Advance(3 * time.Minute)
	save(30, 31)
	l.Flush("u1")
	if entries := search(t, l, "u1", Query{}); len(entries) != 3 {
		t.Fatalf("new session should be written at once, got %d", len(entries))
	}
}

func TestDeleteLandsAfterPendingEdits(t *testing.T) {
	l, clock, _ := newTestLogger(t, 7)
	l.Record(Entry{User: "u1", Action: ActionDiaryUpdate, Target: "d"})
	clock.Advance(time.Second)
	l.Record(Entry{User: "u1", Action: ActionDiaryUpdate, Target: "d"})
	clock.Advance(time.Second)
	l.Record(Entry{User: "u1", Action: ActionDiaryDelete, Target: "d"})
	l.Flush("u1")
	entries := search(t, l, "u1", Query{})
	if len(entries) != 3 || entries[0].Action != ActionDiaryDelete {
		t.Fatalf("delete should be newest, got %+v", entries)
	}
}

func TestRepeatedViewsAreLoggedOncePerWindow(t *testing.T) {
	l, clock, _ := newTestLogger(t, 7)
	view := func(ip string) {
		l.Record(Entry{User: "u1", Action: ActionDiaryView, Target: "2026-09-28", Source: SourceWeb, IP: ip})
		clock.Advance(10 * time.Second)
	}
	view("1.1.1.1")
	view("1.1.1.1")
	view("2.2.2.2") // a different client is always visible
	clock.Advance(3 * time.Minute)
	view("1.1.1.1")
	l.Flush("u1")
	if entries := search(t, l, "u1", Query{Action: ActionDiaryView}); len(entries) != 3 {
		t.Fatalf("expected 3 view entries, got %d", len(entries))
	}
}

func TestSearchFiltersAndLimits(t *testing.T) {
	l, clock, _ := newTestLogger(t, 30)
	for i := 0; i < 5; i++ {
		l.Record(Entry{User: "u1", Action: ActionDiaryCreate, Target: "2026-09-2" + string(rune('0'+i)), IP: "10.0.0.1"})
		clock.Advance(time.Hour)
	}
	l.Record(Entry{User: "u1", Action: ActionDiaryDelete, Target: "2026-09-20"})
	l.Record(Entry{User: "u1", Action: ActionSettingsUpdate, Target: "general.language"})

	if got := search(t, l, "u1", Query{Text: "删除"}); len(got) != 1 || got[0].Action != ActionDiaryDelete {
		t.Fatalf("localized label search failed: %+v", got)
	}
	if got := search(t, l, "u1", Query{Text: "LANGUAGE"}); len(got) != 1 {
		t.Fatalf("case-insensitive search failed: %+v", got)
	}
	if got := search(t, l, "u1", Query{Action: "diary"}); len(got) != 6 {
		t.Fatalf("action prefix filter failed: %d", len(got))
	}
	result, _ := l.Search("u1", Query{Limit: 2})
	if len(result.Entries) != 2 || !result.Truncated {
		t.Fatalf("limit not applied: %+v", result)
	}
	start := time.Date(2026, 9, 28, 11, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 28, 12, 30, 0, 0, time.UTC)
	if got := search(t, l, "u1", Query{Start: start, End: end}); len(got) != 2 {
		t.Fatalf("time range filter failed: %d", len(got))
	}
}

func TestSearchSkipsCorruptLines(t *testing.T) {
	l, _, dir := newTestLogger(t, 7)
	l.Record(Entry{User: "u1", Action: ActionDiaryCreate, Target: "a"})
	l.Flush("u1")
	path := filepath.Join(Dir(dir), "u1", "2026-09-28.log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("{not json\n\n")
	_ = f.Close()
	l.Record(Entry{User: "u1", Action: ActionDiaryDelete, Target: "a"})
	l.Flush("u1")
	if got := search(t, l, "u1", Query{}); len(got) != 2 {
		t.Fatalf("corrupt line should be skipped, got %d", len(got))
	}
}

func TestCleanupKeepsRetentionWindow(t *testing.T) {
	l, clock, dir := newTestLogger(t, 3)
	userDir := filepath.Join(Dir(dir), "u1")
	if err := os.MkdirAll(userDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, date := range []string{"2026-09-20", "2026-09-25", "2026-09-26", "2026-09-27", "notes"} {
		if err := os.WriteFile(filepath.Join(userDir, date+".log"), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	l.Record(Entry{User: "u1", Action: ActionDiaryCreate, Target: "x"})
	l.Flush("u1")
	_ = clock

	// The startup sweep may already have run; either way the result is the same.
	l.Cleanup("u1")
	files, _ := l.Files("u1")
	var dates []string
	for _, f := range files {
		dates = append(dates, f.Date)
	}
	if strings.Join(dates, ",") != "2026-09-28,2026-09-27,2026-09-26" {
		t.Fatalf("unexpected remaining files: %v", dates)
	}
	if _, err := os.Stat(filepath.Join(userDir, "notes.log")); err != nil {
		t.Fatalf("unrelated files must be left alone: %v", err)
	}
}

func TestCloseFlushesAndRejectsLateEntries(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{now: time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)}
	l, err := New(dir, Options{Location: time.UTC, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	l.Record(Entry{User: "u1", Action: ActionDiaryUpdate, Target: "d"})
	clock.Advance(time.Second)
	l.Record(Entry{User: "u1", Action: ActionDiaryUpdate, Target: "d"}) // pending
	l.Close()
	l.Record(Entry{User: "u1", Action: ActionDiaryDelete, Target: "d"}) // dropped, must not panic
	l.Close()                                                           // idempotent

	data, err := os.ReadFile(filepath.Join(Dir(dir), "u1", "2026-09-28.log"))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(data), "\n"); lines != 2 {
		t.Fatalf("pending entry should be flushed on close, got %d lines", lines)
	}
	if l.dropped.Load() != 1 {
		t.Fatalf("late entry should be counted as dropped")
	}
}

func TestNilLoggerIsSafe(t *testing.T) {
	var l *Logger
	l.Record(Entry{User: "u1", Action: ActionDiaryCreate})
	l.Flush("u1")
	l.Close()
	l.CleanupAll()
	if l.RetentionDays("u1") != DefaultRetentionDays {
		t.Fatal("nil logger should report the default retention")
	}
	if result, err := l.Search("u1", Query{}); err != nil || len(result.Entries) != 0 {
		t.Fatalf("nil search: %v %+v", err, result)
	}
}

func TestConcurrentRecordIsSafe(t *testing.T) {
	l, _, _ := newTestLogger(t, 7)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				l.Record(Entry{User: "u1", Action: ActionMediaUpload, Target: string(rune('a' + i))})
			}
		}(i)
	}
	wg.Wait()
	l.Flush("u1")
	result, _ := l.Search("u1", Query{Limit: 5000})
	if len(result.Entries) != 1000 {
		t.Fatalf("expected 1000 entries, got %d", len(result.Entries))
	}
}

func TestOpenFileAndFiles(t *testing.T) {
	l, _, _ := newTestLogger(t, 7)
	l.Record(Entry{User: "u1", Action: ActionDiaryCreate, Target: "a"})
	l.Record(Entry{User: "u1", Action: ActionDiaryDelete, Target: "a"})

	f, err := l.OpenFile("u1", "2026-09-28")
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 4096)
	n, _ := f.Read(data)
	_ = f.Close()
	if strings.Count(string(data[:n]), "\n") != 2 {
		t.Fatalf("raw file = %q", data[:n])
	}
	if _, err := l.OpenFile("u1", "../../x"); err != ErrInvalidDate {
		t.Fatalf("invalid date error = %v", err)
	}
	if _, err := l.OpenFile("../u1", "2026-09-28"); !os.IsNotExist(err) {
		t.Fatalf("invalid user error = %v", err)
	}
	if _, err := l.OpenFile("u1", "2020-01-01"); !os.IsNotExist(err) {
		t.Fatalf("missing file error = %v", err)
	}

	files, err := l.Files("u1")
	if err != nil || len(files) != 1 || files[0].Entries != 2 || files[0].Size == 0 {
		t.Fatalf("files = %+v, %v", files, err)
	}
	if files, _ := l.Files("nobody"); len(files) != 0 {
		t.Fatalf("unknown user should have no files: %+v", files)
	}
	if files, _ := l.Files("../bad"); len(files) != 0 {
		t.Fatalf("invalid user should have no files: %+v", files)
	}
	var nilLogger *Logger
	if _, err := nilLogger.OpenFile("u1", "2026-09-28"); !os.IsNotExist(err) {
		t.Fatalf("nil logger open = %v", err)
	}
}

func TestClampRetention(t *testing.T) {
	cases := map[int]int{-5: MinRetentionDays, 0: MinRetentionDays, 7: 7, 365: 365, 9999: MaxRetentionDays}
	for in, want := range cases {
		if got := ClampRetention(in); got != want {
			t.Fatalf("ClampRetention(%d) = %d, want %d", in, got, want)
		}
	}
	l, _, _ := newTestLogger(t, 1000)
	if l.RetentionDays("u1") != MaxRetentionDays {
		t.Fatal("retention should be clamped")
	}
}

func TestWriteFailuresAreReportedNotFatal(t *testing.T) {
	l, _, dir := newTestLogger(t, 7)
	// A regular file where the user's directory should be makes every write fail.
	if err := os.WriteFile(filepath.Join(Dir(dir), "u1"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	l.Record(Entry{User: "u1", Action: ActionDiaryCreate})
	l.Flush("u1")

	// A directory where today's file should be makes the open fail.
	if err := os.MkdirAll(filepath.Join(Dir(dir), "u2", "2026-09-28.log"), 0o700); err != nil {
		t.Fatal(err)
	}
	l.Record(Entry{User: "u2", Action: ActionDiaryCreate})
	l.Flush("u2")

	// Values JSON cannot encode are dropped, not fatal.
	l.Record(Entry{User: "u3", Action: ActionDiaryCreate, Detail: map[string]any{"bad": make(chan int)}})
	l.Flush("u3")

	// The logger keeps working for everyone else.
	l.Record(Entry{User: "u4", Action: ActionDiaryCreate})
	if got := search(t, l, "u4", Query{}); len(got) != 1 {
		t.Fatalf("logger should survive write failures, got %d", len(got))
	}
	if files, err := l.Files("u1"); err == nil && len(files) != 0 {
		t.Fatalf("u1 should have no readable files: %+v", files)
	}
	if removed := l.Cleanup("u1"); removed != 0 {
		t.Fatalf("cleanup of unreadable dir removed %d", removed)
	}
}

func TestCleanupAllSkipsForeignEntries(t *testing.T) {
	l, _, dir := newTestLogger(t, 1)
	root := Dir(dir)
	for _, user := range []string{"u1", "u2"} {
		if err := os.MkdirAll(filepath.Join(root, user), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, user, "2020-01-01.log"), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(root, "README"), []byte("x"), 0o600)
	_ = os.MkdirAll(filepath.Join(root, "bad name"), 0o700)
	l.CleanupAll()
	for _, user := range []string{"u1", "u2"} {
		if _, err := os.Stat(filepath.Join(root, user, "2020-01-01.log")); !os.IsNotExist(err) {
			t.Fatalf("expired file for %s should be removed", user)
		}
	}
	if l.Cleanup("../x") != 0 {
		t.Fatal("invalid user cleanup should be a no-op")
	}

	// A missing root is reported, not fatal.
	_ = os.RemoveAll(root)
	l.CleanupAll()
}

func TestNewFailsWhenDirectoryCannotBeCreated(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(file, Options{}); err == nil {
		t.Fatal("expected an error when the data dir is a file")
	}
	l, err := New(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if l.loc != time.Local || l.idle != defaultCoalesceIdle || l.maxSpan != defaultCoalesceMaxSpan || l.RetentionDays("u") != DefaultRetentionDays {
		t.Fatal("defaults not applied")
	}
}

func TestIdleSessionsAreFlushedInBackground(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)}
	l, err := New(t.TempDir(), Options{Location: time.UTC, Now: clock.Now, FlushInterval: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Record(Entry{User: "u1", Action: ActionDiaryUpdate, Target: "d"})
	clock.Advance(time.Second)
	l.Record(Entry{User: "u1", Action: ActionDiaryUpdate, Target: "d"}) // merged, pending
	clock.Advance(3 * time.Minute)

	// The ticker, not a read, must write the idle session out.
	deadline := time.Now().Add(2 * time.Second)
	for {
		files, _ := l.listFiles("u1")
		if len(files) == 1 && countLines(filepath.Join(l.userDir("u1"), files[0].Date+fileSuffix)) == 2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("idle session was not flushed by the background loop")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSafelyRecoversPanics(t *testing.T) {
	l, _, _ := newTestLogger(t, 7)
	l.safely("test", func() { panic("boom") })
	l.Record(Entry{User: "u1", Action: ActionDiaryCreate})
	if got := search(t, l, "u1", Query{}); len(got) != 1 {
		t.Fatal("logger should keep working after a recovered panic")
	}
}

func TestFlushAfterCloseReturns(t *testing.T) {
	l, _, _ := newTestLogger(t, 7)
	l.Close()
	done := make(chan struct{})
	go func() {
		l.Flush("u1")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("flush after close should return immediately")
	}
}

func TestUnreadableTrailIsReportedBySearch(t *testing.T) {
	l, _, dir := newTestLogger(t, 7)
	// A regular file where the user's directory should be cannot be listed.
	if err := os.WriteFile(filepath.Join(Dir(dir), "u1"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Search("u1", Query{}); err == nil {
		t.Fatal("search of an unreadable trail should fail")
	}
	if got := l.readFile("u1", "2026-09-28"); got != nil {
		t.Fatalf("missing file should read as nil, got %+v", got)
	}
	if got := countLines(filepath.Join(dir, "missing.log")); got != 0 {
		t.Fatalf("missing file should count 0 lines, got %d", got)
	}
}

func TestFilesSkipsForeignNames(t *testing.T) {
	l, _, dir := newTestLogger(t, 7)
	l.Record(Entry{User: "u1", Action: ActionDiaryCreate})
	l.Flush("u1")
	userDir := filepath.Join(Dir(dir), "u1")
	for _, name := range []string{"notes.txt", "not-a-date.log"} {
		if err := os.WriteFile(filepath.Join(userDir, name), []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(userDir, "2026-01-01.log"), 0o700); err != nil {
		t.Fatal(err)
	}
	files, err := l.Files("u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Date != "2026-09-28" || files[0].Entries != 1 {
		t.Fatalf("files = %+v", files)
	}
}

func TestSearchSkipsFilesOutsideRangeAndMatchesDetail(t *testing.T) {
	l, clock, _ := newTestLogger(t, 7)
	l.Record(Entry{User: "u1", Action: ActionDiaryCreate, Target: "a", Detail: map[string]any{"note": "Needle"}})
	clock.Advance(48 * time.Hour)
	l.Record(Entry{User: "u1", Action: ActionDiaryCreate, Target: "b"})

	result, err := l.Search("u1", Query{Start: clock.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Scanned != 1 || len(result.Entries) != 1 || result.Entries[0].Target != "b" {
		t.Fatalf("range search = %+v", result)
	}
	if got := search(t, l, "u1", Query{Text: "needle"}); len(got) != 1 || got[0].Target != "a" {
		t.Fatalf("detail search = %+v", got)
	}
	if got := search(t, l, "u1", Query{Text: "nowhere"}); len(got) != 0 {
		t.Fatalf("unmatched search = %+v", got)
	}
}

func TestCoalescedUpdateWithoutDetail(t *testing.T) {
	l, clock, _ := newTestLogger(t, 7)
	l.Record(Entry{User: "u1", Action: ActionDiaryUpdate, Target: "d"})
	for range 2 {
		clock.Advance(time.Second)
		l.Record(Entry{User: "u1", Action: ActionDiaryUpdate, Target: "d"})
	}
	l.Close()
	entries := l.readFile("u1", "2026-09-28")
	if len(entries) != 2 || entries[1].Detail["saves"].(float64) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestCleanupReportsRemoveFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	l, clock, dir := newTestLogger(t, 1)
	l.Record(Entry{User: "u1", Action: ActionDiaryCreate})
	l.Flush("u1")
	clock.Advance(72 * time.Hour)
	userDir := filepath.Join(Dir(dir), "u1")
	if err := os.Chmod(userDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(userDir, 0o700) })
	if removed := l.Cleanup("u1"); removed != 0 {
		t.Fatalf("read-only dir should not allow removal, removed %d", removed)
	}
}

func TestRecordDropsWhenQueueIsFull(t *testing.T) {
	// No writer goroutine drains this unbuffered queue.
	l := &Logger{ops: make(chan op), now: time.Now, stop: make(chan struct{}), done: make(chan struct{})}
	l.Record(Entry{User: "u1", Action: ActionDiaryCreate})
	if l.dropped.Load() != 1 {
		t.Fatalf("dropped = %d", l.dropped.Load())
	}
	// Close reports what was dropped during the run.
	close(l.done)
	l.Close()
}
