package store

import (
	"fmt"
	"testing"
)

func TestBatchQueries(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	other := newTestUser(t, s)

	for i := 1; i <= 5; i++ {
		if _, err := s.InsertImportedDiary(user.ID, "", fmt.Sprintf("2024-01-%02d", i), "x", "", ""); err != nil {
			t.Fatalf("InsertImportedDiary: %v", err)
		}
	}
	if _, err := s.InsertImportedDiary(other.ID, "", "2024-01-01", "other", "", ""); err != nil {
		t.Fatalf("InsertImportedDiary other: %v", err)
	}
	var dates []string
	afterDate, afterID := "", ""
	for {
		batch, err := s.ListDiariesAfter(user.ID, afterDate, afterID, 2)
		if err != nil {
			t.Fatalf("ListDiariesAfter: %v", err)
		}
		for _, d := range batch {
			dates = append(dates, DateOnly(d.Date))
		}
		if len(batch) < 2 {
			break
		}
		afterDate, afterID = batch[len(batch)-1].Date, batch[len(batch)-1].ID
	}
	if fmt.Sprint(dates) != "[2024-01-01 2024-01-02 2024-01-03 2024-01-04 2024-01-05]" {
		t.Fatalf("paged dates = %v", dates)
	}
	if all, err := s.ListDiariesAfter(user.ID, "", "", 0); err != nil || len(all) != 5 {
		t.Fatalf("default limit = %d, %v", len(all), err)
	}

	for i := 0; i < 3; i++ {
		if _, err := s.CreateMedia(user.ID, "a.png", "", "", nil); err != nil {
			t.Fatalf("CreateMedia: %v", err)
		}
	}
	first, err := s.ListMediaAfter(user.ID, "", "", 2)
	if err != nil || len(first) != 2 {
		t.Fatalf("ListMediaAfter first = %d, %v", len(first), err)
	}
	rest, err := s.ListMediaAfter(user.ID, first[1].Created, first[1].ID, 0)
	if err != nil || len(rest) != 1 || rest[0].ID == first[0].ID || rest[0].ID == first[1].ID {
		t.Fatalf("ListMediaAfter rest = %#v, %v", rest, err)
	}
	if s.CountMedia(user.ID) != 3 || s.CountMedia(other.ID) != 0 {
		t.Fatalf("CountMedia = %d / %d", s.CountMedia(user.ID), s.CountMedia(other.ID))
	}

	if err := s.SetSetting(user.ID, "backup.auto_enabled", true, false); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := s.SetSetting(other.ID, "backup.auto_enabled", false, false); err != nil {
		t.Fatalf("SetSetting other: %v", err)
	}
	users, err := s.UsersWithSetting("backup.auto_enabled", true)
	if err != nil || len(users) != 1 || users[0] != user.ID {
		t.Fatalf("UsersWithSetting = %v, %v", users, err)
	}

	_ = s.Close()
	if _, err := s.ListDiariesAfter(user.ID, "", "", 1); err == nil {
		t.Fatal("ListDiariesAfter on a closed store should fail")
	}
	if _, err := s.ListMediaAfter(user.ID, "", "", 1); err == nil {
		t.Fatal("ListMediaAfter on a closed store should fail")
	}
	if _, err := s.UsersWithSetting("x", true); err == nil {
		t.Fatal("UsersWithSetting on a closed store should fail")
	}
}
