// Package backup runs full-account backups to S3-compatible storage, on
// demand or on a cron schedule, and restores them through the regular import.
//
// Each run leaves two objects next to each other under
// <prefix>/<user id>/: the archive <id>.zip and its log <id>.log.json. The
// log is written even when the run fails, so there is always something to
// look at. Everything a restore needs lives in the bucket, so backups remain
// usable after the server's own disk is lost.
package backup

import (
	"math"
	"strings"

	"github.com/songtianlun/diarum/internal/config"
)

// Setting keys. They are registered in config.ConfigRegistry.
const (
	KeyEnabled        = "backup.enabled"
	KeyBucket         = "backup.s3.bucket"
	KeyRegion         = "backup.s3.region"
	KeyEndpoint       = "backup.s3.endpoint"
	KeyAccessKey      = "backup.s3.access_key"
	KeySecret         = "backup.s3.secret"
	KeyForcePathStyle = "backup.s3.force_path_style"
	KeyPrefix         = "backup.s3.prefix"
	KeySource         = "backup.s3.source"
	KeyAutoEnabled    = "backup.auto_enabled"
	KeySchedule       = "backup.schedule"
	KeyTimezone       = "backup.timezone"
	KeyKeep           = "backup.keep"
)

const (
	DefaultPrefix   = "diarum-backups"
	DefaultSchedule = "0 2 * * *"
	DefaultKeep     = 3
	MinKeep         = 1
	MaxKeep         = 100
)

// Where a destination's connection comes from: the system S3 an
// administrator manages, or connection details entered by hand.
const (
	SourceSystem = "system"
	SourceCustom = "custom"
)

// SystemS3Func returns the system S3 connection for one use, with Prefix set
// to the folder that use writes under. ok is false when the system S3 is not
// configured (or, for user backups, not offered to users).
type SystemS3Func func() (cfg S3Config, ok bool)

// JoinPrefix joins prefix segments into one normalized prefix.
func JoinPrefix(parts ...string) string {
	return NormalizePrefix(strings.Join(parts, "/"))
}

// S3Config is a backup destination.
type S3Config struct {
	Bucket         string `json:"bucket"`
	Region         string `json:"region"`
	Endpoint       string `json:"endpoint"`
	AccessKey      string `json:"access_key"`
	Secret         string `json:"secret"`
	ForcePathStyle bool   `json:"force_path_style"`
	Prefix         string `json:"prefix"`
}

// Complete reports whether the fields S3 needs are all set.
func (c S3Config) Complete() bool {
	return c.Bucket != "" && c.Region != "" && c.AccessKey != "" && c.Secret != ""
}

// Normalized trims every field and cleans up the prefix.
func (c S3Config) Normalized() S3Config {
	c.Bucket = strings.TrimSpace(c.Bucket)
	c.Region = strings.TrimSpace(c.Region)
	c.Endpoint = strings.TrimSpace(c.Endpoint)
	c.AccessKey = strings.TrimSpace(c.AccessKey)
	c.Secret = strings.TrimSpace(c.Secret)
	c.Prefix = NormalizePrefix(c.Prefix)
	return c
}

// NormalizePrefix trims surrounding slashes and whitespace, collapses empty
// path segments and rejects "." and "..".
func NormalizePrefix(prefix string) string {
	parts := make([]string, 0)
	for _, part := range strings.Split(strings.ReplaceAll(prefix, "\\", "/"), "/") {
		part = strings.TrimSpace(part)
		if part == "" || part == "." || part == ".." {
			continue
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "/")
}

// Settings is a user's backup configuration.
type Settings struct {
	Enabled bool `json:"enabled"`
	// Source is SourceSystem (the server's shared S3) or SourceCustom (S3).
	Source      string   `json:"source"`
	S3          S3Config `json:"s3"`
	AutoEnabled bool     `json:"auto_enabled"`
	Schedule    string   `json:"schedule"`
	Timezone    string   `json:"timezone"`
	Keep        int      `json:"keep"`
}

// LoadSettings reads a user's backup settings, filling in defaults.
func LoadSettings(cfg *config.ConfigService, userID string) (Settings, error) {
	var out Settings
	var err error
	str := func(key string) string {
		if err != nil {
			return ""
		}
		var value string
		value, err = cfg.GetString(userID, key)
		return value
	}
	flag := func(key string) bool {
		if err != nil {
			return false
		}
		var value bool
		value, err = cfg.GetBool(userID, key)
		return value
	}
	out.Enabled = flag(KeyEnabled)
	out.S3 = S3Config{
		Bucket:         str(KeyBucket),
		Region:         str(KeyRegion),
		Endpoint:       str(KeyEndpoint),
		AccessKey:      str(KeyAccessKey),
		Secret:         str(KeySecret),
		ForcePathStyle: flag(KeyForcePathStyle),
		Prefix:         str(KeyPrefix),
	}.Normalized()
	out.Source = NormalizeSource(str(KeySource), out.S3)
	out.AutoEnabled = flag(KeyAutoEnabled)
	out.Schedule = strings.TrimSpace(str(KeySchedule))
	out.Timezone = strings.TrimSpace(str(KeyTimezone))
	if err != nil {
		return Settings{}, err
	}
	if out.Schedule == "" {
		out.Schedule = DefaultSchedule
	}
	keep, _ := cfg.Get(userID, KeyKeep)
	out.Keep = ClampKeep(keep)
	return out, nil
}

// Values returns the settings as config key/value pairs for saving.
func (s Settings) Values() map[string]any {
	return map[string]any{
		KeyEnabled:        s.Enabled,
		KeySource:         s.Source,
		KeyBucket:         s.S3.Bucket,
		KeyRegion:         s.S3.Region,
		KeyEndpoint:       s.S3.Endpoint,
		KeyAccessKey:      s.S3.AccessKey,
		KeySecret:         s.S3.Secret,
		KeyForcePathStyle: s.S3.ForcePathStyle,
		KeyPrefix:         s.S3.Prefix,
		KeyAutoEnabled:    s.AutoEnabled,
		KeySchedule:       s.Schedule,
		KeyTimezone:       s.Timezone,
		KeyKeep:           s.Keep,
	}
}

// NormalizeSource maps a stored source to a known one. Settings saved before
// sources existed have none: they keep their own bucket when one was entered.
func NormalizeSource(source string, custom S3Config) string {
	switch source = strings.TrimSpace(source); source {
	case SourceSystem, SourceCustom:
		return source
	}
	if custom.Bucket != "" {
		return SourceCustom
	}
	return SourceSystem
}

// ClampKeep turns a stored retention count into a usable one.
func ClampKeep(value any) int {
	var n float64
	switch v := value.(type) {
	case float64:
		n = v
	case int:
		n = float64(v)
	default:
		return DefaultKeep
	}
	if math.IsNaN(n) {
		return DefaultKeep
	}
	return int(math.Max(MinKeep, math.Min(MaxKeep, math.Trunc(n))))
}
