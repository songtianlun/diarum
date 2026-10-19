package store

import (
	"database/sql"
	"errors"
	"testing"
)

func TestDaysAndMilestones(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	other := newTestUser(t, s)

	day, err := s.SaveDay(&Day{Owner: user.ID, Name: "Trip", Date: "2026-11-01", Repeat: DayRepeatYear, ShowOnHome: true})
	if err != nil || day.ID == "" || !day.ShowOnHome || day.Repeat != DayRepeatYear {
		t.Fatalf("create day: %+v %v", day, err)
	}
	if _, err := s.SaveDay(&Day{ID: day.ID, Owner: other.ID, Name: "x", Date: "2026-01-01"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign update: %v", err)
	}
	if _, err := s.SaveDayMilestone(&DayMilestone{Day: day.ID, Owner: other.ID, Name: "x", Date: "2026-01-01"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("foreign milestone: %v", err)
	}
	m, err := s.SaveDayMilestone(&DayMilestone{Day: day.ID, Owner: user.ID, Name: "Book", Date: "2026-10-01"})
	if err != nil || m.ID == "" {
		t.Fatalf("milestone: %+v %v", m, err)
	}
	if _, err := s.SaveDayMilestone(&DayMilestone{ID: "missing", Day: day.ID, Owner: user.ID, Name: "x", Date: "2026-01-01"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing milestone: %v", err)
	}
	list, err := s.ListDayMilestones(user.ID, day.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("list milestones: %v %v", list, err)
	}
	days, err := s.ListDays(user.ID)
	if err != nil || len(days) != 1 || days[0].Milestones != 1 {
		t.Fatalf("list days: %v %v", days, err)
	}
	// One end milestone per day; the day reports its date.
	end, err := s.SaveDayMilestone(&DayMilestone{Day: day.ID, Owner: user.ID, Name: "Stop", Date: "2026-12-01", End: true})
	if err != nil || !end.End {
		t.Fatalf("end milestone: %+v %v", end, err)
	}
	if _, err := s.SaveDayMilestone(&DayMilestone{Day: day.ID, Owner: user.ID, Name: "Again", Date: "2027-01-01", End: true}); !errors.Is(err, ErrDayEndExists) {
		t.Fatalf("second end: %v", err)
	}
	if _, err := s.SaveDayMilestone(&DayMilestone{ID: m.ID, Day: day.ID, Owner: user.ID, Name: "Book", Date: "2026-10-01", End: true}); !errors.Is(err, ErrDayEndExists) {
		t.Fatalf("second end by update: %v", err)
	}
	if got, _ := s.GetDay(user.ID, day.ID); got.End != "2026-12-01" {
		t.Fatalf("day end: %q", got.End)
	}
	if all, err := s.ListAllDayMilestones(user.ID); err != nil || len(all) != 2 {
		t.Fatalf("all milestones: %v %v", all, err)
	}
	if err := s.DeleteDayMilestone(user.ID, day.ID, end.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDayMilestone(user.ID, day.ID, m.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDayMilestone(user.ID, day.ID, m.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second milestone delete: %v", err)
	}
	if err := s.DeleteDay(other.ID, day.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign delete: %v", err)
	}
	if err := s.DeleteDay(user.ID, day.ID); err != nil {
		t.Fatal(err)
	}

	// An insert that violates the owner key fails; other errors pass through.
	if _, err := s.SaveDay(&Day{Owner: "nobody", Name: "x", Date: "2026-01-01"}); err == nil {
		t.Fatal("SaveDay for a missing owner")
	}
	if err := milestoneError(errors.New("boom")); errors.Is(err, ErrDayEndExists) {
		t.Fatal("milestoneError mapped a non-unique error")
	}
	if err := s.fillDayTags(user.ID); err != nil {
		t.Fatalf("fillDayTags with no days: %v", err)
	}

	_ = s.Close()
	if _, err := s.ListDays(user.ID); err == nil {
		t.Fatal("ListDays on closed store")
	}
	if _, err := s.ListDayMilestones(user.ID, day.ID); err == nil {
		t.Fatal("ListDayMilestones on closed store")
	}
	if _, err := s.SaveDay(&Day{Owner: user.ID, Name: "x", Date: "2026-01-01"}); err == nil {
		t.Fatal("SaveDay create on closed store")
	}
	if _, err := s.SaveDay(&Day{ID: "x", Owner: user.ID, Name: "x", Date: "2026-01-01"}); err == nil {
		t.Fatal("SaveDay update on closed store")
	}
	if err := s.DeleteDay(user.ID, "x"); err == nil {
		t.Fatal("DeleteDay on closed store")
	}
	if err := s.DeleteDayMilestone(user.ID, "x", "y"); err == nil {
		t.Fatal("DeleteDayMilestone on closed store")
	}
	if err := migrateDaysSchema(s.DB); err == nil {
		t.Fatal("migrateDaysSchema on closed store")
	}
}
