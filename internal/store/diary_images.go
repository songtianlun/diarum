package store

import (
	"database/sql"
	"html"
	"regexp"
	"strings"
)

// DiaryImage is an image a diary entry shows. Images uploaded to Diarum
// (local or S3) carry their media ID; anything else, such as a Chevereto
// upload or a pasted link, is external and can only be removed where it is
// hosted.
type DiaryImage struct {
	URL     string `json:"url"`
	MediaID string `json:"mediaId,omitempty"`
	Managed bool   `json:"managed"`
}

var (
	htmlImagePattern     = regexp.MustCompile(`(?is)<img\b[^>]*?\ssrc\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	markdownImagePattern = regexp.MustCompile(`!\[[^\]]*\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)`)
)

// ExtractDiaryImages lists the images in diary content (HTML, or Markdown
// written through the API), in order of first appearance and each URL once.
// Inline data and blob URLs are not images anyone can manage, so they are
// skipped.
func ExtractDiaryImages(content string) []DiaryImage {
	images := make([]DiaryImage, 0)
	seen := make(map[string]bool)
	add := func(raw string) {
		src := strings.TrimSpace(html.UnescapeString(raw))
		lower := strings.ToLower(src)
		if src == "" || seen[src] || strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "blob:") || len(src) > 2048 {
			return
		}
		seen[src] = true
		image := DiaryImage{URL: src}
		if ids := ReferencedMediaIDs(src); len(ids) > 0 {
			image.MediaID = ids[0]
			image.Managed = true
		}
		images = append(images, image)
	}
	type match struct {
		at  int
		src string
	}
	matches := make([]match, 0)
	for _, m := range htmlImagePattern.FindAllStringSubmatchIndex(content, -1) {
		for group := 1; group <= 3; group++ {
			if m[2*group] >= 0 {
				matches = append(matches, match{m[0], content[m[2*group]:m[2*group+1]]})
				break
			}
		}
	}
	for _, m := range markdownImagePattern.FindAllStringSubmatchIndex(content, -1) {
		matches = append(matches, match{m[0], content[m[2]:m[3]]})
	}
	// Keep document order across both syntaxes.
	for i := 1; i < len(matches); i++ {
		for j := i; j > 0 && matches[j].at < matches[j-1].at; j-- {
			matches[j], matches[j-1] = matches[j-1], matches[j]
		}
	}
	for _, m := range matches {
		add(m.src)
	}
	return images
}

// execer is what both *sql.DB and *sql.Tx offer.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// replaceDiaryImages records the images a diary entry shows now, replacing
// what was recorded for it before.
func replaceDiaryImages(q execer, owner, diaryID, diaryDate, content string) error {
	if _, err := q.Exec(`DELETE FROM diary_images WHERE diary = ?`, diaryID); err != nil {
		return err
	}
	for position, image := range ExtractDiaryImages(content) {
		if _, err := q.Exec(`INSERT INTO diary_images(owner, diary, diary_date, url, media, position) VALUES(?, ?, ?, ?, ?, ?)`,
			owner, diaryID, DateOnly(diaryDate), image.URL, image.MediaID, position); err != nil {
			return err
		}
	}
	return nil
}

// DiaryImages returns the images a diary entry shows, in order.
func (s *Store) DiaryImages(diaryID string) ([]DiaryImage, error) {
	rows, err := s.DB.Query(`SELECT url, media FROM diary_images WHERE diary = ? ORDER BY position`, diaryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	images := make([]DiaryImage, 0)
	for rows.Next() {
		var image DiaryImage
		if err := rows.Scan(&image.URL, &image.MediaID); err != nil {
			return nil, err
		}
		image.Managed = image.MediaID != ""
		images = append(images, image)
	}
	return images, rows.Err()
}

// BackfillDiaryImages records the images of entries saved before images
// were tracked. Only entries that show an image but have nothing recorded
// are read, so after the first run it costs one query.
func (s *Store) BackfillDiaryImages() error {
	rows, err := s.DB.Query(`SELECT id, owner, date, content FROM diaries d
		WHERE (content LIKE '%<img%' OR content LIKE '%![%')
		AND NOT EXISTS (SELECT 1 FROM diary_images i WHERE i.diary = d.id)`)
	if err != nil {
		return err
	}
	type pending struct{ id, owner, date, content string }
	todo := make([]pending, 0)
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.owner, &p.date, &p.content); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, p := range todo {
		if err := replaceDiaryImages(s.DB, p.owner, p.id, p.date, p.content); err != nil {
			return err
		}
	}
	return nil
}

// Gallery item kinds.
const (
	GalleryKindMedia    = "media"
	GalleryKindExternal = "external"
)

// GalleryItem is one image on the library timeline: an image stored by
// Diarum (Managed, with its media record) or an external image entries show,
// listed once by URL.
type GalleryItem struct {
	MediaWithExpand
	Kind    string `json:"kind"`
	Managed bool   `json:"managed"`
	// URL is set for external images.
	URL string `json:"url,omitempty"`
}

// galleryUnion lists every image of an owner on the timeline: stored images
// outside the trash by their own date, and external images by the earliest
// entry showing them. Its parameters are the owner, twice.
const galleryUnion = `
	SELECT 'media' AS kind, id AS key, CASE WHEN date != '' THEN date ELSE created END AS sort_date, created AS sort_created
		FROM media WHERE owner = ? AND deleted = ''
	UNION ALL
	SELECT 'external', url, MIN(diary_date) || ' 00:00:00.000Z', MIN(diary_date) || ' 00:00:00.000Z'
		FROM diary_images WHERE owner = ? AND media = '' GROUP BY url`

// ListGallery returns a page of owner's library timeline, newest first,
// mixing images stored by Diarum with external images entries show.
func (s *Store) ListGallery(owner string, page, perPage int) ([]GalleryItem, int, error) {
	if page <= 0 {
		page = 1
	}
	if perPage <= 0 {
		perPage = 50
	}
	var total int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM (`+galleryUnion+`)`, owner, owner).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.Query(`SELECT kind, key, sort_date FROM (`+galleryUnion+`)
		ORDER BY sort_date DESC, sort_created DESC, key DESC LIMIT ? OFFSET ?`, owner, owner, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, err
	}
	type ref struct{ kind, key, date string }
	refs := make([]ref, 0, perPage)
	for rows.Next() {
		var r ref
		if err := rows.Scan(&r.kind, &r.key, &r.date); err != nil {
			rows.Close()
			return nil, 0, err
		}
		refs = append(refs, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	rows.Close()

	mediaIDs := make([]any, 0, len(refs))
	urls := make([]any, 0, len(refs))
	for _, r := range refs {
		if r.kind == GalleryKindMedia {
			mediaIDs = append(mediaIDs, r.key)
		} else {
			urls = append(urls, r.key)
		}
	}
	media := make(map[string]MediaWithExpand, len(mediaIDs))
	if len(mediaIDs) > 0 {
		rows, err := s.DB.Query(`SELECT `+mediaColumns+` FROM media WHERE owner = ? AND id IN (`+placeholders(len(mediaIDs))+`)`, append([]any{owner}, mediaIDs...)...)
		if err != nil {
			return nil, 0, err
		}
		expanded, _, err := s.expandMediaRows(owner, rows, 0)
		if err != nil {
			return nil, 0, err
		}
		for _, item := range expanded {
			media[item.ID] = item
		}
	}
	external, err := s.externalImageDiaries(owner, urls)
	if err != nil {
		return nil, 0, err
	}

	items := make([]GalleryItem, 0, len(refs))
	for _, r := range refs {
		if r.kind == GalleryKindMedia {
			if item, ok := media[r.key]; ok {
				items = append(items, GalleryItem{MediaWithExpand: item, Kind: GalleryKindMedia, Managed: true})
			}
			continue
		}
		diaries := external[r.key]
		items = append(items, GalleryItem{
			MediaWithExpand: MediaWithExpand{
				Media:  Media{Name: externalImageName(r.key), Date: r.date, Created: r.date, Owner: owner},
				Expand: map[string]any{"diary": diaries},
			},
			Kind: GalleryKindExternal,
			URL:  r.key,
		})
	}
	return items, total, nil
}

// externalImageDiaries returns, for each URL, the entries of owner showing
// it, oldest first.
func (s *Store) externalImageDiaries(owner string, urls []any) (map[string][]Diary, error) {
	result := make(map[string][]Diary, len(urls))
	if len(urls) == 0 {
		return result, nil
	}
	rows, err := s.DB.Query(`SELECT i.url, d.id, d.date FROM diary_images i JOIN diaries d ON d.id = i.diary
		WHERE i.owner = ? AND i.media = '' AND i.url IN (`+placeholders(len(urls))+`) ORDER BY d.date ASC`, append([]any{owner}, urls...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var url string
		var diary Diary
		if err := rows.Scan(&url, &diary.ID, &diary.Date); err != nil {
			return nil, err
		}
		diary.Owner = owner
		result[url] = append(result[url], diary)
	}
	return result, rows.Err()
}

// CountExternalImages counts the distinct external images owner's entries show.
func (s *Store) CountExternalImages(owner string) (int, error) {
	var count int
	err := s.DB.QueryRow(`SELECT COUNT(DISTINCT url) FROM diary_images WHERE owner = ? AND media = ''`, owner).Scan(&count)
	return count, err
}

// externalImageName is a readable name for an image URL: its file name.
func externalImageName(raw string) string {
	name := raw
	if i := strings.IndexAny(name, "?#"); i >= 0 {
		name = name[:i]
	}
	name = strings.TrimRight(name, "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if name == "" {
		return raw
	}
	return name
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
