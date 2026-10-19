package store

import (
	"database/sql"
	"errors"
	"strings"
)

// Days are the user's countdowns and anniversaries ("倒数日"): a named date
// the app counts towards or from. Each day can carry milestones, further
// dated points along the way; the day's own date is always the first one and
// is not stored as a milestone.

// Day repeat cycles. A repeating day counts towards its next occurrence.
const (
	DayRepeatNone  = ""
	DayRepeatWeek  = "week"
	DayRepeatMonth = "month"
	DayRepeatYear  = "year"
)

type Day struct {
	ID           string `json:"id"`
	Owner        string `json:"owner"`
	Name         string `json:"name"`
	Date         string `json:"date"`
	Emoji        string `json:"emoji"`
	Note         string `json:"note"`
	Repeat       string `json:"repeat"`
	IncludeStart bool   `json:"include_start"`
	ShowOnHome   bool   `json:"show_on_home"`
	// Milestones is how many milestones the day has; filled by ListDays.
	Milestones int `json:"milestones"`
	// Tags are the IDs of the day's tags. On save, nil leaves them as they
	// are and an empty slice removes them all.
	Tags []string `json:"tags"`
	// End is the date of the day's end milestone, "" when it has none:
	// from that date on, the count stops.
	End     string `json:"end"`
	Created string `json:"created"`
	Updated string `json:"updated"`
}

type DayMilestone struct {
	ID    string `json:"id"`
	Day   string `json:"day"`
	Owner string `json:"owner"`
	Name  string `json:"name"`
	Date  string `json:"date"`
	Note  string `json:"note"`
	// End marks the milestone where the day stops counting; a day has at
	// most one.
	End     bool   `json:"end"`
	Created string `json:"created"`
	Updated string `json:"updated"`
}

// ErrDayEndExists reports a second end milestone for the same day.
var ErrDayEndExists = errors.New("this day already has an end milestone")

func migrateDaysSchema(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS days (
			created TEXT NOT NULL,
			date TEXT NOT NULL,
			emoji TEXT DEFAULT '' NOT NULL,
			id TEXT PRIMARY KEY NOT NULL,
			include_start BOOLEAN DEFAULT FALSE NOT NULL,
			name TEXT DEFAULT '' NOT NULL,
			note TEXT DEFAULT '' NOT NULL,
			owner TEXT NOT NULL,
			repeat TEXT DEFAULT '' NOT NULL,
			show_on_home BOOLEAN DEFAULT FALSE NOT NULL,
			updated TEXT NOT NULL,
			FOREIGN KEY(owner) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_days_owner_date ON days(owner, date)`,
		`CREATE TABLE IF NOT EXISTS day_milestones (
			created TEXT NOT NULL,
			date TEXT NOT NULL,
			day TEXT NOT NULL,
			id TEXT PRIMARY KEY NOT NULL,
			name TEXT DEFAULT '' NOT NULL,
			note TEXT DEFAULT '' NOT NULL,
			owner TEXT NOT NULL,
			updated TEXT NOT NULL,
			FOREIGN KEY(owner) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY(day) REFERENCES days(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_day_milestones_day_date ON day_milestones(day, date)`,
		`CREATE TABLE IF NOT EXISTS day_tags (
			color TEXT DEFAULT '' NOT NULL,
			created TEXT NOT NULL,
			id TEXT PRIMARY KEY NOT NULL,
			name TEXT NOT NULL,
			owner TEXT NOT NULL,
			updated TEXT NOT NULL,
			FOREIGN KEY(owner) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_day_tags_owner_name ON day_tags(owner, name)`,
		`CREATE TABLE IF NOT EXISTS day_tag_links (
			day TEXT NOT NULL,
			tag TEXT NOT NULL,
			PRIMARY KEY(day, tag),
			FOREIGN KEY(day) REFERENCES days(id) ON DELETE CASCADE,
			FOREIGN KEY(tag) REFERENCES day_tags(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_day_tag_links_tag ON day_tag_links(tag)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	exists, err := columnExists(db, "day_milestones", "is_end")
	if err != nil {
		return err
	}
	if !exists {
		if _, err := db.Exec(`ALTER TABLE day_milestones ADD COLUMN is_end BOOLEAN DEFAULT FALSE NOT NULL`); err != nil {
			return err
		}
	}
	_, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_day_milestones_end ON day_milestones(day) WHERE is_end`)
	return err
}

const dayColumns = `id, owner, name, date, emoji, note, repeat, include_start, show_on_home, created, updated`

// dayExtras are the derived columns ListDays and GetDay read after
// dayColumns: the milestone count and the end date.
const dayExtras = `(SELECT COUNT(*) FROM day_milestones m WHERE m.day = days.id),
	COALESCE((SELECT m.date FROM day_milestones m WHERE m.day = days.id AND m.is_end), '')`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDay(row rowScanner, extra ...any) (*Day, error) {
	day := &Day{}
	dest := append([]any{&day.ID, &day.Owner, &day.Name, &day.Date, &day.Emoji, &day.Note, &day.Repeat, &day.IncludeStart, &day.ShowOnHome, &day.Created, &day.Updated}, extra...)
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	return day, nil
}

// ListDays returns owner's days by date, each with its milestone count.
func (s *Store) ListDays(owner string) ([]*Day, error) {
	rows, err := s.DB.Query(`SELECT `+dayColumns+`, `+dayExtras+`
		FROM days WHERE owner = ? ORDER BY date, created`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	days := make([]*Day, 0)
	for rows.Next() {
		var count int
		var end string
		day, err := scanDay(rows, &count, &end)
		if err != nil {
			return nil, err
		}
		day.Milestones, day.End = count, end
		days = append(days, day)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return days, s.fillDayTags(owner, days...)
}

// GetDay returns one of owner's days, or ErrNotFound.
func (s *Store) GetDay(owner, id string) (*Day, error) {
	var count int
	var end string
	day, err := scanDay(s.DB.QueryRow(`SELECT `+dayColumns+`, `+dayExtras+`
		FROM days WHERE owner = ? AND id = ?`, owner, id), &count, &end)
	if err != nil {
		return nil, err
	}
	day.Milestones, day.End = count, end
	return day, s.fillDayTags(owner, day)
}

// SaveDay creates day when its ID is empty and updates it otherwise; the
// owner and timestamps are filled in.
func (s *Store) SaveDay(day *Day) (*Day, error) {
	now := nowString()
	if day.ID == "" {
		id, err := GenerateID()
		if err != nil {
			return nil, err
		}
		_, err = s.DB.Exec(`INSERT INTO days(`+dayColumns+`) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, day.Owner, day.Name, day.Date, day.Emoji, day.Note, day.Repeat, day.IncludeStart, day.ShowOnHome, now, now)
		if err != nil {
			return nil, err
		}
		if err := s.setDayTags(day.Owner, id, day.Tags); err != nil {
			return nil, err
		}
		return s.GetDay(day.Owner, id)
	}
	result, err := s.DB.Exec(`UPDATE days SET name = ?, date = ?, emoji = ?, note = ?, repeat = ?, include_start = ?, show_on_home = ?, updated = ?
		WHERE owner = ? AND id = ?`,
		day.Name, day.Date, day.Emoji, day.Note, day.Repeat, day.IncludeStart, day.ShowOnHome, now, day.Owner, day.ID)
	if err != nil {
		return nil, err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	if err := s.setDayTags(day.Owner, day.ID, day.Tags); err != nil {
		return nil, err
	}
	return s.GetDay(day.Owner, day.ID)
}

// DeleteDay removes a day together with its milestones.
func (s *Store) DeleteDay(owner, id string) error {
	result, err := s.DB.Exec(`DELETE FROM days WHERE owner = ? AND id = ?`, owner, id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

const milestoneColumns = `id, day, owner, name, date, note, is_end, created, updated`

func scanMilestone(row rowScanner) (*DayMilestone, error) {
	m := &DayMilestone{}
	if err := row.Scan(&m.ID, &m.Day, &m.Owner, &m.Name, &m.Date, &m.Note, &m.End, &m.Created, &m.Updated); err != nil {
		return nil, err
	}
	return m, nil
}

// ListDayMilestones returns a day's milestones by date.
func (s *Store) ListDayMilestones(owner, dayID string) ([]*DayMilestone, error) {
	return s.queryMilestones(`SELECT `+milestoneColumns+` FROM day_milestones WHERE owner = ? AND day = ? ORDER BY date, created`, owner, dayID)
}

// ListAllDayMilestones returns the milestones of all owner's days by date.
func (s *Store) ListAllDayMilestones(owner string) ([]*DayMilestone, error) {
	return s.queryMilestones(`SELECT `+milestoneColumns+` FROM day_milestones WHERE owner = ? ORDER BY date, created`, owner)
}

func (s *Store) queryMilestones(query string, args ...any) ([]*DayMilestone, error) {
	rows, err := s.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	milestones := make([]*DayMilestone, 0)
	for rows.Next() {
		m, err := scanMilestone(rows)
		if err != nil {
			return nil, err
		}
		milestones = append(milestones, m)
	}
	return milestones, rows.Err()
}

// SaveDayMilestone creates m when its ID is empty and updates it otherwise.
// The day must belong to m.Owner.
func (s *Store) SaveDayMilestone(m *DayMilestone) (*DayMilestone, error) {
	if _, err := s.GetDay(m.Owner, m.Day); err != nil {
		return nil, err
	}
	now := nowString()
	id := m.ID
	if id == "" {
		var err error
		if id, err = GenerateID(); err != nil {
			return nil, err
		}
		_, err = s.DB.Exec(`INSERT INTO day_milestones(`+milestoneColumns+`) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, m.Day, m.Owner, m.Name, m.Date, m.Note, m.End, now, now)
		if err != nil {
			return nil, milestoneError(err)
		}
	} else {
		result, err := s.DB.Exec(`UPDATE day_milestones SET name = ?, date = ?, note = ?, is_end = ?, updated = ? WHERE owner = ? AND day = ? AND id = ?`,
			m.Name, m.Date, m.Note, m.End, now, m.Owner, m.Day, id)
		if err != nil {
			return nil, milestoneError(err)
		}
		if n, _ := result.RowsAffected(); n == 0 {
			return nil, ErrNotFound
		}
	}
	return scanMilestone(s.DB.QueryRow(`SELECT `+milestoneColumns+` FROM day_milestones WHERE id = ?`, id))
}

// DeleteDayMilestone removes one milestone of a day.
func (s *Store) DeleteDayMilestone(owner, dayID, id string) error {
	result, err := s.DB.Exec(`DELETE FROM day_milestones WHERE owner = ? AND day = ? AND id = ?`, owner, dayID, id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func milestoneError(err error) error {
	if strings.Contains(err.Error(), "UNIQUE") {
		return ErrDayEndExists
	}
	return err
}
