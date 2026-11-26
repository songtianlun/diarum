package store

import (
	"database/sql"
	"errors"
)

// HolidayYearRecord stores cached public holiday data for one region and year.
type HolidayYearRecord struct {
	Region       string
	Year         int
	Payload      string
	ETag         string
	Source       string
	NotPublished bool
	FetchedAt    string
	Updated      string
}

func migrateHolidaySchema(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS holiday_years (
		region TEXT NOT NULL,
		year INTEGER NOT NULL,
		payload TEXT DEFAULT '' NOT NULL,
		etag TEXT DEFAULT '' NOT NULL,
		source TEXT DEFAULT '' NOT NULL,
		not_published BOOLEAN DEFAULT FALSE NOT NULL,
		fetched_at TEXT DEFAULT '' NOT NULL,
		updated TEXT DEFAULT '' NOT NULL,
		PRIMARY KEY (region, year)
	)`)
	return err
}

// GetHolidayYear loads a cached holiday year record. Returns ErrNotFound when absent.
func (s *Store) GetHolidayYear(region string, year int) (*HolidayYearRecord, error) {
	var rec HolidayYearRecord
	err := s.DB.QueryRow(
		`SELECT region, year, payload, etag, source, not_published, fetched_at, updated
		 FROM holiday_years
		 WHERE region = ? AND year = ?`,
		region, year,
	).Scan(
		&rec.Region,
		&rec.Year,
		&rec.Payload,
		&rec.ETag,
		&rec.Source,
		&rec.NotPublished,
		&rec.FetchedAt,
		&rec.Updated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// UpsertHolidayYear saves or updates a cached holiday year record.
func (s *Store) UpsertHolidayYear(rec *HolidayYearRecord) error {
	now := nowString()
	updated := rec.Updated
	if updated == "" {
		updated = now
	}
	_, err := s.DB.Exec(
		`INSERT INTO holiday_years(region, year, payload, etag, source, not_published, fetched_at, updated)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(region, year) DO UPDATE SET
			payload = excluded.payload,
			etag = excluded.etag,
			source = excluded.source,
			not_published = excluded.not_published,
			fetched_at = excluded.fetched_at,
			updated = excluded.updated`,
		rec.Region,
		rec.Year,
		rec.Payload,
		rec.ETag,
		rec.Source,
		rec.NotPublished,
		rec.FetchedAt,
		updated,
	)
	return err
}

// ListHolidayYears returns all cached holiday records for a region ordered by year.
func (s *Store) ListHolidayYears(region string) ([]HolidayYearRecord, error) {
	rows, err := s.DB.Query(
		`SELECT region, year, payload, etag, source, not_published, fetched_at, updated
		 FROM holiday_years
		 WHERE region = ?
		 ORDER BY year ASC`,
		region,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []HolidayYearRecord
	for rows.Next() {
		var rec HolidayYearRecord
		if err := rows.Scan(
			&rec.Region,
			&rec.Year,
			&rec.Payload,
			&rec.ETag,
			&rec.Source,
			&rec.NotPublished,
			&rec.FetchedAt,
			&rec.Updated,
		); err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	return records, rows.Err()
}
