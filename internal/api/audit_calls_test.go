package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/audit"
	"github.com/songtianlun/diarum/internal/store"
)

func enableTokenAPI(t *testing.T, s *store.Store, user *store.User, mcp bool) {
	t.Helper()
	for key, value := range map[string]any{"api.token": "tok", "api.enabled": true, "api.mcp_enabled": mcp} {
		if err := s.SetSetting(user.ID, key, value, false); err != nil {
			t.Fatal(err)
		}
	}
}

func (env *adminTestEnv) mcp(t *testing.T, token, body string) int {
	t.Helper()
	rec := performRequest(t, env.e, http.MethodPost, "/api/v1/mcp", strings.NewReader(body), map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + token,
	})
	return rec.Code
}

func entryDetail(e *audit.Entry, key string) any {
	if e == nil || e.Detail == nil {
		return nil
	}
	return e.Detail[key]
}

func TestMCPCallsAreAudited(t *testing.T) {
	env := newAdminTestEnv(t)
	RegisterMCPRoutes(env.e, env.store, "test")
	enableTokenAPI(t, env.store, env.user, true)
	if _, _, err := env.store.UpsertDiary(env.user.ID, "2026-09-01 00:00:00.000Z", "hiking day", "", ""); err != nil {
		t.Fatal(err)
	}

	env.mcp(t, "tok", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	env.mcp(t, "tok", `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	env.mcp(t, "tok", `[
		{"jsonrpc":"2.0","id":2,"method":"tools/list"},
		{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_diary","arguments":{"date":"2026-09-01"}}},
		{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_diary","arguments":{"date":"2026-09-02"}}},
		{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"get_diary","arguments":{}}},
		{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"drop_tables","arguments":{"force":true,"long":"`+strings.Repeat("x", 500)+`"}}},
		{"jsonrpc":"2.0","id":7,"method":"resources/list"}
	]`)
	env.mcp(t, "tok", `{not json`)

	entries := env.entries(t, "source=mcp")
	if len(entries) != 9 {
		t.Fatalf("mcp entries = %d, want 9: %+v", len(entries), entries)
	}
	for _, e := range entries {
		if e.UserID != env.user.ID || e.Source != audit.SourceMCP {
			t.Fatalf("entry not attributed: %+v", e)
		}
	}
	calls := func(target string) *audit.Entry {
		return findEntry(entries, func(e audit.Entry) bool { return e.Action == audit.ActionMCPCall && e.Target == target })
	}

	if e := calls("initialize"); entryDetail(e, "method") != "initialize" {
		t.Fatalf("initialize entry = %+v", e)
	}
	if e := calls("notifications/initialized"); entryDetail(e, "notification") != true {
		t.Fatalf("notification entry = %+v", e)
	}
	if calls("tools/list") == nil {
		t.Fatal("tools/list not audited")
	}
	if e := findEntry(entries, func(e audit.Entry) bool { return e.Action == audit.ActionDiaryView }); e == nil || e.Target != "2026-09-01" {
		t.Fatalf("diary read entry = %+v", e)
	}
	notFound := findEntry(entries, func(e audit.Entry) bool {
		args, _ := entryDetail(&e, "arguments").(map[string]any)
		return e.Target == "get_diary" && args["date"] == "2026-09-02"
	})
	if notFound == nil || notFound.Action != audit.ActionMCPCall || entryDetail(notFound, "error") != nil {
		t.Fatalf("missing-diary entry = %+v", notFound)
	}
	invalid := findEntry(entries, func(e audit.Entry) bool { return e.Target == "get_diary" && entryDetail(&e, "arguments") == nil })
	if errDetail, _ := entryDetail(invalid, "error").(map[string]any); !strings.Contains(errDetail["message"].(string), "date") {
		t.Fatalf("tool error entry = %+v", invalid)
	}
	unknown := calls("drop_tables")
	args, _ := entryDetail(unknown, "arguments").(map[string]any)
	if args["force"] != true || len(args["long"].(string)) != 200 {
		t.Fatalf("unknown tool arguments = %+v", args)
	}
	if errDetail, _ := entryDetail(unknown, "error").(map[string]any); errDetail["code"] != float64(jsonRPCInvalidParams) {
		t.Fatalf("unknown tool error = %+v", unknown)
	}
	if errDetail, _ := entryDetail(calls("resources/list"), "error").(map[string]any); errDetail["code"] != float64(jsonRPCMethodNotFound) {
		t.Fatal("unknown method error not recorded")
	}
	if errDetail, _ := entryDetail(calls(""), "error").(map[string]any); errDetail["code"] != float64(jsonRPCParseError) {
		t.Fatal("parse error not recorded")
	}
}

func TestRejectedTokenCallsKeepTheirSource(t *testing.T) {
	env := newAdminTestEnv(t)
	RegisterMCPRoutes(env.e, env.store, "test")
	RegisterMemosRoutes(env.e, env.store, func(next echo.HandlerFunc) echo.HandlerFunc { return next }, nil)
	enableTokenAPI(t, env.store, env.user, false)

	if code := env.mcp(t, "wrong", `{"jsonrpc":"2.0","id":1,"method":"ping"}`); code != http.StatusUnauthorized {
		t.Fatalf("bad token code = %d", code)
	}
	if code := env.mcp(t, "tok", `{"jsonrpc":"2.0","id":1,"method":"ping"}`); code != http.StatusUnauthorized {
		t.Fatalf("mcp disabled code = %d", code)
	}
	env.do(t, nil, http.MethodGet, "/api/v1/diaries?token=wrong&date=2026-09-01", "")
	env.do(t, nil, http.MethodPost, "/api/v1/memos/webhook/nope", "{}")

	mcpEntries := env.entries(t, "source=mcp")
	if len(mcpEntries) != 2 {
		t.Fatalf("mcp entries = %+v", mcpEntries)
	}
	for _, e := range mcpEntries {
		if e.Action != audit.ActionAuthDenied {
			t.Fatalf("mcp denial entry = %+v", e)
		}
	}
	// A valid token with MCP switched off still names its owner.
	if findEntry(mcpEntries, func(e audit.Entry) bool { return e.UserID == env.user.ID }) == nil {
		t.Fatalf("mcp-disabled denial not attributed: %+v", mcpEntries)
	}
	if entries := env.entries(t, "source=api"); len(entries) != 1 || entries[0].Action != audit.ActionAuthDenied || entries[0].UserID != "" {
		t.Fatalf("api denial entries = %+v", entries)
	}
	if entries := env.entries(t, "source=memos"); len(entries) != 1 || entries[0].Status != http.StatusUnauthorized {
		t.Fatalf("memos denial entries = %+v", entries)
	}
}

func TestPublicAPICallsWithoutReadsAreAudited(t *testing.T) {
	env := newAdminTestEnv(t)
	enableTokenAPI(t, env.store, env.user, false)

	env.do(t, nil, http.MethodGet, "/api/v1/diaries?token=tok&date=2026-09-03", "")
	env.do(t, nil, http.MethodGet, "/api/v1/diaries?token=tok&start=2026-01-01&end=2026-01-31", "")
	env.do(t, nil, http.MethodGet, "/api/v1/diaries?token=tok&start=2026-01-01", "")

	entries := env.entries(t, "source=api")
	if len(entries) != 3 {
		t.Fatalf("api entries = %+v", entries)
	}
	for _, e := range entries {
		if e.Action != audit.ActionAPICall || e.UserID != env.user.ID {
			t.Fatalf("api call entry = %+v", e)
		}
		args, _ := entryDetail(&e, "arguments").(map[string]any)
		if len(args) == 0 || args["token"] != nil {
			t.Fatalf("api call arguments = %+v", args)
		}
	}
	if e := findEntry(entries, func(e audit.Entry) bool { return e.Target == "2026-09-03" }); entryDetail(e, "exists") != false {
		t.Fatalf("missing diary entry = %+v", e)
	}
	if e := findEntry(entries, func(e audit.Entry) bool { return e.Target == "2026-01-01..2026-01-31" }); entryDetail(e, "results") != float64(0) {
		t.Fatalf("empty range entry = %+v", e)
	}
	if e := findEntry(entries, func(e audit.Entry) bool { return e.Target == "" }); e == nil || e.Status != http.StatusBadRequest {
		t.Fatalf("bad request entry = %+v", e)
	}
}

func TestAuditArgsBoundsWhatIsKept(t *testing.T) {
	args := map[string]any{"list": []any{1, 2}, "empty": "", "n": float64(3), "none": nil}
	for i := 0; i < 20; i++ {
		args[strings.Repeat("k", i+1)] = "v"
	}
	kept := auditArgs(args)
	if len(kept) != 10 {
		t.Fatalf("kept %d arguments, want 10", len(kept))
	}
	if _, ok := kept["empty"]; ok {
		t.Fatal("empty strings should be dropped")
	}
	if got := auditArgs(map[string]any{"list": []any{1, 2}}); got["list"] != "[1 2]" {
		t.Fatalf("composite argument = %#v", got["list"])
	}
	if len(auditArgs(nil)) != 0 {
		t.Fatal("nil arguments should keep nothing")
	}
}
