package config

import (
	"crypto/subtle"
	"encoding/json"
	"errors"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/daos"
	"github.com/pocketbase/pocketbase/models"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/songtianlun/diarum/internal/logger"
)

// ErrUnknownKey is returned when trying to set an unregistered configuration key
var ErrUnknownKey = errors.New("unknown configuration key")

// ErrAPIDisabled is returned when the API is disabled for a user
var ErrAPIDisabled = errors.New("API is disabled for this user")

// ConfigService provides methods to manage user settings
type ConfigService struct {
	app *pocketbase.PocketBase
}

// NewConfigService creates a new ConfigService instance
func NewConfigService(app *pocketbase.PocketBase) *ConfigService {
	return &ConfigService{app: app}
}

// Get retrieves a single configuration value for a user
func (s *ConfigService) Get(userId, key string) (any, error) {
	logger.Debug("[ConfigService.Get] userId=%s, key=%s", userId, key)

	record, err := s.app.Dao().FindFirstRecordByFilter(
		"user_settings",
		"user = {:user} && key = {:key}",
		map[string]any{
			"user": userId,
			"key":  key,
		},
	)

	if err != nil {
		logger.Debug("[ConfigService.Get] Error finding record: %v", err)
		// Return default value if not found
		return GetDefault(key), nil
	}

	value := record.Get("value")
	if isSensitiveKey(key) {
		logger.Debug("[ConfigService.Get] Found value: %s (type: %T)", maskSensitiveValue(s.parseStringValue(value)), value)
	} else {
		logger.Debug("[ConfigService.Get] Found value: %v (type: %T)", value, value)
	}
	return value, nil
}

// GetString retrieves a string configuration value
func (s *ConfigService) GetString(userId, key string) (string, error) {
	value, err := s.Get(userId, key)
	if err != nil {
		return "", err
	}
	if value == nil {
		return "", nil
	}

	// Handle types.JsonRaw
	if raw, ok := value.(types.JsonRaw); ok {
		var str string
		if err := json.Unmarshal(raw, &str); err != nil {
			return "", nil
		}
		return str, nil
	}

	if str, ok := value.(string); ok {
		return str, nil
	}
	return "", nil
}

// GetBool retrieves a boolean configuration value
func (s *ConfigService) GetBool(userId, key string) (bool, error) {
	value, err := s.Get(userId, key)
	if err != nil {
		return false, err
	}
	if value == nil {
		return false, nil
	}

	// Handle types.JsonRaw
	if raw, ok := value.(types.JsonRaw); ok {
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return false, nil
		}
		return b, nil
	}

	// Handle different types that JSON might return
	switch v := value.(type) {
	case bool:
		return v, nil
	case float64:
		return v != 0, nil
	case string:
		return v == "true", nil
	}
	return false, nil
}

// Set stores a configuration value for a user
func (s *ConfigService) Set(userId, key string, value any) error {
	// Validate key against registry
	if _, ok := GetConfigMeta(key); !ok {
		return ErrUnknownKey
	}

	// Find existing record
	record, err := s.app.Dao().FindFirstRecordByFilter(
		"user_settings",
		"user = {:user} && key = {:key}",
		map[string]any{
			"user": userId,
			"key":  key,
		},
	)

	if err != nil {
		// Create new record
		collection, err := s.app.Dao().FindCollectionByNameOrId("user_settings")
		if err != nil {
			return err
		}

		record = models.NewRecord(collection)
		record.Set("user", userId)
		record.Set("key", key)
	}

	record.Set("value", value)
	record.Set("encrypted", IsEncrypted(key))

	return s.app.Dao().SaveRecord(record)
}

// GetBatch retrieves all configuration values for a user
func (s *ConfigService) GetBatch(userId string) (map[string]any, error) {
	records, err := s.app.Dao().FindRecordsByFilter(
		"user_settings",
		"user = {:user}",
		"",
		-1,
		0,
		map[string]any{"user": userId},
	)

	if err != nil {
		return make(map[string]any), nil
	}

	result := make(map[string]any)
	for _, record := range records {
		key := record.GetString("key")
		result[key] = record.Get("value")
	}

	return result, nil
}

// SetBatch stores multiple configuration values for a user atomically
func (s *ConfigService) SetBatch(userId string, settings map[string]any) error {
	return s.app.Dao().RunInTransaction(func(txDao *daos.Dao) error {
		for key, value := range settings {
			// Skip unknown keys with warning log
			if _, ok := GetConfigMeta(key); !ok {
				logger.Warn("[ConfigService.SetBatch] unknown key: %s, skipping", key)
				continue
			}
			// Find existing record
			record, err := txDao.FindFirstRecordByFilter(
				"user_settings",
				"user = {:user} && key = {:key}",
				map[string]any{
					"user": userId,
					"key":  key,
				},
			)

			if err != nil {
				// Create new record
				collection, err := txDao.FindCollectionByNameOrId("user_settings")
				if err != nil {
					return err
				}

				record = models.NewRecord(collection)
				record.Set("user", userId)
				record.Set("key", key)
			}

			record.Set("value", value)
			record.Set("encrypted", IsEncrypted(key))

			if err := txDao.SaveRecord(record); err != nil {
				return err
			}
		}
		return nil
	})
}

// Delete removes a configuration value for a user
func (s *ConfigService) Delete(userId, key string) error {
	record, err := s.app.Dao().FindFirstRecordByFilter(
		"user_settings",
		"user = {:user} && key = {:key}",
		map[string]any{
			"user": userId,
			"key":  key,
		},
	)

	if err != nil {
		return nil // Not found, nothing to delete
	}

	return s.app.Dao().DeleteRecord(record)
}

// maskSensitiveValue returns a masked version of sensitive values for safe logging
func maskSensitiveValue(value string) string {
	if len(value) <= 8 {
		return "***"
	}
	return value[:4] + "***" + value[len(value)-4:]
}

// isSensitiveKey checks if a key contains sensitive data that should be masked in logs
func isSensitiveKey(key string) bool {
	return IsEncrypted(key) || key == "api.token"
}

// ValidateTokenAndGetUser validates an API token and returns the user ID
func (s *ConfigService) ValidateTokenAndGetUser(token string) (string, error) {
	logger.Debug("[ValidateTokenAndGetUser] validating token: %s", maskSensitiveValue(token))

	// Direct query by token value using index on key field
	record, err := s.app.Dao().FindFirstRecordByFilter(
		"user_settings",
		"key = 'api.token' && value = {:token}",
		map[string]any{"token": token},
	)

	if err != nil {
		logger.Debug("[ValidateTokenAndGetUser] no matching token found: %v", err)
		return "", nil
	}

	userId := record.GetString("user")
	storedToken := s.parseStringValue(record.Get("value"))

	// Use constant-time comparison to prevent timing attacks
	if subtle.ConstantTimeCompare([]byte(storedToken), []byte(token)) != 1 {
		logger.Debug("[ValidateTokenAndGetUser] token mismatch")
		return "", nil
	}

	// Check if API is enabled for this user
	enabled, err := s.GetBool(userId, "api.enabled")
	if err != nil {
		logger.Debug("[ValidateTokenAndGetUser] error checking API enabled: %v", err)
		return "", err
	}
	if !enabled {
		logger.Debug("[ValidateTokenAndGetUser] API disabled for user: %s", userId)
		return "", ErrAPIDisabled
	}

	logger.Debug("[ValidateTokenAndGetUser] token validated for user: %s", userId)
	return userId, nil
}

// parseStringValue extracts a string from various value types
func (s *ConfigService) parseStringValue(value any) string {
	if value == nil {
		return ""
	}

	// Handle types.JsonRaw
	if raw, ok := value.(types.JsonRaw); ok {
		var str string
		if err := json.Unmarshal(raw, &str); err != nil {
			return ""
		}
		return str
	}

	if str, ok := value.(string); ok {
		return str
	}
	return ""
}
