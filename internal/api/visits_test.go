package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	iauth "github.com/songtianlun/diarum/internal/auth"
	"github.com/songtianlun/diarum/internal/backup"
	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/store"
	"github.com/songtianlun/diarum/internal/visits"
)

type visitTestEnv struct {
	e       *echo.Echo
	store   *store.Store
	tracker *visits.Tracker
	bucket  *backup.MemoryStore
	admin   *store.User
	user    *store.User
	tokens  map[string]string
	shared  backup.S3Config
}

func newVisitTestEnv(t *testing.T, enable bool) *visitTestEnv {
	t.Helper()
	s := newTestStore(t)
	env := &visitTestEnv{store: s, bucket: backup.NewMemoryStore(), tokens: map[string]string{}}
	tracker, err := visits.New(s.DataDir, visits.Options{
		Settings:       s,
		Location:       time.UTC,
		SharedS3:       func() backup.S3Config { return env.shared },
		NewObjectStore: func(backup.S3Config) (backup.ObjectStore, error) { return env.bucket, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tracker.Close)
	env.tracker = tracker
	if enable {
		settings := visits.DefaultSettings()
		settings.Enabled = true
		if _, err := tracker.UpdateSettings(settings); err != nil {
			t.Fatal(err)
		}
	}
	authService := iauth.NewService(s)
	e := echo.New()
	e.Use(VisitMiddleware(tracker, s))
	e.Use(middleware.Recover())
	RegisterDiaryRoutes(e, s, authService.Middleware, nil)
	RegisterPublicRoutes(e, s)
	RegisterMCPRoutes(e, s, "test")
	RegisterVisitRoutes(e, s, authService.Middleware, tracker)
	RegisterAdminVisitRoutes(e, s, authService.Middleware, tracker)
	env.e = e

	hash, _ := authService.HashPassword("pw")
	if env.admin, err = s.CreateUser("root", "root@example.com", hash); err != nil {
		t.Fatal(err)
	}
	if env.user, err = s.CreateUser("bob", "bob@example.com", hash); err != nil {
		t.Fatal(err)
	}
	for _, u := range []*store.User{env.admin, env.user} {
		token, err := authService.IssueToken(u)
		if err != nil {
			t.Fatal(err)
		}
		env.tokens[u.ID] = token
	}
	return env
}

func (env *visitTestEnv) do(t *testing.T, as *store.User, method, path, body string, extra ...string) (int, map[string]any) {
	t.Helper()
	headers := map[string]string{"Content-Type": "application/json", "User-Agent": "test-agent"}
	if as != nil {
		headers["Authorization"] = "Bearer " + env.tokens[as.ID]
	}
	for i := 0; i+1 < len(extra); i += 2 {
		headers[extra[i]] = extra[i+1]
	}
	rec := performRequest(t, env.e, method, path, strings.NewReader(body), headers)
	payload := map[string]any{}
	if rec.Body.Len() > 0 && strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	}
	return rec.Code, payload
}

func (env *visitTestEnv) logs(t *testing.T, as *store.User, query string) []visits.Visit {
	t.Helper()
	path := "/api/v1/visits/logs?limit=200&" + query
	if as == env.admin && strings.Contains(query, "owner=") {
		path = "/api/v1/admin/visits/logs?limit=200&" + query
	}
	code, body := env.do(t, as, http.MethodGet, path, "")
	if code != http.StatusOK {
		t.Fatalf("logs %s: %d %v", query, code, body)
	}
	raw, _ := json.Marshal(body["visits"])
	var out []visits.Visit
	_ = json.Unmarshal(raw, &out)
	return out
}

func TestVisitTrackingRecordsReadsAndAttempts(t *testing.T) {
	env := newVisitTestEnv(t, true)
	bob := env.user
	diary, _, err := env.store.UpsertDiary(bob.ID, "2026-09-20", "<p>secret</p>", "", "")
	if err != nil {
		t.Fatal(err)
	}

	// Bob reads his entry three times from his phone: within a minute it
	// counts once. Reading from the laptop counts again.
	for i := 0; i < 3; i++ {
		if code, _ := env.do(t, bob, http.MethodGet, "/api/v1/diaries/by-date/2026-09-20", "", DeviceHeader, "phone"); code != http.StatusOK {
			t.Fatal(code)
		}
	}
	env.do(t, bob, http.MethodGet, "/api/v1/diaries/"+diary.ID, "", DeviceHeader, "laptop")
	env.do(t, bob, http.MethodPost, "/api/v1/diaries/by-ids", `{"ids":["`+diary.ID+`"]}`, DeviceHeader, "tablet")
	// A day without an entry is not a visit.
	env.do(t, bob, http.MethodGet, "/api/v1/diaries/by-date/2026-01-01", "")

	// The admin tries to open bob's entry by ID: refused, but bob sees it.
	if code, _ := env.do(t, env.admin, http.MethodGet, "/api/v1/diaries/"+diary.ID, ""); code != http.StatusForbidden {
		t.Fatalf("cross-user read = %d", code)
	}
	// Someone signed out tries too, and asks for a date.
	if code, _ := env.do(t, nil, http.MethodGet, "/api/v1/diaries/"+diary.ID, ""); code != http.StatusUnauthorized {
		t.Fatal(code)
	}
	env.do(t, nil, http.MethodGet, "/api/v1/diaries/by-date/2026-09-20", "")
	env.do(t, nil, http.MethodGet, "/api/v1/diaries/missing-id", "")
	env.do(t, nil, http.MethodGet, "/api/v1/diaries/revisions/r1", "")
	env.do(t, nil, http.MethodGet, "/api/v1/diaries?token=wrong&date=2026-09-20", "")
	env.do(t, nil, http.MethodGet, "/api/v1/diaries?token=wrong&start=2026-09-01&end=2026-09-30", "")
	// Untracked routes record nothing.
	env.do(t, bob, http.MethodGet, "/api/v1/diaries/recent", "")

	mine := env.logs(t, bob, "")
	if len(mine) != 5 {
		t.Fatalf("bob sees %d visits: %+v", len(mine), mine)
	}
	var ok, refused, anon int
	for _, v := range mine {
		if v.Success {
			ok++
			if !v.Self || v.VisitorID != bob.ID || v.DiaryDate != "2026-09-20" || !strings.HasPrefix(v.Device, "d:") {
				t.Fatalf("own read = %+v", v)
			}
		} else {
			refused++
			if v.VisitorID == "" {
				anon++
			} else if v.VisitorID != env.admin.ID || v.Visitor != "root" || v.Reason != visits.ReasonForbidden || v.Status != 403 {
				t.Fatalf("refused read = %+v", v)
			}
		}
	}
	if ok != 3 || refused != 2 || anon != 1 {
		t.Fatalf("ok=%d refused=%d anon=%d", ok, refused, anon)
	}

	// Attempts that could not be tied to an owner are the admin's to see.
	unattributed := env.logs(t, env.admin, "owner=-")
	if len(unattributed) != 5 {
		t.Fatalf("unattributed = %+v", unattributed)
	}
	sources := map[string]int{}
	for _, v := range unattributed {
		sources[v.Source+"/"+v.Kind]++
		if v.Success || v.VisitorID != "" {
			t.Fatalf("unattributed = %+v", v)
		}
	}
	if sources["api/view"] != 1 || sources["api/range"] != 1 || sources["web/revision"] != 1 || sources["web/view"] != 2 {
		t.Fatalf("sources = %v", sources)
	}
	if all := env.logs(t, env.admin, "owner=*"); len(all) != 10 {
		t.Fatalf("all = %d", len(all))
	}
}

func TestVisitTrackingAPIAndMCP(t *testing.T) {
	env := newVisitTestEnv(t, true)
	bob := env.user
	for _, date := range []string{"2026-09-20", "2026-09-21"} {
		if _, _, err := env.store.UpsertDiary(bob.ID, date, "text "+date, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	cs := config.NewConfigService(env.store)
	for key, value := range map[string]any{"api.token": "tok", "api.enabled": true, "api.mcp_enabled": true} {
		if err := cs.Set(bob.ID, key, value); err != nil {
			t.Fatal(err)
		}
	}
	env.do(t, nil, http.MethodGet, "/api/v1/diaries?token=tok&date=2026-09-20", "")
	env.do(t, nil, http.MethodGet, "/api/v1/diaries?token=tok&start=2026-09-01&end=2026-09-30", "")
	env.do(t, nil, http.MethodPost, "/api/v1/mcp?token=tok", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_diary","arguments":{"date":"2026-09-21"}}}`)
	env.do(t, nil, http.MethodPost, "/api/v1/mcp?token=tok", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_diaries","arguments":{"start":"2026-09-01","end":"2026-09-30"}}}`)

	got := env.logs(t, bob, "")
	kinds := map[string]int{}
	for _, v := range got {
		kinds[v.Source+"/"+v.Kind]++
		if !v.Success || !v.Self {
			t.Fatalf("visit = %+v", v)
		}
	}
	if kinds["api/view"] != 1 || kinds["api/range"] != 2 || kinds["mcp/view"] != 1 || kinds["mcp/range"] != 2 {
		t.Fatalf("kinds = %v (%d)", kinds, len(got))
	}
}

func TestVisitStatsEndpoints(t *testing.T) {
	env := newVisitTestEnv(t, true)
	bob := env.user
	diary, _, _ := env.store.UpsertDiary(bob.ID, "2026-09-20", "x", "", "")
	env.do(t, bob, http.MethodGet, "/api/v1/diaries/by-date/2026-09-20", "", DeviceHeader, "a")
	env.do(t, env.admin, http.MethodGet, "/api/v1/diaries/"+diary.ID, "")
	revisionReader := func() {
		revs, _ := env.store.ListDiaryRevisions(bob.ID, "2026-09-20")
		for _, r := range revs {
			env.do(t, bob, http.MethodGet, "/api/v1/diaries/revisions/"+r.ID, "")
		}
	}
	env.store.UpsertDiary(bob.ID, "2026-09-20", "y", "", "")
	revisionReader()

	code, status := env.do(t, bob, http.MethodGet, "/api/v1/visits/status", "")
	if code != http.StatusOK || status["enabled"] != true {
		t.Fatalf("status = %d %v", code, status)
	}
	code, summary := env.do(t, bob, http.MethodGet, "/api/v1/visits/summary?days=7&tz_offset=480", "")
	if code != http.StatusOK {
		t.Fatalf("summary = %d %v", code, summary)
	}
	totals := summary["totals"].(map[string]any)
	if totals["failed"].(float64) != 1 || totals["views"].(float64) < 2 {
		t.Fatalf("totals = %v", totals)
	}
	visitors := summary["top_visitors"].([]any)
	names := []string{}
	for _, v := range visitors {
		names = append(names, fmt.Sprint(v.(map[string]any)["visitor"]))
	}
	if !strings.Contains(strings.Join(names, ","), "root") {
		t.Fatalf("names = %v", names)
	}

	for _, path := range []string{
		"/api/v1/visits/diaries?sort=last",
		"/api/v1/visits/diaries/2026-09-20",
		"/api/v1/visits/visitors?diary=2026-09-20",
		"/api/v1/visits/logs?result=failed&others=1&start=2020-01-01&end=2030-01-01&q=test",
		"/api/v1/visits/logs?start=2030-01-01T00:00:00Z&end=2020-01-01",
	} {
		if code, body := env.do(t, bob, http.MethodGet, path, ""); code != http.StatusOK {
			t.Fatalf("%s = %d %v", path, code, body)
		}
	}
	_, detail := env.do(t, bob, http.MethodGet, "/api/v1/visits/diaries/2026-09-20", "")
	if detail["stats"] == nil || len(detail["visitors"].([]any)) != 2 {
		t.Fatalf("detail = %v", detail)
	}
	for _, path := range []string{
		"/api/v1/visits/logs?result=maybe",
		"/api/v1/visits/logs?start=yesterday",
		"/api/v1/visits/logs?end=tomorrow",
		"/api/v1/visits/logs?limit=-1",
		"/api/v1/visits/logs?offset=x",
		"/api/v1/visits/diaries?limit=x",
		"/api/v1/visits/diaries?offset=x",
		"/api/v1/visits/visitors?limit=x",
		"/api/v1/visits/visitors?offset=x",
		"/api/v1/visits/summary?days=x",
		"/api/v1/visits/summary?result=x",
		"/api/v1/visits/diaries?result=x",
		"/api/v1/visits/visitors?result=x",
		"/api/v1/visits/diaries/" + strings.Repeat("9", 40),
		"/api/v1/visits/diaries/x?result=x",
		"/api/v1/visits/logs?visitor=" + strings.Repeat("v", 70),
	} {
		if code, _ := env.do(t, bob, http.MethodGet, path, ""); code != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400", path, code)
		}
	}
	// Other users' data stays out of reach: the owner is always the caller.
	_, adminSummary := env.do(t, env.admin, http.MethodGet, "/api/v1/visits/summary", "")
	if adminSummary["totals"].(map[string]any)["views"].(float64) != 0 {
		t.Fatalf("admin sees bob's visits through the user API: %v", adminSummary["totals"])
	}
	if code, _ := env.do(t, nil, http.MethodGet, "/api/v1/visits/summary", ""); code != http.StatusUnauthorized {
		t.Fatal("anonymous summary")
	}
}

func TestVisitRoutesWhenDisabled(t *testing.T) {
	env := newVisitTestEnv(t, false)
	diary, _, _ := env.store.UpsertDiary(env.user.ID, "2026-09-20", "x", "", "")
	env.do(t, env.user, http.MethodGet, "/api/v1/diaries/"+diary.ID, "")
	env.do(t, nil, http.MethodGet, "/api/v1/diaries/"+diary.ID, "")
	env.tracker.Flush()
	if storage, _ := env.tracker.Storage(); storage.Records != 0 {
		t.Fatal("recorded while disabled")
	}
	code, status := env.do(t, env.user, http.MethodGet, "/api/v1/visits/status", "")
	if code != http.StatusOK || status["enabled"] != false {
		t.Fatalf("status = %v", status)
	}
	if code, _ := env.do(t, env.user, http.MethodGet, "/api/v1/visits/summary", ""); code != http.StatusForbidden {
		t.Fatalf("summary while disabled = %d", code)
	}
	// The admin can still look at what was recorded before.
	if code, _ := env.do(t, env.admin, http.MethodGet, "/api/v1/admin/visits/summary?owner=*", ""); code != http.StatusOK {
		t.Fatalf("admin summary = %d", code)
	}
}

func TestAdminVisitRoutes(t *testing.T) {
	env := newVisitTestEnv(t, true)
	bob := env.user
	diary, _, _ := env.store.UpsertDiary(bob.ID, "2026-09-20", "x", "", "")
	env.do(t, bob, http.MethodGet, "/api/v1/diaries/"+diary.ID, "")
	env.do(t, nil, http.MethodGet, "/api/v1/diaries/"+diary.ID, "")

	if code, _ := env.do(t, bob, http.MethodGet, "/api/v1/admin/visits/settings", ""); code != http.StatusForbidden {
		t.Fatalf("non-admin settings = %d", code)
	}
	code, settings := env.do(t, env.admin, http.MethodGet, "/api/v1/admin/visits/settings", "")
	if code != http.StatusOK || settings["enabled"] != true || settings["shared_s3_available"] != false || settings["limits"] == nil {
		t.Fatalf("settings = %d %v", code, settings)
	}

	code, overview := env.do(t, env.admin, http.MethodGet, "/api/v1/admin/visits/overview", "")
	if code != http.StatusOK || overview["storage"].(map[string]any)["records"].(float64) != 2 {
		t.Fatalf("overview = %d %v", code, overview)
	}
	code, owners := env.do(t, env.admin, http.MethodGet, "/api/v1/admin/visits/owners?sort=failed", "")
	if code != http.StatusOK || owners["total"].(float64) != 1 {
		t.Fatalf("owners = %d %v", code, owners)
	}
	if row := owners["owners"].([]any)[0].(map[string]any); row["username"] != "bob" || row["failed"].(float64) != 1 {
		t.Fatalf("owner row = %v", row)
	}
	for _, path := range []string{
		"/api/v1/admin/visits/summary?owner=" + bob.ID,
		"/api/v1/admin/visits/diaries?owner=" + bob.ID,
		"/api/v1/admin/visits/diaries/2026-09-20?owner=" + bob.ID,
		"/api/v1/admin/visits/visitors?owner=-",
		"/api/v1/admin/visits/logs?owner=*",
	} {
		if code, body := env.do(t, env.admin, http.MethodGet, path, ""); code != http.StatusOK {
			t.Fatalf("%s = %d %v", path, code, body)
		}
	}
	for _, path := range []string{
		"/api/v1/admin/visits/summary",
		"/api/v1/admin/visits/logs?owner=" + strings.Repeat("x", 70),
		"/api/v1/admin/visits/logs?owner=*&result=x",
		"/api/v1/admin/visits/owners?limit=x",
		"/api/v1/admin/visits/owners?offset=x",
	} {
		if code, _ := env.do(t, env.admin, http.MethodGet, path, ""); code != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400", path, code)
		}
	}

	// Settings round trip; the secret never comes back.
	body := `{"enabled":true,"retention_days":0,"dedupe_seconds":30,"max_records":100000,"ip_limit_per_minute":10,"global_failed_per_minute":100,"owner_limit_per_minute":100,
		"archive":{"enabled":true,"source":"custom","s3":{"bucket":"b","region":"r","access_key":"a","secret":"s"},"prefix":"v","retention_days":0}}`
	code, saved := env.do(t, env.admin, http.MethodPut, "/api/v1/admin/visits/settings", body)
	if code != http.StatusOK || saved["secret_set"] != true || saved["retention_days"].(float64) != 0 {
		t.Fatalf("save = %d %v", code, saved)
	}
	if s3 := saved["archive"].(map[string]any)["s3"].(map[string]any); s3["secret"] != "" {
		t.Fatalf("secret leaked: %v", s3)
	}
	if code, _ := env.do(t, env.admin, http.MethodPut, "/api/v1/admin/visits/settings", `{"max_records":1}`); code != http.StatusBadRequest {
		t.Fatal("invalid settings accepted")
	}
	if code, _ := env.do(t, env.admin, http.MethodPut, "/api/v1/admin/visits/settings", `{`); code != http.StatusBadRequest {
		t.Fatal("bad json accepted")
	}
	if code, body := env.do(t, env.admin, http.MethodPost, "/api/v1/admin/visits/settings/test", `{"source":"custom","s3":{"bucket":"b","region":"r","access_key":"a"}}`); code != http.StatusOK {
		t.Fatalf("test = %d %v", code, body)
	}
	if code, _ := env.do(t, env.admin, http.MethodPost, "/api/v1/admin/visits/settings/test", `{"source":"audit"}`); code != http.StatusBadRequest {
		t.Fatal("shared test without S3 passed")
	}
	if code, _ := env.do(t, env.admin, http.MethodPost, "/api/v1/admin/visits/settings/test", `{`); code != http.StatusBadRequest {
		t.Fatal("bad json accepted")
	}

	// Archive: move bob's visit to yesterday so it can be archived.
	env.tracker.Flush()
	if code, body := env.do(t, env.admin, http.MethodPost, "/api/v1/admin/visits/archives/run", ""); code != http.StatusOK {
		t.Fatalf("archive run = %d %v", code, body)
	}
	if code, body := env.do(t, env.admin, http.MethodPost, "/api/v1/admin/visits/cleanup", ""); code != http.StatusOK {
		t.Fatalf("cleanup = %d %v", code, body)
	}
	code, archives := env.do(t, env.admin, http.MethodGet, "/api/v1/admin/visits/archives", "")
	if code != http.StatusOK || archives["enabled"] != true {
		t.Fatalf("archives = %d %v", code, archives)
	}
	if code, body := env.do(t, env.admin, http.MethodPost, "/api/v1/admin/visits/archives/pull", `{}`); code != http.StatusOK {
		t.Fatalf("pull = %d %v", code, body)
	}
	if code, _ := env.do(t, env.admin, http.MethodPost, "/api/v1/admin/visits/archives/pull", `{"days":["x"]}`); code != http.StatusBadRequest {
		t.Fatal("bad day accepted")
	}
	if code, _ := env.do(t, env.admin, http.MethodPost, "/api/v1/admin/visits/archives/pull", `{"days":["2001-01-01"]}`); code != http.StatusNotFound {
		t.Fatal("missing day found")
	}
	if code, _ := env.do(t, env.admin, http.MethodPost, "/api/v1/admin/visits/archives/pull", `{`); code != http.StatusBadRequest {
		t.Fatal("bad json accepted")
	}
	many := `{"days":[` + strings.TrimSuffix(strings.Repeat(`"2026-01-01",`, 4001), ",") + `]}`
	if code, _ := env.do(t, env.admin, http.MethodPost, "/api/v1/admin/visits/archives/pull", many); code != http.StatusBadRequest {
		t.Fatal("too many days accepted")
	}
	if code, body := env.do(t, env.admin, http.MethodDelete, "/api/v1/admin/visits/pulled", ""); code != http.StatusOK {
		t.Fatalf("unload = %d %v", code, body)
	}
	if code, _ := env.do(t, env.admin, http.MethodDelete, "/api/v1/admin/visits/pulled?day=x", ""); code != http.StatusBadRequest {
		t.Fatal("bad day accepted")
	}

	// With the archive off, archive actions explain why.
	off := `{"enabled":true,"retention_days":30,"dedupe_seconds":60,"max_records":100000,"ip_limit_per_minute":10,"global_failed_per_minute":100,"owner_limit_per_minute":100,"archive":{"enabled":false}}`
	if code, _ := env.do(t, env.admin, http.MethodPut, "/api/v1/admin/visits/settings", off); code != http.StatusOK {
		t.Fatal("disable archive")
	}
	if code, _ := env.do(t, env.admin, http.MethodPost, "/api/v1/admin/visits/archives/run", ""); code != http.StatusBadRequest {
		t.Fatal("archive ran while off")
	}
	if code, body := env.do(t, env.admin, http.MethodGet, "/api/v1/admin/visits/archives", ""); code != http.StatusOK || body["enabled"] != false {
		t.Fatalf("archives while off = %v", body)
	}
	if code, _ := env.do(t, env.admin, http.MethodPost, "/api/v1/admin/visits/archives/pull", `{}`); code != http.StatusConflict {
		t.Fatal("pull while off")
	}
}

func TestAdminVisitRoutesWithoutTracker(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	RegisterVisitRoutes(e, s, authMiddlewareFor(user), nil)
	RegisterAdminVisitRoutes(e, s, authMiddlewareFor(&store.User{ID: user.ID, Role: store.RoleAdmin}), nil)
	e.Use(VisitMiddleware(nil, s))
	rec := performRequest(t, e, http.MethodGet, "/api/v1/admin/visits/settings", nil, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("settings without tracker = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/visits/status", nil, nil)
	if rec.Code != http.StatusOK || decodeJSONBody(t, rec)["enabled"] != false {
		t.Fatal("status without tracker")
	}
}

func TestParseVisitTime(t *testing.T) {
	if got, err := parseVisitTime("2026-01-02", true); err != nil || got.Hour() != 23 {
		t.Fatalf("end of day = %v %v", got, err)
	}
	if got, _ := parseVisitTime("", false); !got.IsZero() {
		t.Fatal("empty time")
	}
}
