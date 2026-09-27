package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/store"
)

func upsertDiaryViaAPI(t *testing.T, e *echo.Echo, date, content, mood, weather string) map[string]any {
	t.Helper()
	body := fmt.Sprintf(`{"date":%q,"content":%q,"mood":%q,"weather":%q}`, date, content, mood, weather)
	rec := performRequest(t, e, http.MethodPost, "/api/v1/diaries/upsert", strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("upsert status = %d body=%s", rec.Code, rec.Body.String())
	}
	return decodeJSONBody(t, rec)
}

func historyViaAPI(t *testing.T, e *echo.Echo, date string) (map[string]any, []map[string]any) {
	t.Helper()
	rec := performRequest(t, e, http.MethodGet, "/api/v1/diaries/by-date/"+date+"/history", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("history status = %d body=%s", rec.Code, rec.Body.String())
	}
	payload := decodeJSONBody(t, rec)
	raw := payload["revisions"].([]any)
	revisions := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		revisions = append(revisions, item.(map[string]any))
	}
	return payload, revisions
}

func TestDiaryHistoryRoutes(t *testing.T) {
	s := newTestStore(t)
	s.SnapshotInterval = 0
	user := newTestUser(t, s)
	e := echo.New()
	changes := 0
	RegisterDiaryRoutes(e, s, authMiddlewareFor(user), func(string) { changes++ })

	payload, revisions := historyViaAPI(t, e, "2024-05-01")
	if len(revisions) != 0 || payload["limit"].(float64) != store.DefaultDiarySnapshots || payload["date"] != "2024-05-01" {
		t.Fatalf("empty history payload = %#v", payload)
	}

	upsertDiaryViaAPI(t, e, "2024-05-01", "<p>First draft 你好</p>", "😊", "☀️")
	upsertDiaryViaAPI(t, e, "2024-05-01", "<p>Second draft</p>", "😔", "")
	current := upsertDiaryViaAPI(t, e, "2024-05-01", "<p>Final</p>", "", "")

	// Reads keep returning only the latest version, with the same shape as before.
	rec := performRequest(t, e, http.MethodGet, "/api/v1/diaries/by-date/2024-05-01", nil, nil)
	byDate := decodeJSONBody(t, rec)
	if byDate["content"] != "<p>Final</p>" || byDate["id"] != current["id"] || byDate["exists"] != true {
		t.Fatalf("by-date payload = %#v", byDate)
	}

	_, revisions = historyViaAPI(t, e, "2024-05-01")
	if len(revisions) != 2 {
		t.Fatalf("revisions = %#v", revisions)
	}
	newest, oldest := revisions[0], revisions[1]
	if newest["preview"] != "Second draft" || newest["mood"] != "😔" || newest["words"].(float64) != 2 {
		t.Fatalf("newest revision = %#v", newest)
	}
	if oldest["preview"] != "First draft 你好" || oldest["weather"] != "☀️" || oldest["words"].(float64) != 4 {
		t.Fatalf("oldest revision = %#v", oldest)
	}
	for _, field := range []string{"id", "date", "saved", "created"} {
		if value, _ := oldest[field].(string); value == "" {
			t.Fatalf("revision missing %s: %#v", field, oldest)
		}
	}
	if _, hasContent := oldest["content"]; hasContent {
		t.Fatal("history list should not ship full content")
	}

	oldestID := oldest["id"].(string)
	rec = performRequest(t, e, http.MethodGet, "/api/v1/diaries/revisions/"+oldestID, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get revision status = %d body=%s", rec.Code, rec.Body.String())
	}
	full := decodeJSONBody(t, rec)
	if full["content"] != "<p>First draft 你好</p>" || full["mood"] != "😊" || full["date"] != "2024-05-01" || full["id"] != oldestID {
		t.Fatalf("full revision = %#v", full)
	}

	before := changes
	rec = performRequest(t, e, http.MethodPost, "/api/v1/diaries/revisions/"+oldestID+"/restore", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("restore status = %d body=%s", rec.Code, rec.Body.String())
	}
	restored := decodeJSONBody(t, rec)
	if restored["content"] != "<p>First draft 你好</p>" || restored["mood"] != "😊" || restored["weather"] != "☀️" || restored["id"] != current["id"] || restored["date"] != "2024-05-01" {
		t.Fatalf("restored = %#v", restored)
	}
	if changes != before+1 {
		t.Fatalf("restore should notify diary change (vector index etc.), changes=%d before=%d", changes, before)
	}

	// The replaced version is now the newest snapshot, so the restore can be undone.
	_, revisions = historyViaAPI(t, e, "2024-05-01")
	if len(revisions) != 3 || revisions[0]["preview"] != "Final" {
		t.Fatalf("history after restore = %#v", revisions)
	}
}

func TestDiaryHistoryRoutesAfterDeleteAndLimit(t *testing.T) {
	s := newTestStore(t)
	s.SnapshotInterval = 0
	user := newTestUser(t, s)
	e := echo.New()
	RegisterDiaryRoutes(e, s, authMiddlewareFor(user), nil)

	created := upsertDiaryViaAPI(t, e, "2024-05-02", "<p>Gone soon</p>", "", "")
	rec := performRequest(t, e, http.MethodDelete, "/api/v1/diaries/"+created["id"].(string), nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/diaries/by-date/2024-05-02", nil, nil)
	if decodeJSONBody(t, rec)["exists"] != false {
		t.Fatal("deleted entry should not exist")
	}

	_, revisions := historyViaAPI(t, e, "2024-05-02")
	if len(revisions) != 1 || revisions[0]["preview"] != "Gone soon" {
		t.Fatalf("history after delete = %#v", revisions)
	}
	rec = performRequest(t, e, http.MethodPost, "/api/v1/diaries/revisions/"+revisions[0]["id"].(string)+"/restore", nil, nil)
	if rec.Code != http.StatusOK || decodeJSONBody(t, rec)["content"] != "<p>Gone soon</p>" {
		t.Fatalf("restore deleted status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/diaries/by-date/2024-05-02", nil, nil)
	if decodeJSONBody(t, rec)["content"] != "<p>Gone soon</p>" {
		t.Fatal("restore should bring the deleted entry back")
	}

	if err := s.SetSetting(user.ID, store.SettingDiaryMaxSnapshots, 2, false); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	for i := 0; i < 5; i++ {
		upsertDiaryViaAPI(t, e, "2024-05-02", fmt.Sprintf("<p>v%d</p>", i), "", "")
	}
	payload, revisions := historyViaAPI(t, e, "2024-05-02")
	if payload["limit"].(float64) != 2 || len(revisions) != 2 || revisions[0]["preview"] != "v3" || revisions[1]["preview"] != "v2" {
		t.Fatalf("limited history = %#v", payload)
	}
}

func TestDiaryHistoryRoutesRejectBadInputAndOtherUsers(t *testing.T) {
	s := newTestStore(t)
	s.SnapshotInterval = 0
	owner := newTestUser(t, s)
	intruder := newTestUser(t, s)

	ownerAPI := echo.New()
	RegisterDiaryRoutes(ownerAPI, s, authMiddlewareFor(owner), nil)
	intruderAPI := echo.New()
	RegisterDiaryRoutes(intruderAPI, s, authMiddlewareFor(intruder), nil)

	upsertDiaryViaAPI(t, ownerAPI, "2024-05-03", "secret v1", "", "")
	upsertDiaryViaAPI(t, ownerAPI, "2024-05-03", "secret v2", "", "")
	_, revisions := historyViaAPI(t, ownerAPI, "2024-05-03")
	revisionID := revisions[0]["id"].(string)

	if _, others := historyViaAPI(t, intruderAPI, "2024-05-03"); len(others) != 0 {
		t.Fatalf("other users must not see history, got %#v", others)
	}
	for _, request := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/diaries/revisions/" + revisionID},
		{http.MethodPost, "/api/v1/diaries/revisions/" + revisionID + "/restore"},
	} {
		rec := performRequest(t, intruderAPI, request.method, request.path, nil, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("intruder %s %s status = %d, want 404", request.method, request.path, rec.Code)
		}
	}
	rec := performRequest(t, ownerAPI, http.MethodGet, "/api/v1/diaries/by-date/2024-05-03", nil, nil)
	if decodeJSONBody(t, rec)["content"] != "secret v2" {
		t.Fatal("a rejected restore must leave the entry untouched")
	}

	for _, request := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/api/v1/diaries/by-date/not-a-date/history", http.StatusBadRequest},
		{http.MethodGet, "/api/v1/diaries/by-date/2024-13-40/history", http.StatusBadRequest},
		{http.MethodGet, "/api/v1/diaries/revisions/missing", http.StatusNotFound},
		{http.MethodPost, "/api/v1/diaries/revisions/missing/restore", http.StatusNotFound},
	} {
		rec := performRequest(t, ownerAPI, request.method, request.path, nil, nil)
		if rec.Code != request.want {
			t.Fatalf("%s %s status = %d, want %d body=%s", request.method, request.path, rec.Code, request.want, rec.Body.String())
		}
	}
}

func TestDiaryHistoryRoutesServerErrors(t *testing.T) {
	s := newTestStore(t)
	s.SnapshotInterval = 0
	user := newTestUser(t, s)
	e := echo.New()
	RegisterDiaryRoutes(e, s, authMiddlewareFor(user), nil)

	upsertDiaryViaAPI(t, e, "2024-05-04", "v1", "", "")
	upsertDiaryViaAPI(t, e, "2024-05-04", "v2", "", "")
	_, revisions := historyViaAPI(t, e, "2024-05-04")
	revisionID := revisions[0]["id"].(string)

	// Restore fails (without a not-found) when the diaries table is unusable.
	if _, err := s.DB.Exec(`ALTER TABLE diaries RENAME TO diaries_broken`); err != nil {
		t.Fatalf("rename diaries: %v", err)
	}
	rec := performRequest(t, e, http.MethodPost, "/api/v1/diaries/revisions/"+revisionID+"/restore", nil, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("restore status = %d, want 500", rec.Code)
	}
	if _, err := s.DB.Exec(`ALTER TABLE diaries_broken RENAME TO diaries`); err != nil {
		t.Fatalf("rename back: %v", err)
	}

	if _, err := s.DB.Exec(`DROP TABLE diary_revisions`); err != nil {
		t.Fatalf("drop revisions: %v", err)
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/diaries/by-date/2024-05-04/history", nil, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("history status = %d, want 500", rec.Code)
	}
}

func TestSettingsRoutesValidateSnapshotLimit(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	RegisterSettingsRoutes(e, s, authMiddlewareFor(user))
	jsonHeaders := map[string]string{"Content-Type": "application/json"}

	rec := performRequest(t, e, http.MethodGet, "/api/v1/settings/"+store.SettingDiaryMaxSnapshots, nil, nil)
	if rec.Code != http.StatusOK || decodeJSONBody(t, rec)["value"].(float64) != store.DefaultDiarySnapshots {
		t.Fatalf("default setting status = %d body=%s", rec.Code, rec.Body.String())
	}
	for _, body := range []string{`{"value":0}`, `{"value":21}`, `{"value":"12"}`, `{"value":2.5}`} {
		rec = performRequest(t, e, http.MethodPut, "/api/v1/settings/"+store.SettingDiaryMaxSnapshots, strings.NewReader(body), jsonHeaders)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("PUT %s status = %d, want 400", body, rec.Code)
		}
	}
	rec = performRequest(t, e, http.MethodPut, "/api/v1/settings/batch", strings.NewReader(`{"settings":{"diary.max_snapshots":99}}`), jsonHeaders)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("batch out-of-range status = %d, want 400", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPut, "/api/v1/settings/batch", strings.NewReader(`{"settings":{"diary.max_snapshots":20}}`), jsonHeaders)
	if rec.Code != http.StatusOK || s.DiarySnapshotLimit(user.ID) != 20 {
		t.Fatalf("batch valid status = %d limit=%d", rec.Code, s.DiarySnapshotLimit(user.ID))
	}
}
