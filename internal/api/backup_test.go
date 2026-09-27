package api

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/backup"
	"github.com/songtianlun/diarum/internal/store"
)

type backupTestEnv struct {
	e     *echo.Echo
	s     *store.Store
	user  *store.User
	svc   *backup.Service
	sched *backup.Scheduler
	mem   *backup.MemoryStore
}

func newBackupTestEnv(t *testing.T) *backupTestEnv {
	t.Helper()
	s := newTestStore(t)
	user := newTestUser(t, s)
	svc := backup.NewService(s, "test")
	mem := backup.NewMemoryStore()
	svc.OpenStore = func(backup.S3Config) (backup.ObjectStore, error) { return mem, nil }
	t.Cleanup(svc.Wait)
	sched := backup.NewScheduler(svc)
	e := echo.New()
	RegisterBackupRoutes(e, s, authMiddlewareFor(user), svc, sched)
	return &backupTestEnv{e: e, s: s, user: user, svc: svc, sched: sched, mem: mem}
}

func (env *backupTestEnv) do(t *testing.T, method, path, body string) (int, map[string]any) {
	t.Helper()
	rec := performRequest(t, env.e, method, path, strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	payload := map[string]any{}
	if strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		payload = decodeJSONBody(t, rec)
	}
	return rec.Code, payload
}

const validBackupSettings = `{"enabled":true,"s3":{"bucket":"b","region":"r","access_key":"a","secret":"s","prefix":"/my/backups/"},"auto_enabled":true,"schedule":"0 3 * * *","timezone":"UTC","keep":5}`

func TestBackupSettingsRoutes(t *testing.T) {
	env := newBackupTestEnv(t)

	code, payload := env.do(t, http.MethodGet, "/api/v1/backup/settings", "")
	if code != http.StatusOK || payload["enabled"] != false || payload["schedule"] != backup.DefaultSchedule || payload["keep"] != float64(backup.DefaultKeep) || payload["server_timezone"] == "" {
		t.Fatalf("GET settings = %d %#v", code, payload)
	}

	bad := map[string]string{
		`{`:                                    "Invalid request body",
		`{"keep":0}`:                           "between 1 and 100",
		`{"keep":3,"schedule":"*/5 * * * *"}`:  "Invalid schedule",
		`{"keep":3,"timezone":"Nowhere/Land"}`: "Invalid schedule",
		`{"keep":3,"enabled":true,"s3":{"bucket":"b"}}`: "Bucket, region, access key and secret are required for S3",
	}
	for body, want := range bad {
		code, payload := env.do(t, http.MethodPut, "/api/v1/backup/settings", body)
		if code != http.StatusBadRequest || !strings.Contains(payload["message"].(string), want) {
			t.Fatalf("PUT %s = %d %#v, want %q", body, code, payload, want)
		}
	}

	// Switching backups on proves the destination works.
	env.mem.FailPut = func(string) error { return errors.New("access denied") }
	code, payload = env.do(t, http.MethodPut, "/api/v1/backup/settings", validBackupSettings)
	if code != http.StatusBadRequest || !strings.Contains(payload["message"].(string), "S3 check failed: write failed: access denied") {
		t.Fatalf("PUT with failing S3 = %d %#v", code, payload)
	}
	env.mem.FailPut = nil
	code, payload = env.do(t, http.MethodPut, "/api/v1/backup/settings", validBackupSettings)
	if code != http.StatusOK {
		t.Fatalf("PUT settings = %d %#v", code, payload)
	}
	saved := payload["settings"].(map[string]any)
	if saved["s3"].(map[string]any)["prefix"] != "my/backups" || saved["next_run"] == nil || saved["keep"] != float64(5) {
		t.Fatalf("saved settings = %#v", saved)
	}

	// Unchanged destination: no new check, so a flaky S3 does not block
	// changing other settings.
	env.mem.FailPut = func(string) error { return errors.New("flaky") }
	code, payload = env.do(t, http.MethodPut, "/api/v1/backup/settings", strings.Replace(validBackupSettings, `"keep":5`, `"keep":7`, 1))
	if code != http.StatusOK {
		t.Fatalf("PUT unchanged destination = %d %#v", code, payload)
	}
	env.mem.FailPut = nil

	// Blank schedule falls back to the default; disabling needs no S3.
	code, payload = env.do(t, http.MethodPut, "/api/v1/backup/settings", `{"enabled":false,"keep":2,"schedule":" "}`)
	if code != http.StatusOK || payload["settings"].(map[string]any)["schedule"] != backup.DefaultSchedule {
		t.Fatalf("PUT disabled = %d %#v", code, payload)
	}
	if _, ok := env.sched.NextRun(env.user.ID); ok {
		t.Fatal("disabling should unschedule")
	}
}

func TestBackupHelperRoutes(t *testing.T) {
	env := newBackupTestEnv(t)

	code, payload := env.do(t, http.MethodPost, "/api/v1/backup/test", `{"bucket":"b","region":"r","access_key":"a","secret":"s"}`)
	if code != http.StatusOK || payload["success"] != true {
		t.Fatalf("test = %d %#v", code, payload)
	}
	code, payload = env.do(t, http.MethodPost, "/api/v1/backup/test", `{"bucket":"b"}`)
	if code != http.StatusOK || payload["success"] != false {
		t.Fatalf("test incomplete = %d %#v", code, payload)
	}

	code, payload = env.do(t, http.MethodPost, "/api/v1/backup/schedule/preview", `{"schedule":"0 2 * * *","timezone":"Asia/Shanghai"}`)
	if code != http.StatusOK || payload["valid"] != true || len(payload["next_runs"].([]any)) != 5 || payload["timezone"] != "Asia/Shanghai" {
		t.Fatalf("preview = %d %#v", code, payload)
	}
	if !strings.HasSuffix(payload["next_runs"].([]any)[0].(string), "T02:00:00+08:00") {
		t.Fatalf("preview run = %v", payload["next_runs"])
	}
	code, payload = env.do(t, http.MethodPost, "/api/v1/backup/schedule/preview", `{"schedule":"nope"}`)
	if code != http.StatusOK || payload["valid"] != false || payload["error"] == "" {
		t.Fatalf("preview invalid = %d %#v", code, payload)
	}

	for _, path := range []string{"/api/v1/backup/test", "/api/v1/backup/schedule/preview"} {
		if code, _ := env.do(t, http.MethodPost, path, `{`); code != http.StatusBadRequest {
			t.Fatalf("%s with a bad body = %d", path, code)
		}
	}
}

func TestBackupLifecycleRoutes(t *testing.T) {
	env := newBackupTestEnv(t)

	// Not enabled yet.
	if code, _ := env.do(t, http.MethodPost, "/api/v1/backup/backups", ""); code != http.StatusBadRequest {
		t.Fatalf("start while disabled = %d", code)
	}
	if code, _ := env.do(t, http.MethodPut, "/api/v1/backup/settings", validBackupSettings); code != http.StatusOK {
		t.Fatalf("enable = %d", code)
	}
	if _, err := env.s.InsertImportedDiary(env.user.ID, "", "2025-01-01", "hello", "", ""); err != nil {
		t.Fatalf("InsertImportedDiary: %v", err)
	}

	code, payload := env.do(t, http.MethodPost, "/api/v1/backup/backups", "")
	if code != http.StatusAccepted || payload["job"].(map[string]any)["kind"] != "backup" {
		t.Fatalf("start = %d %#v", code, payload)
	}
	env.svc.Wait()
	code, payload = env.do(t, http.MethodGet, "/api/v1/backup/status", "")
	job := payload["job"].(map[string]any)
	if code != http.StatusOK || job["status"] != "success" || payload["next_run"] == nil {
		t.Fatalf("status = %d %#v", code, payload)
	}
	id := job["backup_id"].(string)

	code, payload = env.do(t, http.MethodGet, "/api/v1/backup/backups", "")
	list := payload["backups"].([]any)
	if code != http.StatusOK || len(list) != 1 || list[0].(map[string]any)["id"] != id {
		t.Fatalf("list = %d %#v", code, payload)
	}
	code, payload = env.do(t, http.MethodGet, "/api/v1/backup/backups/"+id+"/log", "")
	if code != http.StatusOK || payload["status"] != "success" {
		t.Fatalf("log = %d %#v", code, payload)
	}
	rec := performRequest(t, env.e, http.MethodGet, "/api/v1/backup/backups/"+id+"/download", nil, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/zip" || !strings.Contains(rec.Header().Get("Content-Disposition"), id) {
		t.Fatalf("download = %d %v", rec.Code, rec.Header())
	}
	if entries := zipEntries(t, rec.Body.Bytes()); entries["diarum_export.json"] == nil {
		t.Fatalf("downloaded archive entries = %v", entries)
	}

	code, payload = env.do(t, http.MethodPost, "/api/v1/backup/backups/"+id+"/restore", "")
	if code != http.StatusAccepted || payload["job"].(map[string]any)["kind"] != "restore" {
		t.Fatalf("restore = %d %#v", code, payload)
	}
	env.svc.Wait()
	_, payload = env.do(t, http.MethodGet, "/api/v1/backup/status", "")
	if stats := payload["job"].(map[string]any)["import_stats"].(map[string]any); stats["diaries"].(map[string]any)["skipped"] != float64(1) {
		t.Fatalf("restore stats = %#v", stats)
	}

	if code, _ := env.do(t, http.MethodDelete, "/api/v1/backup/backups/"+id, ""); code != http.StatusOK {
		t.Fatalf("delete = %d", code)
	}
	if code, _ := env.do(t, http.MethodGet, "/api/v1/backup/backups/"+id+"/log", ""); code != http.StatusNotFound {
		t.Fatalf("log after delete = %d", code)
	}
	if code, _ := env.do(t, http.MethodGet, "/api/v1/backup/backups/"+id+"/download", ""); code != http.StatusNotFound {
		t.Fatalf("download after delete = %d", code)
	}
	for _, req := range [][2]string{{http.MethodDelete, "/api/v1/backup/backups/bad-id"}, {http.MethodPost, "/api/v1/backup/backups/bad-id/restore"}} {
		if code, _ := env.do(t, req[0], req[1], ""); code != http.StatusBadRequest {
			t.Fatalf("%s %s = %d", req[0], req[1], code)
		}
	}

	env.mem.FailList = func(string) error { return errors.New("list denied") }
	if code, payload := env.do(t, http.MethodGet, "/api/v1/backup/backups", ""); code != http.StatusBadGateway || !strings.Contains(payload["message"].(string), "list denied") {
		t.Fatalf("list failure = %d %#v", code, payload)
	}
	if err := backupError("x", backup.ErrBusy); err.(*echo.HTTPError).Code != http.StatusConflict {
		t.Fatalf("busy maps to %v", err)
	}
}
