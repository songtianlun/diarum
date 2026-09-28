package config

import (
	"errors"
	"math"

	"github.com/songtianlun/diarum/internal/store"
)

// ConfigMeta defines metadata for a configuration item
type ConfigMeta struct {
	Type      string // "string", "bool", "int", "float", "json"
	Default   any
	Encrypted bool
	// Min and Max bound "int" values when Max is non-zero.
	Min int
	Max int
}

// ConfigRegistry defines all available configuration items
var ConfigRegistry = map[string]ConfigMeta{
	// API settings
	"api.token":       {Type: "string", Default: "", Encrypted: false},
	"api.enabled":     {Type: "bool", Default: false, Encrypted: false},
	"api.mcp_enabled": {Type: "bool", Default: false, Encrypted: false},

	// Sync settings
	"sync.cacheDays": {Type: "int", Default: 30, Encrypted: false},

	// Memos webhook sync settings
	"memos.enabled":       {Type: "bool", Default: false, Encrypted: false},
	"memos.webhook_token": {Type: "string", Default: "", Encrypted: true},
	"memos.base_url":      {Type: "string", Default: "", Encrypted: false},

	// AI settings (unified API key and base URL)
	"ai.enabled":          {Type: "bool", Default: false, Encrypted: false},
	"ai.api_key":          {Type: "string", Default: "", Encrypted: true},
	"ai.base_url":         {Type: "string", Default: "", Encrypted: false},
	"ai.chat_model":       {Type: "string", Default: "", Encrypted: false},
	"ai.embedding_model":  {Type: "string", Default: "", Encrypted: false},
	"ai.vectors_built_at": {Type: "string", Default: "", Encrypted: false},

	// Chevereto image hosting settings
	"chevereto.enabled":  {Type: "bool", Default: false, Encrypted: false},
	"chevereto.domain":   {Type: "string", Default: "", Encrypted: false},
	"chevereto.api_key":  {Type: "string", Default: "", Encrypted: true},
	"chevereto.album_id": {Type: "string", Default: "", Encrypted: false},

	// Image upload storage settings
	"image_upload.provider":            {Type: "string", Default: "local", Encrypted: false},
	"image_upload.local.path":          {Type: "string", Default: "", Encrypted: false},
	"image_upload.s3.bucket":           {Type: "string", Default: "", Encrypted: false},
	"image_upload.s3.region":           {Type: "string", Default: "", Encrypted: false},
	"image_upload.s3.endpoint":         {Type: "string", Default: "", Encrypted: false},
	"image_upload.s3.access_key":       {Type: "string", Default: "", Encrypted: true},
	"image_upload.s3.secret":           {Type: "string", Default: "", Encrypted: true},
	"image_upload.s3.force_path_style": {Type: "bool", Default: false, Encrypted: false},
	"image_upload.display_quality":     {Type: "string", Default: "md", Encrypted: false},

	// Data backup to S3-compatible storage (see internal/backup)
	"backup.enabled":             {Type: "bool", Default: false, Encrypted: false},
	"backup.s3.bucket":           {Type: "string", Default: "", Encrypted: false},
	"backup.s3.region":           {Type: "string", Default: "", Encrypted: false},
	"backup.s3.endpoint":         {Type: "string", Default: "", Encrypted: false},
	"backup.s3.access_key":       {Type: "string", Default: "", Encrypted: true},
	"backup.s3.secret":           {Type: "string", Default: "", Encrypted: true},
	"backup.s3.force_path_style": {Type: "bool", Default: false, Encrypted: false},
	"backup.s3.prefix":           {Type: "string", Default: "diarum-backups", Encrypted: false},
	"backup.auto_enabled":        {Type: "bool", Default: false, Encrypted: false},
	// Five-field cron expression, evaluated in backup.timezone (empty: server local time)
	"backup.schedule": {Type: "string", Default: "0 2 * * *", Encrypted: false},
	"backup.timezone": {Type: "string", Default: "", Encrypted: false},
	// How many successful backups (and failed attempts) are kept in the bucket
	"backup.keep": {Type: "int", Default: 3, Min: 1, Max: 100},
	// Outcome of the latest backup or restore job, written by the server
	"backup.last_run": {Type: "json", Default: nil, Encrypted: false},

	// Diary editor presets
	"diary.mood_options":    {Type: "json", Default: []string{"😊", "😌", "🥳", "💪", "🤔", "😴", "😔", "😤"}, Encrypted: false},
	"diary.weather_options": {Type: "json", Default: []string{"☀️", "⛅", "☁️", "🌧️", "⛈️", "🌫️", "❄️", "🌬️"}, Encrypted: false},
	// How many history snapshots each diary entry keeps.
	store.SettingDiaryMaxSnapshots: {Type: "int", Default: store.DefaultDiarySnapshots, Min: store.MinDiarySnapshots, Max: store.MaxDiarySnapshots},

	// General app preferences
	// homepage: "today" (open today's diary entry) or "overview" (open the /diary calendar overview)
	"general.homepage": {Type: "string", Default: "today", Encrypted: false},
	// visual_style: "classic" (standard editor page), "immersive" (book flip view)
	// or "win95" (retro Windows 95 desktop / Notepad skin)
	"general.visual_style": {Type: "string", Default: "classic", Encrypted: false},
	// language: "auto" (follow browser), "en" or "zh"
	"general.language": {Type: "string", Default: "auto", Encrypted: false},
}

// GetConfigMeta returns the metadata for a configuration key
func GetConfigMeta(key string) (ConfigMeta, bool) {
	meta, ok := ConfigRegistry[key]
	return meta, ok
}

// IsEncrypted checks if a configuration key should be encrypted
func IsEncrypted(key string) bool {
	if meta, ok := ConfigRegistry[key]; ok {
		return meta.Encrypted
	}
	return false
}

// GetDefault returns the default value for a configuration key
func GetDefault(key string) any {
	if meta, ok := ConfigRegistry[key]; ok {
		return meta.Default
	}
	return nil
}

// ErrInvalidValue is returned when a value does not fit its configuration key.
var ErrInvalidValue = errors.New("invalid configuration value")

// ValidateValue checks a value against its key's declared bounds. Keys
// without bounds accept anything, as before.
func ValidateValue(key string, value any) error {
	meta, ok := ConfigRegistry[key]
	if !ok || meta.Type != "int" || meta.Max == 0 {
		return nil
	}
	var number float64
	switch v := value.(type) {
	case float64:
		number = v
	case int:
		number = float64(v)
	default:
		return ErrInvalidValue
	}
	if number != math.Trunc(number) || number < float64(meta.Min) || number > float64(meta.Max) {
		return ErrInvalidValue
	}
	return nil
}
