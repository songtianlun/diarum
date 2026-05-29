package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/songtianlun/diarum/internal/logger"

	_ "modernc.org/sqlite"
)

const (
	DatabaseName             = "diarum.db"
	LegacyDatabaseName       = "data.db"
	DefaultMediaCollectionID = "media"
)

var ErrNotFound = sql.ErrNoRows

type Store struct {
	DB                *sql.DB
	DataDir           string
	MediaCollectionID string
	AuthSecret        []byte
}

type User struct {
	ID                     string `json:"id"`
	Username               string `json:"username"`
	Email                  string `json:"email"`
	EmailVisibility        bool   `json:"emailVisibility"`
	Name                   string `json:"name"`
	Avatar                 string `json:"avatar"`
	PasswordHash           string `json:"-"`
	TokenKey               string `json:"-"`
	Verified               bool   `json:"verified"`
	LastLoginAlertSentAt   string `json:"lastLoginAlertSentAt"`
	LastResetSentAt        string `json:"lastResetSentAt"`
	LastVerificationSentAt string `json:"lastVerificationSentAt"`
	Created                string `json:"created"`
	Updated                string `json:"updated"`
}

type Diary struct {
	ID      string `json:"id"`
	Date    string `json:"date"`
	Content string `json:"content"`
	Mood    string `json:"mood"`
	Weather string `json:"weather"`
	Owner   string `json:"owner"`
	Tags    string `json:"-"`
	Created string `json:"created"`
	Updated string `json:"updated"`
}

type Media struct {
	ID      string   `json:"id"`
	File    string   `json:"file"`
	Name    string   `json:"name"`
	Alt     string   `json:"alt"`
	Diary   []string `json:"diary"`
	Owner   string   `json:"owner"`
	Created string   `json:"created"`
	Updated string   `json:"updated"`
}

type Conversation struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Owner   string `json:"owner"`
	Created string `json:"created"`
	Updated string `json:"updated"`
}

type Message struct {
	ID                string   `json:"id"`
	Conversation      string   `json:"conversation"`
	Role              string   `json:"role"`
	Content           string   `json:"content"`
	ReferencedDiaries []string `json:"referenced_diaries"`
	Owner             string   `json:"owner"`
	Created           string   `json:"created"`
	Updated           string   `json:"updated"`
}

type MediaWithExpand struct {
	Media
	Expand map[string]any `json:"expand,omitempty"`
}

func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}

	newPath := filepath.Join(dataDir, DatabaseName)
	oldPath := filepath.Join(dataDir, LegacyDatabaseName)
	newExists := fileExists(newPath)
	oldExists := fileExists(oldPath)

	if !newExists {
		tempPath := filepath.Join(dataDir, DatabaseName+".tmp")
		_ = os.Remove(tempPath)

		db, err := openSQLite(tempPath)
		if err != nil {
			return nil, err
		}
		if err := createSchema(db); err != nil {
			db.Close()
			_ = os.Remove(tempPath)
			return nil, err
		}
		if oldExists {
			logger.Info("[Store] legacy database detected, starting migration: source=%s target=%s", oldPath, newPath)
			backupDir, err := backupLegacyData(dataDir, oldPath)
			if err != nil {
				logger.Error("[Store] legacy database backup failed: source=%s err=%v", oldPath, err)
				db.Close()
				_ = os.Remove(tempPath)
				return nil, err
			}
			logger.Info("[Store] legacy database backup completed: path=%s", backupDir)
			if err := migrateLegacyData(db, oldPath); err != nil {
				logger.Error("[Store] legacy database migration failed: source=%s target=%s err=%v", oldPath, newPath, err)
				db.Close()
				_ = os.Remove(tempPath)
				return nil, err
			}
			logger.Info("[Store] legacy database migration completed: source=%s target=%s", oldPath, newPath)
		}
		if err := ensureRuntimeMetadata(db, oldPath); err != nil {
			db.Close()
			_ = os.Remove(tempPath)
			return nil, err
		}
		if err := db.Close(); err != nil {
			_ = os.Remove(tempPath)
			return nil, err
		}
		if err := os.Rename(tempPath, newPath); err != nil {
			_ = os.Remove(tempPath)
			return nil, err
		}
	}

	db, err := openSQLite(newPath)
	if err != nil {
		return nil, err
	}
	if err := createSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureRuntimeMetadata(db, oldPath); err != nil {
		db.Close()
		return nil, err
	}

	mediaCollectionID, _ := getMeta(db, "legacy.media_collection_id")
	if mediaCollectionID == "" {
		mediaCollectionID = DefaultMediaCollectionID
	}
	authSecretHex, _ := getMeta(db, "auth.secret")
	authSecret, err := hex.DecodeString(authSecretHex)
	if err != nil || len(authSecret) == 0 {
		authSecret = []byte(authSecretHex)
	}

	return &Store{DB: db, DataDir: dataDir, MediaCollectionID: mediaCollectionID, AuthSecret: authSecret}, nil
}

func openSQLite(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func createSchema(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS migration_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS users (
			avatar TEXT DEFAULT '' NOT NULL,
			created TEXT NOT NULL,
			email TEXT DEFAULT '' NOT NULL,
			emailVisibility BOOLEAN DEFAULT FALSE NOT NULL,
			id TEXT PRIMARY KEY NOT NULL,
			lastLoginAlertSentAt TEXT DEFAULT '' NOT NULL,
			lastResetSentAt TEXT DEFAULT '' NOT NULL,
			lastVerificationSentAt TEXT DEFAULT '' NOT NULL,
			name TEXT DEFAULT '' NOT NULL,
			passwordHash TEXT NOT NULL,
			tokenKey TEXT NOT NULL,
			updated TEXT NOT NULL,
			username TEXT NOT NULL,
			verified BOOLEAN DEFAULT FALSE NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username ON users(username)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email) WHERE email != ''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_tokenKey ON users(tokenKey)`,
		`CREATE TABLE IF NOT EXISTS diaries (
			content TEXT DEFAULT '' NOT NULL,
			created TEXT NOT NULL,
			date TEXT DEFAULT '' NOT NULL,
			id TEXT PRIMARY KEY NOT NULL,
			mood TEXT DEFAULT '' NOT NULL,
			owner TEXT DEFAULT '' NOT NULL,
			updated TEXT NOT NULL,
			weather TEXT DEFAULT '' NOT NULL,
			tags JSON DEFAULT '[]' NOT NULL,
			FOREIGN KEY(owner) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_diaries_date_owner ON diaries(date, owner)`,
		`CREATE INDEX IF NOT EXISTS idx_diaries_owner_date ON diaries(owner, date)`,
		`CREATE TABLE IF NOT EXISTS tags (
			created TEXT NOT NULL,
			id TEXT PRIMARY KEY NOT NULL,
			name TEXT DEFAULT '' NOT NULL,
			owner TEXT DEFAULT '' NOT NULL,
			updated TEXT NOT NULL,
			FOREIGN KEY(owner) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_tags_name_owner ON tags(name, owner)`,
		`CREATE TABLE IF NOT EXISTS media (
			alt TEXT DEFAULT '' NOT NULL,
			created TEXT NOT NULL,
			file TEXT DEFAULT '' NOT NULL,
			id TEXT PRIMARY KEY NOT NULL,
			name TEXT DEFAULT '' NOT NULL,
			owner TEXT DEFAULT '' NOT NULL,
			updated TEXT NOT NULL,
			diary JSON DEFAULT '[]' NOT NULL,
			FOREIGN KEY(owner) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_media_owner_created ON media(owner, created)`,
		`CREATE TABLE IF NOT EXISTS user_settings (
			created TEXT NOT NULL,
			encrypted BOOLEAN DEFAULT FALSE NOT NULL,
			id TEXT PRIMARY KEY NOT NULL,
			key TEXT DEFAULT '' NOT NULL,
			updated TEXT NOT NULL,
			user TEXT DEFAULT '' NOT NULL,
			value JSON DEFAULT NULL,
			FOREIGN KEY(user) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_user_settings_user_key ON user_settings(user, key)`,
		`CREATE INDEX IF NOT EXISTS idx_user_settings_key ON user_settings(key)`,
		`CREATE TABLE IF NOT EXISTS ai_conversations (
			created TEXT NOT NULL,
			id TEXT PRIMARY KEY NOT NULL,
			owner TEXT DEFAULT '' NOT NULL,
			title TEXT DEFAULT '' NOT NULL,
			updated TEXT NOT NULL,
			FOREIGN KEY(owner) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_conversations_owner ON ai_conversations(owner)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_conversations_updated ON ai_conversations(updated)`,
		`CREATE TABLE IF NOT EXISTS ai_messages (
			content TEXT DEFAULT '' NOT NULL,
			conversation TEXT DEFAULT '' NOT NULL,
			created TEXT NOT NULL,
			id TEXT PRIMARY KEY NOT NULL,
			owner TEXT DEFAULT '' NOT NULL,
			referenced_diaries JSON DEFAULT NULL,
			role TEXT DEFAULT '' NOT NULL,
			updated TEXT NOT NULL,
			FOREIGN KEY(owner) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY(conversation) REFERENCES ai_conversations(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_messages_conversation ON ai_messages(conversation)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_messages_owner ON ai_messages(owner)`,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(1, datetime('now'))`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func migrateLegacyData(db *sql.DB, oldPath string) error {
	if !isLegacyDataDB(oldPath) {
		logger.Warn("[Store] legacy migration skipped: source is not a legacy database: path=%s", oldPath)
		return nil
	}

	quotedPath := strings.ReplaceAll(oldPath, "'", "''")
	if _, err := db.Exec("ATTACH DATABASE '" + quotedPath + "' AS legacy"); err != nil {
		return fmt.Errorf("attach legacy database: %w", err)
	}
	defer db.Exec("DETACH DATABASE legacy")

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	copyStatements := []struct {
		table     string
		statement string
	}{
		{"users", `INSERT OR IGNORE INTO users(avatar, created, email, emailVisibility, id, lastLoginAlertSentAt, lastResetSentAt, lastVerificationSentAt, name, passwordHash, tokenKey, updated, username, verified)
		 SELECT avatar, created, email, emailVisibility, id, lastLoginAlertSentAt, lastResetSentAt, lastVerificationSentAt, name, passwordHash, tokenKey, updated, username, verified FROM legacy.users`,
		},
		{"diaries", `INSERT OR IGNORE INTO diaries(content, created, date, id, mood, owner, updated, weather, tags)
		 SELECT content, created, date, id, mood, owner, updated, weather, COALESCE(tags, '[]') FROM legacy.diaries`,
		},
		{"tags", `INSERT OR IGNORE INTO tags(created, id, name, owner, updated)
		 SELECT created, id, name, owner, updated FROM legacy.tags`,
		},
		{"media", `INSERT OR IGNORE INTO media(alt, created, file, id, name, owner, updated, diary)
		 SELECT alt, created, file, id, name, owner, updated, COALESCE(diary, '[]') FROM legacy.media`,
		},
		{"user_settings", `INSERT OR IGNORE INTO user_settings(created, encrypted, id, key, updated, user, value)
		 SELECT created, encrypted, id, key, updated, user, value FROM legacy.user_settings`,
		},
		{"ai_conversations", `INSERT OR IGNORE INTO ai_conversations(created, id, owner, title, updated)
		 SELECT created, id, owner, title, updated FROM legacy.ai_conversations`,
		},
		{"ai_messages", `INSERT OR IGNORE INTO ai_messages(content, conversation, created, id, owner, referenced_diaries, role, updated)
		 SELECT content, conversation, created, id, owner, referenced_diaries, role, updated FROM legacy.ai_messages`,
		},
	}
	totalCopied := int64(0)
	for _, copyStatement := range copyStatements {
		result, err := tx.Exec(copyStatement.statement)
		if err != nil {
			if isMissingLegacyTableError(err) {
				logger.Warn("[Store] legacy migration skipped table: table=%s err=%v", copyStatement.table, err)
				continue
			}
			return fmt.Errorf("copy legacy %s: %w", copyStatement.table, err)
		}
		rowsCopied, _ := result.RowsAffected()
		totalCopied += rowsCopied
		logger.Info("[Store] legacy migration copied rows: table=%s rows=%d", copyStatement.table, rowsCopied)
	}

	mediaCollectionID := lookupLegacyCollectionID(tx, "media")
	if mediaCollectionID == "" {
		mediaCollectionID = DefaultMediaCollectionID
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO migration_meta(key, value) VALUES('legacy.media_collection_id', ?)`, mediaCollectionID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO migration_meta(key, value) VALUES('migration.source', 'legacy')`); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO migration_meta(key, value) VALUES('migration.completed_at', ?)`, nowString()); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit legacy migration: %w", err)
	}
	logger.Info("[Store] legacy migration committed: rows=%d mediaCollectionID=%s", totalCopied, mediaCollectionID)
	return nil
}

func lookupLegacyCollectionID(tx *sql.Tx, name string) string {
	var id string
	_ = tx.QueryRow(`SELECT id FROM legacy._collections WHERE name = ?`, name).Scan(&id)
	return id
}

func isMissingLegacyTableError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such table") || strings.Contains(msg, "no such column")
}

func ensureRuntimeMetadata(db *sql.DB, oldPath string) error {
	secret, _ := getMeta(db, "auth.secret")
	if secret == "" {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return err
		}
		if err := setMeta(db, "auth.secret", hex.EncodeToString(raw)); err != nil {
			return err
		}
	}
	mediaCollectionID, _ := getMeta(db, "legacy.media_collection_id")
	if mediaCollectionID == "" && fileExists(oldPath) && isLegacyDataDB(oldPath) {
		legacy, err := openSQLite(oldPath)
		if err == nil {
			var id string
			if scanErr := legacy.QueryRow(`SELECT id FROM _collections WHERE name = 'media'`).Scan(&id); scanErr == nil && id != "" {
				_ = setMeta(db, "legacy.media_collection_id", id)
			}
			legacy.Close()
		}
	}
	mediaCollectionID, _ = getMeta(db, "legacy.media_collection_id")
	if mediaCollectionID == "" {
		return setMeta(db, "legacy.media_collection_id", DefaultMediaCollectionID)
	}
	return nil
}

func backupLegacyData(dataDir, oldPath string) (string, error) {
	backupDir := filepath.Join(dataDir, "backups", "pre-native-migration-"+time.Now().UTC().Format("20060102-150405"))
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", err
	}
	if err := copyFile(oldPath, filepath.Join(backupDir, LegacyDatabaseName)); err != nil {
		return "", err
	}
	logsPath := filepath.Join(dataDir, "logs.db")
	if fileExists(logsPath) {
		_ = copyFile(logsPath, filepath.Join(backupDir, "logs.db"))
	}
	return backupDir, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func isLegacyDataDB(path string) bool {
	db, err := openSQLite(path)
	if err != nil {
		return false
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('_collections', '_migrations')`).Scan(&count); err != nil {
		return false
	}
	return count > 0
}

func getMeta(db *sql.DB, key string) (string, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM migration_meta WHERE key = ?`, key).Scan(&value)
	return value, err
}

func setMeta(db *sql.DB, key, value string) error {
	_, err := db.Exec(`INSERT INTO migration_meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func nowString() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05.000Z")
}

func GenerateID() (string, error) {
	buf := make([]byte, 7)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "r" + hex.EncodeToString(buf), nil
}

func GenerateTokenKey() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func encodeJSON(value any) string {
	if value == nil {
		return "null"
	}
	bytes, err := json.Marshal(value)
	if err != nil {
		return "null"
	}
	return string(bytes)
}

func decodeStringSlice(raw string) []string {
	if raw == "" || raw == "null" {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err == nil {
		return values
	}
	var single string
	if err := json.Unmarshal([]byte(raw), &single); err == nil && single != "" {
		return []string{single}
	}
	return []string{}
}

func (s *Store) Close() error {
	if s == nil || s.DB == nil {
		return nil
	}
	return s.DB.Close()
}

func (s *Store) Transaction(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetUserByID(id string) (*User, error) {
	return scanUser(s.DB.QueryRow(`SELECT avatar, created, email, emailVisibility, id, lastLoginAlertSentAt, lastResetSentAt, lastVerificationSentAt, name, passwordHash, tokenKey, updated, username, verified FROM users WHERE id = ?`, id))
}

func (s *Store) GetUserByIdentity(identity string) (*User, error) {
	identity = strings.TrimSpace(identity)
	return scanUser(s.DB.QueryRow(`SELECT avatar, created, email, emailVisibility, id, lastLoginAlertSentAt, lastResetSentAt, lastVerificationSentAt, name, passwordHash, tokenKey, updated, username, verified FROM users WHERE username = ? OR email = ? LIMIT 1`, identity, identity))
}

func (s *Store) CreateUser(username, email, passwordHash string) (*User, error) {
	id, err := GenerateID()
	if err != nil {
		return nil, err
	}
	tokenKey, err := GenerateTokenKey()
	if err != nil {
		return nil, err
	}
	now := nowString()
	_, err = s.DB.Exec(`INSERT INTO users(avatar, created, email, emailVisibility, id, lastLoginAlertSentAt, lastResetSentAt, lastVerificationSentAt, name, passwordHash, tokenKey, updated, username, verified) VALUES('', ?, ?, false, ?, '', '', '', '', ?, ?, ?, ?, false)`, now, email, id, passwordHash, tokenKey, now, username)
	if err != nil {
		return nil, err
	}
	return s.GetUserByID(id)
}

func scanUser(row interface{ Scan(dest ...any) error }) (*User, error) {
	user := &User{}
	err := row.Scan(&user.Avatar, &user.Created, &user.Email, &user.EmailVisibility, &user.ID, &user.LastLoginAlertSentAt, &user.LastResetSentAt, &user.LastVerificationSentAt, &user.Name, &user.PasswordHash, &user.TokenKey, &user.Updated, &user.Username, &user.Verified)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *Store) UpsertDiary(owner, date, content, mood, weather string) (*Diary, bool, error) {
	start, end := dayRange(date)
	existing, err := s.GetDiaryByDate(owner, start, end)
	if err == nil && existing != nil {
		now := nowString()
		_, err := s.DB.Exec(`UPDATE diaries SET content = ?, mood = ?, weather = ?, updated = ? WHERE id = ? AND owner = ?`, content, mood, weather, now, existing.ID, owner)
		if err != nil {
			return nil, false, err
		}
		diary, err := s.GetDiaryByID(existing.ID)
		return diary, false, err
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	id, err := GenerateID()
	if err != nil {
		return nil, false, err
	}
	now := nowString()
	_, err = s.DB.Exec(`INSERT INTO diaries(content, created, date, id, mood, owner, updated, weather, tags) VALUES(?, ?, ?, ?, ?, ?, ?, ?, '[]')`, content, now, date+" 00:00:00.000Z", id, mood, owner, now, weather)
	if err != nil {
		return nil, true, err
	}
	diary, err := s.GetDiaryByID(id)
	return diary, true, err
}

func (s *Store) GetDiaryByDate(owner, start, end string) (*Diary, error) {
	return scanDiary(s.DB.QueryRow(`SELECT content, created, date, id, mood, owner, updated, weather, tags FROM diaries WHERE date >= ? AND date <= ? AND owner = ? LIMIT 1`, start, end, owner))
}

func (s *Store) GetDiaryByID(id string) (*Diary, error) {
	return scanDiary(s.DB.QueryRow(`SELECT content, created, date, id, mood, owner, updated, weather, tags FROM diaries WHERE id = ?`, id))
}

func (s *Store) DeleteDiary(id, owner string) error {
	result, err := s.DB.Exec(`DELETE FROM diaries WHERE id = ? AND owner = ?`, id, owner)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ListDiaries(owner, start, end, order string, limit int) ([]*Diary, error) {
	query := `SELECT content, created, date, id, mood, owner, updated, weather, tags FROM diaries WHERE owner = ?`
	args := []any{owner}
	if start != "" {
		query += ` AND date >= ?`
		args = append(args, start)
	}
	if end != "" {
		query += ` AND date <= ?`
		args = append(args, end)
	}
	if order == "created" {
		query += ` ORDER BY created ASC`
	} else if order == "updated" {
		query += ` ORDER BY updated DESC`
	} else {
		query += ` ORDER BY date DESC`
	}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDiaries(rows)
}

func (s *Store) SearchDiaries(owner, query string, limit int) ([]*Diary, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.DB.Query(`SELECT content, created, date, id, mood, owner, updated, weather, tags FROM diaries WHERE owner = ? AND content LIKE ? ORDER BY date DESC LIMIT ?`, owner, "%"+query+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDiaries(rows)
}

func (s *Store) CountDiaries(owner string) int {
	var total int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM diaries WHERE owner = ?`, owner).Scan(&total)
	return total
}

func scanDiary(row interface{ Scan(dest ...any) error }) (*Diary, error) {
	diary := &Diary{}
	err := row.Scan(&diary.Content, &diary.Created, &diary.Date, &diary.ID, &diary.Mood, &diary.Owner, &diary.Updated, &diary.Weather, &diary.Tags)
	if err != nil {
		return nil, err
	}
	return diary, nil
}

func scanDiaries(rows *sql.Rows) ([]*Diary, error) {
	items := make([]*Diary, 0)
	for rows.Next() {
		item := &Diary{}
		if err := rows.Scan(&item.Content, &item.Created, &item.Date, &item.ID, &item.Mood, &item.Owner, &item.Updated, &item.Weather, &item.Tags); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func dayRange(date string) (string, string) {
	return date + " 00:00:00.000Z", date + " 23:59:59.999Z"
}

func DateOnly(dateTime string) string {
	if len(dateTime) >= 10 {
		return dateTime[:10]
	}
	return dateTime
}

func (s *Store) GetSetting(userID, key string) (any, error) {
	var raw sql.NullString
	err := s.DB.QueryRow(`SELECT value FROM user_settings WHERE user = ? AND key = ?`, userID, key).Scan(&raw)
	if err != nil {
		return nil, err
	}
	if !raw.Valid || raw.String == "" || raw.String == "null" {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal([]byte(raw.String), &value); err != nil {
		return raw.String, nil
	}
	return value, nil
}

func (s *Store) SetSetting(userID, key string, value any, encrypted bool) error {
	id, err := GenerateID()
	if err != nil {
		return err
	}
	now := nowString()
	raw := encodeJSON(value)
	_, err = s.DB.Exec(`INSERT INTO user_settings(created, encrypted, id, key, updated, user, value) VALUES(?, ?, ?, ?, ?, ?, ?) ON CONFLICT(user, key) DO UPDATE SET value = excluded.value, encrypted = excluded.encrypted, updated = excluded.updated`, now, encrypted, id, key, now, userID, raw)
	return err
}

func (s *Store) DeleteSetting(userID, key string) error {
	_, err := s.DB.Exec(`DELETE FROM user_settings WHERE user = ? AND key = ?`, userID, key)
	return err
}

func (s *Store) GetSettings(userID string) (map[string]any, error) {
	rows, err := s.DB.Query(`SELECT key, value FROM user_settings WHERE user = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]any)
	for rows.Next() {
		var key string
		var raw sql.NullString
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		if !raw.Valid || raw.String == "" || raw.String == "null" {
			result[key] = nil
			continue
		}
		var value any
		if err := json.Unmarshal([]byte(raw.String), &value); err != nil {
			result[key] = raw.String
		} else {
			result[key] = value
		}
	}
	return result, rows.Err()
}

func (s *Store) ValidateAPIToken(token string) (string, error) {
	rows, err := s.DB.Query(`
		SELECT token.user, token.value, enabled.value
		FROM user_settings token
		LEFT JOIN user_settings enabled ON enabled.user = token.user AND enabled.key = 'api.enabled'
		WHERE token.key = 'api.token'
	`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var userID string
		var tokenRaw sql.NullString
		var enabledRaw sql.NullString
		if err := rows.Scan(&userID, &tokenRaw, &enabledRaw); err != nil {
			return "", err
		}
		var stored string
		if tokenRaw.Valid {
			_ = json.Unmarshal([]byte(tokenRaw.String), &stored)
		}
		if stored == token {
			var enabled bool
			if enabledRaw.Valid {
				_ = json.Unmarshal([]byte(enabledRaw.String), &enabled)
			}
			if enabled {
				return userID, nil
			}
			return "", errors.New("api disabled")
		}
	}
	return "", rows.Err()
}

func (s *Store) ListMedia(owner string, page, perPage int) ([]MediaWithExpand, int, error) {
	if page <= 0 {
		page = 1
	}
	if perPage <= 0 {
		perPage = 50
	}
	var total int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM media WHERE owner = ?`, owner).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.Query(`SELECT alt, created, file, id, name, owner, updated, diary FROM media WHERE owner = ? ORDER BY created DESC LIMIT ? OFFSET ?`, owner, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]MediaWithExpand, 0)
	diaryIDs := make(map[string]struct{})
	for rows.Next() {
		media, err := scanMediaRow(rows)
		if err != nil {
			return nil, 0, err
		}
		item := MediaWithExpand{Media: *media}
		for _, diaryID := range media.Diary {
			diaryIDs[diaryID] = struct{}{}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}

	diariesByID := make(map[string]Diary, len(diaryIDs))
	for diaryID := range diaryIDs {
		diary, err := s.GetDiaryByID(diaryID)
		if err != nil || diary.Owner != owner {
			continue
		}
		diariesByID[diaryID] = *diary
	}

	for i := range items {
		if len(items[i].Diary) == 0 {
			continue
		}
		diaries := make([]Diary, 0, len(items[i].Diary))
		for _, diaryID := range items[i].Diary {
			if diary, ok := diariesByID[diaryID]; ok {
				diaries = append(diaries, diary)
			}
		}
		items[i].Expand = map[string]any{"diary": diaries}
	}

	return items, total, nil
}

func (s *Store) GetMedia(id, owner string) (*Media, error) {
	media, err := scanMedia(s.DB.QueryRow(`SELECT alt, created, file, id, name, owner, updated, diary FROM media WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	if owner != "" && media.Owner != owner {
		return nil, sql.ErrNoRows
	}
	return media, nil
}

func (s *Store) CreateMedia(owner, file, name, alt string, diary []string) (*Media, error) {
	id, err := GenerateID()
	if err != nil {
		return nil, err
	}
	return s.InsertImportedMedia(owner, id, file, name, alt, diary)
}

func (s *Store) InsertImportedMedia(owner, id, file, name, alt string, diary []string) (*Media, error) {
	if id == "" {
		return nil, fmt.Errorf("media id is required")
	}
	now := nowString()
	_, err := s.DB.Exec(`INSERT INTO media(alt, created, file, id, name, owner, updated, diary) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, alt, now, file, id, name, owner, now, encodeJSON(diary))
	if err != nil {
		return nil, err
	}
	return s.GetMedia(id, owner)
}

func (s *Store) UpdateMediaDiary(id, owner string, diary []string) (*Media, error) {
	_, err := s.DB.Exec(`UPDATE media SET diary = ?, updated = ? WHERE id = ? AND owner = ?`, encodeJSON(diary), nowString(), id, owner)
	if err != nil {
		return nil, err
	}
	return s.GetMedia(id, owner)
}

func (s *Store) DeleteMedia(id, owner string) error {
	result, err := s.DB.Exec(`DELETE FROM media WHERE id = ? AND owner = ?`, id, owner)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func scanMedia(row interface{ Scan(dest ...any) error }) (*Media, error) {
	var diaryRaw string
	media := &Media{}
	err := row.Scan(&media.Alt, &media.Created, &media.File, &media.ID, &media.Name, &media.Owner, &media.Updated, &diaryRaw)
	if err != nil {
		return nil, err
	}
	media.Diary = decodeStringSlice(diaryRaw)
	return media, nil
}

func scanMediaRow(rows *sql.Rows) (*Media, error) {
	return scanMedia(rows)
}

func (s *Store) MediaFilePath(media *Media) string {
	candidates := []string{
		filepath.Join(s.DataDir, "storage", s.MediaCollectionID, media.ID, media.File),
		filepath.Join(s.DataDir, "storage", DefaultMediaCollectionID, media.ID, media.File),
	}
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate
		}
	}
	return candidates[0]
}

func (s *Store) NewMediaFilePath(mediaID, filename string) string {
	return filepath.Join(s.DataDir, "storage", DefaultMediaCollectionID, mediaID, filename)
}

func (s *Store) ListConversations(owner string, limit int) ([]*Conversation, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.DB.Query(`SELECT created, id, owner, title, updated FROM ai_conversations WHERE owner = ? ORDER BY updated DESC LIMIT ?`, owner, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*Conversation, 0)
	for rows.Next() {
		item := &Conversation{}
		if err := rows.Scan(&item.Created, &item.ID, &item.Owner, &item.Title, &item.Updated); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetConversation(id, owner string) (*Conversation, error) {
	item := &Conversation{}
	err := s.DB.QueryRow(`SELECT created, id, owner, title, updated FROM ai_conversations WHERE id = ?`, id).Scan(&item.Created, &item.ID, &item.Owner, &item.Title, &item.Updated)
	if err != nil {
		return nil, err
	}
	if owner != "" && item.Owner != owner {
		return nil, sql.ErrNoRows
	}
	return item, nil
}

func (s *Store) CreateConversation(owner, title string) (*Conversation, error) {
	id, err := GenerateID()
	if err != nil {
		return nil, err
	}
	return s.InsertImportedConversation(owner, id, title)
}

func (s *Store) InsertImportedConversation(owner, id, title string) (*Conversation, error) {
	if id == "" {
		return nil, fmt.Errorf("conversation id is required")
	}
	now := nowString()
	_, err := s.DB.Exec(`INSERT INTO ai_conversations(created, id, owner, title, updated) VALUES(?, ?, ?, ?, ?)`, now, id, owner, title, now)
	if err != nil {
		return nil, err
	}
	return s.GetConversation(id, owner)
}

func (s *Store) UpdateConversationTitle(id, owner, title string) (*Conversation, error) {
	_, err := s.DB.Exec(`UPDATE ai_conversations SET title = ?, updated = ? WHERE id = ? AND owner = ?`, title, nowString(), id, owner)
	if err != nil {
		return nil, err
	}
	return s.GetConversation(id, owner)
}

func (s *Store) DeleteConversation(id, owner string) error {
	result, err := s.DB.Exec(`DELETE FROM ai_conversations WHERE id = ? AND owner = ?`, id, owner)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ListMessages(conversationID string, limit int) ([]*Message, error) {
	query := `SELECT content, conversation, created, id, owner, referenced_diaries, role, updated FROM ai_messages WHERE conversation = ? ORDER BY created ASC`
	args := []any{conversationID}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*Message, 0)
	for rows.Next() {
		item, err := scanMessageRow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CountMessages(conversationID string) (int, error) {
	var total int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM ai_messages WHERE conversation = ?`, conversationID).Scan(&total)
	return total, err
}

func (s *Store) CreateMessage(owner, conversationID, role, content string, referenced []string) (*Message, error) {
	id, err := GenerateID()
	if err != nil {
		return nil, err
	}
	return s.InsertImportedMessage(owner, id, conversationID, role, content, referenced)
}

func (s *Store) InsertImportedMessage(owner, id, conversationID, role, content string, referenced []string) (*Message, error) {
	if id == "" {
		return nil, fmt.Errorf("message id is required")
	}
	now := nowString()
	_, err := s.DB.Exec(`INSERT INTO ai_messages(content, conversation, created, id, owner, referenced_diaries, role, updated) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, content, conversationID, now, id, owner, encodeJSON(referenced), role, now)
	if err != nil {
		return nil, err
	}
	return s.GetMessage(id)
}

func (s *Store) GetMessage(id string) (*Message, error) {
	return scanMessage(s.DB.QueryRow(`SELECT content, conversation, created, id, owner, referenced_diaries, role, updated FROM ai_messages WHERE id = ?`, id))
}

func scanMessage(row interface{ Scan(dest ...any) error }) (*Message, error) {
	var refs sql.NullString
	msg := &Message{}
	err := row.Scan(&msg.Content, &msg.Conversation, &msg.Created, &msg.ID, &msg.Owner, &refs, &msg.Role, &msg.Updated)
	if err != nil {
		return nil, err
	}
	if refs.Valid {
		msg.ReferencedDiaries = decodeStringSlice(refs.String)
	}
	return msg, nil
}

func scanMessageRow(rows *sql.Rows) (*Message, error) {
	return scanMessage(rows)
}

func (s *Store) InsertImportedDiary(owner, id, date, content, mood, weather string) (*Diary, error) {
	if id == "" {
		var err error
		id, err = GenerateID()
		if err != nil {
			return nil, err
		}
	}
	now := nowString()
	_, err := s.DB.Exec(`INSERT INTO diaries(content, created, date, id, mood, owner, updated, weather, tags) VALUES(?, ?, ?, ?, ?, ?, ?, ?, '[]')`, content, now, date+" 00:00:00.000Z", id, mood, owner, now, weather)
	if err != nil {
		return nil, err
	}
	return s.GetDiaryByID(id)
}

func (s *Store) DiaryExistsByDate(owner, date string) bool {
	start, end := dayRange(date)
	_, err := s.GetDiaryByDate(owner, start, end)
	return err == nil
}

func (s *Store) SaveUploadedFile(dst string, reader io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, reader)
	return err
}

func SafeFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "\x00", "")
	if name == "." || name == "" {
		return "upload"
	}
	return name
}

func TotalPages(total, perPage int) int {
	if perPage <= 0 {
		return 0
	}
	pages := total / perPage
	if total%perPage != 0 {
		pages++
	}
	return pages
}

func NormalizeDate(date string) string {
	return DateOnly(date)
}

func IsNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func Errorf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
