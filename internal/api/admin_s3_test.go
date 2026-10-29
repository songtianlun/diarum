package api

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/audit"
	"github.com/songtianlun/diarum/internal/backup"
	"github.com/songtianlun/diarum/internal/store"
	"github.com/songtianlun/diarum/internal/systems3"
)

func TestAdminSystemS3Routes(t *testing.T) {
	s := newTestStore(t)
	bucket := backup.NewMemoryStore()
	var connectErr error
	factory := func(backup.S3Config) (backup.ObjectStore, error) { return bucket, connectErr }
	sys := systems3.New(s, factory)
	l, err := audit.New(s.DataDir, audit.Options{Settings: s, Location: time.UTC, NewObjectStore: factory, SystemS3: sys.Func(systems3.FolderAudit)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Close)
	admin := &store.User{ID: "admin", Role: store.RoleAdmin}
	e := echo.New()
	RegisterAdminRoutes(e, s, authMiddlewareFor(admin), l, "test")
	RegisterAdminS3Routes(e, authMiddlewareFor(admin), sys, l, nil)
	do := func(method, path, body string) (int, map[string]any) {
		t.Helper()
		rec := performRequest(t, e, method, path, strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
		payload := map[string]any{}
		if strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			payload = decodeJSONBody(t, rec)
		}
		return rec.Code, payload
	}

	code, body := do(http.MethodGet, "/api/v1/admin/s3", "")
	paths, _ := body["paths"].(map[string]any)
	if code != http.StatusOK || body["configured"] != false || paths["user_backups"] != "diarum/user-backups/" || len(body["used_by"].([]any)) != 0 {
		t.Fatalf("get = %d %v", code, body)
	}
	for _, bad := range []string{`{bad`, `{"s3":{"bucket":"b"}}`} {
		if code, _ := do(http.MethodPut, "/api/v1/admin/s3", bad); code != http.StatusBadRequest {
			t.Fatalf("%s = %d", bad, code)
		}
	}
	conn := `{"bucket":"b","region":"r","access_key":"ak","secret":"sk","prefix":"/acme/"}`
	connectErr = errors.New("unreachable")
	if code, body := do(http.MethodPut, "/api/v1/admin/s3", `{"s3":`+conn+`}`); code != http.StatusBadRequest || !strings.Contains(body["message"].(string), "unreachable") {
		t.Fatalf("unreachable = %d %v", code, body)
	}
	if code, _ := do(http.MethodPost, "/api/v1/admin/s3/test", `{"s3":`+conn+`}`); code != http.StatusBadRequest {
		t.Fatalf("test unreachable = %d", code)
	}
	connectErr = nil
	if code, _ := do(http.MethodPost, "/api/v1/admin/s3/test", `{bad`); code != http.StatusBadRequest {
		t.Fatalf("test bad body = %d", code)
	}
	if code, body := do(http.MethodPost, "/api/v1/admin/s3/test", `{"s3":`+conn+`}`); code != http.StatusOK || body["ok"] != true {
		t.Fatalf("test = %d %v", code, body)
	}
	code, body = do(http.MethodPut, "/api/v1/admin/s3", `{"s3":`+conn+`,"allow_user_backups":true}`)
	s3, _ := body["s3"].(map[string]any)
	paths, _ = body["paths"].(map[string]any)
	if code != http.StatusOK || body["configured"] != true || body["secret_set"] != true || s3["secret"] != "" || s3["prefix"] != "acme" || paths["audit"] != "acme/audit/" || body["allow_user_backups"] != true {
		t.Fatalf("save = %d %v", code, body)
	}
	// Saving again without the secret keeps it and skips the probe.
	connectErr = errors.New("not called")
	if code, body := do(http.MethodPut, "/api/v1/admin/s3", `{"s3":{"bucket":"b","region":"r","access_key":"ak","prefix":"acme"}}`); code != http.StatusOK || body["secret_set"] != true || body["allow_user_backups"] != false {
		t.Fatalf("resave = %d %v", code, body)
	}
	connectErr = nil

	// The audit archive can now use it.
	code, body = do(http.MethodGet, "/api/v1/admin/audit/settings", "")
	if code != http.StatusOK || body["system_s3_available"] != true || body["system_s3_path"] != "acme/audit/" {
		t.Fatalf("audit settings = %d %v", code, body)
	}
	if code, body := do(http.MethodPost, "/api/v1/admin/audit/settings/test", `{"source":"system"}`); code != http.StatusOK {
		t.Fatalf("audit test = %d %v", code, body)
	}
	if code, body := do(http.MethodPut, "/api/v1/admin/audit/settings", `{"retention_days":3,"archive":{"enabled":true,"source":"system","retention_days":30}}`); code != http.StatusOK {
		t.Fatalf("audit save = %d %v", code, body)
	}
	if _, body := do(http.MethodGet, "/api/v1/admin/s3", ""); len(body["used_by"].([]any)) != 1 {
		t.Fatalf("used_by = %v", body["used_by"])
	}
	// It cannot be removed while the archive depends on it.
	if code, body := do(http.MethodPut, "/api/v1/admin/s3", `{"s3":{}}`); code != http.StatusBadRequest || !strings.Contains(body["message"].(string), "audit log") {
		t.Fatalf("remove in use = %d %v", code, body)
	}
	if code, _ := do(http.MethodPut, "/api/v1/admin/audit/settings", `{"retention_days":3,"archive":{"enabled":false,"source":"system","retention_days":30}}`); code != http.StatusOK {
		t.Fatal("archive not turned off")
	}
	if code, body := do(http.MethodPut, "/api/v1/admin/s3", `{"s3":{}}`); code != http.StatusOK || body["configured"] != false {
		t.Fatalf("remove = %d %v", code, body)
	}
	if joinUses([]string{"audit", "visits"}) != "audit log and visitor statistics" {
		t.Fatal("joinUses")
	}
}

func TestBackupSettingsWithSystemS3(t *testing.T) {
	env := newBackupTestEnv(t)
	systemOn := false
	env.svc.SystemS3 = func() (backup.S3Config, bool) {
		return backup.S3Config{Bucket: "sys", Region: "r", AccessKey: "ak", Secret: "sk", Prefix: "diarum/user-backups"}, systemOn
	}
	body := `{"enabled":true,"source":"system","s3":{"prefix":""},"schedule":"0 3 * * *","keep":3}`
	if code, payload := env.do(t, http.MethodPut, "/api/v1/backup/settings", body); code != http.StatusBadRequest || !strings.Contains(payload["message"].(string), "system S3") {
		t.Fatalf("unavailable = %d %v", code, payload)
	}
	if code, payload := env.do(t, http.MethodGet, "/api/v1/backup/settings", ""); code != http.StatusOK || payload["system_s3_available"] != false || payload["source"] != "system" {
		t.Fatalf("get = %d %v", code, payload)
	}
	if code, _ := env.do(t, http.MethodPut, "/api/v1/backup/settings", `{"enabled":false,"source":"ftp","keep":3}`); code != http.StatusBadRequest {
		t.Fatal("unknown source accepted")
	}

	systemOn = true
	code, payload := env.do(t, http.MethodPut, "/api/v1/backup/settings", body)
	settings, _ := payload["settings"].(map[string]any)
	if code != http.StatusOK || settings["source"] != "system" || settings["system_s3_path"] != "diarum/user-backups/"+env.user.ID+"/" {
		t.Fatalf("save = %d %v", code, payload)
	}
	if code, payload := env.do(t, http.MethodPost, "/api/v1/backup/test", `{"source":"system"}`); code != http.StatusOK || payload["success"] != true {
		t.Fatalf("test = %d %v", code, payload)
	}
	systemOn = false
	if code, payload := env.do(t, http.MethodGet, "/api/v1/backup/backups", ""); code != http.StatusBadRequest {
		t.Fatalf("list after withdrawal = %d %v", code, payload)
	}
}
