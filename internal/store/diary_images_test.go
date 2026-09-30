package store

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestExtractDiaryImages(t *testing.T) {
	content := `<p>a<img src="https://i.example.com/a.png" alt="x"></p>
		<img alt='y' src='/api/v1/files/media/m1/p.jpg'>
		<img data-src="ignored.png" src=https://i.example.com/b.png>
		<img src="https://i.example.com/a.png">
		<img src="data:image/png;base64,AAAA"><img src="blob:xyz">
		<img src="https://i.example.com/c.png?x=1&amp;y=2">
		![md](https://i.example.com/d.png "title")`
	got := ExtractDiaryImages(content)
	want := []DiaryImage{
		{URL: "https://i.example.com/a.png"},
		{URL: "/api/v1/files/media/m1/p.jpg", MediaID: "m1", Managed: true},
		{URL: "https://i.example.com/b.png"},
		{URL: "https://i.example.com/c.png?x=1&y=2"},
		{URL: "https://i.example.com/d.png"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("image %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if images := ExtractDiaryImages("<p>no images</p>"); len(images) != 0 {
		t.Fatalf("no images = %+v", images)
	}
}

func TestDiaryImagesFollowSaves(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	diary, _, err := s.UpsertDiary(user.ID, "2024-02-01", `<img src="https://x/a.png"><img src="https://x/b.png">`, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if images, _ := s.DiaryImages(diary.ID); len(images) != 2 || images[0].URL != "https://x/a.png" {
		t.Fatalf("after insert = %+v", images)
	}
	if _, _, err := s.UpsertDiary(user.ID, "2024-02-01", `<img src="https://x/b.png">`, "", ""); err != nil {
		t.Fatal(err)
	}
	if images, _ := s.DiaryImages(diary.ID); len(images) != 1 || images[0].URL != "https://x/b.png" {
		t.Fatalf("after edit = %+v", images)
	}
	if err := s.DeleteDiary(diary.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountExternalImages(user.ID); n != 0 {
		t.Fatalf("images left after delete = %d", n)
	}
	imported, err := s.InsertImportedDiary(user.ID, "", "2024-03-01", `<img src="https://x/c.png">`, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if images, _ := s.DiaryImages(imported.ID); len(images) != 1 {
		t.Fatalf("imported = %+v", images)
	}
}

func TestBackfillDiaryImages(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	diary, _, err := s.UpsertDiary(user.ID, "2024-02-01", `<img src="https://x/a.png">`, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`DELETE FROM diary_images`); err != nil {
		t.Fatal(err)
	}
	if err := s.BackfillDiaryImages(); err != nil {
		t.Fatal(err)
	}
	if images, _ := s.DiaryImages(diary.ID); len(images) != 1 {
		t.Fatalf("backfilled = %+v", images)
	}
	// Idempotent.
	if err := s.BackfillDiaryImages(); err != nil {
		t.Fatal(err)
	}
	if images, _ := s.DiaryImages(diary.ID); len(images) != 1 {
		t.Fatalf("second backfill = %+v", images)
	}
	_ = s.Close()
	if err := s.BackfillDiaryImages(); err == nil {
		t.Fatal("backfill on a closed store should fail")
	}
	if _, err := s.DiaryImages(diary.ID); err == nil {
		t.Fatal("DiaryImages on a closed store should fail")
	}
	if _, _, err := s.ListGallery(user.ID, 1, 10); err == nil {
		t.Fatal("ListGallery on a closed store should fail")
	}
}

func TestListGalleryMergesStoredAndExternalImages(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	other := newTestUser(t, s)
	// The same external image in three entries is listed once, dated by the
	// earliest entry.
	for _, date := range []string{"2024-05-01", "2023-01-10", "2024-07-01"} {
		if _, _, err := s.UpsertDiary(user.ID, date, `<img src="https://cdn.example.com/images/shared.png">`, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	stored, _, err := s.UpsertDiary(user.ID, "2024-06-01", `<img src="/api/v1/files/media/m1/m1.png"><img src="https://cdn.example.com/only.jpg?w=1">`, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertImportedMedia(user.ID, "m1", "m1.png", "m1", "", []string{stored.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertImportedMedia(user.ID, "trashed", "t.png", "t", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TrashMedia("trashed", user.ID, TrashInfo{By: "u", Reason: MediaReasonUser, Trigger: MediaTriggerManual}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.UpsertDiary(other.ID, "2024-01-01", `<img src="https://cdn.example.com/other.png">`, "", ""); err != nil {
		t.Fatal(err)
	}

	items, total, err := s.ListGallery(user.ID, 1, 10)
	if err != nil || total != 3 || len(items) != 3 {
		t.Fatalf("ListGallery = %d items, total %d, %v", len(items), total, err)
	}
	if items[0].Kind != GalleryKindMedia || items[0].ID != "m1" || !items[0].Managed || items[0].Date != "2024-06-01 00:00:00.000Z" {
		t.Fatalf("first = %+v", items[0])
	}
	if items[1].Kind != GalleryKindExternal || items[1].URL != "https://cdn.example.com/only.jpg?w=1" || items[1].Managed || items[1].Name != "only.jpg" {
		t.Fatalf("second = %+v", items[1])
	}
	shared := items[2]
	diaries, _ := shared.Expand["diary"].([]Diary)
	if shared.URL != "https://cdn.example.com/images/shared.png" || shared.Date != "2023-01-10 00:00:00.000Z" || len(diaries) != 3 || !strings.HasPrefix(diaries[0].Date, "2023-01-10") {
		t.Fatalf("shared = %+v diaries %+v", shared, diaries)
	}
	if page, total, _ := s.ListGallery(user.ID, 2, 2); total != 3 || len(page) != 1 || page[0].URL != shared.URL {
		t.Fatalf("page 2 = %+v", page)
	}
	if n, _ := s.CountExternalImages(user.ID); n != 2 {
		t.Fatalf("external = %d", n)
	}
	if stats, _ := s.MediaStats(user.ID); stats.External != 2 {
		t.Fatalf("stats external = %d", stats.External)
	}
	// Defaults for page and size.
	if items, _, err := s.ListGallery(user.ID, 0, 0); err != nil || len(items) != 3 {
		t.Fatalf("defaults = %d %v", len(items), err)
	}
}

func TestExternalImageName(t *testing.T) {
	for in, want := range map[string]string{
		"https://x/a/b.png?w=1#f": "b.png",
		"https://x/dir/":          "dir",
		"b.png":                   "b.png",
		"?":                       "?",
	} {
		if got := externalImageName(in); got != want {
			t.Errorf("externalImageName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeS3PublicURL(t *testing.T) {
	for in, want := range map[string]string{"": "", " https://cdn.example.com/ ": "https://cdn.example.com", "http://h:9000/bucket": "http://h:9000/bucket"} {
		if got, err := NormalizeS3PublicURL(in); err != nil || got != want {
			t.Errorf("NormalizeS3PublicURL(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"cdn.example.com", "ftp://x", "https://", "https://x/?a=1", "https://x/#f", "https://u:p@x", "https://x/" + strings.Repeat("a", 500)} {
		if _, err := NormalizeS3PublicURL(in); err == nil {
			t.Errorf("NormalizeS3PublicURL(%q) should fail", in)
		}
	}
}

func TestMediaPublicURL(t *testing.T) {
	var heads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		heads.Add(1)
		if r.Method == http.MethodHead && r.URL.Path == "/photos/pre/media/m1/my photo.png" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	s := newTestStore(t)
	user := newTestUser(t, s)
	for key, value := range map[string]any{
		"image_upload.s3.bucket": "photos", "image_upload.s3.region": "us-east-1", "image_upload.s3.endpoint": server.URL,
		"image_upload.s3.access_key": "k", "image_upload.s3.secret": "s", "image_upload.s3.force_path_style": true,
		"image_upload.s3.prefix": "pre",
	} {
		_ = s.SetSetting(user.ID, key, value, false)
	}
	media := &Media{ID: "m1", File: "my photo.png", Owner: user.ID, Storage: MediaStorageS3, S3Prefix: "pre"}
	if _, ok := s.MediaPublicURL(media); ok {
		t.Fatal("no public URL configured")
	}
	_ = s.SetSetting(user.ID, "image_upload.s3.public_url", "https://cdn.example.com/", false)
	got, ok := s.MediaPublicURL(media)
	if !ok || got != "https://cdn.example.com/pre/media/m1/my%20photo.png" {
		t.Fatalf("MediaPublicURL = %q %v", got, ok)
	}
	before := heads.Load()
	if again, ok := s.MediaPublicURL(media); !ok || again != got || heads.Load() != before {
		t.Fatal("the object key should be remembered")
	}
	// A missing object (a variant not generated yet) is not linked, and not
	// looked up again right away.
	variant := &Media{ID: "m1", File: "my photo.th.png", Owner: user.ID, Storage: MediaStorageS3, S3Prefix: "pre"}
	if _, ok := s.MediaPublicURL(variant); ok {
		t.Fatal("missing variant should not get a public URL")
	}
	before = heads.Load()
	if _, ok := s.MediaPublicURL(variant); ok || heads.Load() != before {
		t.Fatal("a recent miss should be remembered")
	}
	s.forgetPublicURLs(media)
	if _, ok := s.publicKeys.Load("m1/my photo.png"); ok {
		t.Fatal("forgetPublicURLs should drop the entry")
	}
	if _, ok := s.MediaPublicURL(&Media{ID: "l", File: "l.png", Owner: user.ID, Storage: MediaStorageLocal}); ok {
		t.Fatal("local files have no public URL")
	}
	if _, ok := s.MediaPublicURL(nil); ok {
		t.Fatal("nil media has no public URL")
	}
}
