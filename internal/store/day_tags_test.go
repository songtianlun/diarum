package store

import (
	"errors"
	"testing"
)

func TestDayTags(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	other := newTestUser(t, s)

	work, err := s.SaveDayTag(&DayTag{Owner: user.ID, Name: "Work", Color: "#aa0000"})
	if err != nil || work.ID == "" || work.Color != "#aa0000" {
		t.Fatalf("create: %+v %v", work, err)
	}
	family, _ := s.SaveDayTag(&DayTag{Owner: user.ID, Name: "Family"})
	foreign, _ := s.SaveDayTag(&DayTag{Owner: other.ID, Name: "Work"})
	if _, err := s.SaveDayTag(&DayTag{Owner: user.ID, Name: "Work"}); !errors.Is(err, ErrDayTagExists) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := s.SaveDayTag(&DayTag{ID: work.ID, Owner: other.ID, Name: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign rename: %v", err)
	}
	if _, err := s.SaveDayTag(&DayTag{ID: family.ID, Owner: user.ID, Name: "Work"}); !errors.Is(err, ErrDayTagExists) {
		t.Fatalf("rename onto existing: %v", err)
	}
	if renamed, err := s.SaveDayTag(&DayTag{ID: work.ID, Owner: user.ID, Name: "Job", Color: "#00aa00"}); err != nil || renamed.Name != "Job" {
		t.Fatalf("rename: %+v %v", renamed, err)
	}

	// Foreign and unknown tag IDs are ignored; nil keeps the tags.
	day, err := s.SaveDay(&Day{Owner: user.ID, Name: "D", Date: "2026-01-01", Tags: []string{family.ID, work.ID, foreign.ID, "nope"}})
	if err != nil || len(day.Tags) != 2 || day.Tags[0] != work.ID || day.Tags[1] != family.ID {
		t.Fatalf("day tags: %+v %v", day, err)
	}
	day, _ = s.SaveDay(&Day{ID: day.ID, Owner: user.ID, Name: "D2", Date: "2026-01-01"})
	if len(day.Tags) != 2 {
		t.Fatalf("nil tags should keep: %v", day.Tags)
	}
	tags, err := s.ListDayTags(user.ID)
	if err != nil || len(tags) != 2 || tags[0].Days != 1 {
		t.Fatalf("list: %+v %v", tags, err)
	}
	if err := s.DeleteDayTag(user.ID, work.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDayTag(user.ID, work.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	days, _ := s.ListDays(user.ID)
	if len(days) != 1 || len(days[0].Tags) != 1 || days[0].Tags[0] != family.ID {
		t.Fatalf("after delete: %+v", days[0].Tags)
	}
	day, _ = s.SaveDay(&Day{ID: day.ID, Owner: user.ID, Name: "D2", Date: "2026-01-01", Tags: []string{}})
	if len(day.Tags) != 0 {
		t.Fatalf("empty tags should clear: %v", day.Tags)
	}

	_ = s.Close()
	if _, err := s.ListDayTags(user.ID); err == nil {
		t.Fatal("ListDayTags on closed store")
	}
	if _, err := s.SaveDayTag(&DayTag{Owner: user.ID, Name: "x"}); err == nil {
		t.Fatal("SaveDayTag on closed store")
	}
	if err := s.DeleteDayTag(user.ID, "x"); err == nil {
		t.Fatal("DeleteDayTag on closed store")
	}
	if err := s.fillDayTags(user.ID, &Day{ID: "x"}); err == nil {
		t.Fatal("fillDayTags on closed store")
	}
	if err := s.setDayTags(user.ID, "x", []string{"y"}); err == nil {
		t.Fatal("setDayTags on closed store")
	}
}
