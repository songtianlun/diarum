package store

import (
	"database/sql"
	"errors"
	"strings"
)

// DayTag groups days; a day can have any number of tags.
type DayTag struct {
	ID    string `json:"id"`
	Owner string `json:"owner"`
	Name  string `json:"name"`
	Color string `json:"color"`
	// Days is how many days carry the tag; filled by ListDayTags.
	Days    int    `json:"days"`
	Created string `json:"created"`
	Updated string `json:"updated"`
}

// ErrDayTagExists reports a tag name the owner already uses.
var ErrDayTagExists = errors.New("a tag with this name already exists")

// ListDayTags returns owner's tags in the order they were created.
func (s *Store) ListDayTags(owner string) ([]*DayTag, error) {
	rows, err := s.DB.Query(`SELECT id, owner, name, color, created, updated,
		(SELECT COUNT(*) FROM day_tag_links l WHERE l.tag = day_tags.id)
		FROM day_tags WHERE owner = ? ORDER BY created, id`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := make([]*DayTag, 0)
	for rows.Next() {
		tag := &DayTag{}
		if err := rows.Scan(&tag.ID, &tag.Owner, &tag.Name, &tag.Color, &tag.Created, &tag.Updated, &tag.Days); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

// SaveDayTag creates tag when its ID is empty and renames or recolours it
// otherwise. Names are unique per owner.
func (s *Store) SaveDayTag(tag *DayTag) (*DayTag, error) {
	now := nowString()
	id := tag.ID
	var err error
	if id == "" {
		if id, err = GenerateID(); err != nil {
			return nil, err
		}
		_, err = s.DB.Exec(`INSERT INTO day_tags(id, owner, name, color, created, updated) VALUES(?, ?, ?, ?, ?, ?)`,
			id, tag.Owner, tag.Name, tag.Color, now, now)
	} else {
		var result sql.Result
		result, err = s.DB.Exec(`UPDATE day_tags SET name = ?, color = ?, updated = ? WHERE owner = ? AND id = ?`,
			tag.Name, tag.Color, now, tag.Owner, id)
		if err == nil {
			if n, _ := result.RowsAffected(); n == 0 {
				return nil, ErrNotFound
			}
		}
	}
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrDayTagExists
		}
		return nil, err
	}
	saved := &DayTag{}
	err = s.DB.QueryRow(`SELECT id, owner, name, color, created, updated, (SELECT COUNT(*) FROM day_tag_links l WHERE l.tag = day_tags.id)
		FROM day_tags WHERE id = ?`, id).Scan(&saved.ID, &saved.Owner, &saved.Name, &saved.Color, &saved.Created, &saved.Updated, &saved.Days)
	return saved, err
}

// DeleteDayTag removes a tag from every day and deletes it.
func (s *Store) DeleteDayTag(owner, id string) error {
	result, err := s.DB.Exec(`DELETE FROM day_tags WHERE owner = ? AND id = ?`, owner, id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// fillDayTags loads the tag IDs of each day, in tag order.
func (s *Store) fillDayTags(owner string, days ...*Day) error {
	byID := make(map[string]*Day, len(days))
	for _, day := range days {
		day.Tags = make([]string, 0)
		byID[day.ID] = day
	}
	if len(days) == 0 {
		return nil
	}
	rows, err := s.DB.Query(`SELECT l.day, l.tag FROM day_tag_links l JOIN day_tags t ON t.id = l.tag
		WHERE t.owner = ? ORDER BY t.created, t.id`, owner)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var dayID, tagID string
		if err := rows.Scan(&dayID, &tagID); err != nil {
			return err
		}
		if day := byID[dayID]; day != nil {
			day.Tags = append(day.Tags, tagID)
		}
	}
	return rows.Err()
}

// setDayTags replaces a day's tags; nil leaves them alone. IDs that are not
// owner's tags are ignored.
func (s *Store) setDayTags(owner, dayID string, tags []string) error {
	if tags == nil {
		return nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM day_tag_links WHERE day = ?`, dayID); err != nil {
		return err
	}
	for _, tag := range tags {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO day_tag_links(day, tag) SELECT ?, id FROM day_tags WHERE owner = ? AND id = ?`, dayID, owner, tag); err != nil {
			return err
		}
	}
	return tx.Commit()
}
