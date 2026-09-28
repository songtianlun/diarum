package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// User roles. Every account is a plain user unless promoted; only admins may
// open the admin console.
const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

// ErrInvalidRole is returned for roles other than RoleUser and RoleAdmin.
var ErrInvalidRole = errors.New("role must be 'user' or 'admin'")

// ValidRole reports whether role is a known role.
func ValidRole(role string) bool {
	return role == RoleUser || role == RoleAdmin
}

// legacyAuditRetentionKey is the per-user audit setting of the former
// per-user audit log, superseded by the system-wide audit log.
const legacyAuditRetentionKey = "audit.retention_days"

// migrateAdminSchema adds the user role column and the system settings table
// to databases created before they existed. Every step is idempotent.
func migrateAdminSchema(db *sql.DB) error {
	hasRole, err := columnExists(db, "users", "role")
	if err != nil {
		return err
	}
	if !hasRole {
		if _, err := db.Exec(`ALTER TABLE users ADD COLUMN role TEXT DEFAULT 'user' NOT NULL`); err != nil {
			return err
		}
		// Existing installations get their first account as admin, the same
		// as a fresh installation would.
		if _, err := db.Exec(`UPDATE users SET role = ? WHERE id = (SELECT id FROM users ORDER BY created, rowid LIMIT 1)`, RoleAdmin); err != nil {
			return err
		}
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS system_settings (
			key TEXT PRIMARY KEY NOT NULL,
			value TEXT NOT NULL,
			updated TEXT NOT NULL
		)`,
		`DELETE FROM user_settings WHERE key = '` + legacyAuditRetentionKey + `'`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func columnExists(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if strings.EqualFold(name, column) {
			return true, nil
		}
	}
	return false, rows.Err()
}

// GetSystemSetting returns a raw system-wide setting; ok is false when it
// has never been set.
func (s *Store) GetSystemSetting(key string) (value string, ok bool, err error) {
	err = s.DB.QueryRow(`SELECT value FROM system_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

// SetSystemSetting stores a raw system-wide setting.
func (s *Store) SetSystemSetting(key, value string) error {
	_, err := s.DB.Exec(`INSERT INTO system_settings(key, value, updated) VALUES(?, ?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated = excluded.updated`, key, value, nowString())
	return err
}

// FindUser looks a user up by ID, username or email.
func (s *Store) FindUser(identity string) (*User, error) {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return nil, ErrNotFound
	}
	if user, err := s.GetUserByID(identity); err == nil {
		return user, nil
	}
	return s.GetUserByIdentity(identity)
}

// SetUserRole changes a user's role.
func (s *Store) SetUserRole(userID, role string) error {
	if !ValidRole(role) {
		return ErrInvalidRole
	}
	result, err := s.DB.Exec(`UPDATE users SET role = ?, updated = ? WHERE id = ?`, role, nowString(), userID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// CountAdmins returns how many accounts hold the admin role.
func (s *Store) CountAdmins() (int, error) {
	var count int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE role = ?`, RoleAdmin).Scan(&count)
	return count, err
}

// UserSummary is a user as shown in the admin user list.
type UserSummary struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	Role          string `json:"role"`
	Created       string `json:"created"`
	Updated       string `json:"updated"`
	Diaries       int    `json:"diaries"`
	Media         int    `json:"media"`
	Conversations int    `json:"conversations"`
	// LastDiaryAt is when the user last saved any diary entry.
	LastDiaryAt string `json:"last_diary_at"`
}

// UserListOptions filters and orders ListUsers.
type UserListOptions struct {
	// Query matches username, email, name or ID (substring, case-insensitive).
	Query string
	// Role keeps only users with this role when set.
	Role string
	// Sort is one of "created", "-created", "username", "diaries",
	// "-diaries", "last_active"; defaults to "created".
	Sort   string
	Limit  int
	Offset int
}

var userSortOrders = map[string]string{
	"created":     "u.created ASC, u.rowid ASC",
	"-created":    "u.created DESC, u.rowid DESC",
	"username":    "u.username COLLATE NOCASE ASC",
	"diaries":     "diaries ASC, u.rowid",
	"-diaries":    "diaries DESC, u.rowid",
	"last_active": "last_diary_at DESC, u.rowid",
}

// ListUsers returns one page of users with their activity counts, plus the
// number of users matching the filter.
func (s *Store) ListUsers(opts UserListOptions) ([]UserSummary, int, error) {
	where := []string{"1 = 1"}
	args := []any{}
	if q := strings.TrimSpace(opts.Query); q != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.ToLower(q)) + "%"
		where = append(where, `(LOWER(u.username) LIKE ? ESCAPE '\' OR LOWER(u.email) LIKE ? ESCAPE '\' OR LOWER(u.name) LIKE ? ESCAPE '\' OR u.id LIKE ? ESCAPE '\')`)
		args = append(args, like, like, like, like)
	}
	if opts.Role != "" {
		where = append(where, "u.role = ?")
		args = append(args, opts.Role)
	}
	filter := strings.Join(where, " AND ")

	var total int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM users u WHERE `+filter, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	order, ok := userSortOrders[opts.Sort]
	if !ok {
		order = userSortOrders["created"]
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = -1
	}
	offset := max(opts.Offset, 0)
	rows, err := s.DB.Query(`
		SELECT u.id, u.username, u.email, u.name, u.role, u.created, u.updated,
			(SELECT COUNT(*) FROM diaries d WHERE d.owner = u.id) AS diaries,
			(SELECT COUNT(*) FROM media m WHERE m.owner = u.id) AS media,
			(SELECT COUNT(*) FROM ai_conversations c WHERE c.owner = u.id) AS conversations,
			COALESCE((SELECT MAX(d.updated) FROM diaries d WHERE d.owner = u.id), '') AS last_diary_at
		FROM users u
		WHERE `+filter+`
		ORDER BY `+order+`
		LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	users := make([]UserSummary, 0)
	for rows.Next() {
		var u UserSummary
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.Name, &u.Role, &u.Created, &u.Updated, &u.Diaries, &u.Media, &u.Conversations, &u.LastDiaryAt); err != nil {
			return nil, 0, err
		}
		users = append(users, u)
	}
	return users, total, rows.Err()
}

// UserNames maps every user ID to its username.
func (s *Store) UserNames() (map[string]string, error) {
	rows, err := s.DB.Query(`SELECT id, username FROM users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := make(map[string]string)
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		names[id] = name
	}
	return names, rows.Err()
}

// SystemStats is a snapshot of the whole installation for the admin console.
type SystemStats struct {
	Users         int   `json:"users"`
	Admins        int   `json:"admins"`
	NewUsers30d   int   `json:"new_users_30d"`
	ActiveUsers7d int   `json:"active_users_7d"`
	Diaries       int   `json:"diaries"`
	DiariesToday  int   `json:"diaries_today"`
	Diaries7d     int   `json:"diaries_7d"`
	Revisions     int   `json:"revisions"`
	Media         int   `json:"media"`
	Conversations int   `json:"conversations"`
	Messages      int   `json:"messages"`
	DatabaseBytes int64 `json:"database_bytes"`
	// DailyDiaries counts entries saved (created or edited) per UTC day over
	// the last 30 days, oldest first; days without activity are included.
	DailyDiaries []DailyCount `json:"daily_diaries"`
	// DailyUsers counts new accounts per UTC day over the last 30 days.
	DailyUsers []DailyCount `json:"daily_users"`
}

// DailyCount is one day of a series.
type DailyCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

const statsSeriesDays = 30

// SystemStats gathers installation-wide counts. Every query is an indexed
// count or a bounded range, so it stays cheap on large databases.
func (s *Store) SystemStats(now time.Time) (*SystemStats, error) {
	now = now.UTC()
	day := func(t time.Time) string { return t.Format("2006-01-02") }
	today := day(now)
	since7 := day(now.AddDate(0, 0, -6))
	since30 := day(now.AddDate(0, 0, -(statsSeriesDays - 1)))

	stats := &SystemStats{}
	counts := []struct {
		dest  *int
		query string
		args  []any
	}{
		{&stats.Users, `SELECT COUNT(*) FROM users`, nil},
		{&stats.Admins, `SELECT COUNT(*) FROM users WHERE role = ?`, []any{RoleAdmin}},
		{&stats.NewUsers30d, `SELECT COUNT(*) FROM users WHERE created >= ?`, []any{since30}},
		{&stats.ActiveUsers7d, `SELECT COUNT(DISTINCT owner) FROM diaries WHERE updated >= ?`, []any{since7}},
		{&stats.Diaries, `SELECT COUNT(*) FROM diaries`, nil},
		{&stats.DiariesToday, `SELECT COUNT(*) FROM diaries WHERE updated >= ?`, []any{today}},
		{&stats.Diaries7d, `SELECT COUNT(*) FROM diaries WHERE updated >= ?`, []any{since7}},
		{&stats.Revisions, `SELECT COUNT(*) FROM diary_revisions`, nil},
		{&stats.Media, `SELECT COUNT(*) FROM media`, nil},
		{&stats.Conversations, `SELECT COUNT(*) FROM ai_conversations`, nil},
		{&stats.Messages, `SELECT COUNT(*) FROM ai_messages`, nil},
	}
	for _, c := range counts {
		if err := s.DB.QueryRow(c.query, c.args...).Scan(c.dest); err != nil {
			return nil, err
		}
	}

	var err error
	if stats.DailyDiaries, err = s.dailySeries(`SELECT substr(updated, 1, 10), COUNT(*) FROM diaries WHERE updated >= ? GROUP BY 1`, since30, now); err != nil {
		return nil, err
	}
	if stats.DailyUsers, err = s.dailySeries(`SELECT substr(created, 1, 10), COUNT(*) FROM users WHERE created >= ? GROUP BY 1`, since30, now); err != nil {
		return nil, err
	}

	for _, name := range []string{DatabaseName, DatabaseName + "-wal"} {
		if info, err := os.Stat(filepath.Join(s.DataDir, name)); err == nil {
			stats.DatabaseBytes += info.Size()
		}
	}
	return stats, nil
}

func (s *Store) dailySeries(query, since string, now time.Time) ([]DailyCount, error) {
	rows, err := s.DB.Query(query, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byDay := make(map[string]int)
	for rows.Next() {
		var date string
		var count int
		if err := rows.Scan(&date, &count); err != nil {
			return nil, err
		}
		byDay[date] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	series := make([]DailyCount, 0, statsSeriesDays)
	for i := statsSeriesDays - 1; i >= 0; i-- {
		date := now.AddDate(0, 0, -i).Format("2006-01-02")
		series = append(series, DailyCount{Date: date, Count: byDay[date]})
	}
	return series, nil
}
