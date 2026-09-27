package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/audit"
)

func TestAuditTrailRecordsDiaryActions(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	auditLog, err := audit.New(s.DataDir, audit.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(auditLog.Close)

	e := echo.New()
	e.Use(AuditMiddleware(auditLog))
	auth := authMiddlewareFor(user)
	RegisterDiaryRoutes(e, s, auth, nil)
	RegisterAuditRoutes(e, s, auth, auditLog)

	created := upsertDiaryViaAPI(t, e, "2024-05-01", "<p>hello world</p>", "😊", "")
	upsertDiaryViaAPI(t, e, "2024-05-01", "<p>hello world</p>", "😊", "") // no change: not logged
	upsertDiaryViaAPI(t, e, "2024-05-01", "<p>hello there big world</p>", "😊", "")
	performRequest(t, e, http.MethodGet, "/api/v1/diaries/by-date/2024-05-01", nil, nil)
	rec := performRequest(t, e, http.MethodDelete, "/api/v1/diaries/"+created["id"].(string), nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d", rec.Code)
	}

	rec = performRequest(t, e, http.MethodGet, "/api/v1/audit/entries", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("entries status = %d body=%s", rec.Code, rec.Body.String())
	}
	entries := decodeJSONBody(t, rec)["entries"].([]any)
	var actions []string
	for _, raw := range entries {
		entry := raw.(map[string]any)
		actions = append(actions, entry["action"].(string))
		if entry["target"] != "2024-05-01" || entry["user"] != user.ID {
			t.Fatalf("unexpected entry %#v", entry)
		}
		if strings.Contains(rec.Body.String(), "hello") {
			t.Fatal("diary content must never reach the audit trail")
		}
	}
	want := "diary.delete,diary.view,diary.update,diary.create"
	if strings.Join(actions, ",") != want {
		t.Fatalf("actions = %v, want %s", actions, want)
	}
	update := entries[2].(map[string]any)["detail"].(map[string]any)
	if update["words_before"].(float64) != 2 || update["words"].(float64) != 4 {
		t.Fatalf("update detail = %#v", update)
	}

	rec = performRequest(t, e, http.MethodGet, "/api/v1/audit/entries?q="+url.QueryEscape("删除"), nil, nil)
	if got := decodeJSONBody(t, rec)["entries"].([]any); len(got) != 1 {
		t.Fatalf("search returned %d entries", len(got))
	}

	rec = performRequest(t, e, http.MethodGet, "/api/v1/audit/files", nil, nil)
	files := decodeJSONBody(t, rec)["files"].([]any)
	if len(files) != 1 || files[0].(map[string]any)["entries"].(float64) != 4 {
		t.Fatalf("files = %#v", files)
	}
	date := files[0].(map[string]any)["date"].(string)
	rec = performRequest(t, e, http.MethodGet, "/api/v1/audit/files/"+date, nil, nil)
	if rec.Code != http.StatusOK || strings.Count(rec.Body.String(), "\n") != 4 {
		t.Fatalf("download status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := performRequest(t, e, http.MethodGet, "/api/v1/audit/files/..%2F..%2Fdiarum", nil, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad date should be rejected, got %d", rec.Code)
	}
}

func TestAuditSettingsRoutes(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	auditLog, err := audit.New(s.DataDir, audit.Options{Retention: func(userID string) int {
		value, _ := s.GetSetting(userID, audit.SettingRetentionDays)
		if number, ok := value.(float64); ok {
			return int(number)
		}
		return audit.DefaultRetentionDays
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(auditLog.Close)
	e := echo.New()
	e.Use(AuditMiddleware(auditLog))
	RegisterAuditRoutes(e, s, authMiddlewareFor(user), auditLog)

	rec := performRequest(t, e, http.MethodGet, "/api/v1/audit/settings", nil, nil)
	if got := decodeJSONBody(t, rec); got["retention_days"].(float64) != 7 {
		t.Fatalf("default settings = %#v", got)
	}
	jsonHeader := map[string]string{"Content-Type": "application/json"}
	for _, body := range []string{`{"retention_days":0}`, `{"retention_days":366}`, `{}`} {
		if rec := performRequest(t, e, http.MethodPut, "/api/v1/audit/settings", strings.NewReader(body), jsonHeader); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s should be rejected, got %d", body, rec.Code)
		}
	}
	rec = performRequest(t, e, http.MethodPut, "/api/v1/audit/settings", strings.NewReader(`{"retention_days":30}`), jsonHeader)
	if got := decodeJSONBody(t, rec); rec.Code != http.StatusOK || got["retention_days"].(float64) != 30 {
		t.Fatalf("update = %d %#v", rec.Code, got)
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/audit/entries?action=settings", nil, nil)
	if got := decodeJSONBody(t, rec)["entries"].([]any); len(got) != 1 {
		t.Fatalf("retention change should be audited, got %#v", got)
	}
}

func TestRoutesWorkWithoutAuditLogger(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	RegisterDiaryRoutes(e, s, authMiddlewareFor(user), nil)
	RegisterAuditRoutes(e, s, authMiddlewareFor(user), nil)
	upsertDiaryViaAPI(t, e, "2024-05-01", "<p>x</p>", "", "")
	rec := performRequest(t, e, http.MethodGet, "/api/v1/audit/entries", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("entries without logger = %d", rec.Code)
	}
}

func TestMCPReadsAreAudited(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	if _, _, err := s.UpsertDiary(user.ID, "2026-01-02", "<p>secret words</p>", "", ""); err != nil {
		t.Fatal(err)
	}
	type call struct{ action, target string }
	var calls []call
	record := func(action, target string, detail map[string]any) {
		calls = append(calls, call{action, target})
		if strings.Contains(fmt.Sprint(detail), "secret") {
			t.Fatalf("content leaked into audit detail: %v", detail)
		}
	}
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_diary","arguments":{"date":"2026-01-02"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_diaries","arguments":{"start":"2026-01-01","end":"2026-01-31"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search_diaries","arguments":{"query":"words"}}}`,
	} {
		var req jsonRPCRequest
		if err := json.Unmarshal([]byte(body), &req); err != nil {
			t.Fatal(err)
		}
		if resp := dispatchMCPAudited(s, user.ID, "v", req, record); resp == nil || resp.Error != nil {
			t.Fatalf("unexpected response %#v", resp)
		}
	}
	want := []call{{audit.ActionDiaryView, "2026-01-02"}, {audit.ActionDiaryView, "2026-01-01..2026-01-31"}, {audit.ActionDiarySearch, ""}}
	if fmt.Sprint(calls) != fmt.Sprint(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestSettingAuditDetailHidesSecrets(t *testing.T) {
	long := strings.Repeat("x", 65)
	cases := []struct {
		key   string
		value any
		want  any
	}{
		{"ai.api_key", "sk-secret", nil},
		{"api.token", "abc", nil},
		{"general.language", "zh", "zh"},
		{"api.enabled", true, true},
		{"diary.max_snapshots", float64(5), float64(5)},
		{"general.homepage", long, nil},
		{"diary.mood_options", []any{"😊"}, nil},
	}
	for _, tc := range cases {
		got := settingAuditDetail(tc.key, tc.value)
		var value any
		if got != nil {
			value = got["value"]
		}
		if value != tc.want {
			t.Fatalf("%s: detail = %#v, want value %#v", tc.key, got, tc.want)
		}
	}
}

func TestAuditEntriesRejectsBadParams(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	auditLog, err := audit.New(s.DataDir, audit.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(auditLog.Close)
	e := echo.New()
	RegisterAuditRoutes(e, s, authMiddlewareFor(user), auditLog)
	for _, query := range []string{"limit=0", "limit=x", "start=yesterday", "end=2026-01-01"} {
		if rec := performRequest(t, e, http.MethodGet, "/api/v1/audit/entries?"+query, nil, nil); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d", query, rec.Code)
		}
	}
	// Reversed bounds are swapped rather than rejected; huge limits are capped.
	rec := performRequest(t, e, http.MethodGet, "/api/v1/audit/entries?limit=999999&start=2026-02-01T00:00:00Z&end=2026-01-01T00:00:00Z", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("reversed range status = %d", rec.Code)
	}
	if rec := performRequest(t, e, http.MethodGet, "/api/v1/audit/files/2020-01-01", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing file status = %d", rec.Code)
	}
	if rec := performRequest(t, e, http.MethodPut, "/api/v1/audit/settings", strings.NewReader("{"), map[string]string{"Content-Type": "application/json"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad body status = %d", rec.Code)
	}
}

func TestRecordAuditWithoutUserIsIgnored(t *testing.T) {
	e := echo.New()
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), httptest.NewRecorder())
	recordAudit(c, audit.ActionDiaryView, "x", nil) // no user, no logger: must not panic
	recordDiarySave(c, "u", "", audit.SourceWeb, nil, nil)
	if diaryAuditDetail(nil) == nil {
		t.Fatal("detail should never be nil")
	}
}
