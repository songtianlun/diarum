package config

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/models"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/songtianlun/diaria/internal/logger"
)

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
	logger.Debug("[ConfigService.Get] Found value: %v (type: %T)", value, value)
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

// GetBatch retrieves multiple configuration values by prefix
func (s *ConfigService) GetBatch(userId string, prefix string) (map[string]any, error) {
	filter := "user = {:user}"
	params := map[string]any{"user": userId}

	if prefix != "" {
		// Use prefix with dot to ensure exact prefix matching (e.g., "ai." matches "ai.xxx" but not "ai_other")
		filter += " && key ~ {:prefix}"
		params["prefix"] = prefix + ".%"
	}

	records, err := s.app.Dao().FindRecordsByFilter(
		"user_settings",
		filter,
		"",
		-1,
		0,
		params,
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

// SetBatch stores multiple configuration values for a user
func (s *ConfigService) SetBatch(userId string, settings map[string]any) error {
	for key, value := range settings {
		if err := s.Set(userId, key, value); err != nil {
			return err
		}
	}
	return nil
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

// ValidateTokenAndGetUser validates an API token and returns the user ID
func (s *ConfigService) ValidateTokenAndGetUser(token string) (string, error) {
	// Find all api.token records
	records, err := s.app.Dao().FindRecordsByFilter(
		"user_settings",
		"key = 'api.token'",
		"",
		-1,
		0,
	)

	if err != nil {
		return "", err
	}

	// Compare token values properly
	for _, record := range records {
		userId := record.GetString("user")
		storedToken, _ := s.GetString(userId, "api.token")

		if storedToken == token {
			// Check if API is enabled for this user
			enabled, err := s.GetBool(userId, "api.enabled")
			if err != nil || !enabled {
				return "", err
			}
			return userId, nil
		}
	}

	return "", nil
}
