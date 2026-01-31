package config

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/daos"
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

// ValidateTokenAndGetUser validates an API token and returns the user ID
func (s *ConfigService) ValidateTokenAndGetUser(token string) (string, error) {
	logger.Debug("[ValidateTokenAndGetUser] input token=%s", token)

	// Find the record with matching token
	records, err := s.app.Dao().FindRecordsByFilter(
		"user_settings",
		"key = 'api.token'",
		"",
		-1,
		0,
	)

	if err != nil {
		logger.Debug("[ValidateTokenAndGetUser] query error: %v", err)
		return "", err
	}

	logger.Debug("[ValidateTokenAndGetUser] found %d api.token records", len(records))

	for _, record := range records {
		// Parse token directly from record value to avoid extra DB query
		storedToken := s.parseStringValue(record.Get("value"))
		userId := record.GetString("user")
		logger.Debug("[ValidateTokenAndGetUser] userId=%s, storedToken=%s", userId, storedToken)

		if storedToken == token {
			// Check if API is enabled for this user
			enabled, err := s.GetBool(userId, "api.enabled")
			if err != nil || !enabled {
				logger.Debug("[ValidateTokenAndGetUser] API not enabled")
				return "", err
			}
			return userId, nil
		}
	}

	logger.Debug("[ValidateTokenAndGetUser] no matching token found")
	return "", nil
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
