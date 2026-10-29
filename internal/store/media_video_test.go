package store

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/songtianlun/diarum/internal/imaging"
)

var testVideoData = append([]byte("\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isomiso2"), bytes.Repeat([]byte{4}, 1000)...)

// videoObjectServer serves one object at key, with HEAD and ranged GETs
// ("bytes=N-"). A GET for a key it does not have fails with 404.
type videoObjectServer struct {
	key  string
	data []byte
	// cut, when > 0, ends each GET body after that many bytes.
	cut    int
	ranges []string
}

func (v *videoObjectServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/"+v.key) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `<Error><Code>NoSuchKey</Code></Error>`)
		return
	}
	start := 0
	if from, ok := strings.CutPrefix(r.Header.Get("Range"), "bytes="); ok {
		start, _ = strconv.Atoi(strings.TrimSuffix(from, "-"))
		v.ranges = append(v.ranges, from)
	}
	body := v.data[start:]
	if v.cut > 0 && len(body) > v.cut && r.Method == http.MethodGet {
		body = body[:v.cut]
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", strconv.Itoa(len(v.data)))
		return
	}
	if start > 0 {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, start+len(body)-1, len(v.data)))
		w.WriteHeader(http.StatusPartialContent)
	}
	_, _ = w.Write(body)
}

func useS3Server(t *testing.T, s *Store, owner string, handler http.Handler) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	for key, value := range map[string]any{
		"image_upload.provider":            "s3",
		"image_upload.s3.bucket":           "videos",
		"image_upload.s3.region":           "us-east-1",
		"image_upload.s3.endpoint":         server.URL,
		"image_upload.s3.access_key":       "key",
		"image_upload.s3.secret":           "secret",
		"image_upload.s3.force_path_style": true,
	} {
		if err := s.SetSetting(owner, key, value, false); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSaveMediaPoster(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	if PosterMedia(nil) != nil || PosterMedia(&Media{File: "a.mp4"}) != nil {
		t.Fatal("no poster expected")
	}
	if err := s.SaveMediaPoster(nil, nil); err == nil {
		t.Fatal("nil media should fail")
	}
	media := newStoredMedia(t, s, user.ID, "clip.mp4", testVideoData)
	if !media.IsVideo() {
		t.Fatal("clip.mp4 is a video")
	}
	if err := s.SaveMediaPoster(media, bytes.NewReader(make([]byte, imaging.MaxPosterBytes+1))); !errors.Is(err, ErrPosterTooLarge) {
		t.Fatalf("oversized poster = %v", err)
	}
	if err := s.SaveMediaPoster(media, failingReader{}); err == nil {
		t.Fatal("read failure should fail")
	}
	if err := s.SaveMediaPoster(media, strings.NewReader("jpeg")); err != nil {
		t.Fatal(err)
	}
	stored, _ := s.GetMedia(media.ID, user.ID)
	if stored.Poster != "clip.poster.jpg" || media.Poster != stored.Poster {
		t.Fatalf("poster = %q / %q", stored.Poster, media.Poster)
	}
	posterPath := s.MediaFilePath(PosterMedia(stored))
	if data, err := os.ReadFile(posterPath); err != nil || string(data) != "jpeg" {
		t.Fatalf("poster file = %q %v", data, err)
	}
	if err := s.DeleteMediaFile(stored); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(posterPath); !os.IsNotExist(err) {
		t.Fatalf("poster left behind: %v", err)
	}

	// A database failure removes the file again.
	if _, err := s.DB.Exec(`CREATE TRIGGER block_poster BEFORE UPDATE OF poster ON media BEGIN SELECT RAISE(ABORT, 'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	other := newStoredMedia(t, s, user.ID, "other.webm", testVideoData)
	if err := s.SaveMediaPoster(other, strings.NewReader("jpeg")); err == nil {
		t.Fatal("database failure should fail")
	}
	if _, err := os.Stat(companionPath(s, other, "other.poster.jpg")); !os.IsNotExist(err) {
		t.Fatalf("poster of failed save left behind: %v", err)
	}
}

func companionPath(s *Store, media *Media, file string) string {
	companion := *media
	companion.File = file
	return s.MediaFilePath(&companion)
}

func TestOpenMediaSeekerLocal(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	media := newStoredMedia(t, s, user.ID, "clip.mp4", testVideoData)
	reader, err := s.OpenMediaSeeker(media)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err := reader.Seek(100, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(reader)
	if !bytes.Equal(got, testVideoData[100:]) {
		t.Fatal("local seek read wrong bytes")
	}
	if _, err := s.OpenMediaSeeker(&Media{ID: "missing", File: "x.mp4"}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing = %v", err)
	}
}

func TestOpenMediaSeekerS3(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	media := &Media{ID: "vid1", File: "clip.mp4", Owner: user.ID, Storage: MediaStorageS3}
	object := &videoObjectServer{key: "media/vid1/clip.mp4", data: testVideoData}
	useS3Server(t, s, user.ID, object)

	reader, err := s.OpenMediaSeeker(media)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if size, err := reader.Seek(0, io.SeekEnd); err != nil || size != int64(len(testVideoData)) {
		t.Fatalf("size = %d %v", size, err)
	}
	if _, err := reader.Seek(-int64(len(testVideoData))+10, io.SeekCurrent); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 20)
	if _, err := io.ReadFull(reader, buf); err != nil || !bytes.Equal(buf, testVideoData[10:30]) {
		t.Fatalf("read = %v", err)
	}
	// Reading on continues the same stream; seeking starts a new one.
	if _, err := io.ReadFull(reader, buf); err != nil || !bytes.Equal(buf, testVideoData[30:50]) {
		t.Fatalf("read on = %v", err)
	}
	if pos, _ := reader.Seek(0, io.SeekCurrent); pos != 50 {
		t.Fatalf("position = %d", pos)
	}
	if _, err := reader.Seek(500, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	rest, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(rest, testVideoData[500:]) {
		t.Fatalf("rest = %d bytes %v", len(rest), err)
	}
	if n, err := reader.Read(buf); n != 0 || err != io.EOF {
		t.Fatalf("read at end = %d %v", n, err)
	}
	if len(object.ranges) != 2 || object.ranges[0] != "10-" || object.ranges[1] != "500-" {
		t.Fatalf("ranged GETs = %v", object.ranges)
	}
	if _, err := reader.Seek(0, 7); err == nil {
		t.Fatal("bad whence should fail")
	}
	if _, err := reader.Seek(-1, io.SeekStart); err == nil {
		t.Fatal("negative position should fail")
	}

	// A stream that ends early is picked up again from where it stopped.
	object.cut = 300
	object.ranges = nil
	again, _ := s.OpenMediaSeeker(media)
	all, err := io.ReadAll(again)
	_ = again.Close()
	if err != nil || !bytes.Equal(all, testVideoData) {
		t.Fatalf("resumed read = %d bytes %v", len(all), err)
	}
	if len(object.ranges) < 3 {
		t.Fatalf("ranged GETs after cuts = %v", object.ranges)
	}

	// An object that is not there is reported missing.
	if _, err := s.OpenMediaSeeker(&Media{ID: "other", File: "x.mp4", Owner: user.ID}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing object = %v", err)
	}
}

func TestOpenMediaSeekerLegacyS3(t *testing.T) {
	s := newTestStore(t)
	object := &videoObjectServer{key: "media/vid2/clip.webm", data: testVideoData}
	server := httptest.NewServer(object)
	t.Cleanup(server.Close)
	s.LegacyS3 = &LegacyS3Config{Enabled: true, Bucket: "legacy", Region: "us-east-1", Endpoint: server.URL, AccessKey: "k", Secret: "s", ForcePathStyle: true}
	if err := s.initLegacyS3Client(); err != nil {
		t.Fatal(err)
	}
	reader, err := s.OpenMediaSeeker(&Media{ID: "vid2", File: "clip.webm"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(reader)
	_ = reader.Close()
	if !bytes.Equal(got, testVideoData) {
		t.Fatal("legacy read differs")
	}

	// A failing GET surfaces as a read error.
	object.key = "gone"
	reader, err = s.openS3Seeker(s.legacyS3Client, s.LegacyS3, &Media{ID: "vid2", File: "clip.webm"})
	if err == nil {
		_ = reader.Close()
		t.Fatal("missing object should fail")
	}
	seeker := &s3Seeker{client: s.legacyS3Client, bucket: "legacy", key: "media/vid2/clip.webm", size: 10}
	if _, err := seeker.Read(make([]byte, 4)); err == nil {
		t.Fatal("failing GET should fail")
	}
	if _, err := s.openS3Seeker(nil, nil, &Media{}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("no client = %v", err)
	}
}

func TestS3SeekerResumesChunkedStreams(t *testing.T) {
	// Chunked responses (no Content-Length) end with a separate (0, EOF)
	// read; each one stops after 100 bytes.
	gets := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(testVideoData)))
			return
		}
		gets++
		from, _ := strings.CutPrefix(r.Header.Get("Range"), "bytes=")
		start, _ := strconv.Atoi(strings.TrimSuffix(from, "-"))
		end := min(start+100, len(testVideoData))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(testVideoData[start:end])
		w.(http.Flusher).Flush()
	})
	s := newTestStore(t)
	user := newTestUser(t, s)
	useS3Server(t, s, user.ID, handler)

	reader, err := s.OpenMediaSeeker(&Media{ID: "v", File: "a.mp4", Owner: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	all, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(all, testVideoData) {
		t.Fatalf("read = %d bytes %v", len(all), err)
	}
	if want := (len(testVideoData) + 99) / 100; gets != want {
		t.Fatalf("GETs = %d, want %d", gets, want)
	}
}
