// Package systems3 holds the system S3 storage an administrator configures
// once in the admin console. The audit archive, the visit archive and user
// backups can each choose it instead of entering their own connection.
//
// Everything is written below one global prefix (default "diarum"), each use
// in its own folder:
//
//	<prefix>/audit/YYYY-MM-DD.log.gz
//	<prefix>/visits/YYYY/YYYY-MM-DD.jsonl.gz
//	<prefix>/user-backups/<user id>/<backup id>.zip
package systems3

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/songtianlun/diarum/internal/backup"
	"github.com/songtianlun/diarum/internal/logger"
)

// SettingsKey is the system setting holding the configuration.
const SettingsKey = "system.s3"

// DefaultPrefix is the global prefix used until an admin sets one.
const DefaultPrefix = "diarum"

// Folders below the global prefix, one per use.
const (
	FolderAudit       = "audit"
	FolderVisits      = "visits"
	FolderUserBackups = "user-backups"
)

const probeName = ".diarum-system-s3-test"

// ErrIncomplete is returned when only part of a connection is entered.
var ErrIncomplete = errors.New("bucket, region, access key and secret are all required (or clear them all to remove the system S3)")

// SettingsStore persists the settings.
type SettingsStore interface {
	GetSystemSetting(key string) (string, bool, error)
	SetSystemSetting(key, value string) error
}

// ObjectStoreFactory connects to an S3 destination.
type ObjectStoreFactory func(cfg backup.S3Config) (backup.ObjectStore, error)

// Settings is the system S3 configuration. S3.Prefix is the global prefix.
type Settings struct {
	S3 backup.S3Config `json:"s3"`
	// AllowUserBackups lets users choose this storage for their backups.
	AllowUserBackups bool `json:"allow_user_backups"`
}

// Configured reports whether a complete connection is set.
func (s Settings) Configured() bool {
	return s.S3.Complete()
}

func (s Settings) normalized() Settings {
	s.S3 = s.S3.Normalized()
	if s.S3.Prefix == "" {
		s.S3.Prefix = DefaultPrefix
	}
	return s
}

func (s Settings) validate() error {
	c := s.S3
	empty := c.Bucket == "" && c.Region == "" && c.AccessKey == "" && c.Secret == "" && c.Endpoint == ""
	if !empty && !c.Complete() {
		return ErrIncomplete
	}
	return nil
}

// Service keeps the current settings. It is safe for concurrent use; a nil
// *Service behaves as an unconfigured one.
type Service struct {
	store   SettingsStore
	factory ObjectStoreFactory

	mu       sync.RWMutex
	settings Settings
}

// New loads the stored settings. factory may be nil.
func New(store SettingsStore, factory ObjectStoreFactory) *Service {
	if factory == nil {
		factory = backup.NewS3Store
	}
	s := &Service{store: store, factory: factory, settings: Settings{}.normalized()}
	if store == nil {
		return s
	}
	raw, ok, err := store.GetSystemSetting(SettingsKey)
	if err != nil {
		logger.Error("[SystemS3] load settings failed, the system S3 is unavailable: %v", err)
		return s
	}
	if !ok {
		return s
	}
	var settings Settings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		logger.Error("[SystemS3] settings are unreadable, the system S3 is unavailable: %v", err)
		return s
	}
	s.settings = settings.normalized()
	return s
}

// Settings returns the current settings, secret included.
func (s *Service) Settings() Settings {
	if s == nil {
		return Settings{}.normalized()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

// prepare fills in the stored secret when next leaves it empty (clients never
// see it) and normalizes and validates the result.
func (s *Service) prepare(next Settings) (Settings, error) {
	previous := s.Settings()
	next.S3 = next.S3.Normalized()
	if next.S3.Secret == "" && next.S3.Bucket != "" {
		next.S3.Secret = previous.S3.Secret
	}
	next = next.normalized()
	return next, next.validate()
}

// Update validates, stores and applies new settings. An empty secret keeps
// the stored one.
func (s *Service) Update(next Settings) (Settings, error) {
	if s == nil {
		return next, errors.New("system S3 is unavailable")
	}
	next, err := s.prepare(next)
	if err != nil {
		return s.Settings(), err
	}
	if s.store != nil {
		raw, err := json.Marshal(next)
		if err != nil {
			return s.Settings(), err
		}
		if err := s.store.SetSystemSetting(SettingsKey, string(raw)); err != nil {
			return s.Settings(), err
		}
	}
	s.mu.Lock()
	s.settings = next
	s.mu.Unlock()
	return next, nil
}

// For returns the connection for one use with Prefix set to that use's
// folder below the global prefix. ok is false while no complete connection
// is configured.
func (s *Service) For(folder string) (backup.S3Config, bool) {
	settings := s.Settings()
	if !settings.Configured() {
		return backup.S3Config{}, false
	}
	cfg := settings.S3
	cfg.Prefix = backup.JoinPrefix(cfg.Prefix, folder)
	return cfg, true
}

// Func returns For(folder) as a backup.SystemS3Func.
func (s *Service) Func(folder string) backup.SystemS3Func {
	return func() (backup.S3Config, bool) { return s.For(folder) }
}

// UserBackups returns the connection for user backups; ok is false unless
// it is configured and offered to users. Each user's backups then live in
// <prefix>/user-backups/<user id>/.
func (s *Service) UserBackups() (backup.S3Config, bool) {
	if !s.Settings().AllowUserBackups {
		return backup.S3Config{}, false
	}
	return s.For(FolderUserBackups)
}

// Path describes where a use writes, for display, e.g. "diarum/audit/".
func (s *Service) Path(folder string) string {
	return backup.JoinPrefix(s.Settings().S3.Prefix, folder) + "/"
}

// Test checks that next (with an empty secret meaning the stored one) can
// write, read, list and delete below the global prefix.
func (s *Service) Test(ctx context.Context, next Settings) error {
	if s == nil {
		return errors.New("system S3 is unavailable")
	}
	next, err := s.prepare(next)
	if err != nil {
		return err
	}
	if !next.Configured() {
		return ErrIncomplete
	}
	return probe(ctx, s.factory, next.S3)
}

func probe(ctx context.Context, factory ObjectStoreFactory, cfg backup.S3Config) error {
	store, err := factory(cfg)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	prefix := backup.JoinPrefix(cfg.Prefix) + "/"
	key := prefix + probeName
	payload := "diarum system S3 test " + time.Now().UTC().Format(time.RFC3339)
	if err := store.Put(ctx, key, strings.NewReader(payload), "text/plain"); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}
	defer func() { _ = store.Delete(context.Background(), key) }()
	rc, err := store.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("read failed: %w", err)
	}
	got, err := io.ReadAll(io.LimitReader(rc, 1024))
	_ = rc.Close()
	if err != nil || string(got) != payload {
		return errors.New("read failed: object content did not match")
	}
	if _, err := store.List(ctx, prefix); err != nil {
		return fmt.Errorf("list failed: %w", err)
	}
	if err := store.Delete(ctx, key); err != nil {
		return fmt.Errorf("delete failed: %w", err)
	}
	return nil
}
