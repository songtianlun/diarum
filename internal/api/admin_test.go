package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"github.com/songtianlun/diarum/internal/audit"
	iauth "github.com/songtianlun/diarum/internal/auth"
	"github.com/songtianlun/diarum/internal/backup"
	"github.com/songtianlun/diarum/internal/store"
)

type adminTestEnv struct {
	e      *echo.Echo
	store  *store.Store
	audit  *audit.Logger
	bucket *backup.MemoryStore
	admin  *store.User
	user   *store.User
	tokens map[string]string
}

func newAdminTestEnv(t *testing.T) *adminTestEnv {
	t.Helper()
	s := newTestStore(t)
	bucket := backup.NewMemoryStore()
	l, err := audit.New(s.DataDir, audit.Options{
		Settings:       s,
		Location:       time.UTC,
		NewObjectStore: func(backup.S3Config) (backup.ObjectStore, error) { return bucket, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Close)
	authService := iauth.NewService(s)
	e := echo.New()
	e.Use(AuditMiddleware(l))
	e.Use(middleware.Recover())
	RegisterAuthRoutes(e, s, authService)
	RegisterPublicRoutes(e, s)
	RegisterAdminRoutes(e, s, authService.Middleware, l, "test")
	e.GET("/api/v1/panic", func(echo.Context) error { panic("boom") })
	e.GET("/api/v1/plain-error", func(echo.Context) error { return errors.New("plain") })
	e.POST("/api/v1/hooks/:token", func(c echo.Context) error {
		recordAuditFor(c, "u1", "", audit.SourceMemos, audit.ActionDiaryUpdate, "a", nil)
		recordAuditFor(c, "u1", "", audit.SourceMemos, audit.ActionDiaryUpdate, "b", nil)
		return c.NoContent(http.StatusOK)
	})
	e.GET("/not-api", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })

	hash, _ := authService.HashPassword("pw")
	admin, err := s.CreateUser("root", "root@example.com", hash)
	if err != nil {
		t.Fatal(err)
	}
	user, err := s.CreateUser("bob", "bob@example.com", hash)
	if err != nil {
		t.Fatal(err)
	}
	env := &adminTestEnv{e: e, store: s, audit: l, bucket: bucket, admin: admin, user: user, tokens: map[string]string{}}
	for _, u := range []*store.User{admin, user} {
		token, err := authService.IssueToken(u)
		if err != nil {
			t.Fatal(err)
		}
		env.tokens[u.ID] = token
	}
	return env
}

func (env *adminTestEnv) do(t *testing.T, as *store.User, method, path, body string) (int, map[string]any) {
	t.Helper()
	headers := map[string]string{"Content-Type": "application/json", "User-Agent": "test-agent"}
	if as != nil {
		headers["Authorization"] = "Bearer " + env.tokens[as.ID]
	}
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	rec := performRequest(t, env.e, method, path, reader, headers)
	payload := map[string]any{}
	if rec.Body.Len() > 0 && strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	}
	return rec.Code, payload
}

func (env *adminTestEnv) entries(t *testing.T, query string) []audit.Entry {
	t.Helper()
	code, body := env.do(t, env.admin, http.MethodGet, "/api/v1/admin/audit/logs?limit=1000&"+query, "")
	if code != http.StatusOK {
		t.Fatalf("logs: %d %v", code, body)
	}
	raw, _ := json.Marshal(body["entries"])
	var entries []audit.Entry
	json.Unmarshal(raw, &entries)
	return entries
}

func findEntry(entries []audit.Entry, match func(audit.Entry) bool) *audit.Entry {
	for i := range entries {
		if match(entries[i]) {
			return &entries[i]
		}
	}
	return nil
}

func TestAuditMiddlewareRecordsEveryAPICall(t *testing.T) {
	env := newAdminTestEnv(t)

	// Sign-in failures are recorded with the identity tried, known or not.
	env.do(t, nil, http.MethodPost, "/api/v1/auth/login", `{"usernameOrEmail":"ghost","password":"x"}`)
	env.do(t, nil, http.MethodPost, "/api/v1/auth/login", `{"usernameOrEmail":"bob","password":"wrong"}`)
	env.do(t, nil, http.MethodPost, "/api/v1/auth/login", `{"usernameOrEmail":"bob","password":"pw"}`)
	env.do(t, nil, http.MethodPost, "/api/v1/auth/register", `{"username":"bob","email":"b2@example.com","password":"p","passwordConfirm":"p"}`)
	env.do(t, nil, http.MethodPost, "/api/v1/auth/register", `{"username":"carol","email":"c@example.com","password":"p","passwordConfirm":"p"}`)
	if code, body := env.do(t, env.user, http.MethodGet, "/api/v1/auth/me", ""); code != http.StatusOK || body["role"] != store.RoleUser {
		t.Fatalf("me = %d %v", code, body)
	}
	if code, _ := env.do(t, env.user, http.MethodPost, "/api/v1/auth/logout", ""); code != http.StatusNoContent {
		t.Fatalf("logout = %d", code)
	}
	env.do(t, nil, http.MethodGet, "/api/v1/admin/overview", "")      // 401
	env.do(t, env.user, http.MethodGet, "/api/v1/admin/overview", "") // 403
	env.do(t, nil, http.MethodGet, "/api/v1/panic", "")               // 500 via Recover
	env.do(t, nil, http.MethodGet, "/api/v1/plain-error", "")         // 500
	env.do(t, nil, http.MethodPost, "/api/v1/hooks/s3cr3t-token", "") // two actions, token hidden
	env.do(t, nil, http.MethodGet, "/not-api", "")                    // not recorded
	env.do(t, nil, http.MethodGet, "/api/v1/diaries?token=nope", "")  // public API, bad token

	all := env.entries(t, "")
	check := func(name string, match func(audit.Entry) bool) *audit.Entry {
		t.Helper()
		entry := findEntry(all, match)
		if entry == nil {
			t.Fatalf("%s not recorded; have %+v", name, all)
		}
		return entry
	}
	ghost := check("unknown login", func(e audit.Entry) bool {
		return e.Action == audit.ActionAuthLoginFail && e.Detail["identity"] == "ghost"
	})
	if ghost.UserID != "" || ghost.Status != 401 || ghost.Detail["known"] != false || ghost.UA != "test-agent" || ghost.Route != "/api/v1/auth/login" {
		t.Fatalf("ghost = %+v", ghost)
	}
	check("known failed login", func(e audit.Entry) bool {
		return e.Action == audit.ActionAuthLoginFail && e.UserID == env.user.ID && e.Detail["known"] == true
	})
	check("login", func(e audit.Entry) bool {
		return e.Action == audit.ActionAuthLogin && e.User == "bob" && e.Status == 200
	})
	check("failed register", func(e audit.Entry) bool {
		return e.Action == audit.ActionAuthRegister && e.Status == 400 && e.Detail["failed"] == true
	})
	check("register", func(e audit.Entry) bool {
		return e.Action == audit.ActionAuthRegister && e.User == "carol" && e.Detail["role"] == store.RoleUser
	})
	check("me", func(e audit.Entry) bool {
		return e.Route == "/api/v1/auth/me" && e.UserID == env.user.ID && e.Action == ""
	})
	check("logout", func(e audit.Entry) bool { return e.Action == audit.ActionAuthLogout && e.UserID == env.user.ID })
	check("denied", func(e audit.Entry) bool { return e.Action == audit.ActionAuthDenied && e.Status == 401 })
	check("forbidden", func(e audit.Entry) bool { return e.Action == audit.ActionAuthForbidden && e.UserID == env.user.ID })
	check("panic", func(e audit.Entry) bool { return e.Route == "/api/v1/panic" && e.Status == 500 })
	check("plain error", func(e audit.Entry) bool {
		return e.Route == "/api/v1/plain-error" && e.Status == 500 && e.Error == "plain"
	})
	check("public api", func(e audit.Entry) bool { return e.Route == "/api/v1/diaries" && e.Status == 401 })
	hooks := 0
	for _, e := range all {
		if strings.Contains(e.Path, "s3cr3t") {
			t.Fatalf("secret leaked into path: %+v", e)
		}
		if e.Path == "/not-api" {
			t.Fatal("non-API request recorded")
		}
		if e.Source == audit.SourceMemos {
			hooks++
			if e.Path != "/api/v1/hooks/***" {
				t.Fatalf("hook path = %q", e.Path)
			}
		}
	}
	if hooks != 2 {
		t.Fatalf("hook entries = %d", hooks)
	}
}

func TestAuditMiddlewareWithoutLogger(t *testing.T) {
	e := echo.New()
	e.Use(AuditMiddleware(nil))
	called := false
	e.GET("/api/v1/x", func(c echo.Context) error {
		called = true
		if auditEnabled(c) {
			t.Fatal("audit should be off")
		}
		recordAudit(c, audit.ActionDiaryView, "", nil)
		auditIdentify(c, "u", "", audit.SourceAPI)
		return c.NoContent(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/x", nil))
	if !called || rec.Code != http.StatusOK {
		t.Fatal("handler not run")
	}
}

func TestPublicAPIIsAttributed(t *testing.T) {
	env := newAdminTestEnv(t)
	for key, value := range map[string]any{"api.token": "tok", "api.enabled": true} {
		if err := env.store.SetSetting(env.user.ID, key, value, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := env.store.UpsertDiary(env.user.ID, "2026-09-01 00:00:00.000Z", "hi", "", ""); err != nil {
		t.Fatal(err)
	}
	env.do(t, nil, http.MethodGet, "/api/v1/diaries?token=tok&date=2026-09-01", "")
	env.do(t, nil, http.MethodGet, "/api/v1/diaries?token=tok&start=2026-09-01&end=2026-09-02", "")
	entries := env.entries(t, "source=api")
	if len(entries) != 2 || entries[0].UserID != env.user.ID || entries[0].Action != audit.ActionDiaryView {
		t.Fatalf("public entries = %+v", entries)
	}
	for _, e := range entries {
		if strings.Contains(e.Path, "tok") {
			t.Fatal("query string must not be recorded")
		}
	}
	// Names missing at record time are filled in when read.
	if entries[0].User != "bob" {
		t.Fatalf("username not filled: %+v", entries[0])
	}
}

func TestAdminOverviewAndUsers(t *testing.T) {
	env := newAdminTestEnv(t)
	code, body := env.do(t, env.admin, http.MethodGet, "/api/v1/admin/overview", "")
	if code != http.StatusOK {
		t.Fatalf("overview = %d %v", code, body)
	}
	stats := body["stats"].(map[string]any)
	if stats["users"].(float64) != 2 || stats["admins"].(float64) != 1 {
		t.Fatalf("stats = %v", stats)
	}
	if body["system"].(map[string]any)["version"] != "test" || body["audit"].(map[string]any)["enabled"] != true {
		t.Fatalf("overview = %v", body)
	}

	code, body = env.do(t, env.admin, http.MethodGet, "/api/v1/admin/users?q=bo&limit=500&offset=0&sort=-created", "")
	if code != http.StatusOK || body["total"].(float64) != 1 || body["admins"].(float64) != 1 {
		t.Fatalf("users = %d %v", code, body)
	}
	for _, bad := range []string{"role=root", "limit=x", "limit=0", "offset=-1"} {
		if code, _ := env.do(t, env.admin, http.MethodGet, "/api/v1/admin/users?"+bad, ""); code != http.StatusBadRequest {
			t.Fatalf("%s = %d", bad, code)
		}
	}
	if code, body := env.do(t, env.admin, http.MethodGet, "/api/v1/admin/users?role=admin", ""); code != http.StatusOK || body["total"].(float64) != 1 {
		t.Fatalf("role filter = %v", body)
	}
}

func TestAdminSetRole(t *testing.T) {
	env := newAdminTestEnv(t)
	path := "/api/v1/admin/users/" + env.user.ID + "/role"
	if code, _ := env.do(t, env.admin, http.MethodPut, path, `{"role":"root"}`); code != http.StatusBadRequest {
		t.Fatalf("invalid role = %d", code)
	}
	if code, _ := env.do(t, env.admin, http.MethodPut, path, `{bad`); code != http.StatusBadRequest {
		t.Fatalf("bad body = %d", code)
	}
	if code, _ := env.do(t, env.admin, http.MethodPut, "/api/v1/admin/users/missing/role", `{"role":"admin"}`); code != http.StatusNotFound {
		t.Fatalf("missing = %d", code)
	}
	if code, _ := env.do(t, env.admin, http.MethodPut, "/api/v1/admin/users/"+env.admin.ID+"/role", `{"role":"user"}`); code != http.StatusBadRequest {
		t.Fatalf("self demote = %d", code)
	}
	if code, body := env.do(t, env.admin, http.MethodPut, path, `{"role":"user"}`); code != http.StatusOK || body["role"] != store.RoleUser {
		t.Fatalf("no-op = %d %v", code, body)
	}
	if code, body := env.do(t, env.admin, http.MethodPut, path, `{"role":"admin"}`); code != http.StatusOK || body["role"] != store.RoleAdmin {
		t.Fatalf("promote = %d %v", code, body)
	}
	// The new admin gets in immediately; roles are read per request.
	if code, _ := env.do(t, env.user, http.MethodGet, "/api/v1/admin/overview", ""); code != http.StatusOK {
		t.Fatalf("promoted user = %d", code)
	}
	entry := findEntry(env.entries(t, "action=admin"), func(e audit.Entry) bool { return e.Action == audit.ActionAdminRole })
	if entry == nil || entry.Target != "bob" || entry.Detail["to"] != store.RoleAdmin {
		t.Fatalf("role change entry = %+v", entry)
	}
	env.store.Close()
	if code, _ := env.do(t, env.admin, http.MethodPut, path, `{"role":"user"}`); code == http.StatusOK {
		t.Fatal("closed store should fail")
	}
}

func TestRequireAdminWithoutUser(t *testing.T) {
	e := echo.New()
	e.GET("/x", func(c echo.Context) error { return nil }, RequireAdmin)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestAdminAuditSettingsAndArchive(t *testing.T) {
	env := newAdminTestEnv(t)
	code, body := env.do(t, env.admin, http.MethodGet, "/api/v1/admin/audit/settings", "")
	if code != http.StatusOK || body["retention_days"].(float64) != 3 || body["secret_set"] != false {
		t.Fatalf("settings = %d %v", code, body)
	}
	if code, _ := env.do(t, env.admin, http.MethodPut, "/api/v1/admin/audit/settings", `{"retention_days":0,"archive":{"retention_days":30}}`); code != http.StatusBadRequest {
		t.Fatalf("invalid = %d", code)
	}
	if code, _ := env.do(t, env.admin, http.MethodPut, "/api/v1/admin/audit/settings", `{bad`); code != http.StatusBadRequest {
		t.Fatalf("bad body = %d", code)
	}
	// Archive operations need an archive.
	if code, _ := env.do(t, env.admin, http.MethodPost, "/api/v1/admin/audit/archives/run", ""); code != http.StatusBadRequest {
		t.Fatalf("run disabled = %d", code)
	}
	if code, body := env.do(t, env.admin, http.MethodGet, "/api/v1/admin/audit/archives", ""); code != http.StatusOK || body["enabled"] != false {
		t.Fatalf("archives disabled = %v", body)
	}

	cfg := `{"bucket":"b","region":"r","access_key":"ak","secret":"sk","prefix":"logs"}`
	if code, _ := env.do(t, env.admin, http.MethodPost, "/api/v1/admin/audit/settings/test", cfg); code != http.StatusOK {
		t.Fatalf("test = %d", code)
	}
	if code, _ := env.do(t, env.admin, http.MethodPost, "/api/v1/admin/audit/settings/test", `{"bucket":"b"}`); code != http.StatusBadRequest {
		t.Fatalf("test incomplete = %d", code)
	}
	if code, _ := env.do(t, env.admin, http.MethodPost, "/api/v1/admin/audit/settings/test", `{bad`); code != http.StatusBadRequest {
		t.Fatalf("test bad body = %d", code)
	}
	code, body = env.do(t, env.admin, http.MethodPut, "/api/v1/admin/audit/settings", `{"retention_days":5,"archive":{"enabled":true,"retention_days":60,"s3":`+cfg+`}}`)
	if code != http.StatusOK || body["secret_set"] != true || body["archive"].(map[string]any)["s3"].(map[string]any)["secret"] != "" {
		t.Fatalf("save = %d %v", code, body)
	}

	// Yesterday's file goes to the bucket.
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	line := `{"time":"` + yesterday + `T01:00:00Z","action":"diary.view"}` + "\n"
	os.WriteFile(filepath.Join(audit.Dir(env.store.DataDir), yesterday+".log"), []byte(line), 0o600)
	code, body = env.do(t, env.admin, http.MethodPost, "/api/v1/admin/audit/archives/run", "")
	if code != http.StatusOK {
		t.Fatalf("run = %d %v", code, body)
	}
	code, body = env.do(t, env.admin, http.MethodGet, "/api/v1/admin/audit/archives", "")
	if code != http.StatusOK || len(body["archives"].([]any)) != 1 {
		t.Fatalf("archives = %v", body)
	}

	// An older day only in the bucket can be pulled, searched and removed.
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write([]byte(`{"time":"2026-01-02T01:00:00Z","action":"diary.delete","user_id":"zz"}` + "\n"))
	w.Close()
	env.bucket.Put(context.Background(), "logs/2026-01-02.log.gz", &buf, "")
	for path, want := range map[string]int{
		"/api/v1/admin/audit/archives/bad/pull":                                           http.StatusBadRequest,
		"/api/v1/admin/audit/archives/2026-01-03/pull":                                    http.StatusNotFound,
		"/api/v1/admin/audit/archives/" + time.Now().UTC().Format("2006-01-02") + "/pull": http.StatusBadRequest,
	} {
		// Today's file exists because this very request is being logged.
		env.audit.Flush()
		if code, body := env.do(t, env.admin, http.MethodPost, path, ""); code != want {
			t.Fatalf("%s = %d %v", path, code, body)
		}
	}
	if code, body := env.do(t, env.admin, http.MethodPost, "/api/v1/admin/audit/archives/2026-01-02/pull", ""); code != http.StatusOK {
		t.Fatalf("pull = %d %v", code, body)
	}
	if got := env.entries(t, "user=zz"); len(got) != 0 {
		t.Fatal("pulled entries shown without pulled=1")
	}
	if got := env.entries(t, "user=zz&pulled=1"); len(got) != 1 {
		t.Fatalf("pulled entries = %v", got)
	}
	code, body = env.do(t, env.admin, http.MethodGet, "/api/v1/admin/audit/files", "")
	if code != http.StatusOK || len(body["files"].([]any)) < 2 {
		t.Fatalf("files = %v", body)
	}
	rec := performRequest(t, env.e, http.MethodGet, "/api/v1/admin/audit/files/2026-01-02?pulled=1", nil, map[string]string{"Authorization": "Bearer " + env.tokens[env.admin.ID]})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "diary.delete") || !strings.Contains(rec.Header().Get("Content-Disposition"), "2026-01-02") {
		t.Fatalf("download = %d %s", rec.Code, rec.Body.String())
	}
	for path, want := range map[string]int{
		"/api/v1/admin/audit/files/bad":        http.StatusBadRequest,
		"/api/v1/admin/audit/files/2020-01-01": http.StatusNotFound,
	} {
		if code, _ := env.do(t, env.admin, http.MethodGet, path, ""); code != want {
			t.Fatalf("%s = %d", path, code)
		}
	}
	for path, want := range map[string]int{
		"/api/v1/admin/audit/pulled/2026-01-02": http.StatusNoContent,
		"/api/v1/admin/audit/pulled/bad":        http.StatusBadRequest,
	} {
		if code, _ := env.do(t, env.admin, http.MethodDelete, path, ""); code != want {
			t.Fatalf("%s = %d", path, code)
		}
	}
	if code, _ := env.do(t, env.admin, http.MethodDelete, "/api/v1/admin/audit/pulled/2026-01-02", ""); code != http.StatusNotFound {
		t.Fatalf("remove twice = %d", code)
	}

	if code, body := env.do(t, env.admin, http.MethodPost, "/api/v1/admin/audit/cleanup", ""); code != http.StatusOK {
		t.Fatalf("cleanup = %d %v", code, body)
	}

	// Bucket failures surface as gateway errors.
	env.bucket.FailList = func(string) error { return errors.New("down") }
	for _, req := range [][2]string{{http.MethodPost, "/api/v1/admin/audit/archives/run"}, {http.MethodPost, "/api/v1/admin/audit/cleanup"}} {
		if code, _ := env.do(t, env.admin, req[0], req[1], ""); code != http.StatusBadGateway {
			t.Fatalf("%s = %d", req[1], code)
		}
	}
	if code, _ := env.do(t, env.admin, http.MethodGet, "/api/v1/admin/audit/archives", ""); code != http.StatusInternalServerError {
		t.Fatalf("archives failure = %d", code)
	}
	env.bucket.FailList = nil
	env.bucket.FailGet = func(string) error { return errors.New("down") }
	if code, _ := env.do(t, env.admin, http.MethodPost, "/api/v1/admin/audit/archives/2026-01-02/pull", ""); code != http.StatusInternalServerError {
		t.Fatalf("pull failure = %d", code)
	}

	settingsChange := findEntry(env.entries(t, "action=admin.audit_settings"), func(audit.Entry) bool { return true })
	if settingsChange == nil || settingsChange.Detail["secret_changed"] != true {
		t.Fatalf("settings change entry = %+v", settingsChange)
	}
}

func TestAdminAuditLogsQuery(t *testing.T) {
	env := newAdminTestEnv(t)
	env.do(t, nil, http.MethodPost, "/api/v1/auth/login", `{"usernameOrEmail":"ghost","password":"x"}`)
	now := time.Now().UTC()
	query := "stats=1&limit=0&tz=Asia/Shanghai&status=error&method=post&action=auth&start=" + now.Add(time.Hour).Format(time.RFC3339) + "&end=" + now.Add(-time.Hour).Format(time.RFC3339)
	code, body := env.do(t, env.admin, http.MethodGet, "/api/v1/admin/audit/logs?"+query, "")
	if code != http.StatusOK {
		t.Fatalf("logs = %d %v", code, body)
	}
	if body["total"].(float64) != 1 || len(body["entries"].([]any)) != 0 || body["stats"].(map[string]any)["login_failed"].(float64) != 1 {
		t.Fatalf("logs = %v", body)
	}
	// Top users get their names even when entries lacked them.
	env.audit.Record(audit.Entry{UserID: env.user.ID, Action: audit.ActionDiaryView})
	code, body = env.do(t, env.admin, http.MethodGet, "/api/v1/admin/audit/logs?stats=1&user="+env.user.ID, "")
	users := body["stats"].(map[string]any)["top_users"].([]any)
	if code != http.StatusOK || users[0].(map[string]any)["label"] != "bob" {
		t.Fatalf("top users = %v", users)
	}
	for _, bad := range []string{"status=6xx", "limit=x", "offset=-1", "start=yesterday", "end=soon"} {
		if code, _ := env.do(t, env.admin, http.MethodGet, "/api/v1/admin/audit/logs?"+bad, ""); code != http.StatusBadRequest {
			t.Fatalf("%s = %d", bad, code)
		}
	}
	// A broken log directory is a server error.
	root := audit.Dir(env.store.DataDir)
	env.audit.Flush()
	os.RemoveAll(root)
	os.WriteFile(root, []byte("x"), 0o600)
	for _, path := range []string{"/api/v1/admin/audit/logs", "/api/v1/admin/audit/files"} {
		if code, _ := env.do(t, env.admin, http.MethodGet, path, ""); code != http.StatusInternalServerError {
			t.Fatalf("%s = %d", path, code)
		}
	}
	env.store.Close()
	if code, _ := env.do(t, env.admin, http.MethodGet, "/api/v1/admin/overview", ""); code == http.StatusOK {
		t.Fatal("closed store should fail")
	}
}

func TestAdminRoutesWithoutAuditLogger(t *testing.T) {
	s := newTestStore(t)
	admin := newTestUser(t, s)
	e := echo.New()
	RegisterAdminRoutes(e, s, authMiddlewareFor(admin), nil, "test")
	for _, req := range [][3]string{
		{http.MethodPut, "/api/v1/admin/audit/settings", `{"retention_days":3,"archive":{"retention_days":30}}`},
		{http.MethodPost, "/api/v1/admin/audit/cleanup", ""},
		{http.MethodPost, "/api/v1/admin/audit/settings/test", `{}`},
	} {
		rec := performRequest(t, e, req[0], req[1], strings.NewReader(req[2]), map[string]string{"Content-Type": "application/json"})
		if rec.Code < 400 {
			t.Fatalf("%s = %d", req[1], rec.Code)
		}
	}
	rec := performRequest(t, e, http.MethodGet, "/api/v1/admin/audit/settings", nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"enabled":false`) {
		t.Fatalf("settings = %d %s", rec.Code, rec.Body.String())
	}
}

func TestTruncate(t *testing.T) {
	if truncate("héllo", 2) != "h" || truncate("abc", 5) != "abc" || truncate("abcdef", 3) != "abc" {
		t.Fatal("truncate")
	}
}
