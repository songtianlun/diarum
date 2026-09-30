package store

import (
	"strings"
	"testing"
	"time"
)

func TestBackfillMediaDatesUsesFirstExistingEntry(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	first, _, err := s.UpsertDiary(user.ID, "2024-02-03", "a", "", "")
	if err != nil {
		t.Fatalf("UpsertDiary: %v", err)
	}
	second, _, err := s.UpsertDiary(user.ID, "2024-01-01", "b", "", "")
	if err != nil {
		t.Fatalf("UpsertDiary: %v", err)
	}
	// Linked to a missing entry first, then to two real ones.
	media, err := s.InsertImportedMedia(user.ID, "m1", "a.png", "a", "", []string{"gone", first.ID, second.ID})
	if err != nil {
		t.Fatalf("InsertImportedMedia: %v", err)
	}
	if media.Date != "2024-02-03 00:00:00.000Z" {
		t.Fatalf("date on insert = %q", media.Date)
	}
	if _, err := s.DB.Exec(`UPDATE media SET date = ''`); err != nil {
		t.Fatalf("clear date: %v", err)
	}
	if _, err := s.InsertImportedMedia(user.ID, "m2", "b.png", "b", "", nil); err != nil {
		t.Fatalf("InsertImportedMedia: %v", err)
	}
	if err := s.BackfillMediaDates(); err != nil {
		t.Fatalf("BackfillMediaDates: %v", err)
	}
	if got, _ := s.GetMedia("m1", user.ID); got.Date != "2024-02-03 00:00:00.000Z" {
		t.Fatalf("backfilled date = %q", got.Date)
	}
	if got, _ := s.GetMedia("m2", user.ID); got.Date != "" {
		t.Fatalf("unlinked image should stay undated, got %q", got.Date)
	}
}

func TestTrashStateTransitions(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	if _, err := s.InsertImportedMedia(user.ID, "m1", "a.png", "a", "", nil); err != nil {
		t.Fatalf("InsertImportedMedia: %v", err)
	}
	info := TrashInfo{By: "alice", Reason: MediaReasonUser, Trigger: MediaTriggerManual}
	if _, err := s.PurgeMedia("m1", user.ID); !IsNoRows(err) {
		t.Fatalf("purge outside trash err = %v", err)
	}
	trashed, err := s.TrashMedia("m1", user.ID, info)
	if err != nil || !trashed.InTrash() || trashed.DeletedBy != "alice" {
		t.Fatalf("TrashMedia = %+v %v", trashed, err)
	}
	if _, err := s.TrashMedia("m1", user.ID, info); !IsNoRows(err) {
		t.Fatalf("second trash err = %v", err)
	}
	if expired, _ := s.TrashedMedia(user.ID, time.Now().Add(-time.Hour)); len(expired) != 0 {
		t.Fatalf("fresh trash should not be expired: %d", len(expired))
	}
	if all, _ := s.TrashedMedia(user.ID, time.Time{}); len(all) != 1 {
		t.Fatalf("trash = %d", len(all))
	}
	if s.CountMedia(user.ID) != 0 {
		t.Fatal("trashed media should not be counted")
	}
	stats, err := s.MediaStats(user.ID)
	if err != nil || stats.Trash != 1 || stats.Total != 0 || stats.Linked != 0 {
		t.Fatalf("stats = %+v %v", stats, err)
	}
	if _, err := s.InsertImportedMedia(user.ID, "m2", "b.png", "b", "", nil); err != nil {
		t.Fatalf("InsertImportedMedia: %v", err)
	}
	if stats, _ := s.MediaStats(user.ID); stats.Total != 1 || stats.Linked != 0 {
		t.Fatalf("an image without entries is not linked: %+v", stats)
	}
	restored, err := s.RestoreMedia("m1", user.ID)
	if err != nil || restored.InTrash() || restored.DeletedBy != "" {
		t.Fatalf("RestoreMedia = %+v %v", restored, err)
	}
	if _, err := s.RestoreMedia("m1", user.ID); !IsNoRows(err) {
		t.Fatalf("second restore err = %v", err)
	}
	if _, err := s.TrashMedia("m1", user.ID, info); err != nil {
		t.Fatalf("TrashMedia: %v", err)
	}
	if _, err := s.PurgeMedia("m1", user.ID); err != nil {
		t.Fatalf("PurgeMedia: %v", err)
	}
	if _, err := s.GetMedia("m1", user.ID); !IsNoRows(err) {
		t.Fatalf("purged media err = %v", err)
	}
}

func TestNormalizeS3Prefix(t *testing.T) {
	for input, want := range map[string]string{"": "", " / ": "", "/diarum/media/": "diarum/media", "a\\b": "a/b", "v1.2_x-y": "v1.2_x-y"} {
		if got, err := NormalizeS3Prefix(input); err != nil || got != want {
			t.Errorf("NormalizeS3Prefix(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"a//b", "../x", "a/./b", "has space", "中文", "a/-b", strings.Repeat("a", 201)} {
		if _, err := NormalizeS3Prefix(input); err == nil {
			t.Errorf("NormalizeS3Prefix(%q) should fail", input)
		}
	}
}

func TestMediaObjectKeysWithPrefixes(t *testing.T) {
	s := newTestStore(t)
	media := &Media{ID: "mid", File: "p.png", Storage: MediaStorageS3, S3Prefix: "old"}
	keys := s.mediaObjectKeys(media, &LegacyS3Config{Prefix: "new/dir"})
	want := []string{"old/media/mid/p.png", "new/dir/media/mid/p.png", "media/mid/p.png"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
	if got := mediaObjectKey(s3UploadPrefix(media, &LegacyS3Config{Prefix: "new"}), DefaultMediaCollectionID, media); got != "old/media/mid/p.png" {
		t.Fatalf("variant upload key = %q", got)
	}
}
