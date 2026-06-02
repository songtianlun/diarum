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

		// Create api_tokens collection
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

		// Add unique index for owner (one token per user)
		apiTokensCollection.Indexes = types.JsonArray[string]{
			"CREATE UNIQUE INDEX idx_api_tokens_owner ON api_tokens (owner)",
			"CREATE UNIQUE INDEX idx_api_tokens_token ON api_tokens (token)",
		}

		if err := dao.SaveCollection(apiTokensCollection); err != nil {
			return err
		}

		return nil
	}, func(db dbx.Builder) error {
		// Rollback: drop api_tokens collection
		dao := daos.New(db)

		apiTokensCollection, err := dao.FindCollectionByNameOrId("api_tokens")
		if err == nil {
			if err := dao.DeleteCollection(apiTokensCollection); err != nil {
				return err
			}
		}

		return nil
	})
}
