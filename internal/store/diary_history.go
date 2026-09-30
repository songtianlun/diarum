package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Diary history keeps earlier states of an entry in a side table. The diaries
// table always holds the latest version, so reads, search, statistics and
// vector indexing never have to know that history exists.
const (
	// SettingDiaryMaxSnapshots is the per-user setting that caps how many
	// history snapshots are kept for each diary entry.
	SettingDiaryMaxSnapshots = "diary.max_snapshots"
	DefaultDiarySnapshots    = 10
	MinDiarySnapshots        = 1
	MaxDiarySnapshots        = 20

	// DefaultDiarySnapshotInterval coalesces autosaves: the editor saves about
	// a second after every pause in typing, and without coalescing a single
	// writing session would push every older version out of the history. The
	// state before an editing session is always kept, and long sessions leave
	// a checkpoint at most this far apart.
	DefaultDiarySnapshotInterval = 5 * time.Minute

	revisionTimeLayout = "2006-01-02 15:04:05.000Z"
)

// DiaryRevision is one archived state of a diary entry.
type DiaryRevision struct {
	ID      string `json:"id"`
	Owner   string `json:"owner"`
	Date    string `json:"date"`
	DiaryID string `json:"diary"`
	Content string `json:"content"`
	Mood    string `json:"mood"`
	Weather string `json:"weather"`
	// Saved is when this version was written by the user (the diary's
	// updated timestamp at the time); Created is when it was archived.
	Saved   string `json:"saved"`
	Created string `json:"created"`
}

const diaryRevisionColumns = `id, owner, date, diary, content, mood, weather, saved, created`

// ClampDiarySnapshots keeps a snapshot limit within the supported range.
func ClampDiarySnapshots(limit int) int {
	if limit < MinDiarySnapshots {
		return MinDiarySnapshots
	}
	if limit > MaxDiarySnapshots {
		return MaxDiarySnapshots
	}
	return limit
}

// DiarySnapshotLimit returns the user's configured snapshot limit, falling
// back to the default when unset or unreadable.
func (s *Store) DiarySnapshotLimit(owner string) int {
	value, err := s.GetSetting(owner, SettingDiaryMaxSnapshots)
	if err != nil || value == nil {
		return DefaultDiarySnapshots
	}
	switch v := value.(type) {
	case float64:
		if v > MaxDiarySnapshots {
			return MaxDiarySnapshots // also keeps huge values from overflowing int
		}
		return ClampDiarySnapshots(int(v))
	case string:
		if parsed, parseErr := strconv.Atoi(strings.TrimSpace(v)); parseErr == nil {
			return ClampDiarySnapshots(parsed)
		}
	}
	return DefaultDiarySnapshots
}

func (s *Store) currentTime() time.Time {
	if s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

// saveDiary writes the latest state of an entry, archiving the previous
// state first. forceSnapshot skips autosave coalescing, for deliberate
// actions such as restoring an older version.
func (s *Store) saveDiary(owner, date, content, mood, weather string, forceSnapshot bool) (*Diary, bool, error) {
	limit := s.DiarySnapshotLimit(owner)
	start, end := dayRange(date)
	id := ""
	created := false
	err := s.Transaction(context.Background(), func(tx *sql.Tx) error {
		existing, err := scanDiary(tx.QueryRow(`SELECT content, created, date, id, mood, owner, updated, weather, tags FROM diaries WHERE date >= ? AND date <= ? AND owner = ? LIMIT 1`, start, end, owner))
		if err == nil {
			id = existing.ID
			changed := existing.Content != content || existing.Mood != mood || existing.Weather != weather
			if changed {
				if err := s.archiveDiary(tx, existing, limit, forceSnapshot); err != nil {
					return err
				}
			}
			if _, err = tx.Exec(`UPDATE diaries SET content = ?, mood = ?, weather = ?, updated = ? WHERE id = ? AND owner = ?`, content, mood, weather, nowString(), existing.ID, owner); err != nil {
				return err
			}
			return replaceDiaryImages(tx, owner, existing.ID, existing.Date, content)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if id, err = GenerateID(); err != nil {
			return err
		}
		created = true
		now := nowString()
		if _, err = tx.Exec(`INSERT INTO diaries(content, created, date, id, mood, owner, updated, weather, tags) VALUES(?, ?, ?, ?, ?, ?, ?, ?, '[]')`, content, now, date+" 00:00:00.000Z", id, mood, owner, now, weather); err != nil {
			return err
		}
		return replaceDiaryImages(tx, owner, id, date, content)
	})
	if err != nil {
		return nil, created, err
	}
	s.noteDiaryWrite(owner)
	// Linking is bookkeeping for the media library; the entry itself is
	// already saved, so a failure here must not fail the save.
	_ = s.LinkDiaryMedia(owner, id, content)
	diary, err := s.GetDiaryByID(id)
	return diary, created, err
}

// archiveDiary copies the given diary state into the history table and trims
// the entry's history to limit. It is a no-op for blank entries and for
// states the newest snapshot already holds. Unless force is set, a snapshot
// taken within SnapshotInterval absorbs this one (autosave coalescing).
func (s *Store) archiveDiary(q *sql.Tx, diary *Diary, limit int, force bool) error {
	if diary.Content == "" && diary.Mood == "" && diary.Weather == "" {
		return nil
	}
	date := DateOnly(diary.Date)
	now := s.currentTime()

	var latestContent, latestMood, latestWeather, latestCreated string
	err := q.QueryRow(`SELECT content, mood, weather, created FROM diary_revisions WHERE owner = ? AND date = ? ORDER BY created DESC, rowid DESC LIMIT 1`, diary.Owner, date).
		Scan(&latestContent, &latestMood, &latestWeather, &latestCreated)
	switch {
	case err == nil:
		if latestContent == diary.Content && latestMood == diary.Mood && latestWeather == diary.Weather {
			return nil
		}
		if !force && s.SnapshotInterval > 0 {
			if taken, parseErr := time.Parse(revisionTimeLayout, latestCreated); parseErr == nil && now.Sub(taken) < s.SnapshotInterval {
				return nil
			}
		}
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}

	id, err := GenerateID()
	if err != nil {
		return err
	}
	if _, err := q.Exec(`INSERT INTO diary_revisions(`+diaryRevisionColumns+`) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, diary.Owner, date, diary.ID, diary.Content, diary.Mood, diary.Weather, diary.Updated, now.Format(revisionTimeLayout)); err != nil {
		return err
	}
	return pruneDiaryRevisions(q, diary.Owner, date, limit)
}

func pruneDiaryRevisions(q *sql.Tx, owner, date string, limit int) error {
	_, err := q.Exec(`DELETE FROM diary_revisions WHERE owner = ? AND date = ? AND id NOT IN (
		SELECT id FROM diary_revisions WHERE owner = ? AND date = ? ORDER BY created DESC, rowid DESC LIMIT ?
	)`, owner, date, owner, date, ClampDiarySnapshots(limit))
	return err
}

// ListDiaryRevisions returns the history of one entry (date as YYYY-MM-DD),
// newest first, never more than the user's current snapshot limit.
func (s *Store) ListDiaryRevisions(owner, date string) ([]*DiaryRevision, error) {
	rows, err := s.DB.Query(`SELECT `+diaryRevisionColumns+` FROM diary_revisions WHERE owner = ? AND date = ? ORDER BY created DESC, rowid DESC LIMIT ?`,
		owner, DateOnly(date), s.DiarySnapshotLimit(owner))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*DiaryRevision, 0)
	for rows.Next() {
		item, err := scanDiaryRevision(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetDiaryRevision returns one snapshot, only if it belongs to owner.
func (s *Store) GetDiaryRevision(owner, id string) (*DiaryRevision, error) {
	return scanDiaryRevision(s.DB.QueryRow(`SELECT `+diaryRevisionColumns+` FROM diary_revisions WHERE id = ? AND owner = ?`, id, owner))
}

// RestoreDiaryRevision makes a snapshot the latest version of its entry. The
// state it replaces is archived first, so a restore can itself be undone.
// This also brings back an entry that was deleted since.
func (s *Store) RestoreDiaryRevision(owner, id string) (*Diary, error) {
	revision, err := s.GetDiaryRevision(owner, id)
	if err != nil {
		return nil, err
	}
	diary, _, err := s.saveDiary(owner, revision.Date, revision.Content, revision.Mood, revision.Weather, true)
	return diary, err
}

func scanDiaryRevision(row interface{ Scan(dest ...any) error }) (*DiaryRevision, error) {
	item := &DiaryRevision{}
	if err := row.Scan(&item.ID, &item.Owner, &item.Date, &item.DiaryID, &item.Content, &item.Mood, &item.Weather, &item.Saved, &item.Created); err != nil {
		return nil, err
	}
	return item, nil
}
