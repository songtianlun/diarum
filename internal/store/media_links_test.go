package store

import (
	"slices"
	"testing"
)

func TestReferencedMediaIDs(t *testing.T) {
	content := `<p>a</p><img src="/api/v1/files/media/abc123/one.png"><img src="https://x.test/api/v1/files/media/def456/two.png?thumb=1">` +
		`<img src="/api/v1/files/media/abc123/one.png"><img src="https://img.example.com/images/2024/05/01/x.png">` +
		`<img src="https://diarum.test/api/files/media/keh96zmpl3s9bzl/ghi789/old.png?thumb=undefined">` +
		`<img src="/api/files/keh96zmpl3s9bzl/jkl012/older.png">`
	got := ReferencedMediaIDs(content)
	if !slices.Equal(got, []string{"abc123", "def456", "ghi789", "jkl012"}) {
		t.Fatalf("ReferencedMediaIDs = %#v", got)
	}
}

func TestSaveDiaryLinksReferencedMedia(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	other := newTestUser(t, s)

	mine, err := s.CreateMedia(user.ID, "a.png", "a", "", nil)
	if err != nil {
		t.Fatalf("CreateMedia: %v", err)
	}
	theirs, err := s.CreateMedia(other.ID, "b.png", "b", "", nil)
	if err != nil {
		t.Fatalf("CreateMedia other: %v", err)
	}

	content := `<img src="/api/v1/files/media/` + mine.ID + `/a.png"><img src="/api/v1/files/media/` + theirs.ID + `/b.png">`
	diary, _, err := s.UpsertDiary(user.ID, "2024-05-01", content, "", "")
	if err != nil {
		t.Fatalf("UpsertDiary: %v", err)
	}
	// Saving again must not duplicate the link.
	if _, _, err := s.UpsertDiary(user.ID, "2024-05-01", content+"<p>more</p>", "", ""); err != nil {
		t.Fatalf("UpsertDiary again: %v", err)
	}

	got, err := s.GetMedia(mine.ID, user.ID)
	if err != nil {
		t.Fatalf("GetMedia: %v", err)
	}
	if !slices.Equal(got.Diary, []string{diary.ID}) {
		t.Fatalf("linked diaries = %#v, want [%s]", got.Diary, diary.ID)
	}
	// Another user's media is never touched, even when referenced.
	if got, _ := s.GetMedia(theirs.ID, other.ID); len(got.Diary) != 0 {
		t.Fatalf("other user's media linked: %#v", got.Diary)
	}
}
