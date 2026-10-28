package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/medialib"
	"github.com/songtianlun/diarum/internal/store"
)

// testVideo is an MP4 (ISO base media, "isom" brand) as far as sniffing goes.
var testVideo = append([]byte("\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isomiso2"), bytes.Repeat([]byte{7}, 4096)...)

func uploadVideo(t *testing.T, e *echo.Echo, name string, video, poster []byte) (*store.Media, int, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", name)
	_, _ = part.Write(video)
	if poster != nil {
		part, _ = writer.CreateFormFile("poster", "poster.jpg")
		_, _ = part.Write(poster)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	rec := performRequest(t, e, http.MethodPost, "/api/v1/media", &body, map[string]string{"Content-Type": writer.FormDataContentType()})
	if rec.Code != http.StatusOK {
		return nil, rec.Code, rec.Body.String()
	}
	var media store.Media
	if err := json.Unmarshal(rec.Body.Bytes(), &media); err != nil {
		t.Fatal(err)
	}
	return &media, rec.Code, ""
}

func TestVideoUploadAndStream(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	auth := authMiddlewareFor(user)
	RegisterMediaRoutes(e, s, auth)
	RegisterMediaLibraryRoutes(e, s, auth, medialib.New(s, nil))

	poster := jpegBytes(t, 64, 36)
	media, code, msg := uploadVideo(t, e, "trip.m4v", testVideo, poster)
	if media == nil {
		t.Fatalf("upload = %d %s", code, msg)
	}
	if media.File != "trip.m4v" || media.Poster != "trip.poster.jpg" || !media.IsVideo() {
		t.Fatalf("media = %+v", media)
	}

	url := "/api/v1/files/media/" + media.ID + "/" + media.File
	rec := performRequest(t, e, http.MethodGet, url, nil, nil)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), testVideo) || rec.Header().Get("Content-Type") != "video/mp4" || rec.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("serve = %d %q (%d bytes)", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
	}
	rec = performRequest(t, e, http.MethodGet, url, nil, map[string]string{"Range": "bytes=100-199"})
	if rec.Code != http.StatusPartialContent || !bytes.Equal(rec.Body.Bytes(), testVideo[100:200]) {
		t.Fatalf("range = %d (%d bytes)", rec.Code, rec.Body.Len())
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/files/media/"+media.ID+"/"+media.Poster, nil, nil)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), poster) {
		t.Fatalf("poster = %d", rec.Code)
	}

	// An entry showing the video keeps it out of the unused scan.
	content := `<p>trip</p><video src="` + url + `" poster="/api/v1/files/media/` + media.ID + `/` + media.Poster + `" controls=""></video>`
	diary, _, err := s.UpsertDiary(user.ID, "2026-10-01 00:00:00.000Z", content, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if images := store.ExtractDiaryImages(content); len(images) != 1 || images[0].MediaID != media.ID {
		t.Fatalf("extracted = %+v", images)
	}
	if err := s.LinkDiaryMedia(user.ID, diary.ID, content); err != nil {
		t.Fatal(err)
	}
	unlinked, err := s.UnlinkedMedia(user.ID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(unlinked) != 0 {
		t.Fatalf("linked video reported unused: %+v", unlinked)
	}

	// Purging removes the poster with the video.
	posterPath := s.MediaFilePath(store.PosterMedia(media))
	if rec := performRequest(t, e, http.MethodDelete, "/api/v1/media/"+media.ID, nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete = %d", rec.Code)
	}
	if result := purgeMediaForTest(t, e, media.ID); len(result.Done) != 1 {
		t.Fatalf("purge = %+v", result)
	}
	if _, err := os.Stat(posterPath); !os.IsNotExist(err) {
		t.Fatalf("poster left behind: %v", err)
	}
}

func TestVideoUploadNamesAndLimits(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	RegisterMediaRoutes(e, s, authMiddlewareFor(user))

	// The extension follows the container, so the type served is playable.
	webm := append([]byte{0x1A, 0x45, 0xDF, 0xA3}, bytes.Repeat([]byte{1}, 512)...)
	cases := map[string][]byte{"clip.mov": testVideo, "clip.mkv": webm, "clip.bin": testVideo}
	want := map[string]string{"clip.mov": "clip.mov", "clip.mkv": "clip.webm", "clip.bin": "clip.mp4"}
	for name, data := range cases {
		media, code, msg := uploadVideo(t, e, name, data, nil)
		if media == nil {
			t.Fatalf("%s: upload = %d %s", name, code, msg)
		}
		if media.File != want[name] || media.Poster != "" {
			t.Fatalf("%s: stored as %q (poster %q)", name, media.File, media.Poster)
		}
	}

	// A poster that is not an image is dropped; the video is kept.
	media, _, _ := uploadVideo(t, e, "a.mp4", testVideo, []byte("<svg></svg>"))
	if media == nil || media.Poster != "" {
		t.Fatalf("bad poster = %+v", media)
	}
}

func TestVideoStreamsFromS3InRanges(t *testing.T) {
	bucket, server := newFakeS3(t, "photos")
	s := newTestStore(t)
	user := newTestUser(t, s)
	for key, value := range map[string]any{
		"image_upload.provider":            "s3",
		"image_upload.s3.bucket":           "photos",
		"image_upload.s3.region":           "us-east-1",
		"image_upload.s3.endpoint":         server.URL,
		"image_upload.s3.access_key":       "key",
		"image_upload.s3.secret":           "secret",
		"image_upload.s3.force_path_style": true,
	} {
		if err := s.SetSetting(user.ID, key, value, false); err != nil {
			t.Fatal(err)
		}
	}
	e := echo.New()
	RegisterMediaRoutes(e, s, authMiddlewareFor(user))

	media, code, msg := uploadVideo(t, e, "IMG_0002.MOV", testVideo, jpegBytes(t, 32, 18))
	if media == nil {
		t.Fatalf("upload = %d %s", code, msg)
	}
	bucket.mu.Lock()
	storedType := bucket.types["media/"+media.ID+"/IMG_0002.MOV"]
	_, hasPoster := bucket.objects["media/"+media.ID+"/IMG_0002.poster.jpg"]
	bucket.mu.Unlock()
	if storedType != "video/mp4" || !hasPoster {
		t.Fatalf("stored type = %q, poster = %v, keys = %v", storedType, hasPoster, bucket.keys())
	}

	url := "/api/v1/files/media/" + media.ID + "/" + media.File
	rec := performRequest(t, e, http.MethodGet, url, nil, map[string]string{"Range": "bytes=1000-1099"})
	if rec.Code != http.StatusPartialContent || !bytes.Equal(rec.Body.Bytes(), testVideo[1000:1100]) || rec.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("range = %d %q (%d bytes)", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
	}
	rec = performRequest(t, e, http.MethodGet, url, nil, nil)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), testVideo) {
		t.Fatalf("full = %d (%d bytes)", rec.Code, rec.Body.Len())
	}
	bucket.mu.Lock()
	ranges := bucket.ranges
	bucket.mu.Unlock()
	if ranges < 2 {
		t.Fatalf("object read whole instead of in ranges (%d ranged GETs)", ranges)
	}
}
