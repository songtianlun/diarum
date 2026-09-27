package archive

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/songtianlun/diarum/internal/store"
)

var pngData = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 64)...)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newUser(t *testing.T, s *store.Store) *store.User {
	t.Helper()
	id, err := store.GenerateID()
	if err != nil {
		t.Fatalf("GenerateID: %v", err)
	}
	user, err := s.CreateUser("user_"+id, id+"@example.com", "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return user
}

func addMedia(t *testing.T, s *store.Store, owner, file string, content []byte, diaries []string) *store.Media {
	t.Helper()
	media, err := s.CreateMedia(owner, file, file, "alt "+file, diaries)
	if err != nil {
		t.Fatalf("CreateMedia: %v", err)
	}
	if content != nil {
		if err := s.SaveUploadedMedia(media, bytes.NewReader(content)); err != nil {
			t.Fatalf("SaveUploadedMedia: %v", err)
		}
	}
	return media
}

func exportAll(t *testing.T, s *store.Store, userID string) ([]byte, *ExportStats) {
	t.Helper()
	var buf bytes.Buffer
	stats, err := Export(context.Background(), s, userID, AllTime(time.Now()), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	return buf.Bytes(), stats
}

func zipReader(t *testing.T, data []byte) *zip.Reader {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	return zr
}

func buildZip(t *testing.T, files map[string][]byte) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		_, _ = w.Write(content)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return zipReader(t, buf.Bytes())
}

func manifestJSON(t *testing.T, data Data) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return raw
}

func TestExportImportRoundTripAcrossBatches(t *testing.T) {
	src := newStore(t)
	owner := newUser(t, src)
	other := newUser(t, src)

	// More diaries than one batch holds, to walk the keyset pagination.
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	count := batchSize + 37
	var firstID string
	for i := 0; i < count; i++ {
		date := start.AddDate(0, 0, i).Format(dateLayout)
		mood := ""
		if i == 0 {
			mood = "a/b"
		}
		d, err := src.InsertImportedDiary(owner.ID, "", date, fmt.Sprintf("entry %d", i), mood, "sunny")
		if err != nil {
			t.Fatalf("InsertImportedDiary: %v", err)
		}
		if i == 0 {
			firstID = d.ID
		}
	}
	if _, err := src.InsertImportedDiary(other.ID, "", "2020-01-01", "not mine", "", ""); err != nil {
		t.Fatalf("InsertImportedDiary other: %v", err)
	}
	// Two files with the same name must both survive the round trip.
	a := addMedia(t, src, owner.ID, "image.png", append(append([]byte{}, pngData...), 'a'), []string{firstID})
	b := addMedia(t, src, owner.ID, "image.png", append(append([]byte{}, pngData...), 'b'), nil)
	addMedia(t, src, owner.ID, "gone.png", nil, nil)
	conv, err := src.CreateConversation(owner.ID, "Chat")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if _, err := src.CreateMessage(owner.ID, conv.ID, "user", "hello", []string{firstID}); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}

	data, stats := exportAll(t, src, owner.ID)
	if stats.Diaries.TotalInSystem != count || stats.Diaries.ActualExported != count {
		t.Fatalf("diary stats = %#v", stats.Diaries)
	}
	if stats.Media.ShouldExport != 3 || stats.Media.ActualExported != 2 || len(stats.FailedItems) != 1 {
		t.Fatalf("media stats = %#v failed=%#v", stats.Media, stats.FailedItems)
	}
	if stats.Conversations.ActualExported != 1 || stats.Messages != 1 {
		t.Fatalf("conversation stats = %#v messages=%d", stats.Conversations, stats.Messages)
	}
	zr := zipReader(t, data)
	names := make(map[string]bool)
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, want := range []string{ManifestName, "markdown/2020-01-01_a_b.md", "media/" + a.ID + "/image.png", "media/" + b.ID + "/image.png"} {
		if !names[want] {
			t.Fatalf("archive lacks %s: %v", want, names)
		}
	}

	dst := newStore(t)
	target := newUser(t, dst)
	result, err := Import(context.Background(), dst, target.ID, zr)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Diaries.Imported != count || result.Media.Imported != 2 || result.Media.Failed != 1 || result.Conversations.Imported != 1 {
		t.Fatalf("import stats = %#v", result)
	}
	if got := dst.CountDiaries(target.ID); got != count {
		t.Fatalf("imported diaries = %d, want %d", got, count)
	}
	contents := make(map[string]bool)
	items, err := dst.ListMediaAfter(target.ID, "", "", 10)
	if err != nil {
		t.Fatalf("ListMediaAfter: %v", err)
	}
	for _, m := range items {
		rc, err := dst.OpenMediaFile(m)
		if err != nil {
			t.Fatalf("OpenMediaFile: %v", err)
		}
		body, _ := io.ReadAll(rc)
		rc.Close()
		contents[string(body[len(body)-1:])] = true
		if len(m.Diary) == 1 && m.Diary[0] == firstID {
			t.Fatal("media diary link should be remapped to the new diary id")
		}
	}
	if !contents["a"] || !contents["b"] {
		t.Fatalf("media with equal names were not both restored: %v", contents)
	}

	// Importing again skips every diary, and links the media it re-adds to
	// the entries that already exist.
	again, err := Import(context.Background(), dst, target.ID, zr)
	if err != nil || again.Diaries.Skipped != count {
		t.Fatalf("second import = %#v, %v", again, err)
	}
	existing, err := dst.FindDiaryByDate(target.ID, "2020-01-01")
	if err != nil {
		t.Fatalf("FindDiaryByDate: %v", err)
	}
	items, _ = dst.ListMediaAfter(target.ID, "", "", 10)
	linked := 0
	for _, m := range items {
		if len(m.Diary) == 1 && m.Diary[0] == existing.ID {
			linked++
		}
	}
	if linked != 2 {
		t.Fatalf("media linked to the existing entry = %d, want 2 (one per import)", linked)
	}
}

func TestExportFiltersAndSections(t *testing.T) {
	s := newStore(t)
	user := newUser(t, s)
	for _, date := range []string{"2024-01-01", "2024-02-02"} {
		if _, err := s.InsertImportedDiary(user.ID, "", date, "diary "+date, "", ""); err != nil {
			t.Fatalf("InsertImportedDiary: %v", err)
		}
	}
	if _, err := s.CreateConversation(user.ID, "Old chat"); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	addMedia(t, s, user.ID, "photo.png", pngData, nil)

	var buf bytes.Buffer
	fixed := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
	stats, err := Export(context.Background(), s, user.ID, Options{
		Start:          time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
		End:            time.Date(2024, 2, 28, 0, 0, 0, 0, time.UTC),
		IncludeDiaries: true,
		Now:            func() time.Time { return fixed },
	}, &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if stats.Diaries.ActualExported != 1 || stats.Media.ShouldExport != 0 || stats.Conversations.ShouldExport != 0 {
		t.Fatalf("stats = %#v", stats)
	}
	zr := zipReader(t, buf.Bytes())
	rc, err := zr.File[0].Open()
	if err != nil {
		t.Fatalf("open manifest: %v", err)
	}
	var data Data
	if err := json.NewDecoder(rc).Decode(&data); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}
	rc.Close()
	if data.ExportedAt != "2024-03-01T12:00:00Z" || len(data.Diaries) != 1 || data.Diaries[0].Date != "2024-02-02" || len(data.Media) != 0 {
		t.Fatalf("manifest = %#v", data)
	}

	// Conversations out of range are left out.
	buf.Reset()
	stats, err = Export(context.Background(), s, user.ID, Options{Start: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2000, 1, 2, 0, 0, 0, 0, time.UTC), IncludeConversations: true, IncludeMedia: true}, &buf)
	if err != nil || stats.Conversations.ShouldExport != 0 || stats.Media.ShouldExport != 0 {
		t.Fatalf("out of range export = %#v, %v", stats, err)
	}
}

func TestExportErrors(t *testing.T) {
	s := newStore(t)
	user := newUser(t, s)
	if _, err := s.InsertImportedDiary(user.ID, "", "2024-01-01", "diary", "", ""); err != nil {
		t.Fatalf("InsertImportedDiary: %v", err)
	}
	addMedia(t, s, user.ID, "photo.png", pngData, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Export(ctx, s, user.ID, AllTime(time.Now()), io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled export err = %v", err)
	}

	// A writer that breaks surfaces as an error, at each stage of the archive.
	for _, limit := range []int{0, 60, 200, 400, 600} {
		if _, err := Export(context.Background(), s, user.ID, AllTime(time.Now()), &failingWriter{limit: limit}); err == nil {
			t.Fatalf("export into a writer failing after %d bytes succeeded", limit)
		}
	}

	closed := newStore(t)
	closedUser := newUser(t, closed)
	_ = closed.Close()
	if _, err := Export(context.Background(), closed, closedUser.ID, AllTime(time.Now()), io.Discard); err == nil {
		t.Fatal("export from a closed store should fail")
	}
}

type failingWriter struct {
	limit, n int
}

func (w *failingWriter) Write(p []byte) (int, error) {
	if w.n+len(p) > w.limit {
		return 0, errors.New("disk full")
	}
	w.n += len(p)
	return len(p), nil
}

func TestExporterBatchErrors(t *testing.T) {
	s := newStore(t)
	user := newUser(t, s)
	e := &exporter{ctx: context.Background(), s: s, userID: user.ID, opts: AllTime(time.Now()), stats: &ExportStats{}}
	_ = s.Close()
	if err := e.eachDiary(func(Diary) error { return nil }); err == nil {
		t.Fatal("eachDiary on a closed store should fail")
	}
	if err := e.eachMedia(func(Media) error { return nil }); err == nil {
		t.Fatal("eachMedia on a closed store should fail")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.ctx = ctx
	if err := e.eachMedia(func(Media) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("eachMedia cancelled err = %v", err)
	}

	bad := &jsonStream{w: bufio.NewWriterSize(&failingWriter{}, 16)}
	if bad.elem(make(chan int)) == nil || bad.elem(1) == nil {
		t.Fatal("jsonStream should report values that cannot be encoded")
	}
	tiny := &jsonStream{w: bufio.NewWriterSize(&failingWriter{}, 16), first: true}
	if tiny.elem(strings.Repeat("x", 32)) == nil || tiny.elem(1) == nil {
		t.Fatal("jsonStream should report write errors")
	}

	j := &jsonStream{w: nil, err: errors.New("broken")}
	if j.elem(1) == nil || j.flush() == nil {
		t.Fatal("jsonStream should keep its first error")
	}
}

func TestExportConversationAndMediaCallbackErrors(t *testing.T) {
	s := newStore(t)
	user := newUser(t, s)
	addMedia(t, s, user.ID, "photo.png", pngData, nil)
	e := &exporter{ctx: context.Background(), s: s, userID: user.ID, opts: AllTime(time.Now()), stats: &ExportStats{}}
	boom := errors.New("boom")
	if err := e.eachMedia(func(Media) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("eachMedia callback err = %v", err)
	}
	if _, err := s.InsertImportedDiary(user.ID, "", "2024-01-01", "x", "", ""); err != nil {
		t.Fatalf("InsertImportedDiary: %v", err)
	}
	if err := e.eachDiary(func(Diary) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("eachDiary callback err = %v", err)
	}

	// Conversations whose messages cannot be read are reported, not fatal;
	// a cancelled context stops the export.
	conv := &store.Conversation{ID: "missing", Title: "x", Updated: "2024-01-01 00:00:00.000Z"}
	e.zw = zip.NewWriter(io.Discard)
	e.opts = Options{Start: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), End: time.Now(), IncludeConversations: true}
	_ = s.Close()
	if _, err := e.writeManifest([]*store.Conversation{conv}); err != nil {
		t.Fatalf("writeManifest: %v", err)
	}
	if len(e.stats.FailedItems) != 1 || e.stats.FailedItems[0].Type != "conversation" {
		t.Fatalf("failed items = %#v", e.stats.FailedItems)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.ctx = ctx
	e.zw = zip.NewWriter(io.Discard)
	if _, err := e.writeManifest([]*store.Conversation{conv}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled writeManifest err = %v", err)
	}
}

// fakeS3 serves media objects for the remote media path of exports.
func fakeS3(t *testing.T, s *store.Store, userID string, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	for key, value := range map[string]any{
		"image_upload.s3.bucket":           "bucket",
		"image_upload.s3.region":           "us-east-1",
		"image_upload.s3.endpoint":         server.URL,
		"image_upload.s3.access_key":       "key",
		"image_upload.s3.secret":           "secret",
		"image_upload.s3.force_path_style": true,
	} {
		if err := s.SetSetting(userID, key, value, false); err != nil {
			t.Fatalf("SetSetting: %v", err)
		}
	}
}

func TestExportSpoolsRemoteMedia(t *testing.T) {
	s := newStore(t)
	user := newUser(t, s)
	ok := addMedia(t, s, user.ID, "remote.png", nil, nil)
	broken := addMedia(t, s, user.ID, "broken.png", nil, nil)
	fakeS3(t, s, user.ID, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, ok.ID):
			_, _ = w.Write(pngData)
		case strings.Contains(r.URL.Path, broken.ID):
			// Promise more than is sent, so the body breaks mid-stream.
			w.Header().Set("Content-Length", "1000")
			_, _ = w.Write(pngData[:10])
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	tempDir := t.TempDir()
	var buf bytes.Buffer
	opts := AllTime(time.Now())
	opts.TempDir = tempDir
	stats, err := Export(context.Background(), s, user.ID, opts, &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if stats.Media.ActualExported != 1 || len(stats.FailedItems) != 1 || stats.FailedItems[0].ID != broken.ID {
		t.Fatalf("stats = %#v failed=%#v", stats.Media, stats.FailedItems)
	}
	if entries, _ := os.ReadDir(tempDir); len(entries) != 0 {
		t.Fatalf("spool files left behind: %v", entries)
	}

	opts.TempDir = filepath.Join(tempDir, "missing")
	if _, err := Export(context.Background(), s, user.ID, opts, io.Discard); err == nil {
		t.Fatal("export with an unusable temp dir should fail")
	}
}

func TestImportValidatesManifestBeforeWriting(t *testing.T) {
	s := newStore(t)
	user := newUser(t, s)

	if _, err := Import(context.Background(), s, user.ID, buildZip(t, map[string][]byte{"media/x.png": pngData})); !errors.Is(err, ErrMissingManifest) {
		t.Fatalf("missing manifest err = %v", err)
	}
	// A manifest that breaks after valid diaries imports nothing.
	truncated := []byte(`{"version":1,"diaries":[{"id":"a","date":"2024-01-01","content":"x"}],"media":[{"id":`)
	cases := map[string][]byte{
		"truncated":   truncated,
		"not object":  []byte(`[1,2]`),
		"empty":       []byte(``),
		"wrong array": []byte(`{"diaries":{"a":1}}`),
		"wrong item":  []byte(`{"diaries":[1]}`),
		"bad media":   []byte(`{"media":[1]}`),
		"bad convs":   []byte(`{"conversations":[1]}`),
		"bad skip":    []byte(`{"extra":}`),
		"bad key":     []byte(`{"diaries":[] x}`),
	}
	for name, manifest := range cases {
		_, err := Import(context.Background(), s, user.ID, buildZip(t, map[string][]byte{ManifestName: manifest}))
		if !errors.Is(err, ErrInvalidManifest) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	if got := s.CountDiaries(user.ID); got != 0 {
		t.Fatalf("invalid manifests imported %d diaries", got)
	}

	// null arrays and unknown keys are fine.
	stats, err := Import(context.Background(), s, user.ID, buildZip(t, map[string][]byte{ManifestName: []byte(`{"version":1,"future":{"a":[1]},"diaries":null,"media":null,"conversations":null}`)}))
	if err != nil || stats.Diaries.Total != 0 {
		t.Fatalf("null arrays = %#v, %v", stats, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	manifest := manifestJSON(t, Data{Diaries: []Diary{{ID: "a", Date: "2024-01-01"}}})
	if _, err := Import(ctx, s, user.ID, buildZip(t, map[string][]byte{ManifestName: manifest})); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled import err = %v", err)
	}
}

func TestImportMediaEdgeCases(t *testing.T) {
	s := newStore(t)
	user := newUser(t, s)
	manifest := manifestJSON(t, Data{
		Version: 1,
		Diaries: []Diary{{ID: "d1", Date: "2024-01-01", Content: "x"}, {ID: "d2", Date: "2024-01-01", Content: "duplicate date"}, {ID: "d3"}},
		Media: []Media{
			{ID: "legacy", File: "legacy.png", Diary: []string{"d1", "unknown"}},
			{ID: "evil", File: "../../escape.png", Path: "media/evil/escape.png"},
			{ID: "text", File: "notes.png"},
			{ID: "nofile", File: ""},
			{ID: "absent", File: "absent.png"},
		},
		Conversations: []Conversation{{ID: "c", Title: "t", Messages: []Message{{Role: "user", Content: "hi", ReferencedDiaries: []string{"d1", "d3"}}}}},
	})
	zr := buildZip(t, map[string][]byte{
		ManifestName:            manifest,
		"media/legacy.png":      pngData,
		"media/evil/escape.png": pngData,
		"media/notes.png":       []byte("plain text"),
		"../outside.png":        pngData,
	})
	stats, err := Import(context.Background(), s, user.ID, zr)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if stats.Diaries.Imported != 1 || stats.Diaries.Skipped != 1 || stats.Diaries.Failed != 1 {
		t.Fatalf("diary stats = %#v", stats.Diaries)
	}
	if stats.Media.Imported != 2 || stats.Media.Failed != 3 {
		t.Fatalf("media stats = %#v", stats.Media)
	}
	items, _ := s.ListMediaAfter(user.ID, "", "", 10)
	for _, m := range items {
		if strings.Contains(m.File, "..") {
			t.Fatalf("media file name was not sanitised: %q", m.File)
		}
		path := s.NewMediaFilePath(m.ID, m.File)
		if !strings.HasPrefix(path, s.DataDir) {
			t.Fatalf("media stored outside the data dir: %s", path)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(s.DataDir), "escape.png")); err == nil {
		t.Fatal("path traversal wrote outside the data dir")
	}

	// Failures while saving roll back the media record.
	blocked := newStore(t)
	blockedUser := newUser(t, blocked)
	if err := os.MkdirAll(filepath.Join(blocked.DataDir, "storage"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blocked.DataDir, "storage", "media"), []byte("file, not dir"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	manifest = manifestJSON(t, Data{Media: []Media{{ID: "m", File: "a.png"}}})
	stats, err = Import(context.Background(), blocked, blockedUser.ID, buildZip(t, map[string][]byte{ManifestName: manifest, "media/a.png": pngData}))
	if err != nil || stats.Media.Failed != 1 || blocked.CountMedia(blockedUser.ID) != 0 {
		t.Fatalf("blocked import = %#v, %v, media=%d", stats, err, blocked.CountMedia(blockedUser.ID))
	}
}

func TestImportRejectsOversizedMediaAndStoreErrors(t *testing.T) {
	s := newStore(t)
	user := newUser(t, s)
	im := &importer{ctx: context.Background(), s: s, userID: user.ID, stats: &ImportStats{}, diaryIDs: map[string]string{}, files: map[string]*zip.File{
		"media/big.png": {FileHeader: zip.FileHeader{Name: "media/big.png", UncompressedSize64: MaxMediaFileSize + 1}},
	}}
	if err := im.importMedia(Media{File: "big.png"}); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Fatalf("oversized media err = %v", err)
	}

	_ = s.Close()
	if err := im.diary(Diary{ID: "x", Date: "2024-01-01"}); err != nil || im.stats.Diaries.Failed != 1 {
		t.Fatalf("diary on closed store = %v, %#v", err, im.stats.Diaries)
	}
	if err := im.conversation(Conversation{Title: "x"}); err != nil || im.stats.Conversations.Failed != 1 {
		t.Fatalf("conversation on closed store = %v, %#v", err, im.stats.Conversations)
	}
	zr := buildZip(t, map[string][]byte{"media/a.png": pngData})
	im.files = map[string]*zip.File{"media/a.png": zr.File[0]}
	if err := im.importMedia(Media{File: "a.png"}); err == nil {
		t.Fatal("importMedia on a closed store should fail")
	}
}

func TestReadManifestOpenError(t *testing.T) {
	zr := buildZip(t, map[string][]byte{ManifestName: []byte(`{}`)})
	f := zr.File[0]
	f.Method = 99 // unknown compression method
	if err := readManifest(f, manifestHandlers{}); err == nil {
		t.Fatal("readManifest with an unsupported method should fail")
	}
	im := &importer{files: map[string]*zip.File{"media/a.png": f}}
	if err := im.importMedia(Media{File: "a.png"}); err == nil {
		t.Fatal("importMedia with an unsupported method should fail")
	}
}

func TestHelpers(t *testing.T) {
	if !ValidPath("media/a.png") || ValidPath("../a") || ValidPath("/a") || ValidPath("\\a") {
		t.Fatal("ValidPath")
	}
	start := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 28, 0, 0, 0, 0, time.UTC)
	if !InRange("2024-02-01", start, end) || InRange("", start, end) || InRange("bad", start, end) || InRange("2024-03-01", start, end) {
		t.Fatal("InRange")
	}
	if got := Markdown(Diary{Date: "2024-01-01", Content: "x"}); got != "# 2024-01-01\n\nx" {
		t.Fatalf("Markdown = %q", got)
	}
	if got := Markdown(Diary{Date: "d", Mood: "m", Weather: "w", Content: "x"}); !strings.Contains(got, "**Mood:** m\n**Weather:** w\n\nx") {
		t.Fatalf("Markdown with meta = %q", got)
	}
}
