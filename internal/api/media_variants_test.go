package api

import (
	"bytes"
	"github.com/songtianlun/diarum/internal/medialib"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/imaging"
	"github.com/songtianlun/diarum/internal/store"
)

func largeJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2000, 1500))
	for y := 0; y < 1500; y += 10 {
		for x := 0; x < 2000; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 120, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func imageWidth(t *testing.T, body []byte) int {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("decode served image: %v", err)
	}
	return cfg.Width
}

func TestMediaVariantsRoute(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	RegisterMediaRoutes(e, s, authMiddlewareFor(user))

	body, contentType := multipartRequestBody(t, "file", "photo.jpg", largeJPEG(t), nil)
	rec := performRequest(t, e, http.MethodPost, "/api/v1/media", body, map[string]string{"Content-Type": contentType})
	if rec.Code != http.StatusOK {
		t.Fatalf("upload status = %d body=%s", rec.Code, rec.Body.String())
	}
	id := decodeJSONBody(t, rec)["id"].(string)
	media, err := s.GetMedia(id, user.ID)
	if err != nil {
		t.Fatalf("GetMedia: %v", err)
	}
	s.WaitMediaVariants()

	base := "/api/v1/files/media/" + id + "/"
	for name, want := range map[string]int{media.File: 2000, imaging.VariantFilename(media.File, imaging.Medium): 1440, imaging.VariantFilename(media.File, imaging.Thumb): 480} {
		rec = performRequest(t, e, http.MethodGet, base+name, nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d", name, rec.Code)
		}
		if got := imageWidth(t, rec.Body.Bytes()); got != want {
			t.Fatalf("GET %s width = %d, want %d", name, got, want)
		}
		if cache := rec.Header().Get("Cache-Control"); !strings.Contains(cache, "immutable") {
			t.Fatalf("GET %s Cache-Control = %q", name, cache)
		}
	}
	if rec = performRequest(t, e, http.MethodGet, base+"photo.xl.jpg", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown variant status = %d", rec.Code)
	}

	// A missing variant serves the original uncached and is rebuilt lazily.
	mediumPath, ok := s.MediaVariantPath(media, imaging.Medium)
	if !ok {
		t.Fatal("medium variant should be stored beside the original")
	}
	if err := os.Remove(mediumPath); err != nil {
		t.Fatalf("remove variant: %v", err)
	}
	// Variants of this media were already attempted in this process; a fresh
	// store stands in for a restart.
	fresh, err := store.Open(s.DataDir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() { _ = fresh.Close() })
	e2 := echo.New()
	RegisterMediaRoutes(e2, fresh, authMiddlewareFor(user))
	rec = performRequest(t, e2, http.MethodGet, base+imaging.VariantFilename(media.File, imaging.Medium), nil, nil)
	if rec.Code != http.StatusOK || imageWidth(t, rec.Body.Bytes()) != 2000 || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("fallback: status=%d cache=%q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	fresh.WaitMediaVariants()
	if _, ok := fresh.MediaVariantPath(media, imaging.Medium); !ok {
		t.Fatal("missing variant should be regenerated in the background")
	}

	// Purging the media from the trash removes its variants too.
	rec = performRequest(t, e, http.MethodDelete, "/api/v1/media/"+id, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d", rec.Code)
	}
	for _, v := range imaging.Variants {
		if _, ok := s.MediaVariantPath(media, v); !ok {
			t.Fatalf("%s variant should stay while the image is in the trash", v)
		}
	}
	RegisterMediaLibraryRoutes(e, s, authMiddlewareFor(user), medialib.New(s, nil))
	if result := purgeMediaForTest(t, e, id); len(result.Done) != 1 {
		t.Fatalf("purge = %+v", result)
	}
	for _, v := range imaging.Variants {
		if _, ok := s.MediaVariantPath(media, v); ok {
			t.Fatalf("%s variant left behind after delete", v)
		}
	}
}

func TestMediaVariantsUnsupportedFormatServesOriginal(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	RegisterMediaRoutes(e, s, authMiddlewareFor(user))

	body, contentType := multipartRequestBody(t, "file", "icon.png", pngBytes(), nil)
	rec := performRequest(t, e, http.MethodPost, "/api/v1/media", body, map[string]string{"Content-Type": contentType})
	if rec.Code != http.StatusOK {
		t.Fatalf("upload status = %d", rec.Code)
	}
	payload := decodeJSONBody(t, rec)
	s.WaitMediaVariants()
	file := payload["file"].(string)
	// A tiny image gets no variants; its variant URLs still work.
	rec = performRequest(t, e, http.MethodGet, "/api/v1/files/media/"+payload["id"].(string)+"/"+strings.TrimSuffix(file, ".png")+".th.png", nil, nil)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), pngBytes()) {
		t.Fatalf("small image thumb status = %d", rec.Code)
	}
}

func TestImageDisplayQualityRoutes(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	RegisterImageUploadRoutes(e, s, authMiddlewareFor(user))

	rec := performRequest(t, e, http.MethodGet, "/api/v1/image-upload/display", nil, nil)
	if payload := decodeJSONBody(t, rec); payload["quality"] != "md" {
		t.Fatalf("default quality = %#v", payload)
	}
	rec = performRequest(t, e, http.MethodPut, "/api/v1/image-upload/display", strings.NewReader(`{"quality":"huge"}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid quality status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPut, "/api/v1/image-upload/display", strings.NewReader(`{"quality":"Original"}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("save quality status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/image-upload/display", nil, nil)
	if payload := decodeJSONBody(t, rec); payload["quality"] != "original" || payload["live_mode"] != "loop" {
		t.Fatalf("saved quality = %#v", payload)
	}

	// The default live photo mode is saved on its own, keeping the quality.
	for body, want := range map[string]int{
		`{}`:                     http.StatusBadRequest,
		`{"live_mode":"bounce"}`: http.StatusBadRequest,
		`not json`:               http.StatusBadRequest,
		`{"live_mode":" Once "}`: http.StatusOK,
	} {
		rec = performRequest(t, e, http.MethodPut, "/api/v1/image-upload/display", strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
		if rec.Code != want {
			t.Fatalf("PUT %s status = %d, want %d", body, rec.Code, want)
		}
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/image-upload/display", nil, nil)
	if payload := decodeJSONBody(t, rec); payload["quality"] != "original" || payload["live_mode"] != "once" {
		t.Fatalf("saved live mode = %#v", payload)
	}
}

func TestLegacyPocketBaseFileURLRedirects(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	RegisterMediaRoutes(e, s, authMiddlewareFor(user))
	s.MediaCollectionID = "keh96zmpl3s9bzl"

	for _, collection := range []string{"media/keh96zmpl3s9bzl", "keh96zmpl3s9bzl", store.DefaultMediaCollectionID} {
		rec := performRequest(t, e, http.MethodGet, "/api/files/"+collection+"/t5of26zbp059kwo/image_6isNjx6PT6.png?thumb=undefined", nil, nil)
		if rec.Code != http.StatusMovedPermanently {
			t.Fatalf("%s: status = %d", collection, rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/api/v1/files/media/t5of26zbp059kwo/image_6isNjx6PT6.png" {
			t.Fatalf("%s: Location = %q", collection, loc)
		}
	}
	for _, path := range []string{"/api/files/users/abc/avatar.png", "/api/files/media/abc", "/api/files/media/a/b/c/d.png"} {
		if rec := performRequest(t, e, http.MethodGet, path, nil, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d", path, rec.Code)
		}
	}
}
