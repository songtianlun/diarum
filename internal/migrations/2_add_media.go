package migrations

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/daos"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/models"
	"github.com/pocketbase/pocketbase/models/schema"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/songtianlun/diarum/internal/config"
)

func init() {
	m.Register(func(db dbx.Builder) error {
		dao := daos.New(db)

		// Create media collection
		mediaCollection := &models.Collection{
			Name:       "media",
			Type:       models.CollectionTypeBase,
			ListRule:   types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			ViewRule:   types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			CreateRule: types.Pointer("@request.auth.id != \"\""),
			UpdateRule: types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			DeleteRule: types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			Schema: schema.NewSchema(
				&schema.SchemaField{
					Name:     "file",
					Type:     schema.FieldTypeFile,
					Required: true,
					Options: &schema.FileOptions{
						MaxSelect: 1,
						MaxSize:   5242880, // 5MB
						MimeTypes: config.AllowedMediaMimeTypes,
						Thumbs: []string{
							"100x100",
							"300x300",
							"800x600",
						},
					},
				},
				&schema.SchemaField{
					Name:     "name",
					Type:     schema.FieldTypeText,
					Required: false,
					Options: &schema.TextOptions{
						Min: nil,
						Max: types.Pointer(255),
					},
				},
				&schema.SchemaField{
					Name:     "alt",
					Type:     schema.FieldTypeText,
					Required: false,
					Options: &schema.TextOptions{
						Min: nil,
						Max: types.Pointer(500),
					},
				},
				&schema.SchemaField{
					Name:     "diary",
					Type:     schema.FieldTypeRelation,
					Required: false,
					Options: &schema.RelationOptions{
						CollectionId:  "", // Will be set after finding diaries collection
						CascadeDelete: true,
						MinSelect:     nil,
						MaxSelect:     types.Pointer(1),
					},
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

		// Find diaries collection to set up relation
		diariesCollection, err := dao.FindCollectionByNameOrId("diaries")
		if err != nil {
			return err
		}

		// Set diary relation collection ID
		for _, field := range mediaCollection.Schema.Fields() {
			if field.Name == "diary" {
				if relationOptions, ok := field.Options.(*schema.RelationOptions); ok {
					relationOptions.CollectionId = diariesCollection.Id
				}
			}
		}

		if err := dao.SaveCollection(mediaCollection); err != nil {
			return err
		}

		return nil
	}, func(db dbx.Builder) error {
		// Rollback: drop media collection
		dao := daos.New(db)

		mediaCollection, err := dao.FindCollectionByNameOrId("media")
		if err == nil {
			if err := dao.DeleteCollection(mediaCollection); err != nil {
				return err
			}
		}

		return nil
	})
}
