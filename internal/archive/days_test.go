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
	day, err := src.SaveDay(&store.Day{Owner: user.ID, Name: "Wedding", Date: "2020-05-20", Emoji: "💍", Note: "note", Repeat: store.DayRepeatYear, IncludeStart: true, ShowOnHome: true})
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
	imported, err := Import(context.Background(), dst, other.ID, zipReader(t, data))
	if err != nil || imported.Days.Imported != 1 {
		t.Fatalf("import: %+v %v", imported, err)
	}
	days, _ := dst.ListDays(other.ID)
	if len(days) != 1 || days[0].Name != "Wedding" || days[0].Repeat != store.DayRepeatYear || !days[0].IncludeStart || !days[0].ShowOnHome || days[0].Emoji != "💍" || days[0].Milestones != 2 {
		t.Fatalf("imported days: %+v", days)
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
		{Name: "Ok", Date: "2020-01-01", Repeat: "daily", Milestones: []Milestone{{Name: "", Date: "2020-02-01"}, {Name: "M", Date: "nope"}, {Name: "Kept", Date: "2020-03-01"}}},
	}})})
	stats, err := Import(context.Background(), s, user.ID, zr)
	if err != nil || stats.Days.Total != 3 || stats.Days.Failed != 2 || stats.Days.Imported != 1 {
		t.Fatalf("stats: %+v %v", stats.Days, err)
	}
	days, _ := s.ListDays(user.ID)
	if len(days) != 1 || days[0].Repeat != store.DayRepeatNone || days[0].Milestones != 1 {
		t.Fatalf("days: %+v", days)
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
