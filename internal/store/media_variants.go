package store

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"sync"

	"github.com/songtianlun/diarum/internal/imaging"
)

// maxVariantSourceBytes bounds how much of an original is read to build its
// variants (uploads are capped at 50MB).
const maxVariantSourceBytes = 64 << 20

// variantWorker runs variant generation in the background, one job per media
// at a time and a couple of jobs overall, since decoding large photos is
// memory hungry. Its zero value is ready to use.
type variantWorker struct {
	mu      sync.Mutex
	running map[string]bool
	// attempted remembers media already processed in this process, so a
	// missing variant (small originals get none) is not rebuilt per request.
	attempted map[string]bool
	slots     chan struct{}
	wg        sync.WaitGroup
}

// VariantMedia returns a copy of media that points at one of its variants,
// so the regular open/save/delete helpers work on it unchanged.
func VariantMedia(media *Media, v imaging.Variant) *Media {
	variant := *media
	variant.File = imaging.VariantFilename(media.File, v)
	return &variant
}

// QueueMediaVariants builds the thumbnail and medium variants of media in the
// background. It is a no-op for formats without variants and for media that
// was already processed (or is being processed) by this process.
func (s *Store) QueueMediaVariants(media *Media) {
	if media == nil || !imaging.Supported(media.File) {
		return
	}
	w := &s.variants
	w.mu.Lock()
	if w.running == nil {
		w.running = map[string]bool{}
		w.attempted = map[string]bool{}
		w.slots = make(chan struct{}, 2)
	}
	if w.running[media.ID] || w.attempted[media.ID] {
		w.mu.Unlock()
		return
	}
	w.running[media.ID] = true
	w.attempted[media.ID] = true
	w.wg.Add(1)
	w.mu.Unlock()

	copied := *media
	go func() {
		defer w.wg.Done()
		w.slots <- struct{}{}
		defer func() { <-w.slots }()
		if err := s.GenerateMediaVariants(&copied); err != nil {
			log.Printf("media %s: generating variants failed: %v", copied.ID, err)
		}
		w.mu.Lock()
		delete(w.running, copied.ID)
		w.mu.Unlock()
	}()
}

// WaitMediaVariants blocks until queued variant jobs have finished.
func (s *Store) WaitMediaVariants() {
	s.variants.wg.Wait()
}

// GenerateMediaVariants builds and stores the variants of media synchronously.
func (s *Store) GenerateMediaVariants(media *Media) error {
	if !imaging.Supported(media.File) {
		return nil
	}
	reader, err := s.OpenMediaFile(media)
	if err != nil {
		return fmt.Errorf("open original: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxVariantSourceBytes))
	reader.Close()
	if err != nil {
		return fmt.Errorf("read original: %w", err)
	}
	variants, err := imaging.Generate(data, media.File)
	if err != nil {
		return err
	}
	for _, v := range imaging.Variants {
		encoded, ok := variants[v]
		if !ok {
			continue
		}
		if err := s.saveMediaVariant(media, v, encoded); err != nil {
			return fmt.Errorf("save %s variant: %w", v, err)
		}
	}
	return nil
}

// saveMediaVariant stores a variant beside its original.
func (s *Store) saveMediaVariant(media *Media, v imaging.Variant, data []byte) error {
	return s.saveMediaCompanion(media, VariantMedia(media, v), bytes.NewReader(data))
}

// saveMediaCompanion stores a file derived from media (a variant, a live
// photo clip) beside it: in the same local directory when the original is on
// disk, otherwise where uploads go now.
func (s *Store) saveMediaCompanion(media, companion *Media, reader io.Reader) error {
	if original := s.MediaFilePath(media); fileExists(original) {
		return s.SaveUploadedFile(filepath.Join(filepath.Dir(original), companion.File), reader)
	}
	if s.imageUploadProvider(media.Owner) == "s3" {
		if cfg := s.userS3Config(media.Owner); cfg != nil {
			return s.saveMediaToS3(cfg, companion, reader)
		}
	}
	return s.SaveUploadedFile(filepath.Join(s.userLocalMediaDir(media.Owner), companion.ID, companion.File), reader)
}

// OpenMediaVariant opens a stored variant; os.ErrNotExist when there is none.
func (s *Store) OpenMediaVariant(media *Media, v imaging.Variant) (io.ReadCloser, error) {
	return s.OpenMediaFile(VariantMedia(media, v))
}

// MediaVariantPath is the local path of a variant, when it is stored on disk.
func (s *Store) MediaVariantPath(media *Media, v imaging.Variant) (string, bool) {
	path := s.MediaFilePath(VariantMedia(media, v))
	return path, fileExists(path)
}
