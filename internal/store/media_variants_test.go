package store

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/songtianlun/diarum/internal/imaging"
)

func largePNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2000, 1500))); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func newStoredMedia(t *testing.T, s *Store, owner, file string, data []byte) *Media {
	t.Helper()
	media, err := s.CreateMedia(owner, file, "Photo", "", nil)
	if err != nil {
		t.Fatalf("CreateMedia: %v", err)
	}
	if err := s.SaveUploadedMedia(media, bytes.NewReader(data)); err != nil {
		t.Fatalf("SaveUploadedMedia: %v", err)
	}
	return media
}

func TestGenerateAndOpenMediaVariants(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	media := newStoredMedia(t, s, user.ID, "photo.png", largePNG(t))

	if path, ok := s.MediaVariantPath(media, imaging.Thumb); ok {
		t.Fatalf("variant should not exist before generation: %s", path)
	}
	if _, err := s.OpenMediaVariant(media, imaging.Thumb); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OpenMediaVariant before generation error = %v", err)
	}

	if err := s.GenerateMediaVariants(media); err != nil {
		t.Fatalf("GenerateMediaVariants: %v", err)
	}
	for _, v := range imaging.Variants {
		path, ok := s.MediaVariantPath(media, v)
		if !ok {
			t.Fatalf("variant %s missing at %s", v, path)
		}
		if filepath.Dir(path) != filepath.Dir(s.MediaFilePath(media)) {
			t.Fatalf("variant %s stored at %s, want beside original", v, path)
		}
		reader, err := s.OpenMediaVariant(media, v)
		if err != nil {
			t.Fatalf("OpenMediaVariant %s: %v", v, err)
		}
		data, _ := io.ReadAll(reader)
		_ = reader.Close()
		if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
			t.Fatalf("variant %s is not a valid image: %v", v, err)
		}
	}
}

func TestGenerateMediaVariantsErrorsAndSkips(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)

	if err := s.GenerateMediaVariants(&Media{ID: "gif", File: "anim.gif", Owner: user.ID}); err != nil {
		t.Fatalf("unsupported format should be skipped, got %v", err)
	}
	if err := s.GenerateMediaVariants(&Media{ID: "missing", File: "photo.png", Owner: user.ID}); err == nil || !strings.Contains(err.Error(), "open original") {
		t.Fatalf("missing original error = %v", err)
	}
	broken := newStoredMedia(t, s, user.ID, "broken.png", []byte("not an image"))
	if err := s.GenerateMediaVariants(broken); err == nil {
		t.Fatal("GenerateMediaVariants should fail on undecodable data")
	}

	// Saving fails when the variant path is taken by a directory.
	media := newStoredMedia(t, s, user.ID, "photo.png", largePNG(t))
	blocked := filepath.Join(filepath.Dir(s.MediaFilePath(media)), imaging.VariantFilename(media.File, imaging.Medium))
	if err := os.MkdirAll(filepath.Join(blocked, "child"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := s.GenerateMediaVariants(media); err == nil || !strings.Contains(err.Error(), "save md variant") {
		t.Fatalf("blocked variant error = %v", err)
	}
}

func TestSaveMediaVariantDestinations(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)

	// No local original: falls back to the user's local media directory.
	media := &Media{ID: "remote", File: "photo.png", Owner: user.ID}
	if err := s.saveMediaVariant(media, imaging.Thumb, []byte("thumb")); err != nil {
		t.Fatalf("saveMediaVariant local fallback: %v", err)
	}
	want := filepath.Join(s.userLocalMediaDir(user.ID), media.ID, imaging.VariantFilename(media.File, imaging.Thumb))
	if data, err := os.ReadFile(want); err != nil || string(data) != "thumb" {
		t.Fatalf("fallback variant = %q, %v", data, err)
	}

	// S3 provider: the variant is uploaded next to where the original would be.
	var puts atomic.Int32
	var key atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts.Add(1)
			key.Store(r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	for k, v := range map[string]any{
		"image_upload.provider":            "s3",
		"image_upload.s3.bucket":           "bucket",
		"image_upload.s3.region":           "us-east-1",
		"image_upload.s3.endpoint":         server.URL,
		"image_upload.s3.access_key":       "key",
		"image_upload.s3.secret":           "secret",
		"image_upload.s3.force_path_style": true,
	} {
		if err := s.SetSetting(user.ID, k, v, false); err != nil {
			t.Fatalf("SetSetting %s: %v", k, err)
		}
	}
	remote := &Media{ID: "s3media", File: "photo.png", Owner: user.ID}
	if err := s.saveMediaVariant(remote, imaging.Medium, []byte("medium")); err != nil {
		t.Fatalf("saveMediaVariant s3: %v", err)
	}
	if puts.Load() != 1 {
		t.Fatalf("S3 PUT count = %d, want 1", puts.Load())
	}
	if path, _ := key.Load().(string); !strings.HasSuffix(path, "/"+remote.ID+"/"+imaging.VariantFilename(remote.File, imaging.Medium)) {
		t.Fatalf("S3 key = %q", path)
	}
}

func TestQueueMediaVariants(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)

	// No-ops: nil media and formats without variants.
	s.QueueMediaVariants(nil)
	s.QueueMediaVariants(&Media{ID: "gif", File: "anim.gif", Owner: user.ID})
	s.WaitMediaVariants()

	media := newStoredMedia(t, s, user.ID, "photo.png", largePNG(t))
	s.QueueMediaVariants(media)
	s.QueueMediaVariants(media) // deduplicated while running or already attempted
	s.WaitMediaVariants()
	if _, ok := s.MediaVariantPath(media, imaging.Thumb); !ok {
		t.Fatal("queued job should have generated the thumbnail")
	}

	// Failures are logged, not returned, and the media is not retried.
	missing := &Media{ID: "missing", File: "photo.png", Owner: user.ID}
	s.QueueMediaVariants(missing)
	s.WaitMediaVariants()
	s.variants.mu.Lock()
	running, attempted := s.variants.running[missing.ID], s.variants.attempted[missing.ID]
	s.variants.mu.Unlock()
	if running || !attempted {
		t.Fatalf("missing media running=%v attempted=%v", running, attempted)
	}
}
