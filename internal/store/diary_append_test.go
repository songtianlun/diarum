package store

import (
	"testing"
)

func TestAppendDiary(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	user, err := s.CreateUser("append_user", "append@example.com", "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// 1. Create a new diary via AppendDiary.
	before, createdDiary, created, err := s.AppendDiary(user.ID, "2026-11-24", func(existing string) string {
		if existing != "" {
			t.Fatalf("expected empty existing content, got %q", existing)
		}
		return "<p>first</p><hr><p></p>"
	})
	if err != nil {
		t.Fatalf("AppendDiary create: %v", err)
	}
	if !created || before != nil {
		t.Fatalf("expected created=true and before=nil, got created=%v before=%v", created, before)
	}
	if createdDiary.Content != "<p>first</p><hr><p></p>" {
		t.Fatalf("unexpected created content: %q", createdDiary.Content)
	}

	// Add mood & weather, then append again to confirm preservation and history archival.
	if _, _, err := s.UpsertDiary(user.ID, "2026-11-24", createdDiary.Content, "😄", "⛅"); err != nil {
		t.Fatalf("UpsertDiary: %v", err)
	}

	before2, updatedDiary, created2, err := s.AppendDiary(user.ID, "2026-11-24", func(existing string) string {
		return existing + "<p>second</p>"
	})
	if err != nil {
		t.Fatalf("AppendDiary update: %v", err)
	}
	if created2 || before2 == nil {
		t.Fatalf("expected created=false and non-nil before, got created=%v before=%v", created2, before2)
	}
	if updatedDiary.Mood != "😄" || updatedDiary.Weather != "⛅" {
		t.Fatalf("mood/weather not preserved: mood=%q weather=%q", updatedDiary.Mood, updatedDiary.Weather)
	}
	revs, err := s.ListDiaryRevisions(user.ID, "2026-11-24")
	if err != nil || len(revs) == 0 {
		t.Fatalf("expected revision history after append, revs=%d err=%v", len(revs), err)
	}

	// 3. Closed DB error path.
	_ = s.Close()
	if _, _, _, err := s.AppendDiary(user.ID, "2026-11-24", func(existing string) string { return existing }); err == nil {
		t.Fatalf("expected error on closed store")
	}
}
