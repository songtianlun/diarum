package visits

import (
	"errors"
	"os"
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

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

type fixture struct {
	t        *Tracker
	dir      string
	clock    *clock
	bucket   *backup.MemoryStore
	settings *memSettings
	shared   backup.S3Config
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{dir: t.TempDir(), clock: &clock{now: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}, bucket: backup.NewMemoryStore(), settings: &memSettings{}}
	tracker, err := New(f.dir, Options{
		Settings: f.settings,
		Location: time.UTC,
		Now:      f.clock.Now,
		SharedS3: func() backup.S3Config { return f.shared },
		NewObjectStore: func(cfg backup.S3Config) (backup.ObjectStore, error) {
			if cfg.Bucket == "fail" {
				return nil, errors.New("connect failed")
			}
			return f.bucket, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.t = tracker
	t.Cleanup(tracker.Close)
	return f
}

func (f *fixture) enable(t *testing.T, mutate func(*Settings)) Settings {
	t.Helper()
	s := DefaultSettings()
	s.Enabled = true
	if mutate != nil {
		mutate(&s)
	}
	got, err := f.t.UpdateSettings(s)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func view(owner, date, visitor, device string) Visit {
	return Visit{OwnerID: owner, DiaryID: "d-" + date, DiaryDate: date, VisitorID: visitor, Kind: KindView, Source: SourceWeb, Status: 200, IP: "10.0.0.1", UA: "Mozilla/5.0", Device: device}
}

func count(t *testing.T, tr *Tracker) int64 {
	t.Helper()
	tr.Flush()
	var n int64
	if err := tr.db.QueryRow(`SELECT COUNT(*) FROM visits`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestNilTrackerIsSafe(t *testing.T) {
	var tr *Tracker
	if tr.Record(Visit{}) || tr.Enabled() || tr.Path() != "" {
		t.Fatal("nil tracker recorded")
	}
	tr.Flush()
	tr.Close()
	tr.StartScheduler()
	if tr.Settings().RetentionDays != DefaultRetentionDays || tr.WriterStats() != (WriterStats{}) || tr.State().Running {
		t.Fatal("nil tracker defaults")
	}
	if _, err := tr.UpdateSettings(DefaultSettings()); err == nil {
		t.Fatal("expected error")
	}
	if tr.Cleanup("x") == nil {
		t.Fatal("nil report")
	}
	if _, err := tr.ArchiveNow("x"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := tr.ListArchives(nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := tr.Pull(nil, nil, "", ""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := tr.Unload(""); err == nil {
		t.Fatal("expected error")
	}
	if err := tr.TestArchive(nil, "", backup.S3Config{}, ""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := tr.Summarize(Filter{}, 1, 0); err == nil {
		t.Fatal("expected error")
	}
	if _, _, err := tr.Diaries(Filter{}, "", 1, 0); err == nil {
		t.Fatal("expected error")
	}
	if _, _, err := tr.Visitors(Filter{}, 1, 0); err == nil {
		t.Fatal("expected error")
	}
	if _, _, err := tr.Logs(Filter{}, 1, 0, true); err == nil {
		t.Fatal("expected error")
	}
	if _, _, err := tr.Owners("", 1, 0); err == nil {
		t.Fatal("expected error")
	}
	if _, err := tr.Storage(); err == nil {
		t.Fatal("expected error")
	}
	if tr.SharedS3Available() || tr.SharedS3Bucket() != "" {
		t.Fatal("nil shared")
	}
}

func TestRecordDisabledByDefault(t *testing.T) {
	f := newFixture(t)
	if f.t.Enabled() || f.t.Record(view("u1", "2026-09-28", "u1", "")) {
		t.Fatal("recorded while disabled")
	}
	if count(t, f.t) != 0 {
		t.Fatal("rows written while disabled")
	}
}

func TestRecordDedupesWithinWindow(t *testing.T) {
	f := newFixture(t)
	f.enable(t, nil)
	v := view("u1", "2026-09-28", "u1", "d:phone")
	if !f.t.Record(v) {
		t.Fatal("first read not recorded")
	}
	f.clock.Add(30 * time.Second)
	if f.t.Record(v) {
		t.Fatal("repeat within a minute recorded")
	}
	other := v
	other.Device = "d:laptop"
	if !f.t.Record(other) {
		t.Fatal("another device was deduped")
	}
	failed := v
	failed.Status = 403
	if !f.t.Record(failed) {
		t.Fatal("a failure was deduped against a success")
	}
	f.clock.Add(31 * time.Second)
	if !f.t.Record(v) {
		t.Fatal("read after the window was deduped")
	}
	if n := count(t, f.t); n != 4 {
		t.Fatalf("rows = %d, want 4", n)
	}
	if f.t.WriterStats().Deduped != 1 || f.t.WriterStats().Written != 4 {
		t.Fatalf("stats = %+v", f.t.WriterStats())
	}
}

func TestDedupeOff(t *testing.T) {
	f := newFixture(t)
	f.enable(t, func(s *Settings) { s.DedupeSeconds = 0 })
	v := view("u1", "2026-09-28", "u1", "d:a")
	f.t.Record(v)
	f.t.Record(v)
	if n := count(t, f.t); n != 2 {
		t.Fatalf("rows = %d", n)
	}
}

func TestRateLimits(t *testing.T) {
	f := newFixture(t)
	f.enable(t, func(s *Settings) {
		s.DedupeSeconds = 0
		s.IPLimitPerMinute = 3
		s.GlobalFailedPerMinute = 5
		s.OwnerLimitPerMinute = 2
	})
	anon := Visit{DiaryDate: "2026-09-28", Status: 401, IP: "1.1.1.1"}
	for i := 0; i < 5; i++ {
		f.t.Record(anon)
	}
	other := anon
	other.IP = "2.2.2.2"
	for i := 0; i < 5; i++ {
		f.t.Record(other)
	}
	// 3 from the first IP, then 2 more until the global limit of 5.
	if n := count(t, f.t); n != 5 {
		t.Fatalf("failed rows = %d, want 5", n)
	}
	own := view("u1", "2026-09-28", "u1", "d:a")
	for i := 0; i < 4; i++ {
		f.t.Record(own)
	}
	if n := count(t, f.t); n != 7 {
		t.Fatalf("rows = %d, want 7", n)
	}
	f.clock.Add(time.Minute)
	if !f.t.Record(own) {
		t.Fatal("limit did not reset after a minute")
	}
	f.t.limiter.sweep(f.clock.Now().Add(2 * time.Hour))
	if len(f.t.limiter.perIP) != 0 || len(f.t.limiter.perOwner) != 0 || len(f.t.limiter.seen) != 0 {
		t.Fatal("sweep left state behind")
	}
	storage, err := f.t.Storage()
	if err != nil {
		t.Fatal(err)
	}
	if storage.Suppressed != 7 || f.t.WriterStats().Suppressed != 7 {
		t.Fatalf("suppressed = %d / %d", storage.Suppressed, f.t.WriterStats().Suppressed)
	}
}

func TestDedupeMapIsBounded(t *testing.T) {
	l := newLimiter()
	s := DefaultSettings()
	s.OwnerLimitPerMinute = MaxRateLimit
	now := time.Now()
	for i := 0; i < dedupeMaxKeys+10; i++ {
		v := view("u1", "x", "u1", "d:"+strings.Repeat("a", i%7)+string(rune('a'+i%26))+time.Duration(i).String())
		l.allow(&v, now, time.Minute, s)
	}
	if len(l.seen) > dedupeMaxKeys {
		t.Fatalf("seen = %d", len(l.seen))
	}
}

func TestNormalize(t *testing.T) {
	v := Visit{
		Time:      time.Date(2026, 1, 2, 3, 4, 5, 999999, time.FixedZone("x", 3600)),
		OwnerID:   " u1\n",
		VisitorID: "u1",
		DiaryDate: "2026-01-02 00:00:00.000Z",
		Source:    "BOGUS",
		Kind:      "Range",
		Status:    404,
		UA:        strings.Repeat("é", 300),
		IP:        "1.2.3.4",
	}
	normalize(&v)
	if v.OwnerID != "u1" || !v.Self || v.DiaryDate != "2026-01-02" || v.Source != SourceWeb || v.Kind != KindRange {
		t.Fatalf("normalized = %+v", v)
	}
	if v.Success || v.Reason != ReasonNotFound || !strings.HasPrefix(v.Device, "h:") {
		t.Fatalf("result fields = %+v", v)
	}
	if len(v.UA) > maxUALength || !strings.HasSuffix(v.UA, "é") {
		t.Fatalf("UA clipped badly: %d", len(v.UA))
	}
	if v.Time.Location() != time.UTC || v.Time.Nanosecond()%int(time.Millisecond) != 0 {
		t.Fatal("time not normalized")
	}
	ok := Visit{Status: 200, Reason: "x"}
	normalize(&ok)
	if !ok.Success || ok.Reason != "" {
		t.Fatal("success keeps reason")
	}
	bad := Visit{Status: 5000}
	normalize(&bad)
	if bad.Status != 0 || bad.Reason != ReasonOther {
		t.Fatalf("bad status = %+v", bad)
	}
	for status, reason := range map[int]string{401: ReasonUnauthorized, 403: ReasonForbidden, 500: ReasonError, 400: ReasonBadRequest, 0: ReasonOther} {
		if got := ReasonForStatus(status); got != reason {
			t.Fatalf("%d -> %s", status, got)
		}
	}
}

func TestCleanDeviceID(t *testing.T) {
	if got := CleanDeviceID(" abc-DEF_123 "); got != "d:abc-DEF_123" {
		t.Fatal(got)
	}
	for _, bad := range []string{"", "a b", "x;y", strings.Repeat("a", 41), "日记"} {
		if CleanDeviceID(bad) != "" {
			t.Fatalf("accepted %q", bad)
		}
	}
	if clip("abc", 5) != "abc" || clip("日记", 4) != "日" {
		t.Fatal("clip")
	}
	if cleanToken("a\x00b") != "ab" || validDay("2026-13-01") || !validDay("2026-12-01") {
		t.Fatal("helpers")
	}
	if len(newID()) != 24 {
		t.Fatal("id length")
	}
}

func TestRecordAfterClose(t *testing.T) {
	f := newFixture(t)
	f.enable(t, nil)
	f.t.Close()
	if f.t.Record(view("u1", "2026-09-28", "u1", "")) {
		t.Fatal("recorded after close")
	}
	f.t.Flush()
	if f.t.WriterStats().Dropped != 1 {
		t.Fatal("drop not counted")
	}
}

func TestSettingsPersistAndValidate(t *testing.T) {
	f := newFixture(t)
	s := f.enable(t, func(s *Settings) {
		s.RetentionDays = 0
		s.Archive.Prefix = "/a//b/"
		s.Archive.Source = ""
	})
	if s.RetentionDays != 0 || s.Archive.Prefix != "a/b" || s.Archive.Source != S3SourceShared {
		t.Fatalf("settings = %+v", s)
	}
	reopened, err := New(t.TempDir(), Options{Settings: f.settings})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if !reopened.Enabled() || reopened.Settings().Archive.Prefix != "a/b" {
		t.Fatal("settings not loaded")
	}

	bad := []func(*Settings){
		func(s *Settings) { s.RetentionDays = -1 },
		func(s *Settings) { s.Archive.RetentionDays = MaxArchiveRetentionDays + 1 },
		func(s *Settings) { s.DedupeSeconds = MaxDedupeSeconds + 1 },
		func(s *Settings) { s.MaxRecords = 1 },
		func(s *Settings) { s.IPLimitPerMinute = 0 },
		func(s *Settings) { s.Archive.Source = "ftp" },
		func(s *Settings) { s.Archive.Enabled, s.Archive.Source = true, S3SourceCustom },
	}
	for i, mutate := range bad {
		next := DefaultSettings()
		mutate(&next)
		if _, err := f.t.UpdateSettings(next); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}

	// The custom secret is kept when the client sends none.
	custom := DefaultSettings()
	custom.Archive = ArchiveSettings{Enabled: true, Source: S3SourceCustom, S3: backup.S3Config{Bucket: "b", Region: "r", AccessKey: "a", Secret: "s"}, RetentionDays: 10}
	if _, err := f.t.UpdateSettings(custom); err != nil {
		t.Fatal(err)
	}
	custom.Archive.S3.Secret = ""
	got, err := f.t.UpdateSettings(custom)
	if err != nil || got.Archive.S3.Secret != "s" {
		t.Fatalf("secret lost: %v %+v", err, got.Archive.S3)
	}

	f.settings.setErr = errors.New("disk full")
	if _, err := f.t.UpdateSettings(DefaultSettings()); err == nil {
		t.Fatal("store error ignored")
	}
}

func TestLoadSettingsFallbacks(t *testing.T) {
	store := &memSettings{values: map[string]string{SettingsKey: `{"enabled":true,"dedupe_seconds":-5,"max_records":1,"ip_limit_per_minute":0,"global_failed_per_minute":-1,"owner_limit_per_minute":0,"retention_days":-3,"archive":{"retention_days":-1,"source":"x","prefix":""}}`}}
	tr, err := New(t.TempDir(), Options{Settings: store})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	got := tr.Settings()
	want := DefaultSettings()
	want.Enabled = true
	if got.DedupeSeconds != want.DedupeSeconds || got.MaxRecords != want.MaxRecords || got.IPLimitPerMinute != want.IPLimitPerMinute ||
		got.GlobalFailedPerMinute != want.GlobalFailedPerMinute || got.OwnerLimitPerMinute != want.OwnerLimitPerMinute ||
		got.RetentionDays != want.RetentionDays || got.Archive.RetentionDays != want.Archive.RetentionDays ||
		got.Archive.Source != S3SourceShared || got.Archive.Prefix != DefaultArchivePrefix || !got.Enabled {
		t.Fatalf("settings = %+v", got)
	}

	for _, s := range []*memSettings{{values: map[string]string{SettingsKey: "{"}}, {getErr: errors.New("boom")}} {
		tr, err := New(t.TempDir(), Options{Settings: s})
		if err != nil {
			t.Fatal(err)
		}
		if tr.Enabled() {
			t.Fatal("broken settings enabled tracking")
		}
		tr.Close()
	}
}

func TestNewFailsOnBadDir(t *testing.T) {
	file := t.TempDir() + "/file"
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(file, Options{}); err == nil {
		t.Fatal("expected error")
	}
}
