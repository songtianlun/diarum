package migrations

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/daos"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/models/schema"
)

func init() {
	m.Register(func(db dbx.Builder) error {
		dao := daos.New(db)

		// Find media collection
		mediaCollection, err := dao.FindCollectionByNameOrId("media")
		if err != nil {
			return err
		}

		// Update file field max size to 50MB
		for _, field := range mediaCollection.Schema.Fields() {
			if field.Name == "file" {
				if fileOptions, ok := field.Options.(*schema.FileOptions); ok {
					fileOptions.MaxSize = 52428800 // 50MB
				}
			}
		}

		return dao.SaveCollection(mediaCollection)
	}, func(db dbx.Builder) error {
		// Rollback: set back to 5MB
		dao := daos.New(db)

		mediaCollection, err := dao.FindCollectionByNameOrId("media")
		if err != nil {
			return err
		}

		for _, field := range mediaCollection.Schema.Fields() {
			if field.Name == "file" {
				if fileOptions, ok := field.Options.(*schema.FileOptions); ok {
					fileOptions.MaxSize = 5242880 // 5MB
				}
			}
		}

		return dao.SaveCollection(mediaCollection)
	})
}
