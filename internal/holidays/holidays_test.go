package holidays

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/songtianlun/diarum/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestEmbeddedBaselineLoaded(t *testing.T) {
	s := openTestStore(t)
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	svc := NewService(s, Options{
		Now:     func() time.Time { return now },
		Sources: []string{"http://127.0.0.1:1/%d.json"},
	})

	for _, y := range []int{2015, 2020, 2025, 2026, 2027} {
		res, err := svc.GetYear(context.Background(), y)
		if err != nil {
			t.Fatalf("GetYear(%d) err = %v", y, err)
		}
		if !res.Published || len(res.Days) == 0 || res.Year != y {
			t.Fatalf("GetYear(%d) unexpected: %+v", y, res)
		}
	}

	if _, err := svc.GetYear(context.Background(), 1999); !errors.Is(err, ErrInvalidYear) {
		t.Fatalf("GetYear(1999) err = %v, want ErrInvalidYear", err)
	}
	if _, err := svc.RefreshYear(context.Background(), 2200); !errors.Is(err, ErrInvalidYear) {
		t.Fatalf("RefreshYear(2200) err = %v, want ErrInvalidYear", err)
	}
}

func TestValidateYearData(t *testing.T) {
	if err := ValidateYearData(2026, nil); !errors.Is(err, ErrInvalidData) {
		t.Fatalf("nil err = %v", err)
	}
	if err := ValidateYearData(2026, &YearData{Year: 2025, Days: []Day{{Name: "元旦", Date: "2025-01-01"}}}); !errors.Is(err, ErrInvalidData) {
		t.Fatalf("year mismatch err = %v", err)
	}
	if err := ValidateYearData(2026, &YearData{Year: 2026, Days: nil}); !errors.Is(err, ErrInvalidData) {
		t.Fatalf("empty days err = %v", err)
	}
	if err := ValidateYearData(2026, &YearData{Year: 2026, Days: []Day{{Name: " ", Date: "2026-01-01"}}}); !errors.Is(err, ErrInvalidData) {
		t.Fatalf("empty name err = %v", err)
	}
	if err := ValidateYearData(2026, &YearData{Year: 2026, Days: []Day{{Name: "元旦", Date: "2025-01-01"}}}); !errors.Is(err, ErrInvalidData) {
		t.Fatalf("wrong prefix err = %v", err)
	}
	if err := ValidateYearData(2026, &YearData{Year: 2026, Days: []Day{{Name: "元旦", Date: "2026-02-30"}}}); !errors.Is(err, ErrInvalidData) {
		t.Fatalf("invalid calendar date err = %v", err)
	}
	if err := ValidateYearData(2026, &YearData{Year: 2026, Days: []Day{
		{Name: "元旦", Date: "2026-01-01", IsOffDay: true},
		{Name: "元旦", Date: "2026-01-01", IsOffDay: true},
	}}); !errors.Is(err, ErrInvalidData) {
		t.Fatalf("duplicate date err = %v", err)
	}
}

func TestRefreshFallbackETagAndNotPublished(t *testing.T) {
	s := openTestStore(t)
	now := time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)

	var hits2028 atomic.Int32
	badMirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/2028.json":
			http.Error(w, "mirror unavailable", http.StatusBadGateway)
		case "/2029.json":
			http.NotFound(w, r)
		case "/2026.json":
			// Return invalid mismatched year payload to ensure fallback happens.
			_, _ = w.Write([]byte(`{"year":2025,"days":[{"name":"元旦","date":"2025-01-01","isOffDay":true}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer badMirror.Close()

	goodMirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/2028.json":
			hits2028.Add(1)
			if r.Header.Get("If-None-Match") == `"etag-2028"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"etag-2028"`)
			_ = json.NewEncoder(w).Encode(YearData{
				Year:   2028,
				Papers: []string{"https://www.gov.cn/test-2028"},
				Days: []Day{
					{Name: "元旦", Date: "2028-01-01", IsOffDay: true},
					{Name: "春节", Date: "2028-01-23", IsOffDay: false},
				},
			})
		case "/2026.json":
			w.Header().Set("ETag", `"etag-2026"`)
			_ = json.NewEncoder(w).Encode(YearData{
				Year:   2026,
				Papers: []string{"https://www.gov.cn/test-2026"},
				Days: []Day{
					{Name: "元旦", Date: "2026-01-01", IsOffDay: true},
				},
			})
		case "/2029.json":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer goodMirror.Close()

	svc := NewService(s, Options{
		Client:  goodMirror.Client(),
		Sources: []string{badMirror.URL + "/%d.json", goodMirror.URL + "/%d.json"},
		Now:     func() time.Time { return now },
	})

	// 1. Fallback from failing mirror to good mirror
	res2028, err := svc.RefreshYear(context.Background(), 2028)
	if err != nil {
		t.Fatalf("RefreshYear(2028) err = %v", err)
	}
	if !res2028.Published || len(res2028.Days) != 2 || res2028.Source == "" {
		t.Fatalf("unexpected 2028 response: %+v", res2028)
	}

	// 2. Second refresh uses If-None-Match and handles 304 Not Modified
	now = now.Add(2 * time.Hour)
	res2028Again, err := svc.RefreshYear(context.Background(), 2028)
	if err != nil {
		t.Fatalf("RefreshYear(2028) 304 err = %v", err)
	}
	if !res2028Again.Published || len(res2028Again.Days) != 2 || hits2028.Load() != 2 {
		t.Fatalf("unexpected 304 response: %+v hits=%d", res2028Again, hits2028.Load())
	}

	// 3. Read from SQLite cache via GetYear
	cached2028, err := svc.GetYear(context.Background(), 2028)
	if err != nil || !cached2028.Published || len(cached2028.Days) != 2 {
		t.Fatalf("GetYear(2028) = %+v, err=%v", cached2028, err)
	}

	// 4. All mirrors return 404 -> marks not_published
	res2029, err := svc.RefreshYear(context.Background(), 2029)
	if err != nil {
		t.Fatalf("RefreshYear(2029) err = %v", err)
	}
	if res2029.Published || res2029.Source != "not_published" {
		t.Fatalf("unexpected 2029 response: %+v", res2029)
	}
	cached2029, err := svc.GetYear(context.Background(), 2029)
	if err != nil || cached2029.Published {
		t.Fatalf("GetYear(2029) = %+v, err=%v", cached2029, err)
	}

	// 5. Fallback when first mirror returns mismatched year JSON
	res2026, err := svc.RefreshYear(context.Background(), 2026)
	if err != nil || !res2026.Published || len(res2026.Days) != 1 {
		t.Fatalf("RefreshYear(2026) = %+v, err=%v", res2026, err)
	}

	// 6. Status includes embedded + DB cached years
	status, err := svc.Status()
	if err != nil {
		t.Fatalf("Status err = %v", err)
	}
	if status.LastFetchedAt == "" || len(status.Years) == 0 {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestRefreshPolicyYearsAndScheduler(t *testing.T) {
	s := openTestStore(t)
	now := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)

	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		switch r.URL.Path {
		case "/2026.json":
			_ = json.NewEncoder(w).Encode(YearData{
				Year: 2026,
				Days: []Day{{Name: "元旦", Date: "2026-01-01", IsOffDay: true}},
			})
		case "/2027.json":
			_ = json.NewEncoder(w).Encode(YearData{
				Year: 2027,
				Days: []Day{{Name: "元旦", Date: "2027-01-01", IsOffDay: true}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	svc := NewService(s, Options{
		Client:       srv.Client(),
		Sources:      []string{srv.URL + "/%d.json"},
		Now:          func() time.Time { return now },
		StartupDelay: 5 * time.Millisecond,
		TickInterval: 20 * time.Millisecond,
	})

	status, err := svc.RefreshPolicyYears(context.Background())
	if err != nil {
		t.Fatalf("RefreshPolicyYears err = %v", err)
	}
	if status.LastFetchedAt == "" {
		t.Fatalf("expected LastFetchedAt set")
	}

	ctx, cancel := context.WithCancel(context.Background())
	svc.StartScheduler(ctx)
	time.Sleep(40 * time.Millisecond)
	cancel()

	if count.Load() < 2 {
		t.Fatalf("expected at least 2 refreshes, got %d", count.Load())
	}

	// Verify TTL logic: Oct-Dec uses 24h for next year; historical never refreshes.
	rec := &store.HolidayYearRecord{
		Year:      2027,
		FetchedAt: now.Add(-25 * time.Hour).UTC().Format(time.RFC3339),
	}
	if !svc.needsRefresh(2027, rec, now) {
		t.Fatal("expected next year in Nov to refresh after 25h")
	}
	if svc.needsRefresh(2025, rec, now) {
		t.Fatal("expected historical year not to refresh")
	}
	spring := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	recSpring := &store.HolidayYearRecord{
		Year:      2027,
		FetchedAt: spring.Add(-25 * time.Hour).UTC().Format(time.RFC3339),
	}
	if svc.needsRefresh(2027, recSpring, spring) {
		t.Fatal("expected next year in March not to refresh after 25h")
	}

	// Cover error path on RefreshPolicyYears + non-embedded GetYear + retry helpers
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer failSrv.Close()
	failSvc := NewService(s, Options{
		Client:  failSrv.Client(),
		Sources: []string{failSrv.URL + "/%d.json"},
		Now:     func() time.Time { return now },
	})
	if _, err := failSvc.RefreshPolicyYears(context.Background()); err == nil {
		t.Fatal("expected RefreshPolicyYears error when all mirrors fail")
	}
	if res, err := failSvc.GetYear(context.Background(), 2028); err != nil || res.Published {
		t.Fatalf("expected unpublished response for missing non-embedded year, got %+v, err=%v", res, err)
	}
	if !failSvc.needsNotPublishedRetry(&store.HolidayYearRecord{FetchedAt: "bad-time"}, now) {
		t.Fatal("expected invalid FetchedAt to retry")
	}
	if !failSvc.needsRefresh(2026, &store.HolidayYearRecord{FetchedAt: "bad-time"}, now) {
		t.Fatal("expected invalid FetchedAt to need refresh")
	}
	if !failSvc.needsEmbeddedRefresh(2026, nil, now) {
		t.Fatal("expected nil record for current year embedded to need refresh")
	}
	if failSvc.needsEmbeddedRefresh(2020, nil, now) {
		t.Fatal("expected historical embedded year not to need refresh")
	}
}
