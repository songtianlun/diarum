package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/auth"
	"github.com/songtianlun/diarum/internal/holidays"
	"github.com/songtianlun/diarum/internal/store"
)

func TestHolidayRoutes(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	authSvc := auth.NewService(s)
	user, err := s.CreateUser("alice", "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	token, err := authSvc.IssueToken(user)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/2026.json":
			_ = json.NewEncoder(w).Encode(holidays.YearData{
				Year: 2026,
				Days: []holidays.Day{{Name: "元旦", Date: "2026-01-01", IsOffDay: true}},
			})
		case "/2027.json":
			_ = json.NewEncoder(w).Encode(holidays.YearData{
				Year: 2027,
				Days: []holidays.Day{{Name: "元旦", Date: "2027-01-01", IsOffDay: true}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	svc := holidays.NewService(s, holidays.Options{
		Client:  upstream.Client(),
		Sources: []string{upstream.URL + "/%d.json"},
		Now:     func() time.Time { return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) },
	})

	e := echo.New()
	RegisterHolidayRoutes(e, svc, authSvc.Middleware)

	doReq := func(method, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	// Single year query
	rec := doReq(http.MethodGet, "/api/v1/holidays/cn?year=2026")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET single year status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var single holidays.YearResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &single); err != nil {
		t.Fatalf("decode single: %v", err)
	}
	if single.Year != 2026 || !single.Published || len(single.Days) == 0 {
		t.Fatalf("unexpected single response: %+v", single)
	}

	// Batch years query
	rec = doReq(http.MethodGet, "/api/v1/holidays/cn?years=2025,2026,2026")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET batch years status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var batch struct {
		Region string                            `json:"region"`
		Years  map[string]*holidays.YearResponse `json:"years"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &batch); err != nil {
		t.Fatalf("decode batch: %v", err)
	}
	if len(batch.Years) != 2 || !batch.Years["2025"].Published || !batch.Years["2026"].Published {
		t.Fatalf("unexpected batch response: %+v", batch)
	}

	// Invalid year rejected
	rec = doReq(http.MethodGet, "/api/v1/holidays/cn?year=1800")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid year, got %d", rec.Code)
	}
	rec = doReq(http.MethodGet, "/api/v1/holidays/cn?years=2025,abc")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid batch year, got %d", rec.Code)
	}

	// Too many batch years rejected
	rec = doReq(http.MethodGet, "/api/v1/holidays/cn?years=2001,2002,2003,2004,2005,2006,2007,2008,2009,2010,2011,2012,2013,2014,2015,2016,2017,2018,2019,2020,2021")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for >20 years, got %d", rec.Code)
	}

	// Default year (no query params) works
	rec = doReq(http.MethodGet, "/api/v1/holidays/cn")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for default year, got %d", rec.Code)
	}

	// Status endpoint
	rec = doReq(http.MethodGet, "/api/v1/holidays/cn/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Refresh endpoint
	rec = doReq(http.MethodPost, "/api/v1/holidays/cn/refresh")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST refresh = %d, body = %s", rec.Code, rec.Body.String())
	}
	var status holidays.StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode refresh status: %v", err)
	}
	if status.LastFetchedAt == "" {
		t.Fatalf("expected LastFetchedAt after refresh")
	}
}
