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

		// Find diaries collection
		diariesCollection, err := dao.FindCollectionByNameOrId("diaries")
		if err != nil {
			return err
		}

		// Update diary field to allow multiple selections
		diaryField := mediaCollection.Schema.GetFieldByName("diary")
		if diaryField != nil {
			if opts, ok := diaryField.Options.(*schema.RelationOptions); ok {
				opts.MaxSelect = nil // nil means unlimited
				opts.CollectionId = diariesCollection.Id
			}
		}

		return dao.SaveCollection(mediaCollection)
	}, func(db dbx.Builder) error {
		// Rollback: revert to single selection
		dao := daos.New(db)

		mediaCollection, err := dao.FindCollectionByNameOrId("media")
		if err != nil {
			return err
		}

		diaryField := mediaCollection.Schema.GetFieldByName("diary")
		if diaryField != nil {
			if opts, ok := diaryField.Options.(*schema.RelationOptions); ok {
				one := 1
				opts.MaxSelect = &one
			}
		}

		return dao.SaveCollection(mediaCollection)
	})
}
