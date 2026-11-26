package store

import (
	"errors"
	"testing"
)

func TestHolidayYearCRUD(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	if _, err := s.GetHolidayYear("cn", 2026); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetHolidayYear missing err = %v, want ErrNotFound", err)
	}

	rec := &HolidayYearRecord{
		Region:       "cn",
		Year:         2026,
		Payload:      `{"year":2026,"days":[]}`,
		ETag:         `"v1"`,
		Source:       "https://example.test/2026.json",
		NotPublished: false,
		FetchedAt:    "2026-01-01T00:00:00Z",
	}
	if err := s.UpsertHolidayYear(rec); err != nil {
		t.Fatalf("UpsertHolidayYear insert: %v", err)
	}

	got, err := s.GetHolidayYear("cn", 2026)
	if err != nil {
		t.Fatalf("GetHolidayYear: %v", err)
	}
	if got.ETag != `"v1"` || got.Payload != rec.Payload || got.NotPublished {
		t.Fatalf("unexpected record: %+v", got)
	}

	rec.ETag = `"v2"`
	rec.FetchedAt = "2026-02-01T00:00:00Z"
	if err := s.UpsertHolidayYear(rec); err != nil {
		t.Fatalf("UpsertHolidayYear update: %v", err)
	}
	if err := s.UpsertHolidayYear(&HolidayYearRecord{
		Region:       "cn",
		Year:         2025,
		Payload:      `{"year":2025,"days":[]}`,
		Source:       "https://example.test/2025.json",
		FetchedAt:    "2025-11-01T00:00:00Z",
		NotPublished: false,
	}); err != nil {
		t.Fatalf("UpsertHolidayYear 2025: %v", err)
	}

	list, err := s.ListHolidayYears("cn")
	if err != nil {
		t.Fatalf("ListHolidayYears: %v", err)
	}
	if len(list) != 2 || list[0].Year != 2025 || list[1].Year != 2026 || list[1].ETag != `"v2"` {
		t.Fatalf("unexpected list: %+v", list)
	}
}
