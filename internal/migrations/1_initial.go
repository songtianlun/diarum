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

		// Create diaries collection
		diariesCollection := &models.Collection{
			Name:       "diaries",
			Type:       models.CollectionTypeBase,
			ListRule:   types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			ViewRule:   types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			CreateRule: types.Pointer("@request.auth.id != \"\""),
			UpdateRule: types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			DeleteRule: types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			Schema: schema.NewSchema(
				&schema.SchemaField{
					Name:     "date",
					Type:     schema.FieldTypeDate,
					Required: true,
					Options: &schema.DateOptions{
						Min: types.DateTime{},
						Max: types.DateTime{},
					},
				},
				&schema.SchemaField{
					Name:     "content",
					Type:     schema.FieldTypeEditor,
					Required: false,
					Options:  &schema.EditorOptions{},
				},
				&schema.SchemaField{
					Name:     "mood",
					Type:     schema.FieldTypeText,
					Required: false,
					Options: &schema.TextOptions{
						Min: nil,
						Max: types.Pointer(50),
					},
				},
				&schema.SchemaField{
					Name:     "weather",
					Type:     schema.FieldTypeText,
					Required: false,
					Options: &schema.TextOptions{
						Min: nil,
						Max: types.Pointer(50),
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

		// Add unique index for (date, owner)
		diariesCollection.Indexes = types.JsonArray[string]{
			"CREATE UNIQUE INDEX idx_diaries_date_owner ON diaries (date, owner)",
		}

		if err := dao.SaveCollection(diariesCollection); err != nil {
			return err
		}

		// Create tags collection (for V2)
		tagsCollection := &models.Collection{
			Name:       "tags",
			Type:       models.CollectionTypeBase,
			ListRule:   types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			ViewRule:   types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			CreateRule: types.Pointer("@request.auth.id != \"\""),
			UpdateRule: types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			DeleteRule: types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			Schema: schema.NewSchema(
				&schema.SchemaField{
					Name:     "name",
					Type:     schema.FieldTypeText,
					Required: true,
					Options: &schema.TextOptions{
						Min: types.Pointer(1),
						Max: types.Pointer(50),
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

		// Add unique index for (name, owner)
		tagsCollection.Indexes = types.JsonArray[string]{
			"CREATE UNIQUE INDEX idx_tags_name_owner ON tags (name, owner)",
		}

		if err := dao.SaveCollection(tagsCollection); err != nil {
			return err
		}

		// Add tags relation to diaries
		diariesCollection.Schema.AddField(&schema.SchemaField{
			Name:     "tags",
			Type:     schema.FieldTypeRelation,
			Required: false,
			Options: &schema.RelationOptions{
				CollectionId:  tagsCollection.Id,
				CascadeDelete: false,
				MinSelect:     nil,
				MaxSelect:     nil, // Multiple tags allowed
			},
		})

		if err := dao.SaveCollection(diariesCollection); err != nil {
			return err
		}

		return nil
	}, func(db dbx.Builder) error {
		// Rollback: drop collections
		dao := daos.New(db)

		diariesCollection, err := dao.FindCollectionByNameOrId("diaries")
		if err == nil {
			if err := dao.DeleteCollection(diariesCollection); err != nil {
				return err
			}
		}

		tagsCollection, err := dao.FindCollectionByNameOrId("tags")
		if err == nil {
			if err := dao.DeleteCollection(tagsCollection); err != nil {
				return err
			}
		}

		return nil
	})
}
