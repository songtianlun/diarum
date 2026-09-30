package store

import (
	"fmt"
	"os"
	"path/filepath"
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

func TestListTrashPagesOnImageTimeline(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	older, _, _ := s.UpsertDiary(user.ID, "2023-01-01", "", "", "")
	newer, _, _ := s.UpsertDiary(user.ID, "2024-06-01", "", "", "")
	for _, m := range []struct {
		id    string
		diary []string
	}{{"old", []string{older.ID}}, {"new", []string{newer.ID}}, {"undated", nil}, {"kept", nil}} {
		if _, err := s.InsertImportedMedia(user.ID, m.id, m.id+".png", m.id, "", m.diary); err != nil {
			t.Fatal(err)
		}
	}
	info := TrashInfo{By: "alice", Reason: MediaReasonUser, Trigger: MediaTriggerManual}
	for _, id := range []string{"old", "new", "undated"} {
		if _, err := s.TrashMedia(id, user.ID, info); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := s.ListTrash(user.ID, 0, 0)
	if err != nil || total != 3 || len(items) != 3 {
		t.Fatalf("ListTrash = %d items, total %d, %v", len(items), total, err)
	}
	// The undated image falls back to its upload time, which is newest.
	if items[0].ID != "undated" || items[1].ID != "new" || items[2].ID != "old" {
		t.Fatalf("order = %s, %s, %s", items[0].ID, items[1].ID, items[2].ID)
	}
	if ids, err := s.TrashedMediaIDs(user.ID); err != nil || strings.Join(ids, ",") != "undated,new,old" {
		t.Fatalf("TrashedMediaIDs = %v %v", ids, err)
	}
	page, total, err := s.ListTrash(user.ID, 2, 2)
	if err != nil || total != 3 || len(page) != 1 || page[0].ID != "old" {
		t.Fatalf("page 2 = %+v, total %d, %v", page, total, err)
	}
	if _, _, err := s.ListTrash("nobody", 1, 10); err != nil {
		t.Fatalf("ListTrash for a user without images: %v", err)
	}
}

func TestUnlinkedMediaAndReferences(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	if _, _, err := s.UpsertDiary(user.ID, "2024-01-01", `<img src="/api/v1/files/media/used/used.png"><img src="/api/files/pbc_media/legacy/l.png">`, "", ""); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"used", "legacy", "free", "trashed"} {
		if _, err := s.InsertImportedMedia(user.ID, id, id+".png", id, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.TrashMedia("trashed", user.ID, TrashInfo{By: "alice", Reason: MediaReasonUser, Trigger: MediaTriggerManual}); err != nil {
		t.Fatal(err)
	}
	unlinked, err := s.UnlinkedMedia(user.ID, time.Time{})
	if err != nil || len(unlinked) != 1 || unlinked[0].ID != "free" {
		t.Fatalf("UnlinkedMedia = %+v %v", unlinked, err)
	}
	if recent, err := s.UnlinkedMedia(user.ID, time.Now().Add(-time.Hour)); err != nil || len(recent) != 0 {
		t.Fatalf("recent uploads should be skipped: %+v %v", recent, err)
	}
	for id, want := range map[string]bool{"used": true, "legacy": true, "free": false, "use": false} {
		if got, err := s.IsMediaReferenced(user.ID, id); err != nil || got != want {
			t.Errorf("IsMediaReferenced(%s) = %v, %v", id, got, err)
		}
	}

	_ = s.Close()
	if _, err := s.UnlinkedMedia(user.ID, time.Time{}); err == nil {
		t.Fatal("UnlinkedMedia on a closed store should fail")
	}
	if _, err := s.IsMediaReferenced(user.ID, "used"); err == nil {
		t.Fatal("IsMediaReferenced on a closed store should fail")
	}
}

func TestMediaOwners(t *testing.T) {
	s := newTestStore(t)
	if owners, err := s.MediaOwners(); err != nil || len(owners) != 0 {
		t.Fatalf("empty owners = %v %v", owners, err)
	}
	a, b := newTestUser(t, s), newTestUser(t, s)
	for i, owner := range []string{a.ID, a.ID, b.ID} {
		if _, err := s.InsertImportedMedia(owner, fmt.Sprintf("m%d", i), "x.png", "x", "", nil); err != nil {
			t.Fatal(err)
		}
	}
	owners, err := s.MediaOwners()
	if err != nil || len(owners) != 2 {
		t.Fatalf("MediaOwners = %v %v", owners, err)
	}
	_ = s.Close()
	if _, err := s.MediaOwners(); err == nil {
		t.Fatal("MediaOwners on a closed store should fail")
	}
	if _, err := s.MediaStats(a.ID); err == nil {
		t.Fatal("MediaStats on a closed store should fail")
	}
	if _, _, err := s.ListTrash(a.ID, 1, 1); err == nil {
		t.Fatal("ListTrash on a closed store should fail")
	}
	if _, err := s.TrashedMediaIDs(a.ID); err == nil {
		t.Fatal("TrashedMediaIDs on a closed store should fail")
	}
}

func TestLinkDiaryMediaRestoresOnlyAutoTrashedImages(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	var events []string
	s.MediaEvent = func(action string, media *Media, detail map[string]any) {
		events = append(events, action+":"+media.ID+":"+fmt.Sprint(detail["reason"]))
	}
	diary, _, err := s.UpsertDiary(user.ID, "2024-03-04", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"auto", "manual", "fresh"} {
		if _, err := s.InsertImportedMedia(user.ID, id, id+".png", id, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.TrashMedia("auto", user.ID, TrashInfo{By: "system", Reason: MediaReasonUnlinked, Trigger: MediaTriggerAuto}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TrashMedia("manual", user.ID, TrashInfo{By: "alice", Reason: MediaReasonUser, Trigger: MediaTriggerManual}); err != nil {
		t.Fatal(err)
	}
	content := ""
	for _, id := range []string{"auto", "manual", "fresh", "missing"} {
		content += fmt.Sprintf(`<img src="/api/v1/files/media/%s/%s.png">`, id, id)
	}
	if err := s.LinkDiaryMedia(user.ID, diary.ID, content); err != nil {
		t.Fatalf("LinkDiaryMedia: %v", err)
	}
	if len(events) != 1 || events[0] != "media.restore:auto:"+MediaReasonReferenced {
		t.Fatalf("events = %v", events)
	}
	for id, wantTrash := range map[string]bool{"auto": false, "manual": true, "fresh": false} {
		media, err := s.GetMedia(id, user.ID)
		if err != nil || media.InTrash() != wantTrash {
			t.Errorf("%s = %+v %v", id, media, err)
			continue
		}
		if !wantTrash && (media.Date != "2024-03-04 00:00:00.000Z" || len(media.Diary) != 1) {
			t.Errorf("%s date/diary = %q %v", id, media.Date, media.Diary)
		}
	}
	// Linking again changes nothing.
	if err := s.LinkDiaryMedia(user.ID, diary.ID, content); err != nil || len(events) != 1 {
		t.Fatalf("second link = %v, events %v", err, events)
	}
}

func TestBackfillMediaStorage(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	for _, id := range []string{"ondisk", "missing"} {
		if _, err := s.InsertImportedMedia(user.ID, id, id+".png", id, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	path := s.NewMediaFilePath("ondisk", "ondisk.png")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE media SET storage = ''`); err != nil {
		t.Fatal(err)
	}
	if err := s.backfillMediaStorage(); err != nil {
		t.Fatalf("backfillMediaStorage: %v", err)
	}
	for id, want := range map[string]string{"ondisk": MediaStorageLocal, "missing": MediaStorageUnknown} {
		if media, _ := s.GetMedia(id, user.ID); media.Storage != want {
			t.Errorf("%s storage = %q, want %q", id, media.Storage, want)
		}
	}

	// With S3 configured, a file not on disk is taken to be in the bucket.
	s.LegacyS3 = &LegacyS3Config{Bucket: "b"}
	if _, err := s.DB.Exec(`UPDATE media SET storage = '' WHERE id = 'missing'`); err != nil {
		t.Fatal(err)
	}
	if err := s.backfillMediaStorage(); err != nil {
		t.Fatal(err)
	}
	if media, _ := s.GetMedia("missing", user.ID); media.Storage != MediaStorageS3 {
		t.Fatalf("storage with S3 = %q", media.Storage)
	}
	_ = s.Close()
	if err := s.backfillMediaStorage(); err == nil {
		t.Fatal("backfillMediaStorage on a closed store should fail")
	}
}

func TestMigrateMediaSchemaAddsMissingColumns(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.DB.Exec(`ALTER TABLE media DROP COLUMN s3_prefix`); err != nil {
		t.Fatalf("drop column: %v", err)
	}
	if err := migrateMediaSchema(s.DB); err != nil {
		t.Fatalf("migrateMediaSchema: %v", err)
	}
	if exists, err := columnExists(s.DB, "media", "s3_prefix"); err != nil || !exists {
		t.Fatalf("s3_prefix exists = %v %v", exists, err)
	}
	if err := migrateMediaSchema(s.DB); err != nil {
		t.Fatalf("second migrateMediaSchema: %v", err)
	}
	_ = s.Close()
	if err := migrateMediaSchema(s.DB); err == nil {
		t.Fatal("migrateMediaSchema on a closed database should fail")
	}
}

func TestMediaDate(t *testing.T) {
	for in, want := range map[string]string{"2024-01-02": "2024-01-02 00:00:00.000Z", "2024-01-02 10:00:00.000Z": "2024-01-02 00:00:00.000Z", "": "", "bad": ""} {
		if got := mediaDate(in); got != want {
			t.Errorf("mediaDate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPurgeMediaKeepsRecordWhenFileRemovalFails(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	if _, err := s.InsertImportedMedia(user.ID, "m1", "m1.png", "m1", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TrashMedia("m1", user.ID, TrashInfo{By: "alice", Reason: MediaReasonUser, Trigger: MediaTriggerManual}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(s.NewMediaFilePath("m1", "m1.png"), "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PurgeMedia("m1", user.ID); err == nil {
		t.Fatal("PurgeMedia should fail when the file cannot be removed")
	}
	if media, err := s.GetMedia("m1", user.ID); err != nil || !media.InTrash() {
		t.Fatalf("image should stay in the trash: %+v %v", media, err)
	}
	if _, err := s.PurgeMedia("missing", user.ID); !IsNoRows(err) {
		t.Fatalf("purge missing = %v", err)
	}
}

// An entry showing an image makes it linked in the statistics, as in the
// unused image scan, even when the image's diary field was never filled.
func TestMediaStatsLinkedFollowsEntries(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	if _, err := s.InsertImportedMedia(user.ID, "m1", "a.png", "a", "", nil); err != nil {
		t.Fatalf("InsertImportedMedia: %v", err)
	}
	if _, err := s.InsertImportedMedia(user.ID, "m2", "b.png", "b", "", nil); err != nil {
		t.Fatalf("InsertImportedMedia: %v", err)
	}
	if _, _, err := s.UpsertDiary(user.ID, "2024-01-01", `<p><img src="/api/v1/files/media/m1/a.png"></p>`, "", ""); err != nil {
		t.Fatalf("UpsertDiary: %v", err)
	}
	if _, err := s.DB.Exec(`UPDATE media SET diary = '[]'`); err != nil {
		t.Fatalf("clear diary: %v", err)
	}
	stats, err := s.MediaStats(user.ID)
	if err != nil || stats.Total != 2 || stats.Linked != 1 {
		t.Fatalf("stats = %+v %v", stats, err)
	}
	unlinked, err := s.UnlinkedMedia(user.ID, time.Time{})
	if err != nil || len(unlinked) != 1 || unlinked[0].ID != "m2" {
		t.Fatalf("UnlinkedMedia = %v %v", unlinked, err)
	}
}
