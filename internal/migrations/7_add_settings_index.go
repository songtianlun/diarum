package migrations

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/daos"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"
)

func init() {
	m.Register(func(db dbx.Builder) error {
		dao := daos.New(db)

		collection, err := dao.FindCollectionByNameOrId("user_settings")
		if err != nil {
			return err
		}

		// Add index for key to speed up token validation queries
		collection.Indexes = types.JsonArray[string]{
			"CREATE UNIQUE INDEX idx_user_settings_user_key ON user_settings (user, key)",
			"CREATE INDEX idx_user_settings_key ON user_settings (key)",
		}

		return dao.SaveCollection(collection)
	}, func(db dbx.Builder) error {
		dao := daos.New(db)

		collection, err := dao.FindCollectionByNameOrId("user_settings")
		if err != nil {
			return err
		}

		// Rollback: remove the key index
		collection.Indexes = types.JsonArray[string]{
			"CREATE UNIQUE INDEX idx_user_settings_user_key ON user_settings (user, key)",
		}

		return dao.SaveCollection(collection)
	})
}
