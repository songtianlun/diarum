package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/medialib"
	"github.com/songtianlun/diarum/internal/store"
)

func postMediaIDs(t *testing.T, e *echo.Echo, path string, ids ...string) *mediaBatchResult {
	t.Helper()
	payload, _ := json.Marshal(mediaIDsBody{IDs: ids})
	rec := performRequest(t, e, http.MethodPost, path, bytes.NewReader(payload), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s status = %d body=%s", path, rec.Code, rec.Body.String())
	}
	var result mediaBatchResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return &result
}

func purgeMediaForTest(t *testing.T, e *echo.Echo, ids ...string) *mediaBatchResult {
	t.Helper()
	return postMediaIDs(t, e, "/api/v1/media/trash/purge", ids...)
}

// uploadTestMedia stores a media record with a real file on disk, created at
// the given time.
func uploadTestMedia(t *testing.T, s *store.Store, owner, id string, created time.Time) *store.Media {
	t.Helper()
	media, err := s.InsertImportedMedia(owner, id, id+".png", id, "", nil)
	if err != nil {
		t.Fatalf("InsertImportedMedia: %v", err)
	}
	if err := s.SaveUploadedMedia(media, bytes.NewReader(pngBytes())); err != nil {
		t.Fatalf("SaveUploadedMedia: %v", err)
	}
	if _, err := s.DB.Exec(`UPDATE media SET created = ? WHERE id = ?`, created.UTC().Format("2006-01-02 15:04:05.000Z"), id); err != nil {
		t.Fatalf("set created: %v", err)
	}
	media, _ = s.GetMedia(id, owner)
	return media
}

func TestMediaTrashLifecycle(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	auth := authMiddlewareFor(user)
	RegisterMediaRoutes(e, s, auth)
	RegisterMediaLibraryRoutes(e, s, auth, medialib.New(s, nil))

	old := time.Now().Add(-48 * time.Hour)
	used := uploadTestMedia(t, s, user.ID, "used", old)
	uploadTestMedia(t, s, user.ID, "unused", old)
	fresh := uploadTestMedia(t, s, user.ID, "fresh", time.Now())
	if used.Storage != store.MediaStorageLocal {
		t.Fatalf("storage = %q, want local", used.Storage)
	}

	// Saving an entry links the image and gives it the entry's date.
	content := `<p><img src="/api/v1/files/media/used/used.png"></p>`
	if _, _, err := s.UpsertDiary(user.ID, "2024-05-06", content, "", ""); err != nil {
		t.Fatalf("UpsertDiary: %v", err)
	}
	if media, _ := s.GetMedia("used", user.ID); media.Date != "2024-05-06 00:00:00.000Z" {
		t.Fatalf("image date = %q", media.Date)
	}

	// The scan finds the unused image, not the used one nor the fresh one.
	rec := performRequest(t, e, http.MethodGet, "/api/v1/media/unlinked", nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"unused"`) ||
		strings.Contains(rec.Body.String(), `"id":"used"`) || strings.Contains(rec.Body.String(), `"id":"fresh"`) {
		t.Fatalf("unlinked scan = %d %s", rec.Code, rec.Body.String())
	}
	_ = fresh

	// Cleaning re-checks every image: the used one is refused.
	result := postMediaIDs(t, e, "/api/v1/media/unlinked/clean", "unused", "used")
	if len(result.Done) != 1 || result.Done[0] != "unused" || result.Failed["used"] == "" {
		t.Fatalf("clean = %+v", result)
	}

	// Deleting moves to the trash, out of the library, file kept.
	rec = performRequest(t, e, http.MethodDelete, "/api/v1/media/used", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d", rec.Code)
	}
	items, total, _ := s.ListMedia(user.ID, 1, 50)
	if total != 1 || items[0].ID != "fresh" {
		t.Fatalf("library after delete = %d %+v", total, items)
	}
	if _, err := os.Stat(s.MediaFilePath(used)); err != nil {
		t.Fatalf("trashed file should remain: %v", err)
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/media/trash", nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"totalItems":2`) || !strings.Contains(rec.Body.String(), `"retentionDays":30`) {
		t.Fatalf("trash = %d %s", rec.Code, rec.Body.String())
	}

	// Saving an entry with the auto-cleaned image brings it back; the one
	// deleted by hand stays in the trash.
	content += `<p><img src="/api/v1/files/media/unused/unused.png"></p>`
	if _, _, err := s.UpsertDiary(user.ID, "2024-05-06", content, "", ""); err != nil {
		t.Fatalf("UpsertDiary: %v", err)
	}
	if media, _ := s.GetMedia("unused", user.ID); media.InTrash() {
		t.Fatal("referenced unlinked image should be restored")
	}
	if media, _ := s.GetMedia("used", user.ID); !media.InTrash() {
		t.Fatal("image deleted by hand should stay in the trash")
	}

	// Restore and purge.
	if result := postMediaIDs(t, e, "/api/v1/media/trash/restore", "used"); len(result.Done) != 1 {
		t.Fatalf("restore = %+v", result)
	}
	if result := purgeMediaForTest(t, e, "used"); result.Failed["used"] == "" {
		t.Fatalf("purging an image outside the trash should fail: %+v", result)
	}
	rec = performRequest(t, e, http.MethodDelete, "/api/v1/media/fresh", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE fresh status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPost, "/api/v1/media/trash/empty", nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"fresh"`) {
		t.Fatalf("empty = %d %s", rec.Code, rec.Body.String())
	}
	if _, err := s.GetMedia("fresh", user.ID); !store.IsNoRows(err) {
		t.Fatalf("purged media still exists: %v", err)
	}
	if _, err := os.Stat(s.MediaFilePath(fresh)); !os.IsNotExist(err) {
		t.Fatalf("purged file still exists: %v", err)
	}

	rec = performRequest(t, e, http.MethodGet, "/api/v1/media/stats", nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"current":2`) || !strings.Contains(rec.Body.String(), `"trash":0`) {
		t.Fatalf("stats = %d %s", rec.Code, rec.Body.String())
	}
}

func TestMediaLibrarySettingsAndHousekeeping(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	auth := authMiddlewareFor(user)
	svc := medialib.New(s, nil)
	RegisterMediaRoutes(e, s, auth)
	RegisterMediaLibraryRoutes(e, s, auth, svc)

	rec := performRequest(t, e, http.MethodPut, "/api/v1/media/settings", strings.NewReader(`{"trash_retention_days":-1}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid retention status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPut, "/api/v1/media/settings", strings.NewReader(`{"trash_retention_days":7,"auto_clean_unlinked":true}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT settings = %d %s", rec.Code, rec.Body.String())
	}
	if got := svc.LoadSettings(user.ID); got.TrashRetentionDays != 7 || !got.AutoCleanUnlinked {
		t.Fatalf("settings = %+v", got)
	}

	uploadTestMedia(t, s, user.ID, "stale", time.Now().Add(-72*time.Hour))
	uploadTestMedia(t, s, user.ID, "recent", time.Now())
	expired := uploadTestMedia(t, s, user.ID, "expired", time.Now().Add(-72*time.Hour))
	if _, err := s.TrashMedia("expired", user.ID, store.TrashInfo{By: "tester", Reason: store.MediaReasonUser, Trigger: store.MediaTriggerManual}); err != nil {
		t.Fatalf("TrashMedia: %v", err)
	}
	if _, err := s.DB.Exec(`UPDATE media SET deleted = ? WHERE id = 'expired'`, time.Now().AddDate(0, 0, -8).UTC().Format("2006-01-02 15:04:05.000Z")); err != nil {
		t.Fatalf("age trash: %v", err)
	}

	result := svc.RunUser(user.ID)
	if result.Trashed != 1 || result.Purged != 1 || result.Failed != 0 {
		t.Fatalf("housekeeping = %+v", result)
	}
	if media, _ := s.GetMedia("stale", user.ID); !media.InTrash() || media.DeleteTrigger != store.MediaTriggerAuto || media.DeleteReason != store.MediaReasonUnlinked {
		t.Fatalf("stale image = %+v", media)
	}
	if media, _ := s.GetMedia("recent", user.ID); media.InTrash() {
		t.Fatal("recent uploads are within the grace period")
	}
	if _, err := s.GetMedia("expired", user.ID); !store.IsNoRows(err) {
		t.Fatalf("expired image should be purged: %v", err)
	}
	if _, err := os.Stat(s.MediaFilePath(expired)); !os.IsNotExist(err) {
		t.Fatalf("expired file should be removed: %v", err)
	}

	// Retention 0 never purges.
	rec = performRequest(t, e, http.MethodPut, "/api/v1/media/settings", strings.NewReader(`{"trash_retention_days":0}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT settings = %d", rec.Code)
	}
	if _, err := s.DB.Exec(`UPDATE media SET deleted = '2000-01-01 00:00:00.000Z' WHERE id = 'stale'`); err != nil {
		t.Fatalf("age trash: %v", err)
	}
	if result := svc.RunUser(user.ID); result.Purged != 0 {
		t.Fatalf("retention 0 purged: %+v", result)
	}
}
