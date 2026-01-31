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

		// Create ai_conversations collection
		conversationsCollection := &models.Collection{
			Name:       "ai_conversations",
			Type:       models.CollectionTypeBase,
			ListRule:   types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			ViewRule:   types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			CreateRule: types.Pointer("@request.auth.id != \"\""),
			UpdateRule: types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			DeleteRule: types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			Schema: schema.NewSchema(
				&schema.SchemaField{
					Name:     "title",
					Type:     schema.FieldTypeText,
					Required: false,
					Options: &schema.TextOptions{
						Min: nil,
						Max: types.Pointer(200),
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

		conversationsCollection.Indexes = types.JsonArray[string]{
			"CREATE INDEX idx_ai_conversations_owner ON ai_conversations (owner)",
			"CREATE INDEX idx_ai_conversations_updated ON ai_conversations (updated)",
		}

		if err := dao.SaveCollection(conversationsCollection); err != nil {
			return err
		}

		// Create ai_messages collection
		messagesCollection := &models.Collection{
			Name:       "ai_messages",
			Type:       models.CollectionTypeBase,
			ListRule:   types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			ViewRule:   types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			CreateRule: types.Pointer("@request.auth.id != \"\""),
			UpdateRule: types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			DeleteRule: types.Pointer("@request.auth.id != \"\" && owner = @request.auth.id"),
			Schema: schema.NewSchema(
				&schema.SchemaField{
					Name:     "conversation",
					Type:     schema.FieldTypeRelation,
					Required: true,
					Options: &schema.RelationOptions{
						CollectionId:  "", // Will be set after conversations collection is created
						CascadeDelete: true,
						MinSelect:     nil,
						MaxSelect:     types.Pointer(1),
					},
				},
				&schema.SchemaField{
					Name:     "role",
					Type:     schema.FieldTypeSelect,
					Required: true,
					Options: &schema.SelectOptions{
						MaxSelect: 1,
						Values:    []string{"user", "assistant"},
					},
				},
				&schema.SchemaField{
					Name:     "content",
					Type:     schema.FieldTypeText,
					Required: true,
					Options:  &schema.TextOptions{},
				},
				&schema.SchemaField{
					Name:     "referenced_diaries",
					Type:     schema.FieldTypeJson,
					Required: false,
					Options:  &schema.JsonOptions{},
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

		// Set the conversation relation to point to ai_conversations
		convCollection, err := dao.FindCollectionByNameOrId("ai_conversations")
		if err != nil {
			return err
		}
		messagesCollection.Schema.GetFieldByName("conversation").Options.(*schema.RelationOptions).CollectionId = convCollection.Id

		messagesCollection.Indexes = types.JsonArray[string]{
			"CREATE INDEX idx_ai_messages_conversation ON ai_messages (conversation)",
			"CREATE INDEX idx_ai_messages_owner ON ai_messages (owner)",
		}

		if err := dao.SaveCollection(messagesCollection); err != nil {
			return err
		}

		return nil
	}, func(db dbx.Builder) error {
		dao := daos.New(db)

		// Delete ai_messages collection first (due to foreign key)
		messagesCollection, err := dao.FindCollectionByNameOrId("ai_messages")
		if err == nil {
			if err := dao.DeleteCollection(messagesCollection); err != nil {
				return err
			}
		}

		// Delete ai_conversations collection
		conversationsCollection, err := dao.FindCollectionByNameOrId("ai_conversations")
		if err == nil {
			if err := dao.DeleteCollection(conversationsCollection); err != nil {
				return err
			}
		}

		return nil
	})
}
