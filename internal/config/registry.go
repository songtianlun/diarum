package config

// ConfigMeta defines metadata for a configuration item
type ConfigMeta struct {
	Type      string // "string", "bool", "int", "float", "json"
	Default   any
	Encrypted bool
}

// ConfigRegistry defines all available configuration items
var ConfigRegistry = map[string]ConfigMeta{
	// API settings
	"api.token":   {Type: "string", Default: "", Encrypted: false},
	"api.enabled": {Type: "bool", Default: false, Encrypted: false},

	// Sync settings
	"sync.autoSaveInterval": {Type: "int", Default: 3000, Encrypted: false},  // milliseconds
	"sync.cacheDays":        {Type: "int", Default: 30, Encrypted: false},

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
