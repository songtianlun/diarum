package audit

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func at(day, hour, minute int) time.Time {
	return time.Date(2026, 9, day, hour, minute, 0, 0, time.UTC)
}

// seed writes a small but varied trail across three days.
func seed(t *testing.T, l *Logger) {
	t.Helper()
	entries := []Entry{
		{Time: at(26, 9, 0), UserID: "u1", User: "alice", Source: SourceWeb, Action: ActionAuthLogin, Method: "POST", Route: "/api/v1/auth/login", Status: 200, Duration: 120, IP: "10.0.0.1"},
		{Time: at(26, 9, 5), UserID: "u1", User: "alice", Source: SourceWeb, Action: ActionDiaryUpdate, Target: "2026-09-26", Method: "PUT", Route: "/api/v1/diaries/:id", Status: 200, Duration: 8, IP: "10.0.0.1"},
		{Time: at(27, 3, 0), Source: SourceWeb, Action: ActionAuthLoginFail, Method: "POST", Route: "/api/v1/auth/login", Status: 401, Duration: 90, IP: "6.6.6.6", Detail: map[string]any{"identity": "root"}},
		{Time: at(27, 3, 1), Source: SourceWeb, Action: ActionAuthLoginFail, Method: "POST", Route: "/api/v1/auth/login", Status: 401, Duration: 95, IP: "6.6.6.6", Detail: map[string]any{"identity": "root"}},
		{Time: at(27, 3, 2), UserID: "u2", Source: SourceWeb, Action: ActionAuthLoginFail, Method: "POST", Route: "/api/v1/auth/login", Status: 401, Duration: 97, IP: "6.6.6.6", User: "bob"},
		{Time: at(27, 3, 3), Source: SourceWeb, Action: ActionAuthDenied, Method: "GET", Route: "/api/v1/admin/overview", Status: 401, IP: "6.6.6.6"},
		{Time: at(28, 10, 0), UserID: "u2", User: "bob", Source: SourceMCP, Action: ActionDiarySearch, Method: "POST", Route: "/api/v1/mcp", Status: 200, Duration: 30, IP: "10.0.0.2", Detail: map[string]any{"query": "Holiday"}},
		{Time: at(28, 10, 1), UserID: "u2", Source: SourceWeb, Method: "GET", Route: "/api/v1/media", Status: 500, Duration: 400, IP: "10.0.0.2", Error: "boom"},
		{Time: at(28, 10, 2), UserID: "u1", User: "alice", Source: SourceWeb, Action: ActionDiaryDelete, Target: "2026-09-20", Method: "DELETE", Route: "/api/v1/diaries/:id", Status: 204, Duration: 5, IP: "10.0.0.1"},
		{Time: at(28, 10, 3), UserID: "u1", User: "alice", Source: SourceWeb, Method: "GET", Route: "/api/v1/settings", Status: 304, Duration: 1, IP: "10.0.0.1"},
	}
	for _, e := range entries {
		l.Record(e)
	}
	l.Flush()
}

func TestSearchFilters(t *testing.T) {
	l, _, _ := newTestLogger(t, Options{})
	seed(t, l)

	cases := []struct {
		name string
		q    Query
		want int
	}{
		{"all", Query{}, 10},
		{"user", Query{UserID: "u1"}, 4},
		{"anonymous", Query{UserID: "-"}, 3},
		{"ip", Query{IP: "6.6.6.6"}, 4},
		{"method", Query{Method: "post"}, 5},
		{"source", Query{Source: SourceMCP}, 1},
		{"2xx", Query{Status: "2xx"}, 4},
		{"3xx", Query{Status: "3xx"}, 1},
		{"4xx", Query{Status: "4xx"}, 4},
		{"5xx", Query{Status: "5xx"}, 1},
		{"errors", Query{Status: "error"}, 5},
		{"action family", Query{Actions: []string{"auth"}}, 5},
		{"exact action", Query{Actions: []string{ActionAuthLogin}}, 1},
		{"no action", Query{Actions: []string{"-", " "}}, 2},
		{"several", Query{Actions: []string{"diary", ActionAuthDenied}}, 4},
		{"text raw", Query{Text: "holiday"}, 1},
		{"text label zh", Query{Text: "删除"}, 1},
		{"text label en", Query{Text: "sign in"}, 4}, // also "failed sign in"
		{"text exact case", Query{Text: "alice"}, 4},
		{"text none", Query{Text: "zzz-nothing"}, 0},
		{"range", Query{Start: at(27, 0, 0), End: at(27, 23, 59)}, 4},
		{"start only", Query{Start: at(28, 10, 2)}, 2},
		{"end only", Query{End: at(26, 23, 0)}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := l.Search(tc.q)
			if err != nil {
				t.Fatal(err)
			}
			if result.Total != tc.want || len(result.Entries) != tc.want {
				t.Fatalf("total=%d entries=%d, want %d", result.Total, len(result.Entries), tc.want)
			}
		})
	}
}

func TestSearchPagingAndOrder(t *testing.T) {
	l, _, _ := newTestLogger(t, Options{})
	seed(t, l)
	page, err := l.Search(Query{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 10 || len(page.Entries) != 3 || !page.Entries[0].Time.Equal(at(28, 10, 3)) || page.Files != 3 || page.Bytes == 0 {
		t.Fatalf("first page = %+v", page)
	}
	next, _ := l.Search(Query{Limit: 3, Offset: 3})
	if len(next.Entries) != 3 || !next.Entries[0].Time.Equal(at(28, 10, 0)) {
		t.Fatalf("second page starts at %v", next.Entries[0].Time)
	}
	beyond, _ := l.Search(Query{Limit: 3, Offset: 50})
	if len(beyond.Entries) != 0 || beyond.Total != 10 {
		t.Fatalf("beyond = %+v", beyond)
	}
	statsOnly, _ := l.Search(Query{Limit: -1, Stats: true})
	if len(statsOnly.Entries) != 0 || statsOnly.Stats == nil || statsOnly.Stats.Total != 10 {
		t.Fatalf("stats only = %+v", statsOnly)
	}
	huge, _ := l.Search(Query{Limit: 1 << 20, Offset: -5})
	if len(huge.Entries) != 10 {
		t.Fatalf("clamped limit returned %d", len(huge.Entries))
	}
}

func TestSearchStats(t *testing.T) {
	l, _, _ := newTestLogger(t, Options{})
	seed(t, l)
	result, err := l.Search(Query{Stats: true, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	s := result.Stats
	if s.Total != 10 || s.Users != 2 || s.IPs != 3 || s.Anonymous != 3 {
		t.Fatalf("headline = %+v", s)
	}
	if s.ClientErrors != 4 || s.ServerErrors != 1 || s.Denied != 4 || s.LoginOK != 1 || s.LoginFailed != 3 || s.Writes != 7 {
		t.Fatalf("counters = %+v", s)
	}
	if s.MaxMS != 400 || s.P95MS != 400 || s.AvgMS <= 0 {
		t.Fatalf("latency = avg %v p95 %v max %v", s.AvgMS, s.P95MS, s.MaxMS)
	}
	if s.First != at(26, 9, 0).Format(time.RFC3339) || s.Last != at(28, 10, 3).Format(time.RFC3339) {
		t.Fatalf("span = %s .. %s", s.First, s.Last)
	}
	// Ties are broken by address.
	if s.TopIPs[0].IP != "10.0.0.1" || s.TopIPs[1].IP != "6.6.6.6" {
		t.Fatalf("top ips = %+v", s.TopIPs)
	}
	if ip := s.TopIPs[1]; ip.Failed != 3 || ip.Errors != 4 || ip.Users != 1 || ip.Count != 4 {
		t.Fatalf("attacker ip = %+v", ip)
	}
	if s.FailedLogins[0].Key != "root" || s.FailedLogins[0].Count != 2 || s.FailedLogins[1].Key != "bob" {
		t.Fatalf("failed logins = %+v", s.FailedLogins)
	}
	if s.TopUsers[0].Key != "u1" || s.TopUsers[0].Label != "alice" || s.TopUsers[1].Label != "bob" {
		t.Fatalf("top users = %+v", s.TopUsers)
	}
	if s.Routes[0].Route != "/api/v1/auth/login" || s.Routes[0].Count != 4 || s.Routes[0].Errors != 3 {
		t.Fatalf("routes = %+v", s.Routes)
	}
	if len(s.Actions) == 0 || len(s.Status) != 4 || len(s.Methods) != 4 || len(s.Sources) != 2 {
		t.Fatalf("breakdowns = %+v %+v %+v %+v", s.Actions, s.Status, s.Methods, s.Sources)
	}
	// Three days read, no explicit range: hourly buckets across 72 hours.
	if s.Bucket != "hour" || len(s.Timeline) != 72 {
		t.Fatalf("timeline = %s x%d", s.Bucket, len(s.Timeline))
	}
	total, errs := 0, 0
	for _, b := range s.Timeline {
		total += b.Count
		errs += b.Errors
	}
	if total != 10 || errs != 5 {
		t.Fatalf("timeline sums = %d/%d", total, errs)
	}

	// A longer explicit range switches to days, in the requested zone.
	shanghai, _ := time.LoadLocation("Asia/Shanghai")
	daily, _ := l.Search(Query{Stats: true, Start: at(20, 0, 0), End: at(28, 23, 0), Location: shanghai})
	if daily.Stats.Bucket != "day" || len(daily.Stats.Timeline) != 10 {
		t.Fatalf("daily timeline = %s x%d", daily.Stats.Bucket, len(daily.Stats.Timeline))
	}

	empty, _ := l.Search(Query{Stats: true, Text: "nothing-matches"})
	if empty.Stats.Total != 0 || empty.Stats.First != "" {
		t.Fatalf("empty stats = %+v", empty.Stats)
	}
}

func TestEmptyTimeline(t *testing.T) {
	a := newAggregator(Query{Location: time.UTC}, nil, time.UTC)
	if got := a.finish().Timeline; len(got) != 0 {
		t.Fatalf("timeline = %v", got)
	}
	if statusClass(0) != "-" || statusClass(404) != "4xx" {
		t.Fatal("status class")
	}
}

func TestFilesPulledAndDownload(t *testing.T) {
	l, dir, _ := newTestLogger(t, Options{})
	seed(t, l)
	pulledDir := filepath.Join(Dir(dir), PulledDirName)
	line := `{"time":"2026-09-01T08:00:00Z","user_id":"u9","action":"diary.view","status":200}` + "\n"
	if err := os.WriteFile(filepath.Join(pulledDir, "2026-09-01.log"), []byte(line+"not json\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Junk that must be ignored by listings.
	os.WriteFile(filepath.Join(Dir(dir), "notes.txt"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(Dir(dir), "bad-date.log"), []byte("x"), 0o600)
	os.Mkdir(filepath.Join(Dir(dir), "2026-01-01.log"), 0o700)

	files, err := l.Files(false)
	if err != nil || len(files) != 3 || files[0].Date != "2026-09-28" {
		t.Fatalf("files = %+v (%v)", files, err)
	}
	withPulled, _ := l.Files(true)
	if len(withPulled) != 4 || !withPulled[3].Pulled || withPulled[3].PulledAt == nil {
		t.Fatalf("with pulled = %+v", withPulled)
	}

	without, _ := l.Search(Query{UserID: "u9"})
	with, _ := l.Search(Query{UserID: "u9", IncludePulled: true})
	if without.Total != 0 || with.Total != 1 {
		t.Fatalf("pulled search: without=%d with=%d", without.Total, with.Total)
	}

	f, err := l.OpenFile("2026-09-01", true)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(f)
	f.Close()
	if !strings.HasPrefix(string(raw), line) {
		t.Fatalf("download = %q", raw)
	}
	if _, err := l.OpenFile("../etc", false); !errors.Is(err, ErrInvalidDate) {
		t.Fatalf("invalid date err = %v", err)
	}
	if _, err := l.OpenFile("2020-01-01", false); !os.IsNotExist(err) {
		t.Fatalf("missing err = %v", err)
	}
}

func TestScanFileSkipsOversizedAndBrokenLines(t *testing.T) {
	l, dir, _ := newTestLogger(t, Options{})
	path := filepath.Join(Dir(dir), "2026-09-28.log")
	good := `{"time":"2026-09-28T01:00:00Z","action":"diary.view"}`
	content := good + "\n" + `{"time":"` + strings.Repeat("x", 300<<10) + "\n" + "{broken\n" + `{"action":"no-time"}` + "\n" + good
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := l.Search(Query{})
	if err != nil || result.Total != 2 {
		t.Fatalf("total = %d (%v)", result.Total, err)
	}
	if err := l.scanFile(filepath.Join(Dir(dir), "missing.log"), newMatcher(Query{}), func(*Entry) {}); !os.IsNotExist(err) {
		t.Fatalf("missing file err = %v", err)
	}
}

func TestFilesReportsUnreadableDirectory(t *testing.T) {
	l, dir, _ := newTestLogger(t, Options{})
	pulled := filepath.Join(Dir(dir), PulledDirName)
	os.RemoveAll(pulled)
	if err := os.WriteFile(pulled, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Files(true); err == nil {
		t.Fatal("expected error for unreadable pulled dir")
	}
	if _, err := l.Search(Query{IncludePulled: true}); err == nil {
		t.Fatal("search should report the error")
	}
	os.Remove(pulled)
	if files, err := l.Files(true); err != nil || len(files) != 0 {
		t.Fatalf("missing pulled dir should be empty: %v %v", files, err)
	}
	root := Dir(dir)
	os.RemoveAll(root)
	os.WriteFile(root, []byte("x"), 0o600)
	if _, err := l.Files(false); err == nil {
		t.Fatal("expected error for unreadable root")
	}
}

func TestRing(t *testing.T) {
	r := newRing(3)
	for i := 1; i <= 5; i++ {
		r.push(Entry{Time: at(1, i, 0)})
	}
	got := r.newestFirst()
	if len(got) != 3 || got[0].Time.Hour() != 5 || got[2].Time.Hour() != 3 {
		t.Fatalf("ring = %v", got)
	}
	zero := newRing(0)
	zero.push(Entry{})
	if len(zero.newestFirst()) != 0 {
		t.Fatal("zero ring kept entries")
	}
}
