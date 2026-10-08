package store

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"

	"github.com/songtianlun/diarum/internal/imaging"
)

func init() {
	// Go's built-in table has no video types, and the container may have no
	// /etc/mime.types: register the one live photo clips are served as.
	_ = mime.AddExtensionType(".mp4", "video/mp4")
}

// ErrLiveVideoTooLarge is returned for a live photo clip over the size cap.
var ErrLiveVideoTooLarge = errors.New("live photo video is too large")

// LiveMedia returns a copy of media that points at its live photo clip, so
// the regular open/save/delete helpers work on it; nil when it has none.
func LiveMedia(media *Media) *Media {
	if media == nil || media.Live == "" {
		return nil
	}
	live := *media
	live.File = media.Live
	return &live
}

// SaveMediaLive stores reader as the live photo clip of media, beside its
// original (which must be saved first), and records it on the media.
func (s *Store) SaveMediaLive(media *Media, reader io.Reader) error {
	if media == nil {
		return fmt.Errorf("media is required")
	}
	// Buffered: clips are small and capped, and S3 uploads need a seekable
	// body. An oversized clip is refused before anything is written.
	data, err := io.ReadAll(io.LimitReader(reader, imaging.MaxLiveVideoBytes+1))
	if err != nil {
		return err
	}
	if len(data) > imaging.MaxLiveVideoBytes {
		return ErrLiveVideoTooLarge
	}
	live := *media
	live.File = imaging.LiveFilename(media.File)
	if err := s.saveMediaCompanion(media, &live, bytes.NewReader(data)); err != nil {
		return err
	}
	if _, err := s.DB.Exec(`UPDATE media SET live = ?, updated = ? WHERE id = ?`, live.File, nowString(), media.ID); err != nil {
		_ = s.deleteMediaObject(&live)
		return err
	}
	media.Live = live.File
	// A lookup made before the clip existed may have been remembered as missing.
	s.forgetPublicURLs(media)
	return nil
}
