package archive

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/songtianlun/diarum/internal/store"
)

var liveClip = append([]byte("\x00\x00\x00\x14ftypqt  \x00\x00\x00\x00"), bytes.Repeat([]byte{5}, 128)...)

func TestExportImportLivePhoto(t *testing.T) {
	s := newStore(t)
	user := newUser(t, s)
	media := addMedia(t, s, user.ID, "IMG_0001.png", pngData, nil)
	if err := s.SaveMediaLive(media, bytes.NewReader(liveClip)); err != nil {
		t.Fatalf("SaveMediaLive: %v", err)
	}

	data, stats := exportAll(t, s, user.ID)
	if stats.Media.ActualExported != 1 || len(stats.FailedItems) != 0 {
		t.Fatalf("export stats = %#v", stats)
	}
	zr := zipReader(t, data)
	livePath := "media/" + media.ID + "/IMG_0001.live.mp4"
	found := false
	for _, f := range zr.File {
		if f.Name == livePath {
			rc, _ := f.Open()
			got, _ := io.ReadAll(rc)
			_ = rc.Close()
			found = bytes.Equal(got, liveClip)
		}
	}
	if !found {
		t.Fatalf("archive lacks the clip at %s", livePath)
	}

	dst := newStore(t)
	target := newUser(t, dst)
	result, err := Import(context.Background(), dst, target.ID, zr)
	if err != nil || result.Media.Imported != 1 {
		t.Fatalf("import = %#v %v", result, err)
	}
	items, _ := dst.ListMediaAfter(target.ID, "", "", 10)
	if len(items) != 1 || items[0].Live != "IMG_0001.live.mp4" {
		t.Fatalf("imported media = %+v", items)
	}
	rc, err := dst.OpenMediaFile(store.LiveMedia(items[0]))
	if err != nil {
		t.Fatalf("open imported clip: %v", err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(got, liveClip) {
		t.Fatal("imported clip differs")
	}
}

func TestExportLivePhotoMissingClip(t *testing.T) {
	s := newStore(t)
	user := newUser(t, s)
	media := addMedia(t, s, user.ID, "a.png", pngData, nil)
	if err := s.SaveMediaLive(media, bytes.NewReader(liveClip)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(s.MediaFilePath(store.LiveMedia(media))); err != nil {
		t.Fatal(err)
	}
	_, stats := exportAll(t, s, user.ID)
	if stats.Media.ActualExported != 1 || len(stats.FailedItems) != 1 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestImportLivePhotoEdgeCases(t *testing.T) {
	s := newStore(t)
	user := newUser(t, s)
	manifest := manifestJSON(t, Data{Media: []Media{
		{ID: "absent", File: "absent.png", Path: "media/absent/absent.png", Live: "absent.live.mp4", LivePath: "media/absent/absent.live.mp4"},
		{ID: "invalid", File: "invalid.png", Path: "media/invalid/invalid.png", Live: "invalid.live.mp4", LivePath: "media/invalid/invalid.live.mp4"},
	}})
	zr := buildZip(t, map[string][]byte{
		ManifestName:                     manifest,
		"media/absent/absent.png":        pngData,
		"media/invalid/invalid.png":      pngData,
		"media/invalid/invalid.live.mp4": pngData,
	})
	stats, err := Import(context.Background(), s, user.ID, zr)
	if err != nil || stats.Media.Imported != 2 {
		t.Fatalf("import = %#v %v", stats, err)
	}
	items, _ := s.ListMediaAfter(user.ID, "", "", 10)
	for _, m := range items {
		if m.Live != "" {
			t.Fatalf("%s should stay a plain still, live = %q", m.File, m.Live)
		}
	}

	// A clip that cannot be saved still leaves the photo imported.
	blocked := newStore(t)
	blockedUser := newUser(t, blocked)
	if _, err := blocked.DB.Exec(`CREATE TRIGGER block_live BEFORE UPDATE OF live ON media BEGIN SELECT RAISE(ABORT, 'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	manifest = manifestJSON(t, Data{Media: []Media{{ID: "m", File: "a.png", Path: "media/m/a.png", Live: "a.live.mp4", LivePath: "media/m/a.live.mp4"}}})
	stats, err = Import(context.Background(), blocked, blockedUser.ID, buildZip(t, map[string][]byte{ManifestName: manifest, "media/m/a.png": pngData, "media/m/a.live.mp4": liveClip}))
	if err != nil || stats.Media.Imported != 1 {
		t.Fatalf("blocked import = %#v %v", stats, err)
	}
	if items, _ := blocked.ListMediaAfter(blockedUser.ID, "", "", 10); len(items) != 1 || items[0].Live != "" || strings.Contains(items[0].File, "live") {
		t.Fatalf("blocked items = %+v", items)
	}
}
