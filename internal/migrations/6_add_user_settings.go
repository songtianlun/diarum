package migrations

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/daos"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/models"
	"github.com/pocketbase/pocketbase/models/schema"
	"github.com/pocketbase/pocketbase/tools/types"
)

func init() {
	m.Register(func(db dbx.Builder) error {
		dao := daos.New(db)

		// Create user_settings collection
		userSettingsCollection := &models.Collection{
			Name:       "user_settings",
			Type:       models.CollectionTypeBase,
			ListRule:   types.Pointer("@request.auth.id != \"\" && user = @request.auth.id"),
			ViewRule:   types.Pointer("@request.auth.id != \"\" && user = @request.auth.id"),
			CreateRule: types.Pointer("@request.auth.id != \"\""),
			UpdateRule: types.Pointer("@request.auth.id != \"\" && user = @request.auth.id"),
			DeleteRule: types.Pointer("@request.auth.id != \"\" && user = @request.auth.id"),
			Schema: schema.NewSchema(
				&schema.SchemaField{
					Name:     "user",
					Type:     schema.FieldTypeRelation,
					Required: true,
					Options: &schema.RelationOptions{
						CollectionId:  "_pb_users_auth_",
						CascadeDelete: true,
						MinSelect:     nil,
						MaxSelect:     types.Pointer(1),
					},
				},
				&schema.SchemaField{
					Name:     "key",
					Type:     schema.FieldTypeText,
					Required: true,
					Options: &schema.TextOptions{
						Min: types.Pointer(1),
						Max: types.Pointer(255),
					},
				},
				&schema.SchemaField{
					Name:     "value",
					Type:     schema.FieldTypeJson,
					Required: false,
					Options:  &schema.JsonOptions{},
				},
				&schema.SchemaField{
					Name:     "encrypted",
					Type:     schema.FieldTypeBool,
					Required: false,
					Options:  &schema.BoolOptions{},
				},
			),
		}

		// Add unique index for (user, key)
		userSettingsCollection.Indexes = types.JsonArray[string]{
			"CREATE UNIQUE INDEX idx_user_settings_user_key ON user_settings (user, key)",
		}

		if err := dao.SaveCollection(userSettingsCollection); err != nil {
			return err
		}

		// Migrate data from api_tokens to user_settings
		apiTokensCollection, err := dao.FindCollectionByNameOrId("api_tokens")
		if err == nil {
			// Find all api_tokens records
			records, err := dao.FindRecordsByFilter("api_tokens", "1=1", "", -1, 0)
			if err == nil {
				for _, record := range records {
					userId := record.GetString("owner")
					token := record.GetString("token")
					enabled := record.GetBool("enabled")

					// Create api.token setting
					tokenSetting := models.NewRecord(userSettingsCollection)
					tokenSetting.Set("user", userId)
					tokenSetting.Set("key", "api.token")
					tokenSetting.Set("value", token)
					tokenSetting.Set("encrypted", false)
					if err := dao.SaveRecord(tokenSetting); err != nil {
						return err
					}

					// Create api.enabled setting
					enabledSetting := models.NewRecord(userSettingsCollection)
					enabledSetting.Set("user", userId)
					enabledSetting.Set("key", "api.enabled")
					enabledSetting.Set("value", enabled)
					enabledSetting.Set("encrypted", false)
					if err := dao.SaveRecord(enabledSetting); err != nil {
						return err
					}
				}
			}

			// Delete api_tokens collection
			if err := dao.DeleteCollection(apiTokensCollection); err != nil {
				return err
			}
		}

		return nil
	}, func(db dbx.Builder) error {
		// Rollback: recreate api_tokens and migrate data back
		dao := daos.New(db)

		// Recreate api_tokens collection
		apiTokensCollection := &models.Collection{
			Name:       "api_tokens",
			Type:       models.CollectionTypeBase,
			ListRule:   types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			ViewRule:   types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			CreateRule: types.Pointer("@request.auth.id != \"\""),
			UpdateRule: types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			DeleteRule: types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			Schema: schema.NewSchema(
				&schema.SchemaField{
					Name:     "token",
					Type:     schema.FieldTypeText,
					Required: true,
					Options: &schema.TextOptions{
						Min: types.Pointer(32),
						Max: types.Pointer(64),
					},
				},
				&schema.SchemaField{
					Name:     "enabled",
					Type:     schema.FieldTypeBool,
					Required: false,
					Options:  &schema.BoolOptions{},
				},
				&schema.SchemaField{
					Name:     "owner",
					Type:     schema.FieldTypeRelation,
					Required: true,
					Options: &schema.RelationOptions{
						CollectionId:  "_pb_users_auth_",
						CascadeDelete: true,
						MinSelect:     nil,
						MaxSelect:     types.Pointer(1),
					},
				},
			),
		}

		apiTokensCollection.Indexes = types.JsonArray[string]{
			"CREATE UNIQUE INDEX idx_api_tokens_owner ON api_tokens (owner)",
			"CREATE UNIQUE INDEX idx_api_tokens_token ON api_tokens (token)",
		}

		if err := dao.SaveCollection(apiTokensCollection); err != nil {
			return err
		}

		// Migrate data back from user_settings
		userSettingsCollection, err := dao.FindCollectionByNameOrId("user_settings")
		if err == nil {
			// Get all api.token settings
			tokenRecords, _ := dao.FindRecordsByFilter("user_settings", "key = 'api.token'", "", -1, 0)
			for _, tokenRecord := range tokenRecords {
				userId := tokenRecord.GetString("user")
				token := tokenRecord.Get("value")

				// Find corresponding enabled setting
				enabledRecord, _ := dao.FindFirstRecordByFilter("user_settings",
					"user = {:user} && key = 'api.enabled'",
					map[string]any{"user": userId})

				enabled := false
				if enabledRecord != nil {
					if v, ok := enabledRecord.Get("value").(bool); ok {
						enabled = v
					}
				}

				// Create api_tokens record
				apiTokenRecord := models.NewRecord(apiTokensCollection)
				apiTokenRecord.Set("owner", userId)
				apiTokenRecord.Set("token", token)
				apiTokenRecord.Set("enabled", enabled)
				dao.SaveRecord(apiTokenRecord)
			}

			// Delete user_settings collection
			dao.DeleteCollection(userSettingsCollection)
		}

		return nil
	})
}
