package archive

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/songtianlun/diarum/internal/store"
)

// Options selects what an export contains.
type Options struct {
	// Start and End bound the export, inclusive. Diaries are matched by
	// their date, media by creation and conversations by last update.
	Start, End           time.Time
	IncludeDiaries       bool
	IncludeMedia         bool
	IncludeConversations bool
	// TempDir holds media downloaded from remote storage while it is copied
	// into the archive. Empty means the system temp directory.
	TempDir string
	// Now overrides the clock for exported_at; nil means time.Now.
	Now func() time.Time
}

// AllTime returns options that export everything.
func AllTime(now time.Time) Options {
	return Options{
		Start:                time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
		End:                  now.UTC(),
		IncludeDiaries:       true,
		IncludeMedia:         true,
		IncludeConversations: true,
	}
}

// Export writes userID's data to w as a ZIP archive.
//
// Media that cannot be read is skipped and reported in FailedItems. An error
// is returned only when the archive itself cannot be written (or ctx ends),
// in which case whatever reached w is incomplete and must be discarded.
func Export(ctx context.Context, s *store.Store, userID string, opts Options, w io.Writer) (*ExportStats, error) {
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	at := now().UTC()
	stats := &ExportStats{
		StartDate:   opts.Start.Format(dateLayout),
		EndDate:     opts.End.Format(dateLayout),
		FailedItems: make([]FailedItem, 0),
	}
	stats.Diaries.TotalInSystem = s.CountDiaries(userID)
	stats.Media.TotalInSystem = s.CountMedia(userID)
	conversations, err := s.ListConversations(userID, math.MaxInt32)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	stats.Conversations.TotalInSystem = len(conversations)

	e := &exporter{ctx: ctx, s: s, userID: userID, opts: opts, stats: stats, zw: zip.NewWriter(w), at: at}
	media, err := e.writeManifest(conversations)
	if err != nil {
		return nil, err
	}
	if opts.IncludeDiaries {
		if err := e.writeMarkdown(); err != nil {
			return nil, err
		}
	}
	for _, m := range media {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := e.writeMediaFile(m); err != nil {
			return nil, err
		}
	}
	if err := e.zw.Close(); err != nil {
		return nil, fmt.Errorf("finalize archive: %w", err)
	}
	return stats, nil
}

type exporter struct {
	ctx    context.Context
	s      *store.Store
	userID string
	opts   Options
	stats  *ExportStats
	zw     *zip.Writer
	at     time.Time
}

func (e *exporter) create(name string) (io.Writer, error) {
	w, err := e.zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: e.at})
	if err != nil {
		return nil, fmt.Errorf("add %s: %w", name, err)
	}
	return w, nil
}

// writeManifest streams diarum_export.json and returns the media whose files
// still have to be copied into the archive.
func (e *exporter) writeManifest(conversations []*store.Conversation) ([]Media, error) {
	fw, err := e.create(ManifestName)
	if err != nil {
		return nil, err
	}
	out := &jsonStream{w: bufio.NewWriterSize(fw, 256<<10)}
	out.raw(fmt.Sprintf(`{"version":%d,"exported_at":"%s","diaries":[`, FormatVersion, e.at.Format(time.RFC3339)))

	if e.opts.IncludeDiaries {
		err := e.eachDiary(func(d Diary) error {
			e.stats.Diaries.ShouldExport++
			return out.elem(d)
		})
		if err != nil {
			return nil, err
		}
	}
	e.stats.Diaries.ActualExported = e.stats.Diaries.ShouldExport

	out.raw(`],"media":[`)
	media := make([]Media, 0)
	if e.opts.IncludeMedia {
		err := e.eachMedia(func(m Media) error {
			media = append(media, m)
			return out.elem(m)
		})
		if err != nil {
			return nil, err
		}
	}
	e.stats.Media.ShouldExport = len(media)

	out.raw(`],"conversations":[`)
	if e.opts.IncludeConversations {
		for _, conv := range conversations {
			if err := e.ctx.Err(); err != nil {
				return nil, err
			}
			if !InRange(store.DateOnly(conv.Updated), e.opts.Start, e.opts.End) {
				continue
			}
			messages, err := e.s.ListMessages(conv.ID, 0)
			if err != nil {
				e.fail("conversation", conv.ID, err)
				continue
			}
			item := Conversation{ID: conv.ID, Title: conv.Title, Messages: make([]Message, 0, len(messages))}
			for _, msg := range messages {
				item.Messages = append(item.Messages, Message{ID: msg.ID, Role: msg.Role, Content: msg.Content, ReferencedDiaries: msg.ReferencedDiaries})
			}
			if err := out.elem(item); err != nil {
				return nil, err
			}
			e.stats.Messages += len(item.Messages)
			e.stats.Conversations.ShouldExport++
		}
	}
	e.stats.Conversations.ActualExported = e.stats.Conversations.ShouldExport

	out.raw(`]}`)
	if err := out.flush(); err != nil {
		return nil, fmt.Errorf("write %s: %w", ManifestName, err)
	}
	return media, nil
}

// eachDiary visits the user's in-range diaries in date order.
func (e *exporter) eachDiary(fn func(Diary) error) error {
	afterDate, afterID := "", ""
	for {
		if err := e.ctx.Err(); err != nil {
			return err
		}
		batch, err := e.s.ListDiariesAfter(e.userID, afterDate, afterID, batchSize)
		if err != nil {
			return fmt.Errorf("list diaries: %w", err)
		}
		for _, d := range batch {
			date := store.DateOnly(d.Date)
			if !InRange(date, e.opts.Start, e.opts.End) {
				continue
			}
			if err := fn(Diary{ID: d.ID, Date: date, Content: d.Content, Mood: d.Mood, Weather: d.Weather}); err != nil {
				return err
			}
		}
		if len(batch) < batchSize {
			return nil
		}
		afterDate, afterID = batch[len(batch)-1].Date, batch[len(batch)-1].ID
	}
}

// eachMedia visits the user's in-range media in creation order.
func (e *exporter) eachMedia(fn func(Media) error) error {
	afterCreated, afterID := "", ""
	for {
		if err := e.ctx.Err(); err != nil {
			return err
		}
		batch, err := e.s.ListMediaAfter(e.userID, afterCreated, afterID, batchSize)
		if err != nil {
			return fmt.Errorf("list media: %w", err)
		}
		for _, m := range batch {
			if !InRange(store.DateOnly(m.Created), e.opts.Start, e.opts.End) {
				continue
			}
			item := Media{ID: m.ID, File: m.File, Path: "media/" + m.ID + "/" + store.SafeFilename(m.File), Name: m.Name, Alt: m.Alt, Diary: m.Diary, Owner: m.Owner}
			if m.Live != "" {
				item.Live, item.LivePath = m.Live, "media/"+m.ID+"/"+store.SafeFilename(m.Live)
			}
			if err := fn(item); err != nil {
				return err
			}
		}
		if len(batch) < batchSize {
			return nil
		}
		afterCreated, afterID = batch[len(batch)-1].Created, batch[len(batch)-1].ID
	}
}

func (e *exporter) writeMarkdown() error {
	return e.eachDiary(func(d Diary) error {
		name := d.Date
		if d.Mood != "" {
			name += "_" + strings.NewReplacer("/", "_", "\\", "_").Replace(d.Mood)
		}
		w, err := e.create("markdown/" + name + ".md")
		if err != nil {
			return err
		}
		if _, err := io.WriteString(w, Markdown(d)); err != nil {
			return fmt.Errorf("write markdown for %s: %w", d.Date, err)
		}
		return nil
	})
}

// writeMediaFile copies one media file, and its live photo clip if any, into
// the archive. A file that cannot be read is recorded as failed; an error
// means the archive is broken.
func (e *exporter) writeMediaFile(m Media) error {
	copied, err := e.copyMediaObject(m, m.File, m.Path)
	if err != nil || !copied {
		return err
	}
	e.stats.Media.ActualExported++
	if m.Live != "" {
		if _, err := e.copyMediaObject(m, m.Live, m.LivePath); err != nil {
			return err
		}
	}
	return nil
}

// copyMediaObject copies the stored file of m named file to the archive
// entry path; false (and no error) when the file cannot be read.
func (e *exporter) copyMediaObject(m Media, file, path string) (bool, error) {
	reader, err := e.s.OpenMediaFile(&store.Media{ID: m.ID, File: file, Owner: m.Owner})
	if err != nil {
		e.fail("media", m.ID, err)
		return false, nil
	}
	defer reader.Close()

	var src io.Reader = reader
	if _, local := reader.(*os.File); !local {
		// Download remote objects first: a stream that breaks halfway must
		// not leave a truncated entry behind in the archive.
		spool, err := os.CreateTemp(e.opts.TempDir, "diarum-media-*")
		if err != nil {
			return false, fmt.Errorf("create spool file: %w", err)
		}
		defer func() {
			_ = spool.Close()
			_ = os.Remove(spool.Name())
		}()
		if _, err := io.Copy(spool, reader); err != nil {
			e.fail("media", m.ID, err)
			return false, nil
		}
		if _, err := spool.Seek(0, io.SeekStart); err != nil {
			return false, fmt.Errorf("rewind spool file: %w", err)
		}
		src = spool
	}

	w, err := e.create(path)
	if err != nil {
		return false, err
	}
	if _, err := io.Copy(w, src); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

func (e *exporter) fail(kind, id string, err error) {
	e.stats.FailedItems = append(e.stats.FailedItems, FailedItem{Type: kind, ID: id, Reason: err.Error()})
}

// jsonStream writes a JSON document piece by piece, remembering the first
// write error so callers can check once.
type jsonStream struct {
	w     *bufio.Writer
	first bool
	err   error
}

func (j *jsonStream) raw(s string) {
	if j.err == nil {
		_, j.err = j.w.WriteString(s)
	}
	// Every raw fragment written here opens a new array.
	j.first = true
}

func (j *jsonStream) elem(v any) error {
	if j.err != nil {
		return j.err
	}
	data, err := json.Marshal(v)
	if err != nil {
		j.err = err
		return err
	}
	if !j.first {
		if err := j.w.WriteByte(','); err != nil {
			j.err = err
			return err
		}
	}
	j.first = false
	_, j.err = j.w.Write(data)
	return j.err
}

func (j *jsonStream) flush() error {
	if j.err != nil {
		return j.err
	}
	return j.w.Flush()
}
