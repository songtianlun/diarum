package store

// Keyset-paginated reads for long-running jobs such as exports and backups.
// The database allows a single open connection, so a job must never hold a
// cursor open while it does other work; each batch is a short query that
// releases the connection before the caller processes the rows.

// ListDiariesAfter returns up to limit diaries of owner ordered by (date, id),
// starting after the given key. Pass empty keys for the first batch.
func (s *Store) ListDiariesAfter(owner, afterDate, afterID string, limit int) ([]*Diary, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.DB.Query(`SELECT content, created, date, id, mood, owner, updated, weather, tags FROM diaries
		WHERE owner = ? AND (date > ? OR (date = ? AND id > ?))
		ORDER BY date ASC, id ASC LIMIT ?`, owner, afterDate, afterDate, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDiaries(rows)
}

// ListMediaAfter returns up to limit media records of owner ordered by
// (created, id), starting after the given key, without expanding diaries.
// Images in the trash are left out.
func (s *Store) ListMediaAfter(owner, afterCreated, afterID string, limit int) ([]*Media, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.DB.Query(`SELECT `+mediaColumns+` FROM media
		WHERE owner = ? AND deleted = '' AND (created > ? OR (created = ? AND id > ?))
		ORDER BY created ASC, id ASC LIMIT ?`, owner, afterCreated, afterCreated, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*Media, 0)
	for rows.Next() {
		item, err := scanMediaRow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// CountMedia returns how many media records owner has outside the trash.
func (s *Store) CountMedia(owner string) int {
	var total int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM media WHERE owner = ? AND deleted = ''`, owner).Scan(&total)
	return total
}

// UsersWithSetting returns the users whose setting key holds value.
func (s *Store) UsersWithSetting(key string, value any) ([]string, error) {
	rows, err := s.DB.Query(`SELECT user FROM user_settings WHERE key = ? AND value = ? ORDER BY user`, key, encodeJSON(value))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]string, 0)
	for rows.Next() {
		var user string
		if err := rows.Scan(&user); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}
