package visits

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/songtianlun/diarum/internal/backup"
)

func sharedS3() backup.S3Config {
	return backup.S3Config{Bucket: "b", Region: "r", AccessKey: "ak", Secret: "sk", Prefix: "diarum-audit"}
}

// recordOn records one visit per given day (at noon UTC) for owner u1.
func recordOn(t *testing.T, f *fixture, days ...string) {
	t.Helper()
	for _, day := range days {
		at, err := time.Parse(dayLayout, day)
		if err != nil {
			t.Fatal(err)
		}
		v := view("u1", day, "u1", "d:"+day)
		v.Time = at.Add(12 * time.Hour)
		if !f.t.Record(v) {
			t.Fatalf("not recorded on %s", day)
		}
	}
	f.t.Flush()
}

func days(t *testing.T, f *fixture, pulled int) []string {
	t.Helper()
	out, err := f.t.queryDays(`SELECT DISTINCT day FROM visits WHERE pulled = ? ORDER BY day`, pulled)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestArchiveCleanupAndPull(t *testing.T) {
	f := newFixture(t)
	f.shared = sharedS3()
	f.enable(t, func(s *Settings) {
		s.DedupeSeconds = 0
		s.RetentionDays = 5
		s.Archive.Enabled = true
		s.Archive.RetentionDays = 30
	})
	if !f.t.SharedS3Available() || f.t.SharedS3Bucket() != "b" {
		t.Fatal("shared S3 not seen")
	}
	// Today is 2026-09-28; retention keeps 2026-09-24 onwards.
	recordOn(t, f, "2026-08-01", "2026-09-20", "2026-09-23", "2026-09-27", "2026-09-28")
	// An archive older than the archive retention is removed by cleanup.
	_ = f.bucket.Put(context.Background(), "diarum-visits/2026/2026-01-01.jsonl.gz", strings.NewReader("x"), "")
	_ = f.bucket.Put(context.Background(), "diarum-visits/notes.txt", strings.NewReader("x"), "")

	report := f.t.Cleanup("test")
	if report.Error != "" {
		t.Fatal(report.Error)
	}
	if strings.Join(report.Archived, ",") != "2026-08-01,2026-09-20,2026-09-23,2026-09-27" {
		t.Fatalf("archived = %v", report.Archived)
	}
	if strings.Join(report.RemovedRemote, ",") != "2026-01-01" {
		t.Fatalf("removed remote = %v", report.RemovedRemote)
	}
	if report.Deleted != 3 {
		t.Fatalf("deleted = %d", report.Deleted)
	}
	if got := strings.Join(days(t, f, 0), ","); got != "2026-09-27,2026-09-28" {
		t.Fatalf("local days = %s", got)
	}
	state := f.t.State()
	if state.LastCleanup == nil || state.LastArchive == nil {
		t.Fatal("state not kept")
	}

	// The archive holds a header and the record.
	raw, ok := f.bucket.Object("diarum-visits/2026/2026-09-20.jsonl.gz")
	if !ok {
		t.Fatalf("keys = %v", f.bucket.Keys())
	}
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	var plain bytes.Buffer
	_, _ = plain.ReadFrom(gz)
	if lines := strings.Split(strings.TrimSpace(plain.String()), "\n"); len(lines) != 2 || !strings.Contains(lines[0], archiveFormat) || !strings.Contains(lines[1], `"diary_date":"2026-09-20"`) {
		t.Fatalf("archive = %q", plain.String())
	}

	archives, err := f.t.ListArchives(context.Background())
	if err != nil || len(archives) != 4 || archives[0].Day != "2026-09-27" || !archives[0].Local || archives[1].Local {
		t.Fatalf("archives = %v %+v", err, archives)
	}

	// Pull everything back: days still local are skipped.
	pull, err := f.t.Pull(context.Background(), nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(pull.Days, ",") != "2026-09-23,2026-09-20,2026-08-01" || strings.Join(pull.Skipped, ",") != "2026-09-27" || pull.Records != 3 {
		t.Fatalf("pull = %+v", pull)
	}
	if got := strings.Join(days(t, f, 1), ","); got != "2026-08-01,2026-09-20,2026-09-23" {
		t.Fatalf("pulled days = %s", got)
	}
	// Pulled records read like any other.
	rows, total, err := f.t.Logs(Filter{Owner: "u1", DiaryDate: "2026-09-20"}, 10, 0, true)
	if err != nil || total != 1 || !rows[0].Pulled || rows[0].Device != "d:2026-09-20" || !rows[0].Self {
		t.Fatalf("pulled rows = %v %+v", err, rows)
	}
	archives, _ = f.t.ListArchives(context.Background())
	if !archives[1].Pulled {
		t.Fatalf("pulled flag = %+v", archives)
	}
	// Pulling again changes nothing.
	again, err := f.t.Pull(context.Background(), []string{"2026-09-20"}, "", "")
	if err != nil || len(again.Days) != 0 || len(again.Skipped) != 1 {
		t.Fatalf("again = %v %+v", err, again)
	}
	if _, err := f.t.Pull(context.Background(), []string{"2025-01-01"}, "", ""); !errors.Is(err, ErrArchiveNotFound) {
		t.Fatalf("missing day = %v", err)
	}
	if _, err := f.t.Pull(context.Background(), []string{"bad"}, "", ""); !errors.Is(err, ErrInvalidDay) {
		t.Fatalf("bad day = %v", err)
	}

	// Cleanup keeps pulled records for PulledKeepDays, then unloads them.
	if report := f.t.Cleanup("test"); report.Unloaded != 0 || report.Deleted != 0 {
		t.Fatalf("early unload = %+v", report)
	}
	f.clock.Add(time.Duration(PulledKeepDays+1) * 24 * time.Hour)
	if report := f.t.Cleanup("test"); report.Unloaded != 3 {
		t.Fatalf("unload = %+v", report)
	}

	// Unload by hand.
	if _, err := f.t.Pull(context.Background(), nil, "2026-09-01", "2026-09-30"); err != nil {
		t.Fatal(err)
	}
	if n, err := f.t.Unload("2026-09-20"); err != nil || n != 1 {
		t.Fatalf("unload day = %v %d", err, n)
	}
	if n, err := f.t.Unload(""); err != nil || n < 1 {
		t.Fatalf("unload all = %v %d", err, n)
	}
	if _, err := f.t.Unload("x"); !errors.Is(err, ErrInvalidDay) {
		t.Fatal("bad day accepted")
	}
}

func TestRetentionKeepsUnarchivedDays(t *testing.T) {
	f := newFixture(t)
	f.shared = sharedS3()
	f.enable(t, func(s *Settings) {
		s.RetentionDays = 5
		s.Archive.Enabled = true
	})
	recordOn(t, f, "2026-09-20", "2026-08-01")
	f.shared.Bucket = "fail"
	report := f.t.Cleanup("test")
	if report.Error == "" || strings.Join(report.Kept, ",") != "2026-09-20" || report.Deleted != 1 {
		t.Fatalf("report = %+v", report)
	}
	if _, err := f.t.ArchiveNow("test"); err == nil {
		t.Fatal("archive error hidden")
	}
	if _, err := f.t.ListArchives(context.Background()); err == nil {
		t.Fatal("list error hidden")
	}
}

func TestRetentionForever(t *testing.T) {
	f := newFixture(t)
	f.enable(t, func(s *Settings) { s.RetentionDays = 0 })
	recordOn(t, f, "2000-01-01")
	if report := f.t.Cleanup("test"); report.Deleted != 0 || report.Error != "" {
		t.Fatalf("report = %+v", report)
	}
	if _, err := f.t.ArchiveNow("test"); !errors.Is(err, ErrArchiveDisabled) {
		t.Fatal("archive ran while disabled")
	}
	if _, err := f.t.Pull(context.Background(), nil, "", ""); !errors.Is(err, ErrArchiveDisabled) {
		t.Fatal("pull ran while disabled")
	}
}

func TestTrimToCap(t *testing.T) {
	f := newFixture(t)
	f.enable(t, func(s *Settings) { s.DedupeSeconds = 0; s.OwnerLimitPerMinute = MaxRateLimit })
	for i := 0; i < 120; i++ {
		v := view("u1", "2026-09-28", "u1", "")
		v.Time = f.clock.Now().Add(time.Duration(i) * time.Second)
		f.t.Record(v)
	}
	f.t.Flush()
	if _, err := f.t.db.Exec(`UPDATE visits SET pulled = 1 WHERE rowid <= 10`); err != nil {
		t.Fatal(err)
	}
	trimmed, err := f.t.trimToCap(100)
	if err != nil || trimmed != 25 {
		t.Fatalf("trimmed = %v %d", err, trimmed)
	}
	if n := count(t, f.t); n != 95 {
		t.Fatalf("rows = %d", n)
	}
	var pulled int
	_ = f.t.db.QueryRow(`SELECT COUNT(*) FROM visits WHERE pulled = 1`).Scan(&pulled)
	if pulled != 0 {
		t.Fatal("pulled records trimmed last")
	}
	if n, _ := f.t.trimToCap(1000); n != 0 {
		t.Fatal("trimmed under the cap")
	}
	if n, _ := f.t.trimToCap(0); n != 0 {
		t.Fatal("trimmed without a cap")
	}
}

func TestPullCapacityAndBadArchives(t *testing.T) {
	f := newFixture(t)
	f.shared = sharedS3()
	f.enable(t, func(s *Settings) {
		s.DedupeSeconds = 0
		s.Archive.Enabled = true
		s.MaxRecords = MinMaxRecords
		s.OwnerLimitPerMinute = MaxRateLimit
	})
	ctx := context.Background()
	put := func(day, body string) {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		_, _ = gz.Write([]byte(body))
		_ = gz.Close()
		_ = f.bucket.Put(ctx, "diarum-visits/"+day[:4]+"/"+day+archiveSuffix, &buf, "")
	}
	// Header, a good record without ID, a record without time, junk.
	put("2026-01-01", `{"_format":"diarum-visits","_version":1}`+"\n"+`{"time":"2026-01-01T10:00:00Z","owner_id":"u1","diary_date":"2026-01-01","status":200}`+"\n\n"+`{"owner_id":"u1"}`+"\n"+`not json`+"\n")
	_ = f.bucket.Put(ctx, "diarum-visits/2026/2026-01-02"+archiveSuffix, strings.NewReader("not gzip"), "")
	report, err := f.t.Pull(ctx, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Records != 1 || report.Invalid != 3 || len(report.Errors) != 1 || f.t.State().LastPull == nil {
		t.Fatalf("report = %+v", report)
	}
	rows, _, _ := f.t.Logs(Filter{Owner: "u1"}, 10, 0, false)
	if len(rows) != 1 || !strings.HasPrefix(rows[0].ID, "p") || !rows[0].Success {
		t.Fatalf("rows = %+v", rows)
	}
	// Re-pulling the same lines after an unload gives the same IDs.
	if _, err := f.t.Unload(""); err != nil {
		t.Fatal(err)
	}
	if again, _ := f.t.Pull(ctx, []string{"2026-01-01"}, "", ""); again.Records != 1 {
		t.Fatalf("again = %+v", again)
	}
	again, _, _ := f.t.Logs(Filter{Owner: "u1"}, 10, 0, false)
	if again[0].ID != rows[0].ID {
		t.Fatal("IDs differ between pulls")
	}

	// A full table refuses to pull.
	if _, err := f.t.Unload(""); err != nil {
		t.Fatal(err)
	}
	var big strings.Builder
	for i := 0; i < 10; i++ {
		big.WriteString(`{"id":"x` + string(rune('a'+i)) + `","time":"2026-01-03T10:00:00Z","owner_id":"u1","status":200}` + "\n")
	}
	put("2026-01-03", big.String())
	for i := 0; i < MinMaxRecords-5; i++ {
		f.t.Record(Visit{OwnerID: "u9", VisitorID: "u9", Status: 200, Time: f.clock.Now(), Device: "d:x"})
		if i%500 == 0 {
			f.t.Flush()
		}
	}
	f.t.Flush()
	report, err = f.t.Pull(ctx, []string{"2026-01-03"}, "", "")
	if err == nil && (len(report.Errors) == 0 || report.Records != 5) {
		t.Fatalf("capacity report = %v %+v", err, report)
	}
	if _, err := f.t.Pull(ctx, []string{"2026-01-01"}, "", ""); !errors.Is(err, ErrCapacity) {
		t.Fatalf("full table = %v", err)
	}
}

func TestPullBusy(t *testing.T) {
	f := newFixture(t)
	f.shared = sharedS3()
	f.enable(t, func(s *Settings) { s.Archive.Enabled = true })
	f.t.archiver.runMu.Lock()
	_, err := f.t.Pull(context.Background(), nil, "", "")
	f.t.archiver.runMu.Unlock()
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v", err)
	}
}

func TestTestArchive(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.t.TestArchive(ctx, S3SourceShared, backup.S3Config{}, ""); err == nil || !strings.Contains(err.Error(), "audit") {
		t.Fatalf("shared without S3 = %v", err)
	}
	if err := f.t.TestArchive(ctx, S3SourceCustom, backup.S3Config{Bucket: "b"}, ""); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("incomplete = %v", err)
	}
	if err := f.t.TestArchive(ctx, S3SourceCustom, backup.S3Config{Bucket: "fail", Region: "r", AccessKey: "a", Secret: "s"}, ""); err == nil {
		t.Fatal("connect error hidden")
	}
	f.shared = sharedS3()
	if err := f.t.TestArchive(ctx, "", backup.S3Config{}, "/x/"); err != nil {
		t.Fatal(err)
	}
	if len(f.bucket.Keys()) != 0 {
		t.Fatalf("probe left behind: %v", f.bucket.Keys())
	}
}

func TestSchedulerRunsCleanup(t *testing.T) {
	dir := t.TempDir()
	tr, err := New(dir, Options{Settings: &memSettings{}, SchedulerDelay: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	tr.StartScheduler()
	tr.StartScheduler()
	deadline := time.Now().Add(5 * time.Second)
	for tr.State().LastCleanup == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if tr.State().LastCleanup == nil || tr.State().NextCleanup.IsZero() {
		t.Fatal("scheduler did not run")
	}
	tr.Close()
	tr.Close()
	if next := nextCleanupAfter(time.Date(2026, 1, 1, 0, 30, 0, 0, time.UTC), time.UTC); next.Day() != 2 || next.Minute() != cleanupMinute {
		t.Fatalf("next = %v", next)
	}
}
