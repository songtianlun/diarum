package visits

import (
	"testing"
	"time"
)

// seed records a small history for owner u1:
//   - 2026-09-20: u1 reads it 3 times (phone, laptop, phone again later)
//   - 2026-09-21: u1 reads once; u2 is refused once; an anonymous visitor
//     is refused twice from two devices
//   - one unattributed anonymous attempt
func seed(t *testing.T, f *fixture) {
	t.Helper()
	f.enable(t, func(s *Settings) { s.DedupeSeconds = 0 })
	rec := func(v Visit) {
		v.Time = f.clock.Now()
		if !f.t.Record(v) {
			t.Fatalf("not recorded: %+v", v)
		}
		f.clock.Add(time.Minute)
	}
	rec(view("u1", "2026-09-20", "u1", "d:phone"))
	rec(view("u1", "2026-09-20", "u1", "d:laptop"))
	rec(view("u1", "2026-09-20", "u1", "d:phone"))
	rec(view("u1", "2026-09-21", "u1", "d:phone"))
	refused := view("u1", "2026-09-21", "u2", "d:other")
	refused.Status, refused.Visitor, refused.IP = 403, "bob", "9.9.9.9"
	rec(refused)
	anon := Visit{OwnerID: "u1", DiaryID: "d-2026-09-21", DiaryDate: "2026-09-21", Status: 401, IP: "8.8.8.8", UA: "curl/8"}
	rec(anon)
	anon.UA = "python"
	rec(anon)
	rec(Visit{DiaryDate: "2026-01-01", Status: 401, IP: "7.7.7.7", Source: SourceAPI})
	f.t.Flush()
}

func TestSummarize(t *testing.T) {
	f := newFixture(t)
	seed(t, f)
	s, err := f.t.Summarize(Filter{Owner: "u1"}, 7, 0)
	if err != nil {
		t.Fatal(err)
	}
	tot := s.Totals
	if tot.Views != 7 || tot.OK != 4 || tot.Failed != 3 || tot.Visitors != 4 || tot.Anonymous != 2 || tot.Others != 3 || tot.Diaries != 2 || tot.IPs != 3 {
		t.Fatalf("totals = %+v", tot)
	}
	if tot.First == nil || tot.Last == nil || !tot.Last.After(*tot.First) {
		t.Fatal("first/last missing")
	}
	if len(s.Timeline) != 7 || s.Timeline[6].Day != "2026-09-28" || s.Timeline[6].OK != 4 || s.Timeline[6].Failed != 3 {
		t.Fatalf("timeline = %+v", s.Timeline)
	}
	if len(s.TopDiaries) != 2 || s.TopDiaries[0].DiaryDate != "2026-09-21" || s.TopDiaries[0].Views != 4 {
		t.Fatalf("top diaries = %+v", s.TopDiaries)
	}
	if len(s.TopVisitors) != 4 || s.TopVisitors[0].VisitorID != "u1" || s.TopVisitors[0].Views != 4 || s.TopVisitors[0].Devices != 2 || !s.TopVisitors[0].Self {
		t.Fatalf("top visitors = %+v", s.TopVisitors)
	}
	if len(s.RecentFails) != 3 || s.RecentFails[0].Success {
		t.Fatalf("recent failures = %+v", s.RecentFails)
	}
	if len(s.Reasons) != 2 || len(s.Sources) != 1 {
		t.Fatalf("reasons = %+v sources = %+v", s.Reasons, s.Sources)
	}

	// The timeline follows the reader's offset, and long ranges skip it.
	shifted, err := f.t.Summarize(Filter{Owner: "u1"}, 0, 13*60)
	if err != nil || len(shifted.Timeline) != 30 || shifted.Timeline[29].Day != "2026-09-29" {
		t.Fatalf("shifted = %v %+v", err, shifted.Timeline[29:])
	}
	long, err := f.t.Summarize(Filter{Owner: "u1"}, 5000, 0)
	if err != nil || len(long.Timeline) != 0 || long.Totals.Views != 7 {
		t.Fatalf("long = %v %+v", err, long.Totals)
	}
	// Nothing is shared between owners.
	other, err := f.t.Summarize(Filter{Owner: "u2"}, 30, 0)
	if err != nil || other.Totals.Views != 0 || other.Totals.First != nil {
		t.Fatalf("u2 sees %+v", other.Totals)
	}
}

func TestFilters(t *testing.T) {
	f := newFixture(t)
	seed(t, f)
	cases := []struct {
		name   string
		filter Filter
		want   int64
	}{
		{"owner", Filter{Owner: "u1"}, 7},
		{"all", Filter{AllOwners: true}, 8},
		{"unattributed", Filter{Owner: ""}, 1},
		{"diary", Filter{Owner: "u1", DiaryDate: "2026-09-20"}, 3},
		{"diary id", Filter{Owner: "u1", DiaryID: "d-2026-09-21"}, 4},
		{"visitor", Filter{Owner: "u1", VisitorID: "u2"}, 1},
		{"anonymous", Filter{Owner: "u1", VisitorID: AnonymousVisitor}, 2},
		{"device", Filter{Owner: "u1", Device: "d:laptop"}, 1},
		{"ok", Filter{Owner: "u1", Result: "ok"}, 4},
		{"failed", Filter{Owner: "u1", Result: "failed"}, 3},
		{"source", Filter{AllOwners: true, Source: SourceAPI}, 1},
		{"others", Filter{Owner: "u1", Others: true}, 3},
		{"text ip", Filter{Owner: "u1", Text: "9.9.9"}, 1},
		{"text ua", Filter{Owner: "u1", Text: "curl"}, 1},
		{"text like escape", Filter{Owner: "u1", Text: "%"}, 0},
		{"start", Filter{Owner: "u1", Start: f.clock.Now().Add(-3 * time.Minute)}, 2},
		{"end", Filter{Owner: "u1", End: time.Date(2026, 9, 28, 12, 1, 0, 0, time.UTC)}, 2},
	}
	for _, c := range cases {
		rows, total, err := f.t.Logs(c.filter, 100, 0, true)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if total != c.want || int64(len(rows)) != c.want {
			t.Fatalf("%s: total %d rows %d, want %d", c.name, total, len(rows), c.want)
		}
	}
	rows, _, err := f.t.Logs(Filter{Owner: "u1"}, 2, 1, false)
	if err != nil || len(rows) != 2 || rows[0].Time.After(f.clock.Now()) {
		t.Fatalf("paging = %v %+v", err, rows)
	}
	if clampLimit(0) != defaultLimit || clampLimit(5000) != maxLimit {
		t.Fatal("clamp")
	}
}

func TestDiariesAndVisitors(t *testing.T) {
	f := newFixture(t)
	seed(t, f)
	for sort, first := range map[string]string{"views": "2026-09-21", "failed": "2026-09-21", "last": "2026-09-21", "visitors": "2026-09-21", "others": "2026-09-21", "date": "2026-09-21", "bogus": "2026-09-21"} {
		rows, total, err := f.t.Diaries(Filter{Owner: "u1"}, sort, 10, 0)
		if err != nil || total != 2 || rows[0].DiaryDate != first {
			t.Fatalf("sort %s: %v %d %+v", sort, err, total, rows)
		}
	}
	rows, _, _ := f.t.Diaries(Filter{Owner: "u1"}, "views", 10, 0)
	if rows[0].Views != 4 || rows[0].Failed != 3 || rows[0].Visitors != 4 || rows[0].Others != 3 || rows[0].DiaryID != "d-2026-09-21" {
		t.Fatalf("diary stat = %+v", rows[0])
	}
	visitors, total, err := f.t.Visitors(Filter{Owner: "u1", DiaryDate: "2026-09-21"}, 10, 0)
	if err != nil || total != 4 || len(visitors) != 4 {
		t.Fatalf("visitors = %v %d %+v", err, total, visitors)
	}
	var anon, bob *VisitorStat
	for i := range visitors {
		switch {
		case visitors[i].VisitorID == "u2":
			bob = &visitors[i]
		case visitors[i].VisitorID == "" && visitors[i].LastUA == "curl/8":
			anon = &visitors[i]
		}
	}
	if bob == nil || bob.Visitor != "bob" || bob.Failed != 1 || bob.LastIP != "9.9.9.9" || bob.Self {
		t.Fatalf("bob = %+v", bob)
	}
	if anon == nil || anon.Device == "" || anon.LastIP != "8.8.8.8" {
		t.Fatalf("anon = %+v", anon)
	}
}

func TestOwnersAndStorage(t *testing.T) {
	f := newFixture(t)
	seed(t, f)
	for _, sort := range []string{"views", "failed", "others", "last", ""} {
		owners, total, err := f.t.Owners(sort, 10, 0)
		if err != nil || total != 2 || len(owners) != 2 {
			t.Fatalf("owners %s = %v %d %+v", sort, err, total, owners)
		}
	}
	owners, _, _ := f.t.Owners("views", 10, 0)
	if owners[0].Owner != "u1" || owners[0].Views != 7 || owners[0].Failed != 3 || owners[1].Owner != "" {
		t.Fatalf("owners = %+v", owners)
	}
	storage, err := f.t.Storage()
	if err != nil || storage.Records != 8 || storage.Bytes == 0 || storage.Oldest == nil || storage.Newest == nil || storage.Path == "" {
		t.Fatalf("storage = %v %+v", err, storage)
	}
}
