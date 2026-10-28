package archive

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/songtianlun/diarum/internal/store"
)

var videoData = append([]byte("\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isomiso2"), bytes.Repeat([]byte{3}, 256)...)

func TestExportImportVideo(t *testing.T) {
	s := newStore(t)
	user := newUser(t, s)
	media := addMedia(t, s, user.ID, "trip.m4v", videoData, nil)
	if err := s.SaveMediaPoster(media, bytes.NewReader(pngData)); err != nil {
		t.Fatalf("SaveMediaPoster: %v", err)
	}

	data, stats := exportAll(t, s, user.ID)
	if stats.Media.ActualExported != 1 || len(stats.FailedItems) != 0 {
		t.Fatalf("export stats = %#v", stats)
	}
	zr := zipReader(t, data)

	dst := newStore(t)
	target := newUser(t, dst)
	result, err := Import(context.Background(), dst, target.ID, zr)
	if err != nil || result.Media.Imported != 1 {
		t.Fatalf("import = %#v %v", result, err)
	}
	items, _ := dst.ListMediaAfter(target.ID, "", "", 10)
	if len(items) != 1 || items[0].File != "trip.m4v" || items[0].Poster != "trip.poster.jpg" {
		t.Fatalf("imported media = %+v", items)
	}
	for _, m := range []*store.Media{items[0], store.PosterMedia(items[0])} {
		rc, err := dst.OpenMediaFile(m)
		if err != nil {
			t.Fatalf("open imported %s: %v", m.File, err)
		}
		got, _ := io.ReadAll(rc)
		_ = rc.Close()
		want := videoData
		if m.File != items[0].File {
			want = pngData
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("imported %s differs", m.File)
		}
	}
}
