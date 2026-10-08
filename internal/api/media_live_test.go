package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"slices"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/medialib"
	"github.com/songtianlun/diarum/internal/store"
)

var testLiveClip = append([]byte("\x00\x00\x00\x14ftypqt  \x00\x00\x00\x00"), bytes.Repeat([]byte{9}, 2048)...)

// livePhotoBody is an upload form with a still and, optionally, its clip.
func livePhotoBody(t *testing.T, still, clip []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "IMG_0001.jpg")
	_, _ = part.Write(still)
	if clip != nil {
		part, _ = writer.CreateFormFile("live", "IMG_0001.MOV")
		_, _ = part.Write(clip)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, writer.FormDataContentType()
}

func uploadLivePhoto(t *testing.T, e *echo.Echo, still, clip []byte) (*store.Media, int, string) {
	t.Helper()
	body, contentType := livePhotoBody(t, still, clip)
	rec := performRequest(t, e, http.MethodPost, "/api/v1/media", body, map[string]string{"Content-Type": contentType})
	if rec.Code != http.StatusOK {
		return nil, rec.Code, rec.Body.String()
	}
	var media store.Media
	if err := json.Unmarshal(rec.Body.Bytes(), &media); err != nil {
		t.Fatal(err)
	}
	return &media, rec.Code, ""
}

func TestLivePhotoUploadAndServe(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	auth := authMiddlewareFor(user)
	RegisterMediaRoutes(e, s, auth)
	RegisterMediaLibraryRoutes(e, s, auth, medialib.New(s, nil))

	media, code, msg := uploadLivePhoto(t, e, jpegBytes(t, 64, 48), testLiveClip)
	if media == nil {
		t.Fatalf("upload = %d %s", code, msg)
	}
	if media.Live != "IMG_0001.live.mp4" {
		t.Fatalf("live = %q", media.Live)
	}
	s.WaitMediaVariants()

	url := "/api/v1/files/media/" + media.ID + "/" + media.Live
	rec := performRequest(t, e, http.MethodGet, url, nil, nil)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), testLiveClip) || rec.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("serve clip = %d %q (%d bytes)", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
	}
	// Browsers fetch video in ranges.
	rec = performRequest(t, e, http.MethodGet, url, nil, map[string]string{"Range": "bytes=0-9"})
	if rec.Code != http.StatusPartialContent || rec.Body.Len() != 10 {
		t.Fatalf("range = %d (%d bytes)", rec.Code, rec.Body.Len())
	}

	// Purging removes the clip with the still.
	clipPath := s.MediaFilePath(store.LiveMedia(media))
	if rec := performRequest(t, e, http.MethodDelete, "/api/v1/media/"+media.ID, nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete = %d", rec.Code)
	}
	if result := purgeMediaForTest(t, e, media.ID); len(result.Done) != 1 {
		t.Fatalf("purge = %+v", result)
	}
	if _, err := os.Stat(clipPath); !os.IsNotExist(err) {
		t.Fatalf("clip left behind: %v", err)
	}

	// A plain still has no clip to serve.
	still, _, _ := uploadLivePhoto(t, e, jpegBytes(t, 64, 48), nil)
	if still.Live != "" {
		t.Fatalf("still live = %q", still.Live)
	}
	if rec := performRequest(t, e, http.MethodGet, "/api/v1/files/media/"+still.ID+"/IMG_0001.live.mp4", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("clip of a still = %d", rec.Code)
	}
}

func TestLivePhotoUploadRejectsInvalidClip(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	RegisterMediaRoutes(e, s, authMiddlewareFor(user))

	if media, code, _ := uploadLivePhoto(t, e, jpegBytes(t, 64, 48), jpegBytes(t, 8, 8)); media != nil || code != http.StatusBadRequest {
		t.Fatalf("image as clip = %d", code)
	}
	if total := s.CountMedia(user.ID); total != 0 {
		t.Fatalf("rejected upload left %d media behind", total)
	}
}

func TestSaveLiveVideoErrors(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	media, err := s.CreateMedia(user.ID, "a.jpg", "a", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveUploadedMedia(media, bytes.NewReader(jpegBytes(t, 8, 8))); err != nil {
		t.Fatal(err)
	}
	if err := saveLiveVideo(s, media, bytes.NewReader(testLiveClip), 51<<20); err == nil {
		t.Fatal("oversized clip should be rejected")
	}
	if err := saveLiveVideo(s, media, failingSeeker{bytes.NewReader(testLiveClip)}, int64(len(testLiveClip))); err == nil {
		t.Fatal("unseekable clip should fail")
	}
	if _, err := s.DB.Exec(`CREATE TRIGGER block_live BEFORE UPDATE OF live ON media BEGIN SELECT RAISE(ABORT, 'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	if err := saveLiveVideo(s, media, bytes.NewReader(testLiveClip), int64(len(testLiveClip))); err == nil {
		t.Fatal("database failure should fail")
	}
}

type failingSeeker struct{ *bytes.Reader }

func (failingSeeker) Seek(int64, int) (int64, error) { return 0, os.ErrInvalid }

func TestLivePhotoOnS3(t *testing.T) {
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

	media, code, msg := uploadLivePhoto(t, e, jpegBytes(t, 64, 48), testLiveClip)
	if media == nil {
		t.Fatalf("upload = %d %s", code, msg)
	}
	s.WaitMediaVariants()
	key := "media/" + media.ID + "/IMG_0001.live.mp4"
	if !slices.Contains(bucket.keys(), key) {
		t.Fatalf("objects = %v", bucket.keys())
	}
	bucket.mu.Lock()
	storedType := bucket.types[key]
	bucket.mu.Unlock()
	if storedType != "video/mp4" {
		t.Fatalf("clip content type = %q", storedType)
	}

	url := "/api/v1/files/media/" + media.ID + "/" + media.Live
	rec := performRequest(t, e, http.MethodGet, url, nil, map[string]string{"Range": "bytes=2-5"})
	if rec.Code != http.StatusPartialContent || !bytes.Equal(rec.Body.Bytes(), testLiveClip[2:6]) || rec.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("range from S3 = %d %q", rec.Code, rec.Body.Bytes())
	}

	if err := s.SetSetting(user.ID, "image_upload.s3.public_url", "https://cdn.example.com", false); err != nil {
		t.Fatal(err)
	}
	rec = performRequest(t, e, http.MethodGet, url, nil, nil)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://cdn.example.com/"+key {
		t.Fatalf("public redirect = %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if target, ok := s.MediaURLPublicURL(user.ID, url); !ok || target != "https://cdn.example.com/"+key {
		t.Fatalf("share URL = %q %v", target, ok)
	}

	// A clip gone from the bucket is a 404, not a broken stream.
	bucket.mu.Lock()
	delete(bucket.objects, key)
	bucket.mu.Unlock()
	if rec := performRequest(t, e, http.MethodGet, url+"?direct=1", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing clip = %d", rec.Code)
	}
}
