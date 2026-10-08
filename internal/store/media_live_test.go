package store

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"os"
	"path/filepath"
	"testing"

	"github.com/songtianlun/diarum/internal/imaging"
)

var liveClip = append([]byte("\x00\x00\x00\x14ftypqt  \x00\x00\x00\x00"), bytes.Repeat([]byte{7}, 64)...)

func TestSaveMediaLive(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	media := newStoredMedia(t, s, user.ID, "IMG_0001.jpg", []byte("still"))

	if LiveMedia(media) != nil || LiveMedia(nil) != nil {
		t.Fatal("media without a clip has no live media")
	}
	if err := s.SaveMediaLive(media, bytes.NewReader(liveClip)); err != nil {
		t.Fatalf("SaveMediaLive: %v", err)
	}
	if media.Live != "IMG_0001.live.mp4" {
		t.Fatalf("Live = %q", media.Live)
	}
	stored, err := s.GetMedia(media.ID, user.ID)
	if err != nil || stored.Live != media.Live {
		t.Fatalf("stored = %+v %v", stored, err)
	}
	live := LiveMedia(stored)
	if live == nil || live.File != media.Live || live.ID != media.ID {
		t.Fatalf("LiveMedia = %+v", live)
	}
	path := s.MediaFilePath(live)
	if filepath.Dir(path) != filepath.Dir(s.MediaFilePath(stored)) {
		t.Fatalf("clip stored at %s, want beside the still", path)
	}
	reader, err := s.OpenMediaFile(live)
	if err != nil {
		t.Fatalf("open clip: %v", err)
	}
	data, _ := io.ReadAll(reader)
	_ = reader.Close()
	if !bytes.Equal(data, liveClip) {
		t.Fatal("stored clip differs from the upload")
	}
	if got := mime.TypeByExtension(".mp4"); got != "video/mp4" {
		t.Fatalf(".mp4 type = %q", got)
	}

	// Deleting the media file removes its clip too.
	if err := s.DeleteMediaFile(stored); err != nil {
		t.Fatalf("DeleteMediaFile: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("clip left behind: %v", err)
	}
}

func TestSaveMediaLiveErrors(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	if err := s.SaveMediaLive(nil, bytes.NewReader(liveClip)); err == nil {
		t.Fatal("nil media should fail")
	}

	media := newStoredMedia(t, s, user.ID, "big.jpg", []byte("still"))
	oversized := io.LimitReader(zeroReader{}, imaging.MaxLiveVideoBytes+1)
	if err := s.SaveMediaLive(media, oversized); !errors.Is(err, ErrLiveVideoTooLarge) {
		t.Fatalf("oversized error = %v", err)
	}
	clip := filepath.Join(filepath.Dir(s.MediaFilePath(media)), imaging.LiveFilename(media.File))
	if _, err := os.Stat(clip); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("oversized clip left behind: %v", err)
	}
	if stored, _ := s.GetMedia(media.ID, user.ID); stored.Live != "" {
		t.Fatalf("oversized clip recorded: %q", stored.Live)
	}

	if err := s.SaveMediaLive(media, failingReader{}); err == nil {
		t.Fatal("unreadable clip should fail")
	}

	// A clip that cannot be written is not recorded.
	blocked := newStoredMedia(t, s, user.ID, "blocked.jpg", []byte("still"))
	if err := os.Mkdir(filepath.Join(filepath.Dir(s.MediaFilePath(blocked)), imaging.LiveFilename(blocked.File)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMediaLive(blocked, bytes.NewReader(liveClip)); err == nil {
		t.Fatal("unwritable clip should fail")
	}

	// A database failure removes the clip again.
	failing := newStoredMedia(t, s, user.ID, "failing.jpg", []byte("still"))
	if _, err := s.DB.Exec(`CREATE TRIGGER block_live BEFORE UPDATE OF live ON media BEGIN SELECT RAISE(ABORT, 'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMediaLive(failing, bytes.NewReader(liveClip)); err == nil {
		t.Fatal("database failure should fail")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(s.MediaFilePath(failing)), imaging.LiveFilename(failing.File))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("clip left behind after database failure: %v", err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, os.ErrInvalid }
