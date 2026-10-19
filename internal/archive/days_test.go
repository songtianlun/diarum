package archive

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/songtianlun/diarum/internal/store"
)

func TestDaysRoundTrip(t *testing.T) {
	src := newStore(t)
	user := newUser(t, src)
	tag, err := src.SaveDayTag(&store.DayTag{Owner: user.ID, Name: "Love", Color: "#c08"})
	if err != nil {
		t.Fatal(err)
	}
	day, err := src.SaveDay(&store.Day{Owner: user.ID, Name: "Wedding", Date: "2020-05-20", Emoji: "💍", Note: "note", Repeat: store.DayRepeatYear, IncludeStart: true, ShowOnHome: true, Tags: []string{tag.ID}})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []store.DayMilestone{{Name: "Trip", Date: "2021-01-01", Note: "beach"}, {Name: "House", Date: "2022-02-02"}} {
		m.Day, m.Owner = day.ID, user.ID
		if _, err := src.SaveDayMilestone(&m); err != nil {
			t.Fatal(err)
		}
	}
	data, stats := exportAll(t, src, user.ID)
	if stats.Days.TotalInSystem != 1 || stats.Days.ActualExported != 1 {
		t.Fatalf("export stats: %+v", stats.Days)
	}

	// Leaving days out still counts what the account has.
	var buf bytes.Buffer
	opts := AllTime(time.Now())
	opts.IncludeDays = false
	skipped, err := Export(context.Background(), src, user.ID, opts, &buf)
	if err != nil || skipped.Days.TotalInSystem != 1 || skipped.Days.ActualExported != 0 {
		t.Fatalf("export without days: %+v %v", skipped.Days, err)
	}

	dst := newStore(t)
	other := newUser(t, dst)
	// A tag the account already has keeps its colour.
	if _, err := dst.SaveDayTag(&store.DayTag{Owner: other.ID, Name: "Love", Color: "#111"}); err != nil {
		t.Fatal(err)
	}
	imported, err := Import(context.Background(), dst, other.ID, zipReader(t, data))
	if err != nil || imported.Days.Imported != 1 {
		t.Fatalf("import: %+v %v", imported, err)
	}
	days, _ := dst.ListDays(other.ID)
	if len(days) != 1 || days[0].Name != "Wedding" || days[0].Repeat != store.DayRepeatYear || !days[0].IncludeStart || !days[0].ShowOnHome || days[0].Emoji != "💍" || days[0].Milestones != 2 {
		t.Fatalf("imported days: %+v", days)
	}

	tags, _ := dst.ListDayTags(other.ID)
	if len(tags) != 1 || tags[0].Color != "#111" || len(days[0].Tags) != 1 || days[0].Tags[0] != tags[0].ID {
		t.Fatalf("imported tags: %+v %v", tags, days[0].Tags)
	}

	// Importing again skips the day instead of duplicating it.
	again, err := Import(context.Background(), dst, other.ID, zipReader(t, data))
	if err != nil || again.Days.Skipped != 1 || again.Days.Imported != 0 {
		t.Fatalf("second import: %+v %v", again.Days, err)
	}
}

func TestImportDaysValidation(t *testing.T) {
	s := newStore(t)
	user := newUser(t, s)
	zr := buildZip(t, map[string][]byte{ManifestName: manifestJSON(t, Data{Version: FormatVersion, Days: []Day{
		{Name: "", Date: "2020-01-01"},
		{Name: "Bad date", Date: "2020-13-01"},
		{Name: "Ok", Date: "2020-01-01", Repeat: "daily", Tags: []string{"New", " "}, Milestones: []Milestone{{Name: "", Date: "2020-02-01"}, {Name: "M", Date: "nope"}, {Name: "Kept", Date: "2020-03-01"}}},
	}})})
	stats, err := Import(context.Background(), s, user.ID, zr)
	if err != nil || stats.Days.Total != 3 || stats.Days.Failed != 2 || stats.Days.Imported != 1 {
		t.Fatalf("stats: %+v %v", stats.Days, err)
	}
	days, _ := s.ListDays(user.ID)
	if tags, _ := s.ListDayTags(user.ID); len(tags) != 1 || tags[0].Name != "New" || len(days) != 1 || len(days[0].Tags) != 1 {
		t.Fatalf("tag created from day: %+v", tags)
	}
	if len(days) != 1 || days[0].Repeat != store.DayRepeatNone || days[0].Milestones != 1 {
		t.Fatalf("days: %+v", days)
	}

	// Tags listed on their own are created too, failures only logged.
	tagsOnly := buildZip(t, map[string][]byte{ManifestName: manifestJSON(t, Data{Version: FormatVersion, DayTags: []DayTag{{Name: "Solo", Color: "#123"}, {Name: ""}}})})
	if _, err := Import(context.Background(), s, user.ID, tagsOnly); err != nil {
		t.Fatal(err)
	}
	if tags, _ := s.ListDayTags(user.ID); len(tags) != 2 || tags[1].Color != "#123" {
		t.Fatalf("tags only: %+v", tags)
	}
	brokenTags := buildZip(t, map[string][]byte{ManifestName: []byte(`{"version":1,"day_tags":[{"name":1}]}`)})
	if _, err := Import(context.Background(), s, user.ID, brokenTags); err == nil {
		t.Fatal("expected invalid manifest for day_tags")
	}

	// A broken days array makes the whole manifest invalid.
	broken := buildZip(t, map[string][]byte{ManifestName: []byte(`{"version":1,"days":[{"name":1}]}`)})
	if _, err := Import(context.Background(), s, user.ID, broken); err == nil {
		t.Fatal("expected invalid manifest")
	}

	// Store failures are counted, not fatal.
	_ = s.Close()
	var buf bytes.Buffer
	exported, err := Export(context.Background(), s, user.ID, Options{IncludeDays: true}, &buf)
	if err == nil && (len(exported.FailedItems) == 0 || exported.FailedItems[len(exported.FailedItems)-1].Type != "days") {
		t.Fatalf("export on closed store: %+v", exported.FailedItems)
	}
	failed, err := Import(context.Background(), s, user.ID, buildZip(t, map[string][]byte{ManifestName: manifestJSON(t, Data{Version: FormatVersion, Days: []Day{{Name: "X", Date: "2020-01-01"}}})}))
	if err != nil || failed.Days.Failed != 1 {
		t.Fatalf("closed store: %+v %v", failed, err)
	}
}
