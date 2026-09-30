package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/medialib"
	"github.com/songtianlun/diarum/internal/store"
)

// fakeS3 is a path-style S3 bucket in memory: enough of PutObject,
// GetObject, HeadObject and DeleteObject for the media library.
type fakeS3 struct {
	mu      sync.Mutex
	bucket  string
	objects map[string][]byte
}

func newFakeS3(t *testing.T, bucket string) (*fakeS3, *httptest.Server) {
	t.Helper()
	f := &fakeS3{bucket: bucket, objects: map[string][]byte{}}
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	return f, server
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bucket, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if bucket != f.bucket || key == "" {
		http.Error(w, "NoSuchBucket", http.StatusNotFound)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		f.objects[key] = body
		w.Header().Set("ETag", `"etag"`)
	case http.MethodGet, http.MethodHead:
		body, ok := f.objects[key]
		if !ok {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			if r.Method == http.MethodGet {
				_, _ = io.WriteString(w, `<Error><Code>NoSuchKey</Code></Error>`)
			}
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method == http.MethodGet {
			_, _ = w.Write(body)
		}
	case http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (f *fakeS3) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := make([]string, 0, len(f.objects))
	for key := range f.objects {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func jpegBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestMediaLifecycleOnS3WithPrefix(t *testing.T) {
	for _, prefix := range []string{"diarum/media", ""} {
		t.Run("prefix="+prefix, func(t *testing.T) {
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
				"image_upload.s3.prefix":           prefix,
			} {
				if err := s.SetSetting(user.ID, key, value, false); err != nil {
					t.Fatal(err)
				}
			}
			e := echo.New()
			auth := authMiddlewareFor(user)
			RegisterMediaRoutes(e, s, auth)
			RegisterMediaLibraryRoutes(e, s, auth, medialib.New(s, nil))

			original := jpegBytes(t, 1600, 900)
			body, contentType := multipartRequestBody(t, "file", "photo.jpg", original, nil)
			rec := performRequest(t, e, http.MethodPost, "/api/v1/media", body, map[string]string{"Content-Type": contentType})
			if rec.Code != http.StatusOK {
				t.Fatalf("upload = %d %s", rec.Code, rec.Body.String())
			}
			var media store.Media
			if err := json.Unmarshal(rec.Body.Bytes(), &media); err != nil {
				t.Fatal(err)
			}
			if stored, err := s.GetMedia(media.ID, user.ID); err != nil || stored.Storage != store.MediaStorageS3 || stored.S3Prefix != prefix {
				t.Fatalf("stored = %+v %v", stored, err)
			}
			s.WaitMediaVariants()

			root := "media/" + media.ID + "/"
			if prefix != "" {
				root = prefix + "/" + root
			}
			want := []string{root + "photo.jpg", root + "photo.md.jpg", root + "photo.th.jpg"}
			if got := bucket.keys(); !slices.Equal(got, want) {
				t.Fatalf("objects = %v, want %v", got, want)
			}

			url := "/api/v1/files/media/" + media.ID + "/photo.jpg"
			if rec := performRequest(t, e, http.MethodGet, url, nil, nil); rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), original) || rec.Header().Get("Content-Type") != "image/jpeg" {
				t.Fatalf("serve original = %d %q (%d bytes)", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
			}

			// Moving the prefix keeps older objects reachable.
			if err := s.SetSetting(user.ID, "image_upload.s3.prefix", "moved/elsewhere", false); err != nil {
				t.Fatal(err)
			}
			if rec := performRequest(t, e, http.MethodGet, url, nil, nil); rec.Code != http.StatusOK {
				t.Fatalf("serve after prefix change = %d", rec.Code)
			}

			// The trash keeps the objects and keeps serving them.
			if rec := performRequest(t, e, http.MethodDelete, "/api/v1/media/"+media.ID, nil, nil); rec.Code != http.StatusOK {
				t.Fatalf("delete = %d %s", rec.Code, rec.Body.String())
			}
			if got := bucket.keys(); len(got) != 3 {
				t.Fatalf("objects in trash = %v", got)
			}
			if rec := performRequest(t, e, http.MethodGet, url, nil, nil); rec.Code != http.StatusOK {
				t.Fatalf("serve trashed = %d", rec.Code)
			}

			if result := purgeMediaForTest(t, e, media.ID); len(result.Done) != 1 {
				t.Fatalf("purge = %+v", result)
			}
			if got := bucket.keys(); len(got) != 0 {
				t.Fatalf("objects after purge = %v", got)
			}
			if rec := performRequest(t, e, http.MethodGet, url, nil, nil); rec.Code != http.StatusNotFound {
				t.Fatalf("serve purged = %d", rec.Code)
			}
		})
	}
}
