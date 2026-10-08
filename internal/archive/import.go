package archive

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/imaging"
	"github.com/songtianlun/diarum/internal/logger"
	"github.com/songtianlun/diarum/internal/store"
)

// Import adds the archive's data to userID's account. Diaries whose date
// already exists are skipped, so importing never overwrites anything.
//
// The manifest is validated in full before anything is written: a corrupt or
// truncated manifest returns ErrInvalidManifest and leaves the account as it
// was. After that, individual items that cannot be imported are counted as
// failed and the import carries on.
func Import(ctx context.Context, s *store.Store, userID string, zr *zip.Reader) (*ImportStats, error) {
	var manifest *zip.File
	files := make(map[string]*zip.File)
	for _, f := range zr.File {
		if !ValidPath(f.Name) {
			continue
		}
		switch {
		case f.Name == ManifestName:
			manifest = f
		case strings.HasPrefix(f.Name, "media/"):
			files[f.Name] = f
		}
	}
	if manifest == nil {
		return nil, ErrMissingManifest
	}

	// Dry run: decode every element without touching the account.
	if err := readManifest(manifest, manifestHandlers{
		diary:        func(Diary) error { return ctx.Err() },
		media:        func(Media) error { return ctx.Err() },
		conversation: func(Conversation) error { return ctx.Err() },
	}); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}

	im := &importer{ctx: ctx, s: s, userID: userID, files: files, stats: &ImportStats{}, diaryIDs: make(map[string]string)}
	if err := readManifest(manifest, manifestHandlers{diary: im.diary, media: im.media, conversation: im.conversation}); err != nil {
		return im.stats, err
	}
	return im.stats, nil
}

type manifestHandlers struct {
	diary        func(Diary) error
	media        func(Media) error
	conversation func(Conversation) error
}

// readManifest decodes the manifest one element at a time. Arrays are handled
// in file order, which is diaries, media, conversations for every archive
// Diarum writes, so diary IDs are known before media refer to them.
func readManifest(f *zip.File, h manifestHandlers) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	dec := json.NewDecoder(bufio.NewReaderSize(rc, 256<<10))

	if tok, err := dec.Token(); err != nil {
		return err
	} else if tok != json.Delim('{') {
		return errors.New("manifest must be a JSON object")
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch key, _ := tok.(string); key {
		case "diaries":
			err = eachElement(dec, func() error {
				var d Diary
				if err := dec.Decode(&d); err != nil {
					return err
				}
				return h.diary(d)
			})
		case "media":
			err = eachElement(dec, func() error {
				var m Media
				if err := dec.Decode(&m); err != nil {
					return err
				}
				return h.media(m)
			})
		case "conversations":
			err = eachElement(dec, func() error {
				var c Conversation
				if err := dec.Decode(&c); err != nil {
					return err
				}
				return h.conversation(c)
			})
		default:
			var skip json.RawMessage
			err = dec.Decode(&skip)
		}
		if err != nil {
			return err
		}
	}
	_, err = dec.Token()
	return err
}

// eachElement walks a JSON array (or null) calling fn once per element.
func eachElement(dec *json.Decoder, fn func() error) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok == nil {
		return nil
	}
	if tok != json.Delim('[') {
		return fmt.Errorf("expected an array, got %v", tok)
	}
	for dec.More() {
		if err := fn(); err != nil {
			return err
		}
	}
	_, err = dec.Token()
	return err
}

type importer struct {
	ctx      context.Context
	s        *store.Store
	userID   string
	files    map[string]*zip.File
	stats    *ImportStats
	diaryIDs map[string]string // archive diary ID -> ID in this account
}

func (im *importer) diary(d Diary) error {
	im.stats.Diaries.Total++
	if d.Date == "" {
		im.stats.Diaries.Failed++
		return im.ctx.Err()
	}
	if existing, err := im.s.FindDiaryByDate(im.userID, d.Date); err == nil {
		// Keep the existing entry, but let media and messages that pointed
		// at the archived one link to it.
		im.stats.Diaries.Skipped++
		im.diaryIDs[d.ID] = existing.ID
		return im.ctx.Err()
	}
	diary, err := im.s.InsertImportedDiary(im.userID, "", d.Date, d.Content, d.Mood, d.Weather)
	if err != nil {
		im.stats.Diaries.Failed++
		return im.ctx.Err()
	}
	im.diaryIDs[d.ID] = diary.ID
	im.stats.Diaries.Imported++
	return im.ctx.Err()
}

func (im *importer) media(m Media) error {
	im.stats.Media.Total++
	if err := im.importMedia(m); err != nil {
		logger.Warn("[Import] media %s (%s) failed: %v", m.ID, m.File, err)
		im.stats.Media.Failed++
	} else {
		im.stats.Media.Imported++
	}
	return im.ctx.Err()
}

func (im *importer) importMedia(m Media) error {
	if m.File == "" {
		return errors.New("missing file name")
	}
	name := m.Path
	if name == "" {
		name = "media/" + m.File
	}
	f, ok := im.files[name]
	if !ok {
		return fmt.Errorf("%s not found in archive", name)
	}
	if f.UncompressedSize64 > MaxMediaFileSize {
		return fmt.Errorf("file is larger than %d bytes", MaxMediaFileSize)
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	body := bufio.NewReaderSize(rc, 4096)
	head, _ := body.Peek(1024)
	if detected, allowed := config.IsAllowedMediaType(head); !allowed {
		return fmt.Errorf("disallowed MIME type %s", detected)
	}

	diaries := make([]string, 0, len(m.Diary))
	for _, oldID := range m.Diary {
		if newID := im.diaryIDs[oldID]; newID != "" {
			diaries = append(diaries, newID)
		}
	}
	media, err := im.s.CreateMedia(im.userID, store.SafeFilename(m.File), m.Name, m.Alt, diaries)
	if err != nil {
		return err
	}
	dst := im.s.NewMediaFilePath(media.ID, media.File)
	if err := im.s.SaveUploadedFile(dst, io.LimitReader(body, MaxMediaFileSize)); err != nil {
		_ = os.Remove(dst)
		_ = im.s.DeleteMedia(media.ID, im.userID)
		return err
	}
	im.importLive(m, media)
	return nil
}

// importLive restores the clip of a live photo. The still is imported
// either way: a clip that is missing or invalid only leaves it a plain image.
func (im *importer) importLive(m Media, media *store.Media) {
	f, ok := im.files[m.LivePath]
	if m.LivePath == "" || !ok {
		return
	}
	rc, err := f.Open()
	if err != nil {
		logger.Warn("[Import] media %s live video failed: %v", m.ID, err)
		return
	}
	defer rc.Close()
	body := bufio.NewReaderSize(rc, 4096)
	head, _ := body.Peek(16)
	if !imaging.IsLiveVideo(head) {
		logger.Warn("[Import] media %s live video is not an MP4/QuickTime file", m.ID)
		return
	}
	if err := im.s.SaveMediaLive(media, body); err != nil {
		logger.Warn("[Import] media %s live video failed: %v", m.ID, err)
	}
}

func (im *importer) conversation(c Conversation) error {
	im.stats.Conversations.Total++
	record, err := im.s.CreateConversation(im.userID, c.Title)
	if err != nil {
		im.stats.Conversations.Failed++
		return im.ctx.Err()
	}
	for _, msg := range c.Messages {
		refs := make([]string, 0, len(msg.ReferencedDiaries))
		for _, oldID := range msg.ReferencedDiaries {
			if newID := im.diaryIDs[oldID]; newID != "" {
				refs = append(refs, newID)
			}
		}
		_, _ = im.s.CreateMessage(im.userID, record.ID, msg.Role, msg.Content, refs)
	}
	im.stats.Conversations.Imported++
	return im.ctx.Err()
}
