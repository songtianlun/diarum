package visits

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/songtianlun/diarum/internal/backup"
	"github.com/songtianlun/diarum/internal/logger"
)

// SettingsKey is the system setting holding the visit tracking settings.
const SettingsKey = "visits.settings"

// Bounds and defaults. A retention of 0 keeps records forever.
const (
	DefaultRetentionDays = 3 * 365
	MaxRetentionDays     = 100 * 365

	DefaultArchiveRetentionDays = 10 * 365
	MaxArchiveRetentionDays     = 100 * 365

	DefaultDedupeSeconds = 60
	MinDedupeSeconds     = 0
	MaxDedupeSeconds     = 3600
	maxDedupeWindow      = MaxDedupeSeconds * 1e9

	DefaultMaxRecords = 5_000_000
	MinMaxRecords     = 10_000
	MaxMaxRecords     = 200_000_000

	DefaultIPLimit     = 120
	DefaultGlobalLimit = 2000
	DefaultOwnerLimit  = 3000
	MaxRateLimit       = 1_000_000

	DefaultArchivePrefix = "diarum-visits"

	// S3 sources for the archive. S3SourceShared (reuse the audit archive's
	// connection) predates the system S3 and is kept for existing settings.
	S3SourceSystem = backup.SourceSystem
	S3SourceShared = "audit"
	S3SourceCustom = backup.SourceCustom
)

// SettingsStore persists the settings.
type SettingsStore interface {
	GetSystemSetting(key string) (string, bool, error)
	SetSystemSetting(key, value string) error
}

// SharedS3Func returns the S3 connection of the system audit archive.
type SharedS3Func func() backup.S3Config

// ObjectStoreFactory connects to an S3 destination.
type ObjectStoreFactory func(cfg backup.S3Config) (backup.ObjectStore, error)

// Settings configures visit tracking.
type Settings struct {
	// Enabled turns recording on; users only see their statistics while it
	// is on.
	Enabled bool `json:"enabled"`
	// RetentionDays is how long records stay in the database; 0 is forever.
	RetentionDays int `json:"retention_days"`
	// DedupeSeconds: repeated reads by the same visitor and device within
	// this window count once; 0 records every read.
	DedupeSeconds int `json:"dedupe_seconds"`
	// MaxRecords caps the table; the oldest records are trimmed beyond it.
	MaxRecords int `json:"max_records"`
	// Rate limits per minute: refused or anonymous attempts per IP and in
	// total, and successful reads per diary owner.
	IPLimitPerMinute      int             `json:"ip_limit_per_minute"`
	GlobalFailedPerMinute int             `json:"global_failed_per_minute"`
	OwnerLimitPerMinute   int             `json:"owner_limit_per_minute"`
	Archive               ArchiveSettings `json:"archive"`
}

// ArchiveSettings configures the S3 archive of finished days.
type ArchiveSettings struct {
	Enabled bool `json:"enabled"`
	// Source is S3SourceSystem (the admin-managed system S3, writing to
	// <global prefix>/visits/), S3SourceCustom (S3 below) or the legacy
	// S3SourceShared (the audit archive's connection).
	Source string          `json:"source"`
	S3     backup.S3Config `json:"s3"`
	// Prefix is the folder archives are written under for the custom and
	// shared sources; the system source uses its own folder.
	Prefix string `json:"prefix"`
	// RetentionDays is how long archives are kept in the bucket; 0 is forever.
	RetentionDays int `json:"retention_days"`
}

// DefaultSettings are used until an admin saves settings. Recording starts
// switched off.
func DefaultSettings() Settings {
	return Settings{
		RetentionDays:         DefaultRetentionDays,
		DedupeSeconds:         DefaultDedupeSeconds,
		MaxRecords:            DefaultMaxRecords,
		IPLimitPerMinute:      DefaultIPLimit,
		GlobalFailedPerMinute: DefaultGlobalLimit,
		OwnerLimitPerMinute:   DefaultOwnerLimit,
		Archive: ArchiveSettings{
			Source:        S3SourceSystem,
			Prefix:        DefaultArchivePrefix,
			RetentionDays: DefaultArchiveRetentionDays,
		},
	}
}

// Validate checks the bounds and normalizes the S3 fields.
func (s Settings) Validate() (Settings, error) {
	if s.RetentionDays < 0 || s.RetentionDays > MaxRetentionDays {
		return s, fmt.Errorf("retention_days must be between 0 (forever) and %d", MaxRetentionDays)
	}
	if s.Archive.RetentionDays < 0 || s.Archive.RetentionDays > MaxArchiveRetentionDays {
		return s, fmt.Errorf("archive retention_days must be between 0 (forever) and %d", MaxArchiveRetentionDays)
	}
	if s.DedupeSeconds < MinDedupeSeconds || s.DedupeSeconds > MaxDedupeSeconds {
		return s, fmt.Errorf("dedupe_seconds must be between %d and %d", MinDedupeSeconds, MaxDedupeSeconds)
	}
	if s.MaxRecords < MinMaxRecords || s.MaxRecords > MaxMaxRecords {
		return s, fmt.Errorf("max_records must be between %d and %d", MinMaxRecords, MaxMaxRecords)
	}
	for _, limit := range []struct {
		name  string
		value int
	}{{"ip_limit_per_minute", s.IPLimitPerMinute}, {"global_failed_per_minute", s.GlobalFailedPerMinute}, {"owner_limit_per_minute", s.OwnerLimitPerMinute}} {
		if limit.value < 1 || limit.value > MaxRateLimit {
			return s, fmt.Errorf("%s must be between 1 and %d", limit.name, MaxRateLimit)
		}
	}
	switch s.Archive.Source {
	case "":
		s.Archive.Source = backup.NormalizeSource("", s.Archive.S3.Normalized())
	case S3SourceSystem, S3SourceShared, S3SourceCustom:
	default:
		return s, errors.New("archive source must be system, audit or custom")
	}
	s.Archive.S3 = s.Archive.S3.Normalized()
	s.Archive.S3.Prefix = ""
	s.Archive.Prefix = backup.NormalizePrefix(s.Archive.Prefix)
	if s.Archive.Prefix == "" {
		s.Archive.Prefix = DefaultArchivePrefix
	}
	if s.Archive.Enabled && s.Archive.Source == S3SourceCustom && !s.Archive.S3.Complete() {
		return s, errors.New("bucket, region, access key and secret are required to enable the archive")
	}
	return s, nil
}

// fillDefaults repairs stored settings from older or hand-edited versions.
func (s Settings) fillDefaults() Settings {
	d := DefaultSettings()
	if s.DedupeSeconds < 0 || s.DedupeSeconds > MaxDedupeSeconds {
		s.DedupeSeconds = d.DedupeSeconds
	}
	if s.MaxRecords < MinMaxRecords || s.MaxRecords > MaxMaxRecords {
		s.MaxRecords = d.MaxRecords
	}
	if s.IPLimitPerMinute < 1 || s.IPLimitPerMinute > MaxRateLimit {
		s.IPLimitPerMinute = d.IPLimitPerMinute
	}
	if s.GlobalFailedPerMinute < 1 || s.GlobalFailedPerMinute > MaxRateLimit {
		s.GlobalFailedPerMinute = d.GlobalFailedPerMinute
	}
	if s.OwnerLimitPerMinute < 1 || s.OwnerLimitPerMinute > MaxRateLimit {
		s.OwnerLimitPerMinute = d.OwnerLimitPerMinute
	}
	if s.RetentionDays < 0 || s.RetentionDays > MaxRetentionDays {
		s.RetentionDays = d.RetentionDays
	}
	if s.Archive.RetentionDays < 0 || s.Archive.RetentionDays > MaxArchiveRetentionDays {
		s.Archive.RetentionDays = d.Archive.RetentionDays
	}
	switch s.Archive.Source {
	case S3SourceSystem, S3SourceCustom:
	default:
		// Settings saved before the system S3 existed: missing meant shared.
		s.Archive.Source = S3SourceShared
	}
	s.Archive.S3 = s.Archive.S3.Normalized()
	s.Archive.Prefix = backup.NormalizePrefix(s.Archive.Prefix)
	if s.Archive.Prefix == "" {
		s.Archive.Prefix = DefaultArchivePrefix
	}
	return s
}

func (a *archiver) loadSettings() {
	if a.store == nil {
		return
	}
	raw, ok, err := a.store.GetSystemSetting(SettingsKey)
	if err != nil {
		logger.Error("[Visits] load settings failed, using defaults: %v", err)
		return
	}
	if !ok {
		return
	}
	settings := DefaultSettings()
	settings.Archive.Source = ""
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		logger.Error("[Visits] settings are unreadable, using defaults: %v", err)
		return
	}
	a.settings = settings.fillDefaults()
}

func (a *archiver) current() Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings
}

// Settings returns the current settings.
func (t *Tracker) Settings() Settings {
	if t == nil {
		return DefaultSettings()
	}
	return t.archiver.current()
}

// UpdateSettings validates, stores and applies new settings. An empty custom
// S3 secret keeps the stored one, so clients never need to see it.
func (t *Tracker) UpdateSettings(next Settings) (Settings, error) {
	if t == nil {
		return next, errClosed
	}
	a := t.archiver
	previous := a.current()
	if next.Archive.S3.Secret == "" {
		next.Archive.S3.Secret = previous.Archive.S3.Secret
	}
	next, err := next.Validate()
	if err != nil {
		return previous, err
	}
	if next.Archive.Enabled && next.Archive.Source == S3SourceSystem && !a.archiveReady(next) {
		return previous, ErrSystemS3Unavailable
	}
	if a.store != nil {
		raw, err := json.Marshal(next)
		if err != nil {
			return previous, err
		}
		if err := a.store.SetSystemSetting(SettingsKey, string(raw)); err != nil {
			return previous, err
		}
	}
	a.mu.Lock()
	a.settings = next
	a.mu.Unlock()
	return next, nil
}

// archiveS3 resolves the S3 connection the archive uses, with the visit
// archive's own prefix. ok is false when no complete connection exists.
func (a *archiver) archiveS3(s Settings) (cfg backup.S3Config, ok bool) {
	switch s.Archive.Source {
	case S3SourceSystem:
		if a.system == nil {
			return backup.S3Config{}, false
		}
		cfg, ok = a.system()
		cfg = cfg.Normalized()
		if cfg.Prefix == "" {
			cfg.Prefix = DefaultArchivePrefix
		}
		return cfg, ok && cfg.Complete()
	case S3SourceCustom:
		cfg = s.Archive.S3
	default:
		if a.shared != nil {
			cfg = a.shared().Normalized()
		}
	}
	cfg.Prefix = s.Archive.Prefix
	return cfg, cfg.Complete()
}

// archiveReady reports whether finished days should go to S3.
func (a *archiver) archiveReady(s Settings) bool {
	if !s.Archive.Enabled {
		return false
	}
	_, ok := a.archiveS3(s)
	return ok
}

// UsesSystemS3 reports whether the archive is switched on and uses the
// system S3.
func (t *Tracker) UsesSystemS3() bool {
	if t == nil {
		return false
	}
	s := t.archiver.current()
	return s.Archive.Enabled && s.Archive.Source == S3SourceSystem
}

// SystemS3Path returns the folder the archive writes to on the system S3,
// e.g. "diarum/visits/"; ok is false while the system S3 is not configured.
func (t *Tracker) SystemS3Path() (path string, ok bool) {
	if t == nil {
		return "", false
	}
	target, ok := t.archiver.archiveS3(Settings{Archive: ArchiveSettings{Source: S3SourceSystem}})
	return archivePrefix(target), ok
}

// SharedS3Available reports whether the audit archive has a complete S3
// connection the visit archive can reuse.
func (t *Tracker) SharedS3Available() bool {
	if t == nil || t.archiver.shared == nil {
		return false
	}
	return t.archiver.shared().Normalized().Complete()
}

// SharedS3Bucket names the bucket of the shared connection, for display.
func (t *Tracker) SharedS3Bucket() string {
	if t == nil || t.archiver.shared == nil {
		return ""
	}
	return t.archiver.shared().Normalized().Bucket
}
