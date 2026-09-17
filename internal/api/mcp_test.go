package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/store"
)

// mcpTestEnv wires an Echo instance with the MCP routes and a user whose API
// token and MCP switch are both enabled.
func mcpTestEnv(t *testing.T, apiEnabled, mcpEnabled bool) (*echo.Echo, *store.Store, *store.User, string) {
	t.Helper()

	s := newTestStore(t)
	user := newTestUser(t, s)
	configService := config.NewConfigService(s)

	const token = "mcp-test-token"
	if err := configService.Set(user.ID, "api.token", token); err != nil {
		t.Fatalf("set api.token: %v", err)
	}
	if err := configService.Set(user.ID, "api.enabled", apiEnabled); err != nil {
		t.Fatalf("set api.enabled: %v", err)
	}
	if err := configService.Set(user.ID, "api.mcp_enabled", mcpEnabled); err != nil {
		t.Fatalf("set api.mcp_enabled: %v", err)
	}

	e := echo.New()
	RegisterMCPRoutes(e, s, "test-version")
	return e, s, user, token
}

func mcpCall(t *testing.T, e *echo.Echo, token, body string) *httptest.ResponseRecorder {
	t.Helper()

	headers := map[string]string{"Content-Type": "application/json"}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	return performRequest(t, e, http.MethodPost, "/api/v1/mcp", strings.NewReader(body), headers)
}

// toolResultPayload unwraps the JSON document a tool returns inside its text
// content block.
func toolResultPayload(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	payload := decodeJSONBody(t, rec)
	result, ok := payload["result"].(map[string]any)
	if !ok {
		t.Fatalf("response has no result object: %s", rec.Body.String())
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("result has no content: %s", rec.Body.String())
	}
	first, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("content[0] is not an object: %s", rec.Body.String())
	}
	text, _ := first["text"].(string)

	var decoded map[string]any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("decode tool payload: %v\ntext=%s", err, text)
	}
	return decoded
}

func TestMCPInitializeAndToolsList(t *testing.T) {
	e, _, _, token := mcpTestEnv(t, true, true)

	rec := mcpCall(t, e, token, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("initialize status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	payload := decodeJSONBody(t, rec)
	result, ok := payload["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize has no result: %s", rec.Body.String())
	}
	if result["protocolVersion"] != mcpProtocolVersion {
		t.Fatalf("protocolVersion = %v, want %s", result["protocolVersion"], mcpProtocolVersion)
	}
	serverInfo, _ := result["serverInfo"].(map[string]any)
	if serverInfo["version"] != "test-version" {
		t.Fatalf("serverInfo.version = %v, want test-version", serverInfo["version"])
	}

	rec = mcpCall(t, e, token, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	payload = decodeJSONBody(t, rec)
	result, _ = payload["result"].(map[string]any)
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) != 3 {
		t.Fatalf("tools/list returned %v, want 3 tools: %s", result["tools"], rec.Body.String())
	}
	names := make(map[string]bool)
	for _, tool := range tools {
		entry, _ := tool.(map[string]any)
		name, _ := entry["name"].(string)
		names[name] = true
	}
	for _, want := range []string{"get_diary", "list_diaries", "search_diaries"} {
		if !names[want] {
			t.Fatalf("tools/list missing %q, got %v", want, names)
		}
	}
}

func TestMCPGetDiaryTool(t *testing.T) {
	e, s, user, token := mcpTestEnv(t, true, true)

	if _, _, err := s.UpsertDiary(user.ID, "2026-03-14", "pi day entry", "😊", "☀️"); err != nil {
		t.Fatalf("UpsertDiary: %v", err)
	}

	rec := mcpCall(t, e, token, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_diary","arguments":{"date":"2026-03-14"}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	decoded := toolResultPayload(t, rec)
	if decoded["exists"] != true {
		t.Fatalf("exists = %v, want true", decoded["exists"])
	}
	if decoded["content"] != "pi day entry" {
		t.Fatalf("content = %v, want 'pi day entry'", decoded["content"])
	}

	// A day with no entry reports exists=false rather than erroring.
	rec = mcpCall(t, e, token, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_diary","arguments":{"date":"2026-03-15"}}}`)
	decoded = toolResultPayload(t, rec)
	if decoded["exists"] != false {
		t.Fatalf("exists = %v, want false", decoded["exists"])
	}
}

func TestMCPListAndSearchTools(t *testing.T) {
	e, s, user, token := mcpTestEnv(t, true, true)

	if _, _, err := s.UpsertDiary(user.ID, "2026-03-01", "hiking in the hills", "", ""); err != nil {
		t.Fatalf("UpsertDiary: %v", err)
	}
	if _, _, err := s.UpsertDiary(user.ID, "2026-03-05", "reading at home", "", ""); err != nil {
		t.Fatalf("UpsertDiary: %v", err)
	}

	rec := mcpCall(t, e, token, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_diaries","arguments":{"start":"2026-03-01","end":"2026-03-31"}}}`)
	decoded := toolResultPayload(t, rec)
	if total, _ := decoded["total"].(float64); total != 2 {
		t.Fatalf("list_diaries total = %v, want 2: %s", decoded["total"], rec.Body.String())
	}

	rec = mcpCall(t, e, token, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search_diaries","arguments":{"query":"hiking"}}}`)
	decoded = toolResultPayload(t, rec)
	if total, _ := decoded["total"].(float64); total != 1 {
		t.Fatalf("search_diaries total = %v, want 1: %s", decoded["total"], rec.Body.String())
	}
}

// Diaries belonging to another user must never surface through a token.
func TestMCPToolsAreScopedToTokenOwner(t *testing.T) {
	e, s, _, token := mcpTestEnv(t, true, true)

	other := newTestUser(t, s)
	if _, _, err := s.UpsertDiary(other.ID, "2026-03-01", "someone else's secret", "", ""); err != nil {
		t.Fatalf("UpsertDiary: %v", err)
	}

	rec := mcpCall(t, e, token, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_diaries","arguments":{"query":"secret"}}}`)
	decoded := toolResultPayload(t, rec)
	if total, _ := decoded["total"].(float64); total != 0 {
		t.Fatalf("search returned %v entries from another user: %s", decoded["total"], rec.Body.String())
	}
}

func TestMCPRequiresValidToken(t *testing.T) {
	e, _, _, token := mcpTestEnv(t, true, true)

	rec := mcpCall(t, e, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want 401", rec.Code)
	}

	rec = mcpCall(t, e, "wrong-"+token, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token status = %d, want 401", rec.Code)
	}
}

func TestMCPRespectsDisabledSwitches(t *testing.T) {
	// MCP off, API on.
	e, _, _, token := mcpTestEnv(t, true, false)
	rec := mcpCall(t, e, token, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("mcp disabled status = %d, want 401: %s", rec.Code, rec.Body.String())
	}

	// API off, MCP on: the token itself is no longer valid.
	e, _, _, token = mcpTestEnv(t, false, true)
	rec = mcpCall(t, e, token, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("api disabled status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
}

// Token in the query string is supported for clients that cannot set headers.
func TestMCPAcceptsQueryToken(t *testing.T) {
	e, _, _, token := mcpTestEnv(t, true, true)

	rec := performRequest(t, e, http.MethodPost, "/api/v1/mcp?token="+token,
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`),
		map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("query token status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

// Notifications carry no ID and must produce an empty 202 rather than a body.
func TestMCPNotificationGetsNoBody(t *testing.T) {
	e, _, _, token := mcpTestEnv(t, true, true)

	rec := mcpCall(t, e, token, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("notification status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "" {
		t.Fatalf("notification returned a body: %s", rec.Body.String())
	}
}

func TestMCPBatchRequest(t *testing.T) {
	e, _, _, token := mcpTestEnv(t, true, true)

	rec := mcpCall(t, e, token, `[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","id":2,"method":"tools/list"}]`)
	if rec.Code != http.StatusOK {
		t.Fatalf("batch status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var responses []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &responses); err != nil {
		t.Fatalf("batch response is not an array: %v\nbody=%s", err, rec.Body.String())
	}
	if len(responses) != 2 {
		t.Fatalf("batch returned %d responses, want 2", len(responses))
	}
}

func TestMCPUnknownMethodAndTool(t *testing.T) {
	e, _, _, token := mcpTestEnv(t, true, true)

	rec := mcpCall(t, e, token, `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`)
	payload := decodeJSONBody(t, rec)
	rpcErr, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("unknown method did not return an error: %s", rec.Body.String())
	}
	if code, _ := rpcErr["code"].(float64); int(code) != jsonRPCMethodNotFound {
		t.Fatalf("error code = %v, want %d", rpcErr["code"], jsonRPCMethodNotFound)
	}

	rec = mcpCall(t, e, token, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"delete_everything","arguments":{}}}`)
	payload = decodeJSONBody(t, rec)
	if _, ok := payload["error"].(map[string]any); !ok {
		t.Fatalf("unknown tool did not return an error: %s", rec.Body.String())
	}
}

// The settings toggle is the UI's switch: MCP may only be turned on while API
// access is enabled, and turning API access off must drag MCP down with it.
func TestMCPSettingsToggle(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	configService := config.NewConfigService(s)

	e := echo.New()
	RegisterSettingsRoutes(e, s, authMiddlewareFor(user))

	jsonHeaders := map[string]string{"Content-Type": "application/json"}

	// Without a token and api.enabled, enabling MCP is rejected.
	rec := performRequest(t, e, http.MethodPost, "/api/v1/settings/mcp/toggle", strings.NewReader(`{"enabled":true}`), jsonHeaders)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("enable without API status = %d, want 400: %s", rec.Code, rec.Body.String())
	}

	// Turning API access on mints a token.
	rec = performRequest(t, e, http.MethodPost, "/api/v1/settings/api-token/toggle", nil, jsonHeaders)
	if rec.Code != http.StatusOK {
		t.Fatalf("api toggle status = %d: %s", rec.Code, rec.Body.String())
	}

	rec = performRequest(t, e, http.MethodPost, "/api/v1/settings/mcp/toggle", strings.NewReader(`{"enabled":true}`), jsonHeaders)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable mcp status = %d: %s", rec.Code, rec.Body.String())
	}
	if payload := decodeJSONBody(t, rec); payload["mcp_enabled"] != true {
		t.Fatalf("mcp_enabled = %v, want true", payload["mcp_enabled"])
	}

	// The GET endpoint reports the new state.
	rec = performRequest(t, e, http.MethodGet, "/api/v1/settings/api-token", nil, nil)
	if payload := decodeJSONBody(t, rec); payload["mcp_enabled"] != true {
		t.Fatalf("api-token mcp_enabled = %v, want true", payload["mcp_enabled"])
	}

	// Disabling API access cascades to MCP.
	rec = performRequest(t, e, http.MethodPost, "/api/v1/settings/api-token/toggle", nil, jsonHeaders)
	if payload := decodeJSONBody(t, rec); payload["enabled"] != false || payload["mcp_enabled"] != false {
		t.Fatalf("after disabling API: %s", rec.Body.String())
	}
	if enabled, err := configService.GetBool(user.ID, "api.mcp_enabled"); err != nil || enabled {
		t.Fatalf("api.mcp_enabled = %v (err=%v), want false", enabled, err)
	}
}

// A malformed toggle body is rejected before any setting is written.
func TestMCPSettingsToggleMalformedBody(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)

	e := echo.New()
	RegisterSettingsRoutes(e, s, authMiddlewareFor(user))

	rec := performRequest(t, e, http.MethodPost, "/api/v1/settings/mcp/toggle",
		strings.NewReader(`{"enabled":`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// Omitting "enabled" flips the current value, which is how a plain toggle
// button behaves.
func TestMCPSettingsToggleWithoutExplicitValue(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	configService := config.NewConfigService(s)

	if err := configService.Set(user.ID, "api.token", "tok"); err != nil {
		t.Fatalf("set api.token: %v", err)
	}
	if err := configService.Set(user.ID, "api.enabled", true); err != nil {
		t.Fatalf("set api.enabled: %v", err)
	}

	e := echo.New()
	RegisterSettingsRoutes(e, s, authMiddlewareFor(user))
	jsonHeaders := map[string]string{"Content-Type": "application/json"}

	rec := performRequest(t, e, http.MethodPost, "/api/v1/settings/mcp/toggle", strings.NewReader(`{}`), jsonHeaders)
	if payload := decodeJSONBody(t, rec); payload["mcp_enabled"] != true {
		t.Fatalf("first toggle mcp_enabled = %v, want true: %s", payload["mcp_enabled"], rec.Body.String())
	}

	rec = performRequest(t, e, http.MethodPost, "/api/v1/settings/mcp/toggle", strings.NewReader(`{}`), jsonHeaders)
	if payload := decodeJSONBody(t, rec); payload["mcp_enabled"] != false {
		t.Fatalf("second toggle mcp_enabled = %v, want false: %s", payload["mcp_enabled"], rec.Body.String())
	}
}

// A store that cannot be written must report the failure instead of silently
// reporting success.
func TestMCPSettingsToggleStoreClosed(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	configService := config.NewConfigService(s)

	if err := configService.Set(user.ID, "api.token", "tok"); err != nil {
		t.Fatalf("set api.token: %v", err)
	}
	if err := configService.Set(user.ID, "api.enabled", true); err != nil {
		t.Fatalf("set api.enabled: %v", err)
	}

	e := echo.New()
	RegisterSettingsRoutes(e, s, authMiddlewareFor(user))

	if err := s.DB.Close(); err != nil {
		t.Fatalf("close DB: %v", err)
	}

	// Disabling needs no precondition read, so it reaches the failing write.
	rec := performRequest(t, e, http.MethodPost, "/api/v1/settings/mcp/toggle",
		strings.NewReader(`{"enabled":false}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// The api-token endpoints degrade gracefully when settings cannot be read: the
// GET reports an unconfigured token rather than failing the whole page.
func TestAPITokenEndpointsOnClosedStore(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)

	e := echo.New()
	RegisterSettingsRoutes(e, s, authMiddlewareFor(user))

	if err := s.DB.Close(); err != nil {
		t.Fatalf("close DB: %v", err)
	}

	rec := performRequest(t, e, http.MethodGet, "/api/v1/settings/api-token", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	payload := decodeJSONBody(t, rec)
	if payload["exists"] != false || payload["mcp_enabled"] != false {
		t.Fatalf("GET payload = %s, want an unconfigured token", rec.Body.String())
	}

	// Writes cannot succeed against a closed store.
	rec = performRequest(t, e, http.MethodPost, "/api/v1/settings/api-token/toggle", nil, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("toggle status = %d, want 400: %s", rec.Code, rec.Body.String())
	}

	rec = performRequest(t, e, http.MethodPost, "/api/v1/settings/api-token/reset", nil, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reset status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// Resetting a token while API access is off turns access back on, and reports
// the MCP state alongside it.
func TestAPITokenResetEnablesAccess(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	configService := config.NewConfigService(s)

	if err := configService.Set(user.ID, "api.token", "old-token"); err != nil {
		t.Fatalf("set api.token: %v", err)
	}
	if err := configService.Set(user.ID, "api.enabled", false); err != nil {
		t.Fatalf("set api.enabled: %v", err)
	}

	e := echo.New()
	RegisterSettingsRoutes(e, s, authMiddlewareFor(user))

	rec := performRequest(t, e, http.MethodPost, "/api/v1/settings/api-token/reset", nil, nil)
	payload := decodeJSONBody(t, rec)
	if payload["enabled"] != true {
		t.Fatalf("enabled = %v, want true: %s", payload["enabled"], rec.Body.String())
	}
	if payload["mcp_enabled"] != false {
		t.Fatalf("mcp_enabled = %v, want false: %s", payload["mcp_enabled"], rec.Body.String())
	}
	if token, _ := payload["token"].(string); token == "old-token" || token == "" {
		t.Fatalf("token = %q, want a freshly generated token", token)
	}
}

// Missing required arguments surface as tool errors, not protocol errors, so
// the model can read and correct them.
func TestMCPMissingArgumentsReturnToolError(t *testing.T) {
	e, _, _, token := mcpTestEnv(t, true, true)

	cases := []struct {
		name string
		body string
	}{
		{"get_diary without date", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_diary","arguments":{}}}`},
		{"list_diaries without range", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_diaries","arguments":{"start":"2026-01-01"}}}`},
		{"search_diaries without query", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search_diaries","arguments":{"query":"   "}}}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := mcpCall(t, e, token, tc.body)
			payload := decodeJSONBody(t, rec)
			result, ok := payload["result"].(map[string]any)
			if !ok {
				t.Fatalf("expected a result object: %s", rec.Body.String())
			}
			if result["isError"] != true {
				t.Fatalf("isError = %v, want true: %s", result["isError"], rec.Body.String())
			}
		})
	}
}

// A malformed body cannot be attributed to any request ID, so it comes back as
// a parse error with a null ID.
func TestMCPMalformedBody(t *testing.T) {
	e, _, _, token := mcpTestEnv(t, true, true)

	bodies := []string{
		`{"jsonrpc":`,
		`[{"jsonrpc":"2.0"`,
		`"just a string"`,
		`{"jsonrpc":"2.0","id":1,"method":5}`,
		// Well-formed JSON arrays whose elements are not request objects.
		`[1,2,3]`,
		`[{"jsonrpc":"2.0","method":7}]`,
	}

	for _, body := range bodies {
		rec := mcpCall(t, e, token, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 for body %s: %s", rec.Code, body, rec.Body.String())
		}
		payload := decodeJSONBody(t, rec)
		rpcErr, ok := payload["error"].(map[string]any)
		if !ok {
			t.Fatalf("body %s did not produce an error: %s", body, rec.Body.String())
		}
		if code, _ := rpcErr["code"].(float64); int(code) != jsonRPCParseError {
			t.Fatalf("error code = %v, want %d for body %s", rpcErr["code"], jsonRPCParseError, body)
		}
	}
}

// Malformed tool arguments (wrong types) are a protocol-level invalid params
// error rather than a tool error.
func TestMCPInvalidToolArguments(t *testing.T) {
	e, _, _, token := mcpTestEnv(t, true, true)

	rec := mcpCall(t, e, token, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_diary","arguments":{"date":123}}}`)
	payload := decodeJSONBody(t, rec)
	rpcErr, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected an error object: %s", rec.Body.String())
	}
	if code, _ := rpcErr["code"].(float64); int(code) != jsonRPCInvalidParams {
		t.Fatalf("error code = %v, want %d", rpcErr["code"], jsonRPCInvalidParams)
	}
}

// tools/call with no params at all must not panic on the nil Params slice.
func TestMCPToolCallWithoutParams(t *testing.T) {
	e, _, _, token := mcpTestEnv(t, true, true)

	rec := mcpCall(t, e, token, `{"jsonrpc":"2.0","id":1,"method":"tools/call"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	payload := decodeJSONBody(t, rec)
	if _, ok := payload["error"].(map[string]any); !ok {
		t.Fatalf("expected an error for an unnamed tool: %s", rec.Body.String())
	}
}

// Every request method that can also arrive as a notification must stay silent
// when it has no ID.
func TestMCPNotificationsForAllMethods(t *testing.T) {
	e, _, _, token := mcpTestEnv(t, true, true)

	bodies := []string{
		`{"jsonrpc":"2.0","method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"ping"}`,
		`{"jsonrpc":"2.0","method":"tools/list"}`,
		`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"get_diary","arguments":{"date":"2026-01-01"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled"}`,
		`{"jsonrpc":"2.0","method":"some/unknown/method"}`,
	}

	for _, body := range bodies {
		rec := mcpCall(t, e, token, body)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202 for %s: %s", rec.Code, body, rec.Body.String())
		}
		if strings.TrimSpace(rec.Body.String()) != "" {
			t.Fatalf("notification %s returned a body: %s", body, rec.Body.String())
		}
	}
}

func TestMCPPing(t *testing.T) {
	e, _, _, token := mcpTestEnv(t, true, true)

	rec := mcpCall(t, e, token, `{"jsonrpc":"2.0","id":7,"method":"ping"}`)
	payload := decodeJSONBody(t, rec)
	if _, ok := payload["result"].(map[string]any); !ok {
		t.Fatalf("ping has no result: %s", rec.Body.String())
	}
}

// GET is declined (this server is stateless) and DELETE ends a session as a
// no-op, both of which Streamable HTTP clients probe for.
func TestMCPGetAndDeleteMethods(t *testing.T) {
	e, _, _, token := mcpTestEnv(t, true, true)

	headers := map[string]string{"Authorization": "Bearer " + token}

	rec := performRequest(t, e, http.MethodGet, "/api/v1/mcp", nil, headers)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405: %s", rec.Code, rec.Body.String())
	}

	rec = performRequest(t, e, http.MethodDelete, "/api/v1/mcp", nil, headers)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
}

// A store that can no longer be queried must surface an internal error rather
// than an empty result set that looks like "no diaries".
func TestMCPToolStoreErrors(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	if err := s.DB.Close(); err != nil {
		t.Fatalf("close DB: %v", err)
	}

	cases := []struct {
		name string
		body string
	}{
		{"list_diaries", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_diaries","arguments":{"start":"2026-01-01","end":"2026-12-31"}}}`},
		{"search_diaries", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search_diaries","arguments":{"query":"anything"}}}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req jsonRPCRequest
			if err := json.Unmarshal([]byte(tc.body), &req); err != nil {
				t.Fatalf("unmarshal request: %v", err)
			}
			resp := dispatchMCP(s, user.ID, "test-version", req)
			if resp == nil || resp.Error == nil {
				t.Fatalf("expected an error response, got %#v", resp)
			}
			if resp.Error.Code != jsonRPCInternalError {
				t.Fatalf("error code = %d, want %d", resp.Error.Code, jsonRPCInternalError)
			}
		})
	}
}

// get_diary treats a closed store the same as a missing entry: the tool cannot
// distinguish them and reports exists=false either way.
func TestMCPGetDiaryOnClosedStore(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	if err := s.DB.Close(); err != nil {
		t.Fatalf("close DB: %v", err)
	}

	var req jsonRPCRequest
	if err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_diary","arguments":{"date":"2026-01-01"}}}`), &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	resp := dispatchMCP(s, user.ID, "test-version", req)
	if resp == nil || resp.Error != nil {
		t.Fatalf("expected a tool result, got %#v", resp)
	}
}

func TestClampLimit(t *testing.T) {
	cases := []struct {
		limit    int
		fallback int
		want     int
	}{
		{0, 50, 50},
		{-5, 20, 20},
		{10, 50, 10},
		{200, 50, 200},
		{5000, 50, 200},
	}

	for _, tc := range cases {
		if got := clampLimit(tc.limit, tc.fallback); got != tc.want {
			t.Fatalf("clampLimit(%d, %d) = %d, want %d", tc.limit, tc.fallback, got, tc.want)
		}
	}
}

// A payload that cannot be marshalled becomes an internal error rather than a
// half-written result.
func TestToolSuccessEncodingFailure(t *testing.T) {
	resp := toolSuccess(json.RawMessage(`1`), map[string]any{"bad": make(chan int)})
	if resp.Error == nil {
		t.Fatalf("expected an error response, got %#v", resp)
	}
	if resp.Error.Code != jsonRPCInternalError {
		t.Fatalf("error code = %d, want %d", resp.Error.Code, jsonRPCInternalError)
	}
}

// A switch value that is not a boolean must read as disabled rather than
// falling through to the tools.
func TestMCPEnabledCheckError(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	configService := config.NewConfigService(s)

	const token = "mcp-test-token"
	if err := configService.Set(user.ID, "api.token", token); err != nil {
		t.Fatalf("set api.token: %v", err)
	}
	if err := configService.Set(user.ID, "api.enabled", true); err != nil {
		t.Fatalf("set api.enabled: %v", err)
	}
	// Store a non-boolean so GetBool cannot report an enabled MCP.
	if err := s.SetSetting(user.ID, "api.mcp_enabled", "not-a-bool", false); err != nil {
		t.Fatalf("set api.mcp_enabled: %v", err)
	}

	e := echo.New()
	RegisterMCPRoutes(e, s, "test-version")

	rec := mcpCall(t, e, token, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
}
