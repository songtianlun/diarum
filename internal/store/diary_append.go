package store

import (
	"context"
	"database/sql"
	"errors"
)

// AppendDiary atomically reads the current entry for date, computes the next
// content via build(existingContent), archives the replaced state into diary
// history, and saves the updated entry while preserving existing mood and
// weather. It returns the previous state (nil if created), the saved diary,
// whether a new entry was created, and any error.
func (s *Store) AppendDiary(owner, date string, build func(existingContent string) string) (*Diary, *Diary, bool, error) {
	limit := s.DiarySnapshotLimit(owner)
	start, end := dayRange(date)
	id := ""
	created := false
	var before *Diary
	var nextContent string

	err := s.Transaction(context.Background(), func(tx *sql.Tx) error {
		existing, err := scanDiary(tx.QueryRow(`SELECT content, created, date, id, mood, owner, updated, weather, tags FROM diaries WHERE date >= ? AND date <= ? AND owner = ? LIMIT 1`, start, end, owner))
		if err == nil {
			prev := *existing
			before = &prev
			id = existing.ID
			nextContent = build(existing.Content)
			if existing.Content != nextContent {
				if err := s.archiveDiary(tx, existing, limit, false); err != nil {
					return err
				}
			}
			if _, err = tx.Exec(`UPDATE diaries SET content = ?, updated = ? WHERE id = ? AND owner = ?`, nextContent, nowString(), existing.ID, owner); err != nil {
				return err
			}
			return replaceDiaryImages(tx, owner, existing.ID, existing.Date, nextContent)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if id, err = GenerateID(); err != nil {
			return err
		}
		created = true
		nextContent = build("")
		now := nowString()
		if _, err = tx.Exec(`INSERT INTO diaries(content, created, date, id, mood, owner, updated, weather, tags) VALUES(?, ?, ?, ?, '', ?, ?, '', '[]')`, nextContent, now, date+" 00:00:00.000Z", id, owner, now); err != nil {
			return err
		}
		return replaceDiaryImages(tx, owner, id, date, nextContent)
	})
	if err != nil {
		return before, nil, created, err
	}
	s.noteDiaryWrite(owner)
	_ = s.LinkDiaryMedia(owner, id, nextContent)
	diary, err := s.GetDiaryByID(id)
	return before, diary, created, err
}
