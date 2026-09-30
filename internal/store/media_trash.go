package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
)

// Where an uploaded image's file lives.
const (
	MediaStorageLocal   = "local"
	MediaStorageS3      = "s3"
	MediaStorageUnknown = "unknown"
)

// Why an image was moved to the trash, or removed from it for good.
const (
	// MediaReasonUser: deleted by hand from the media library.
	MediaReasonUser = "user"
	// MediaReasonUnlinked: no diary entry uses the image any more.
	MediaReasonUnlinked = "unlinked"
	// MediaReasonRetention: it stayed in the trash past the retention period.
	MediaReasonRetention = "retention"
	// MediaReasonEmptyTrash: the whole trash was emptied.
	MediaReasonEmptyTrash = "empty_trash"
	// MediaReasonReferenced: a trashed image was restored because an entry
	// was saved with it again.
	MediaReasonReferenced = "referenced"
)

// Who started a media action: a person, or Diarum on its own.
const (
	MediaTriggerManual = "manual"
	MediaTriggerAuto   = "auto"
)

// mediaColumns is the column list scanMedia reads, in order.
const mediaColumns = `alt, created, file, id, name, owner, updated, diary, date, storage, deleted, deleted_by, delete_reason, delete_trigger, s3_prefix`

// mediaTimelineOrder sorts images by their own date, newest first. Images not
// used in any entry yet have no date and fall back to their upload time.
const mediaTimelineOrder = `CASE WHEN date != '' THEN date ELSE created END DESC, created DESC, id DESC`

// ErrMediaBusy is returned when an image changed state while it was being
// worked on, for example restored while it was being purged.
var ErrMediaBusy = errors.New("media changed state")

// migrateMediaSchema adds the image date, storage and trash columns to
// databases created before they existed. Every step is idempotent.
func migrateMediaSchema(db *sql.DB) error {
	columns := []struct{ name, ddl string }{
		{"date", `ALTER TABLE media ADD COLUMN date TEXT DEFAULT '' NOT NULL`},
		{"storage", `ALTER TABLE media ADD COLUMN storage TEXT DEFAULT '' NOT NULL`},
		{"deleted", `ALTER TABLE media ADD COLUMN deleted TEXT DEFAULT '' NOT NULL`},
		{"deleted_by", `ALTER TABLE media ADD COLUMN deleted_by TEXT DEFAULT '' NOT NULL`},
		{"delete_reason", `ALTER TABLE media ADD COLUMN delete_reason TEXT DEFAULT '' NOT NULL`},
		{"delete_trigger", `ALTER TABLE media ADD COLUMN delete_trigger TEXT DEFAULT '' NOT NULL`},
		{"s3_prefix", `ALTER TABLE media ADD COLUMN s3_prefix TEXT DEFAULT '' NOT NULL`},
	}
	for _, column := range columns {
		exists, err := columnExists(db, "media", column.name)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if _, err := db.Exec(column.ddl); err != nil {
			return err
		}
	}
	_, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_media_owner_deleted ON media(owner, deleted)`)
	return err
}

// mediaDate turns a diary date into an image date: the same day at midnight.
func mediaDate(diaryDate string) string {
	day := DateOnly(diaryDate)
	if len(day) != len("2006-01-02") {
		return ""
	}
	return day + " 00:00:00.000Z"
}

// BackfillMediaDates dates every image that has none yet but is linked to an
// entry, after the first of its entries that still exists. It is cheap and
// idempotent: dated images are never touched.
func (s *Store) BackfillMediaDates() error {
	_, err := s.DB.Exec(`UPDATE media SET date = (
			SELECT substr(d.date, 1, 10) || ' 00:00:00.000Z'
			FROM json_each(media.diary) AS link
			JOIN diaries d ON d.id = link.value AND d.owner = media.owner
			WHERE d.date != ''
			ORDER BY link.key LIMIT 1
		)
		WHERE date = '' AND diary != '[]' AND json_valid(diary) AND EXISTS (
			SELECT 1 FROM json_each(media.diary) AS link
			JOIN diaries d ON d.id = link.value AND d.owner = media.owner
			WHERE d.date != ''
		)`)
	return err
}

// backfillMediaStorage records where images uploaded before the storage
// column existed live: on disk if the file is there, otherwise in the S3
// bucket the owner (or the legacy PocketBase setup) configured.
func (s *Store) backfillMediaStorage() error {
	rows, err := s.DB.Query(`SELECT ` + mediaColumns + ` FROM media WHERE storage = ''`)
	if err != nil {
		return err
	}
	pending := make([]*Media, 0)
	for rows.Next() {
		media, err := scanMediaRow(rows)
		if err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, media)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, media := range pending {
		storage := MediaStorageUnknown
		if fileExists(s.MediaFilePath(media)) {
			storage = MediaStorageLocal
		} else if s.userS3Config(media.Owner) != nil || s.LegacyS3 != nil {
			storage = MediaStorageS3
		}
		if _, err := s.DB.Exec(`UPDATE media SET storage = ? WHERE id = ? AND storage = ''`, storage, media.ID); err != nil {
			return err
		}
	}
	return nil
}

// setMediaStorage records where an upload went: the storage and, for S3,
// the key prefix in force, so later prefix changes still find the object.
func (s *Store) setMediaStorage(media *Media, storage, prefix string) error {
	if _, err := s.DB.Exec(`UPDATE media SET storage = ?, s3_prefix = ? WHERE id = ?`, storage, prefix, media.ID); err != nil {
		return err
	}
	media.Storage, media.S3Prefix = storage, prefix
	return nil
}

// setMediaDateIfEmpty gives an undated image the date of the entry it is
// being linked to.
func (s *Store) setMediaDateIfEmpty(media *Media, diaryID string) error {
	if media.Date != "" {
		return nil
	}
	diary, err := s.GetDiaryByID(diaryID)
	if err != nil || diary.Owner != media.Owner {
		return nil
	}
	date := mediaDate(diary.Date)
	if date == "" {
		return nil
	}
	if _, err := s.DB.Exec(`UPDATE media SET date = ? WHERE id = ? AND date = ''`, date, media.ID); err != nil {
		return err
	}
	media.Date = date
	return nil
}

// TrashInfo describes who moved an image to the trash, and why.
type TrashInfo struct {
	By      string
	Reason  string
	Trigger string
}

// TrashMedia moves an image of owner to the trash. Its file stays where it
// is, and keeps being served, until the image is purged. It returns
// sql.ErrNoRows if the image does not exist or is already in the trash.
func (s *Store) TrashMedia(id, owner string, info TrashInfo) (*Media, error) {
	s.mediaMu.Lock()
	defer s.mediaMu.Unlock()
	now := nowString()
	result, err := s.DB.Exec(`UPDATE media SET deleted = ?, deleted_by = ?, delete_reason = ?, delete_trigger = ?, updated = ?
		WHERE id = ? AND owner = ? AND deleted = ''`, now, info.By, info.Reason, info.Trigger, now, id, owner)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, sql.ErrNoRows
	}
	return s.GetMedia(id, owner)
}

// RestoreMedia takes an image of owner back out of the trash. It returns
// sql.ErrNoRows if the image does not exist or is not in the trash.
func (s *Store) RestoreMedia(id, owner string) (*Media, error) {
	s.mediaMu.Lock()
	defer s.mediaMu.Unlock()
	result, err := s.DB.Exec(`UPDATE media SET deleted = '', deleted_by = '', delete_reason = '', delete_trigger = '', updated = ?
		WHERE id = ? AND owner = ? AND deleted != ''`, nowString(), id, owner)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, sql.ErrNoRows
	}
	return s.GetMedia(id, owner)
}

// PurgeMedia removes a trashed image of owner for good: its file and variants
// from disk or object storage first, then its record. If the files cannot be
// removed the record stays in the trash, so a later attempt can finish the
// job. It returns sql.ErrNoRows if the image is not in the trash.
func (s *Store) PurgeMedia(id, owner string) (*Media, error) {
	s.mediaMu.Lock()
	defer s.mediaMu.Unlock()
	media, err := s.GetMedia(id, owner)
	if err != nil {
		return nil, err
	}
	if !media.InTrash() {
		return nil, sql.ErrNoRows
	}
	if err := s.DeleteMediaFile(media); err != nil && !errors.Is(err, os.ErrNotExist) {
		return media, fmt.Errorf("delete file: %w", err)
	}
	result, err := s.DB.Exec(`DELETE FROM media WHERE id = ? AND owner = ? AND deleted != ''`, id, owner)
	if err != nil {
		return media, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return media, ErrMediaBusy
	}
	return media, nil
}

// ListTrash returns a page of owner's trashed images on the image timeline.
func (s *Store) ListTrash(owner string, page, perPage int) ([]MediaWithExpand, int, error) {
	if page <= 0 {
		page = 1
	}
	if perPage <= 0 {
		perPage = 50
	}
	var total int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM media WHERE owner = ? AND deleted != ''`, owner).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.Query(`SELECT `+mediaColumns+` FROM media WHERE owner = ? AND deleted != '' ORDER BY `+mediaTimelineOrder+` LIMIT ? OFFSET ?`, owner, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, err
	}
	return s.expandMediaRows(owner, rows, total)
}

// TrashedMediaIDs returns the IDs of all of owner's trashed images, in
// timeline order, for acting on the whole trash without paging through it.
func (s *Store) TrashedMediaIDs(owner string) ([]string, error) {
	rows, err := s.DB.Query(`SELECT id FROM media WHERE owner = ? AND deleted != '' ORDER BY `+mediaTimelineOrder, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// TrashedMedia returns owner's trashed images moved there before the given
// time; a zero time returns all of them.
func (s *Store) TrashedMedia(owner string, before time.Time) ([]*Media, error) {
	query := `SELECT ` + mediaColumns + ` FROM media WHERE owner = ? AND deleted != ''`
	args := []any{owner}
	if !before.IsZero() {
		query += ` AND deleted < ?`
		args = append(args, before.UTC().Format(timeLayout))
	}
	return s.queryMedia(query+` ORDER BY deleted ASC, id ASC`, args...)
}

// UnlinkedMedia returns owner's images outside the trash that no diary entry
// references any more, oldest first. Images uploaded after createdBefore are
// skipped, so an image whose entry has not been saved yet is not mistaken for
// an unused one; a zero time skips none.
func (s *Store) UnlinkedMedia(owner string, createdBefore time.Time) ([]*Media, error) {
	referenced := make(map[string]struct{})
	if err := s.ScanDiaryContents(owner, func(_, content string) {
		for _, id := range ReferencedMediaIDs(content) {
			referenced[id] = struct{}{}
		}
	}); err != nil {
		return nil, err
	}
	query := `SELECT ` + mediaColumns + ` FROM media WHERE owner = ? AND deleted = ''`
	args := []any{owner}
	if !createdBefore.IsZero() {
		query += ` AND created < ?`
		args = append(args, createdBefore.UTC().Format(timeLayout))
	}
	all, err := s.queryMedia(query+` ORDER BY created ASC, id ASC`, args...)
	if err != nil {
		return nil, err
	}
	unlinked := make([]*Media, 0)
	for _, media := range all {
		if _, ok := referenced[media.ID]; !ok {
			unlinked = append(unlinked, media)
		}
	}
	return unlinked, nil
}

// IsMediaReferenced reports whether any of owner's entries uses the image.
func (s *Store) IsMediaReferenced(owner, id string) (bool, error) {
	found := false
	err := s.ScanDiaryContents(owner, func(_, content string) {
		if !found && strings.Contains(content, id) && slices.Contains(ReferencedMediaIDs(content), id) {
			found = true
		}
	})
	return found, err
}

// MediaStats summarises owner's media library.
type MediaStats struct {
	// Total counts images outside the trash; ByStorage splits them by where
	// their files live.
	Total     int            `json:"total"`
	ByStorage map[string]int `json:"byStorage"`
	// Linked counts images linked to at least one entry.
	Linked int `json:"linked"`
	// Trash counts images in the trash; OldestTrash is when the longest
	// waiting one was moved there.
	Trash       int    `json:"trash"`
	OldestTrash string `json:"oldestTrash"`
}

// MediaStats counts owner's images, by storage and trash state.
func (s *Store) MediaStats(owner string) (*MediaStats, error) {
	stats := &MediaStats{ByStorage: map[string]int{}}
	rows, err := s.DB.Query(`SELECT storage, COUNT(*), SUM(CASE WHEN diary NOT IN ('[]', '', 'null') THEN 1 ELSE 0 END)
		FROM media WHERE owner = ? AND deleted = '' GROUP BY storage`, owner)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var storage string
		var count, linked int
		if err := rows.Scan(&storage, &count, &linked); err != nil {
			rows.Close()
			return nil, err
		}
		if storage == "" {
			storage = MediaStorageUnknown
		}
		stats.ByStorage[storage] += count
		stats.Total += count
		stats.Linked += linked
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	var oldest sql.NullString
	if err := s.DB.QueryRow(`SELECT COUNT(*), MIN(deleted) FROM media WHERE owner = ? AND deleted != ''`, owner).Scan(&stats.Trash, &oldest); err != nil {
		return nil, err
	}
	stats.OldestTrash = oldest.String
	return stats, nil
}

// MediaOwners returns the users that have images, in or out of the trash.
func (s *Store) MediaOwners() ([]string, error) {
	rows, err := s.DB.Query(`SELECT DISTINCT owner FROM media WHERE owner != '' ORDER BY owner`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	owners := make([]string, 0)
	for rows.Next() {
		var owner string
		if err := rows.Scan(&owner); err != nil {
			return nil, err
		}
		owners = append(owners, owner)
	}
	return owners, rows.Err()
}

// queryMedia runs a media query and reads every row before returning, so the
// single database connection is free again for the caller.
func (s *Store) queryMedia(query string, args ...any) ([]*Media, error) {
	rows, err := s.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*Media, 0)
	for rows.Next() {
		media, err := scanMediaRow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, media)
	}
	return items, rows.Err()
}
