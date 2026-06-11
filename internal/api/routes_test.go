package api

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	iauth "github.com/songtianlun/diarum/internal/auth"
	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func withMockTransport(t *testing.T, fn roundTripFunc) {
	t.Helper()

	original := http.DefaultTransport
	http.DefaultTransport = fn
	t.Cleanup(func() {
		http.DefaultTransport = original
	})
}

func httpResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()

	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

func newTestUser(t *testing.T, s *store.Store) *store.User {
	t.Helper()

	id, err := store.GenerateID()
	if err != nil {
		t.Fatalf("GenerateID: %v", err)
	}
	user, err := s.CreateUser("user_"+id, id+"@example.com", "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return user
}

func authMiddlewareFor(user *store.User) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set(iauth.ContextUserKey, user)
			return next(c)
		}
	}
}

func performRequest(t *testing.T, e *echo.Echo, method, path string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, path, body)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func decodeJSONBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response body: %v\nbody=%s", err, rec.Body.String())
	}
	return payload
}

func pngBytes() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x03, 0x01, 0x01, 0x00, 0xc9, 0xfe, 0x92,
		0xef, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
		0x44, 0xae, 0x42, 0x60, 0x82,
	}
}

func multipartRequestBody(t *testing.T, fieldName, filename string, fileContent []byte, extra map[string][]string) (*bytes.Buffer, string) {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(fieldName, filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(fileContent); err != nil {
		t.Fatalf("Write file content: %v", err)
	}
	for key, values := range extra {
		for _, value := range values {
			if err := writer.WriteField(key, value); err != nil {
				t.Fatalf("WriteField %s: %v", key, err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close multipart writer: %v", err)
	}
	return &body, writer.FormDataContentType()
}

func TestHelpersVersionAndOpenAPI(t *testing.T) {
	for name, test := range map[string]struct {
		err  error
		code int
		fn   func(string, error) error
	}{
		"badRequest":  {err: io.EOF, code: http.StatusBadRequest, fn: badRequest},
		"serverError": {err: io.EOF, code: http.StatusInternalServerError, fn: serverError},
	} {
		t.Run(name, func(t *testing.T) {
			httpErr, ok := test.fn("message", test.err).(*echo.HTTPError)
			if !ok || httpErr.Code != test.code || !strings.Contains(httpErr.Message.(string), "message") {
				t.Fatalf("%s error = %#v", name, httpErr)
			}
		})
	}
	if unauthorized("nope").(*echo.HTTPError).Code != http.StatusUnauthorized {
		t.Fatal("unauthorized should return 401")
	}
	if forbidden("nope").(*echo.HTTPError).Code != http.StatusForbidden {
		t.Fatal("forbidden should return 403")
	}
	if notFound("nope").(*echo.HTTPError).Code != http.StatusNotFound {
		t.Fatal("notFound should return 404")
	}

	e := echo.New()
	RegisterVersionRoutes(e, "1.2.3", "Diarum")
	RegisterOpenAPIRoutes(e, "1.2.3", "Diarum")
	e.GET("/api/v1/widgets/:id", func(c echo.Context) error { return nil })
	e.POST("/api/v1/widgets", func(c echo.Context) error { return nil })
	e.GET("/*", func(c echo.Context) error { return nil })

	rec := performRequest(t, e, http.MethodGet, "/api/v1/version", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/v1/version status = %d", rec.Code)
	}
	if payload := decodeJSONBody(t, rec); payload["version"] != "1.2.3" || payload["name"] != "Diarum" {
		t.Fatalf("/api/v1/version payload = %#v", payload)
	}

	rec = performRequest(t, e, http.MethodGet, "/api/openapi.json", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/openapi.json status = %d", rec.Code)
	}
	spec := decodeJSONBody(t, rec)
	paths := spec["paths"].(map[string]any)
	if _, ok := paths["/api/v1/widgets/{id}"]; !ok {
		t.Fatalf("OpenAPI paths = %#v", paths)
	}

	rec = performRequest(t, e, http.MethodGet, "/api/docs", nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "swagger-ui") {
		t.Fatalf("/api/docs response = %d %q", rec.Code, rec.Body.String())
	}

	if got := toOpenAPIPath("/api/v1/items/:id"); got != "/api/v1/items/{id}" {
		t.Fatalf("toOpenAPIPath = %q, want /api/v1/items/{id}", got)
	}
	postOp := buildOperation(http.MethodPost, "/api/v1/items")
	if _, ok := postOp["requestBody"]; !ok {
		t.Fatalf("buildOperation POST = %#v", postOp)
	}
	patchOp := buildOperation(http.MethodPatch, "/api/v1/items/{id}")
	if _, ok := patchOp["requestBody"]; !ok {
		t.Fatalf("buildOperation PATCH = %#v", patchOp)
	}
	deleteOp := buildOperation(http.MethodDelete, "/api/v1/items/{id}")
	if _, ok := deleteOp["requestBody"]; ok {
		t.Fatalf("buildOperation DELETE should not have requestBody: %#v", deleteOp)
	}
	publicDiariesOp := buildOperation(http.MethodGet, "/api/v1/diaries")
	security := publicDiariesOp["security"].([]map[string][]string)
	if _, ok := security[0]["apiTokenQuery"]; !ok {
		t.Fatalf("public diaries operation security = %#v", publicDiariesOp)
	}
	versionOp := buildOperation(http.MethodGet, "/api/v1/version")
	if _, ok := versionOp["security"]; ok {
		t.Fatalf("version operation should not require security: %#v", versionOp)
	}
}

func TestAuthAndSettingsRoutes(t *testing.T) {
	s := newTestStore(t)
	authService := iauth.NewService(s)
	e := echo.New()
	RegisterAuthRoutes(e, s, authService)

	registerBody := `{"username":"alice","email":"alice@example.com","password":"secret","passwordConfirm":"secret"}`
	rec := performRequest(t, e, http.MethodPost, "/api/v1/auth/register", strings.NewReader(registerBody), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("register status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = performRequest(t, e, http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"usernameOrEmail":"alice","password":"secret"}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", rec.Code, rec.Body.String())
	}
	if payload := decodeJSONBody(t, rec); payload["token"] == "" {
		t.Fatalf("login payload = %#v", payload)
	}
	rec = performRequest(t, e, http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"usernameOrEmail":"alice@example.com","password":"secret"}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("login by email status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = performRequest(t, e, http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"usernameOrEmail":"alice","password":"wrong"}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("login wrong password status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"usernameOrEmail":"nobody","password":"secret"}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("login missing user status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"usernameOrEmail":"","password":""}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("login missing fields status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("login malformed status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPost, "/api/v1/auth/register", strings.NewReader(registerBody), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("register duplicate status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = performRequest(t, e, http.MethodPost, "/api/v1/auth/register", strings.NewReader(`{"username":"","email":"","password":"","passwordConfirm":""}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("register missing fields status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPost, "/api/v1/auth/register", strings.NewReader(`{`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("register malformed status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPost, "/api/v1/auth/register", strings.NewReader(`{"username":"bob","email":"bob@example.com","password":"a","passwordConfirm":"b"}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("register mismatched passwords status = %d", rec.Code)
	}

	user, err := s.GetUserByIdentity("alice")
	if err != nil {
		t.Fatalf("GetUserByIdentity alice: %v", err)
	}

	RegisterSettingsRoutes(e, s, authMiddlewareFor(user))

	if token, err := generateToken(); err != nil || len(token) != 32 {
		t.Fatalf("generateToken = %q, %v", token, err)
	}

	rec = performRequest(t, e, http.MethodGet, "/api/v1/settings/api-token", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET api-token status = %d", rec.Code)
	}
	if payload := decodeJSONBody(t, rec); payload["exists"] != false {
		t.Fatalf("GET api-token payload = %#v", payload)
	}

	rec = performRequest(t, e, http.MethodPost, "/api/v1/settings/api-token/toggle", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle api-token status = %d body=%s", rec.Code, rec.Body.String())
	}
	firstToken := decodeJSONBody(t, rec)["token"].(string)

	rec = performRequest(t, e, http.MethodGet, "/api/v1/settings/api-token", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET api-token after create status = %d", rec.Code)
	}
	if payload := decodeJSONBody(t, rec); payload["exists"] != true {
		t.Fatalf("GET api-token after create payload = %#v", payload)
	}

	rec = performRequest(t, e, http.MethodPost, "/api/v1/settings/api-token/reset", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset api-token status = %d", rec.Code)
	}
	secondToken := decodeJSONBody(t, rec)["token"].(string)
	if secondToken == "" || secondToken == firstToken {
		t.Fatalf("reset token = %q, first token = %q", secondToken, firstToken)
	}

	rec = performRequest(t, e, http.MethodPut, "/api/v1/settings/batch", strings.NewReader(`{"settings":{"ai.base_url":"https://api.example.com","unknown.key":"x"}}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("settings batch invalid status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPut, "/api/v1/settings/batch", strings.NewReader(`{"settings":`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("settings batch malformed status = %d", rec.Code)
	}

	rec = performRequest(t, e, http.MethodPut, "/api/v1/settings/batch", strings.NewReader(`{"settings":{"ai.base_url":"https://api.example.com","ai.chat_model":"gpt"}}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("settings batch valid status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = performRequest(t, e, http.MethodPost, "/api/v1/settings/api-token/toggle", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle existing api-token status = %d", rec.Code)
	}
	if payload := decodeJSONBody(t, rec); payload["enabled"] != false || payload["token"] != secondToken {
		t.Fatalf("toggle existing api-token payload = %#v", payload)
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/settings/api-token", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET disabled api-token status = %d", rec.Code)
	}
	if payload := decodeJSONBody(t, rec); payload["exists"] != true || payload["enabled"] != false || payload["token"] != secondToken {
		t.Fatalf("GET disabled api-token payload = %#v", payload)
	}

	rec = performRequest(t, e, http.MethodGet, "/api/v1/settings", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET settings status = %d", rec.Code)
	}
	if payload := decodeJSONBody(t, rec); payload["settings"] == nil {
		t.Fatalf("GET settings payload = %#v", payload)
	}

}

func TestDiaryMediaAndPublicRoutes(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	changeCount := 0
	RegisterDiaryRoutes(e, s, authMiddlewareFor(user), func(userID string) {
		if userID == user.ID {
			changeCount++
		}
	})
	RegisterMediaRoutes(e, s, authMiddlewareFor(user))
	RegisterPublicRoutes(e, s)

	configService := config.NewConfigService(s)
	if err := configService.Set(user.ID, "api.token", "public-token"); err != nil {
		t.Fatalf("Set api.token: %v", err)
	}
	if err := configService.Set(user.ID, "api.enabled", true); err != nil {
		t.Fatalf("Set api.enabled: %v", err)
	}

	rec := performRequest(t, e, http.MethodPost, "/api/v1/diaries/upsert", strings.NewReader(`{"date":"2024-03-01","content":"My first diary","mood":"happy","weather":"sunny"}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("diary upsert status = %d body=%s", rec.Code, rec.Body.String())
	}
	diaryPayload := decodeJSONBody(t, rec)
	diaryID := diaryPayload["id"].(string)
	if changeCount != 1 {
		t.Fatalf("changeCount after upsert = %d, want 1", changeCount)
	}

	if _, err := s.InsertImportedDiary(user.ID, "", "2024-03-02", "Search me later", "calm", "cloudy"); err != nil {
		t.Fatalf("InsertImportedDiary: %v", err)
	}

	for _, path := range []string{
		"/api/v1/diaries/by-date/2024-03-01",
		"/api/v1/diaries/exists?start=2024-03-01&end=2024-03-31",
		"/api/v1/diaries/stats?tz=UTC",
		"/api/v1/diaries/search?q=Search",
		"/api/v1/diaries/recent?limit=0",
		"/api/v1/diaries/" + diaryID,
	} {
		rec = performRequest(t, e, http.MethodGet, path, nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", path, rec.Code, rec.Body.String())
		}
	}

	rec = performRequest(t, e, http.MethodPost, "/api/v1/diaries/by-ids", strings.NewReader(`{"ids":["`+diaryID+`","missing"]}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /diaries/by-ids status = %d", rec.Code)
	}

	rec = performRequest(t, e, http.MethodGet, "/api/v1/diaries?token=public-token&date=2024-03-01", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("public date status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/diaries?token=public-token&start=2024-03-01&end=2024-03-31", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("public range status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/diaries?token=public-token&date=2024-04-01", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("public missing diary status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/diaries", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("public missing token status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/diaries?token=wrong", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("public wrong token status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/diaries?token=public-token", nil, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("public missing params status = %d", rec.Code)
	}

	body, contentType := multipartRequestBody(t, "file", "photo.png", pngBytes(), map[string][]string{
		"name":  {"Uploaded"},
		"alt":   {"Alt"},
		"diary": {diaryID},
	})
	rec = performRequest(t, e, http.MethodPost, "/api/v1/media", body, map[string]string{"Content-Type": contentType})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /media status = %d body=%s", rec.Code, rec.Body.String())
	}
	mediaPayload := decodeJSONBody(t, rec)
	mediaID := mediaPayload["id"].(string)

	rec = performRequest(t, e, http.MethodGet, "/api/v1/media?page=bad&perPage=-1", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /media list status = %d", rec.Code)
	}
	badBody, badContentType := multipartRequestBody(t, "file", "note.txt", []byte("hello"), nil)
	rec = performRequest(t, e, http.MethodPost, "/api/v1/media", badBody, map[string]string{"Content-Type": badContentType})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /media invalid file status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = performRequest(t, e, http.MethodPost, "/api/v1/media", strings.NewReader(""), map[string]string{"Content-Type": "multipart/form-data; boundary=missing"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /media missing file status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPatch, "/api/v1/media/"+mediaID, strings.NewReader(`{"diary":[]}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH /media status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/media/missing", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /media/missing status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/media/"+mediaID, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /media/:id status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/files/media/"+mediaID+"/photo.png", nil, nil)
	if rec.Code != http.StatusOK || len(rec.Body.Bytes()) == 0 {
		t.Fatalf("GET /files/media status = %d body=%q", rec.Code, rec.Body.Bytes())
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/files/media/"+mediaID+"/wrong.png", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /files/media filename mismatch status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodDelete, "/api/v1/media/"+mediaID, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /media status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodDelete, "/api/v1/media/"+mediaID, nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE /media missing status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/files/media/"+mediaID+"/photo.png", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET deleted media file status = %d", rec.Code)
	}

	rec = performRequest(t, e, http.MethodDelete, "/api/v1/diaries/"+diaryID, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /diaries/:id status = %d", rec.Code)
	}
	if changeCount != 2 {
		t.Fatalf("changeCount after delete = %d, want 2", changeCount)
	}

	if got := parsePositiveInt("", 3); got != 3 {
		t.Fatalf("parsePositiveInt empty = %d, want 3", got)
	}
	if got := parsePositiveInt("7", 3); got != 7 {
		t.Fatalf("parsePositiveInt valid = %d, want 7", got)
	}
}

func TestMemosWebhookSync(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	changeCount := 0
	RegisterMemosRoutes(e, s, authMiddlewareFor(user), func(userID string) {
		if userID == user.ID {
			changeCount++
		}
	})

	rec := performRequest(t, e, http.MethodPut, "/api/v1/memos/settings", strings.NewReader(`{"enabled":true,"base_url":"https://memos.example.com"}`), map[string]string{"Content-Type": "application/json", "Host": "diarum.example.com", "X-Forwarded-Proto": "https"})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT memos settings status = %d body=%s", rec.Code, rec.Body.String())
	}
	settings := decodeJSONBody(t, rec)
	webhookURL := settings["webhook_url"].(string)
	if !strings.HasPrefix(webhookURL, "https://example.com/api/v1/memos/webhook/") {
		t.Fatalf("webhook_url = %q", webhookURL)
	}
	webhookPath := strings.TrimPrefix(webhookURL, "https://example.com")

	body := `{"activityType":"memos.memo.created","memo":{"name":"memos/123","content":"hello memos","create_time":{"seconds":1712311872},"update_time":{"seconds":1712311872}}}`
	rec = performRequest(t, e, http.MethodPost, webhookPath, strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST memos webhook create status = %d body=%s", rec.Code, rec.Body.String())
	}
	diary, err := s.GetDiaryByDate(user.ID, "2024-04-05 00:00:00.000Z", "2024-04-05 23:59:59.999Z")
	if err != nil {
		t.Fatalf("GetDiaryByDate after create: %v", err)
	}
	if !strings.Contains(diary.Content, `id="123"`) || !strings.Contains(diary.Content, "<hr>") || !strings.Contains(diary.Content, "hello memos") || !strings.Contains(diary.Content, "https://memos.example.com/m/123") {
		t.Fatalf("diary content after create = %q", diary.Content)
	}

	body = `{"activityType":"memos.memo.updated","memo":{"name":"memos/123","content":"updated memos","create_time":{"seconds":1712311872},"update_time":{"seconds":1712398272}}}`
	rec = performRequest(t, e, http.MethodPost, webhookPath, strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST memos webhook update status = %d body=%s", rec.Code, rec.Body.String())
	}
	diary, err = s.GetDiaryByDate(user.ID, "2024-04-05 00:00:00.000Z", "2024-04-05 23:59:59.999Z")
	if err != nil {
		t.Fatalf("GetDiaryByDate after update: %v", err)
	}
	if strings.Contains(diary.Content, "hello memos") || !strings.Contains(diary.Content, "updated memos") {
		t.Fatalf("diary content after update = %q", diary.Content)
	}

	body = `{"activityType":"memos.memo.deleted","memo":{"name":"memos/123","create_time":{"seconds":1712311872}}}`
	rec = performRequest(t, e, http.MethodPost, webhookPath, strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST memos webhook delete status = %d body=%s", rec.Code, rec.Body.String())
	}
	diary, err = s.GetDiaryByDate(user.ID, "2024-04-05 00:00:00.000Z", "2024-04-05 23:59:59.999Z")
	if err != nil {
		t.Fatalf("GetDiaryByDate after delete: %v", err)
	}
	if strings.Contains(diary.Content, "DIARUM:MEMOS") || strings.Contains(diary.Content, "updated memos") {
		t.Fatalf("diary content after delete = %q", diary.Content)
	}
	if changeCount != 3 {
		t.Fatalf("changeCount = %d, want 3", changeCount)
	}
}

func TestMemosDateParsingAndAppendFormatting(t *testing.T) {
	event := parseMemosWebhookEvent(map[string]any{
		"activityType": "memos.memo.updated",
		"memo": map[string]any{
			"name":        "memos/hqy6gQoqC6k9LamgN39yma",
			"content":     "sample memo",
			"create_time": map[string]any{"seconds": float64(1780558277)},
			"update_time": map[string]any{"seconds": float64(1781144444)},
		},
	})
	if event.Memo.ID != "hqy6gQoqC6k9LamgN39yma" {
		t.Fatalf("parsed memo ID = %q", event.Memo.ID)
	}
	if event.Memo.CreateTime != "2026-06-04T07:31:17Z" || event.Memo.UpdateTime != "2026-06-11T02:20:44Z" {
		t.Fatalf("parsed memo times = create %q update %q", event.Memo.CreateTime, event.Memo.UpdateTime)
	}
	if got := memoDate(event.Memo); got != "2026-06-04" {
		t.Fatalf("memoDate protobuf timestamp = %q, want 2026-06-04", got)
	}
	metadata := renderMemosMetadataHTML(event.Memo)
	if !strings.Contains(metadata, "Created: 2026-06-04T07:31:17Z") || !strings.Contains(metadata, "Updated: 2026-06-11T02:20:44Z") {
		t.Fatalf("metadata missing timestamps: %q", metadata)
	}

	created := "1712448000"
	memo := memosMemo{ID: "unix-time", CreateTime: created, Content: "timestamp memo"}
	if got := memoDate(memo); got != "2024-04-07" {
		t.Fatalf("memoDate unix seconds = %q, want 2024-04-07", got)
	}

	block := renderMemosBlock(memo, "2024-04-07")
	content := appendMemosBlock("<p>existing</p><hr>", block)
	if strings.Count(content, "<hr>") != 2 {
		t.Fatalf("appendMemosBlock created consecutive horizontal rules: %q", content)
	}
	if !strings.Contains(content, "<!-- DIARUM:MEMOS:BEGIN") || !strings.Contains(content, "<pre><code>") {
		t.Fatalf("appendMemosBlock content = %q", content)
	}
}

func TestMemosSettingsAndWebhookFailures(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	RegisterMemosRoutes(e, s, authMiddlewareFor(user), nil)

	rec := performRequest(t, e, http.MethodGet, "/api/v1/memos/settings", nil, map[string]string{"Host": "diarum.example.com"})
	if rec.Code != http.StatusOK {
		t.Fatalf("GET memos settings status = %d body=%s", rec.Code, rec.Body.String())
	}
	settings := decodeJSONBody(t, rec)
	if settings["token_exists"] != false || settings["webhook_url"] != "" {
		t.Fatalf("default memos settings = %#v", settings)
	}

	rec = performRequest(t, e, http.MethodPost, "/api/v1/memos/webhook/no-token", strings.NewReader(`{}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown webhook token status = %d", rec.Code)
	}

	cfg := config.NewConfigService(s)
	if err := cfg.Set(user.ID, "memos.webhook_token", "known-token"); err != nil {
		t.Fatalf("Set webhook token: %v", err)
	}
	rec = performRequest(t, e, http.MethodPost, "/api/v1/memos/webhook/known-token", strings.NewReader(`{}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("disabled webhook status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = performRequest(t, e, http.MethodPut, "/api/v1/memos/settings", strings.NewReader(`{"enabled":true,"base_url":"https://memos.example.com/"}`), map[string]string{"Content-Type": "application/json", "Host": "diarum.example.com", "X-Forwarded-Host": "public.example.com", "X-Forwarded-Proto": "https"})
	if rec.Code != http.StatusOK {
		t.Fatalf("enable memos settings status = %d body=%s", rec.Code, rec.Body.String())
	}
	settings = decodeJSONBody(t, rec)
	if settings["webhook_url"] != "https://public.example.com/api/v1/memos/webhook/known-token" {
		t.Fatalf("forwarded webhook_url = %q", settings["webhook_url"])
	}

	rec = performRequest(t, e, http.MethodPut, "/api/v1/memos/settings", strings.NewReader(`{`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid settings body status = %d", rec.Code)
	}

	rec = performRequest(t, e, http.MethodPost, "/api/v1/memos/settings/reset-token", nil, map[string]string{"Host": "diarum.example.com"})
	if rec.Code != http.StatusOK {
		t.Fatalf("reset memos token status = %d body=%s", rec.Code, rec.Body.String())
	}
	settings = decodeJSONBody(t, rec)
	if settings["token_exists"] != true || settings["webhook_url"] == "" {
		t.Fatalf("reset token settings = %#v", settings)
	}
	webhookPath := strings.TrimPrefix(settings["webhook_url"].(string), "http://diarum.example.com")

	rec = performRequest(t, e, http.MethodPost, webhookPath, strings.NewReader(`{`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid webhook JSON status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPost, webhookPath, strings.NewReader(`{"action":"create","memo":{"content":"missing id"}}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing memo id status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPost, webhookPath, strings.NewReader(`{"action":"create","memo":{"id":"bad-time","content":"x"}}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("missing memo time status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMemosRoutesDatabaseErrors(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	RegisterMemosRoutes(e, s, authMiddlewareFor(user), nil)
	if err := s.DB.Close(); err != nil {
		t.Fatalf("Close store DB: %v", err)
	}

	rec := performRequest(t, e, http.MethodPost, "/api/v1/memos/webhook/token", strings.NewReader(`{}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("webhook token validation DB error status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPut, "/api/v1/memos/settings", strings.NewReader(`{"enabled":true}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("PUT settings token save DB error status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPut, "/api/v1/memos/settings", strings.NewReader(`{"enabled":false}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("PUT settings batch DB error status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPost, "/api/v1/memos/settings/reset-token", nil, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("reset settings token DB error status = %d", rec.Code)
	}
}

func TestMemosPayloadParsingVariants(t *testing.T) {
	if got, err := validateMemosWebhookToken(newTestStore(t), ""); err != nil || got != "" {
		t.Fatalf("validate empty token = %q, %v", got, err)
	}
	closedStore := newTestStore(t)
	if err := closedStore.DB.Close(); err != nil {
		t.Fatalf("Close store DB: %v", err)
	}
	if _, err := validateMemosWebhookToken(closedStore, "token"); err == nil {
		t.Fatal("validate token should return query error after DB close")
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "https://diarum.example.com/settings", nil)
	req.TLS = &tls.ConnectionState{}
	c := e.NewContext(req, httptest.NewRecorder())
	if got := absoluteURL(c, "/api/v1/memos/webhook/tls-token"); got != "https://diarum.example.com/api/v1/memos/webhook/tls-token" {
		t.Fatalf("absoluteURL TLS = %q", got)
	}

	event := parseMemosWebhookEvent(map[string]any{
		"type": "memo.archived",
		"data": map[string]any{
			"resource": map[string]any{
				"uid":        float64(42),
				"content":    " nested ",
				"created_at": map[string]any{"seconds": "1712448000", "nanos": float64(123)},
				"html_url":   "https://memos.example.com/m/42",
			},
		},
	})
	if event.Action != "delete" || event.Memo.ID != "42" || event.Memo.CreateTime != "2024-04-07T00:00:00Z" || event.Memo.URL == "" {
		t.Fatalf("nested parsed event = %#v", event)
	}

	event = parseMemosWebhookEvent(map[string]any{
		"resource": "memos/abc",
		"state":    "ARCHIVED",
		"created":  "2024-04-08 10:11:12",
	})
	if event.Action != "delete" || event.Memo.ID != "abc" || memoDate(event.Memo) != "2024-04-08" {
		t.Fatalf("fallback parsed event = %#v date=%q", event, memoDate(event.Memo))
	}
	event = parseMemosWebhookEvent(map[string]any{
		"memo": map[string]any{"id": "upsert-default", "created": "2024-04-08"},
	})
	if event.Action != "upsert" || event.Memo.ID != "upsert-default" {
		t.Fatalf("default upsert event = %#v", event)
	}

	if got := normalizeMemosAction("memos.memo.restored"); got != "memos.memo.restored" {
		t.Fatalf("normalizeMemosAction unknown = %q", got)
	}
	if got := stringValue(true); got != "" {
		t.Fatalf("stringValue bool = %q", got)
	}
	if got, ok := int64Value(map[string]any{}); ok || got != 0 {
		t.Fatalf("int64Value invalid = %d, %v", got, ok)
	}
	if got := protobufTimestampString(map[string]any{"seconds": map[string]any{}}); got != "" {
		t.Fatalf("protobufTimestampString invalid seconds = %q", got)
	}
	if got := protobufTimestampString(map[string]any{"nanos": float64(1)}); got != "" {
		t.Fatalf("protobufTimestampString missing seconds = %q", got)
	}
	if got := memoID(map[string]any{}); got != "" {
		t.Fatalf("memoID empty = %q", got)
	}
	if got := buildMemosURL("", "id"); got != "" {
		t.Fatalf("buildMemosURL empty base = %q", got)
	}
	if got := buildMemosURL("https://memos.example.com", ""); got != "" {
		t.Fatalf("buildMemosURL empty id = %q", got)
	}
}

func TestMemosBlockReplacementAndDateVariants(t *testing.T) {
	block := renderMemosBlock(memosMemo{ID: `id"-->`, Content: "hello <memo>\nline\n\nnext"}, "2024-04-07")
	if !strings.Contains(block, `id="id&quot;--&gt;"`) || !strings.Contains(block, "hello &lt;memo&gt;<br>line") || !strings.Contains(block, "<p>next</p>") {
		t.Fatalf("escaped rendered block = %q", block)
	}
	if empty := renderMemoContentHTML("   "); empty != "<p></p>" {
		t.Fatalf("empty memo content = %q", empty)
	}

	legacy := "before\n<hr><pre><code>Source: Memos\nMemo ID: legacy&amp;id</code></pre><p>old</p><hr>\nafter"
	replaced, ok := replaceMemosBlock(legacy, "legacy&id", "NEW")
	if !ok || !strings.Contains(replaced, "beforeNEWafter") {
		t.Fatalf("replace legacy block = %q, %v", replaced, ok)
	}
	removed, ok := removeMemosBlockFromContent(legacy, "legacy&id")
	if !ok || strings.Contains(removed, "Memo ID") {
		t.Fatalf("remove legacy block = %q, %v", removed, ok)
	}
	unchanged, ok := replaceMemosBlock("plain", "missing", "NEW")
	if ok || unchanged != "plain" {
		t.Fatalf("replace missing = %q, %v", unchanged, ok)
	}
	unchanged, ok = removeMemosBlockFromContent("plain", "missing")
	if ok || unchanged != "plain" {
		t.Fatalf("remove missing = %q, %v", unchanged, ok)
	}

	for name, test := range map[string]struct {
		value string
		want  string
	}{
		"millis": {value: "1712448000000", want: "2024-04-07"},
		"micros": {value: "1712448000000000", want: "2024-04-07"},
		"nanos":  {value: "1712448000000000000", want: "2024-04-07"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := memoDateFromValue(test.value); got != test.want {
				t.Fatalf("memoDateFromValue(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
	if got := memoDateFromValue("not-a-date"); got != "" {
		t.Fatalf("invalid memoDateFromValue = %q", got)
	}
	if got := memoDateFromValue("9999999999999999999"); got != "" {
		t.Fatalf("overflow memoDateFromValue = %q", got)
	}
	if got := memoDate(memosMemo{UpdateTime: "2024-04-09T01:02:03"}); got != "2024-04-09" {
		t.Fatalf("memoDate update fallback = %q", got)
	}
	if got := appendMemosBlock("", "block"); got != "block" {
		t.Fatalf("append empty content = %q", got)
	}
}

func TestMemosSyncFindsAndRemovesExistingBlock(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	oldBlock := renderMemosBlock(memosMemo{ID: "memo-1", Content: "old", CreateTime: "2024-04-01"}, "2024-04-01")
	if _, _, err := s.UpsertDiary(user.ID, "2024-04-01", "intro\n\n"+oldBlock, "calm", "cloudy"); err != nil {
		t.Fatalf("UpsertDiary old: %v", err)
	}

	changed, err := syncMemosMemo(s, user.ID, memosWebhookEvent{Action: "upsert", Memo: memosMemo{ID: "memo-1", Content: "new", CreateTime: "2024-04-02", URL: "https://memos.example.com/m/memo-1"}})
	if err != nil || !changed {
		t.Fatalf("sync existing memo changed=%v err=%v", changed, err)
	}
	diary, err := s.GetDiaryByDate(user.ID, "2024-04-01 00:00:00.000Z", "2024-04-01 23:59:59.999Z")
	if err != nil {
		t.Fatalf("GetDiaryByDate existing memo: %v", err)
	}
	if strings.Contains(diary.Content, "old") || !strings.Contains(diary.Content, "new") || diary.Mood != "calm" || diary.Weather != "cloudy" {
		t.Fatalf("updated existing diary = %#v", diary)
	}

	changed, err = syncMemosMemo(s, user.ID, memosWebhookEvent{Action: "delete", Memo: memosMemo{ID: "memo-1"}})
	if err != nil || !changed {
		t.Fatalf("delete existing memo changed=%v err=%v", changed, err)
	}
	diary, err = s.GetDiaryByDate(user.ID, "2024-04-01 00:00:00.000Z", "2024-04-01 23:59:59.999Z")
	if err != nil {
		t.Fatalf("GetDiaryByDate deleted memo: %v", err)
	}
	if strings.Contains(diary.Content, "DIARUM:MEMOS") || strings.Contains(diary.Content, "new") {
		t.Fatalf("deleted memo diary content = %q", diary.Content)
	}

	changed, err = removeMemosBlock(s, user.ID, "missing", "2024-04-03")
	if err != nil || changed {
		t.Fatalf("remove missing memo changed=%v err=%v", changed, err)
	}

	if _, _, err := s.UpsertDiary(user.ID, "2024-04-04", "plain diary", "ok", "sun"); err != nil {
		t.Fatalf("UpsertDiary plain: %v", err)
	}
	changed, err = syncMemosMemo(s, user.ID, memosWebhookEvent{Action: "upsert", Memo: memosMemo{ID: "memo-2", Content: "appended", CreateTime: "2024-04-04"}})
	if err != nil || !changed {
		t.Fatalf("append memo to existing date changed=%v err=%v", changed, err)
	}
	diary, err = s.GetDiaryByDate(user.ID, "2024-04-04 00:00:00.000Z", "2024-04-04 23:59:59.999Z")
	if err != nil {
		t.Fatalf("GetDiaryByDate appended memo: %v", err)
	}
	if !strings.Contains(diary.Content, "plain diary") || !strings.Contains(diary.Content, "appended") {
		t.Fatalf("appended memo diary content = %q", diary.Content)
	}
	changed, err = removeMemosBlock(s, user.ID, "missing", "2024-04-04")
	if err != nil || changed {
		t.Fatalf("remove missing memo from existing date changed=%v err=%v", changed, err)
	}

	closedStore := newTestStore(t)
	if err := closedStore.DB.Close(); err != nil {
		t.Fatalf("Close store DB: %v", err)
	}
	if _, err := findMemosDiary(closedStore, user.ID, "memo", ""); err == nil {
		t.Fatal("findMemosDiary should return list error after DB close")
	}
}

func TestDiaryRoutesSearchStatsAndAccessBranches(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	other := newTestUser(t, s)
	e := echo.New()
	changeCount := 0
	RegisterDiaryRoutes(e, s, authMiddlewareFor(user), func(userID string) {
		if userID == user.ID {
			changeCount++
		}
	})

	today := time.Now().UTC().Format("2006-01-02")
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	oldContent := strings.Repeat("x", 220) + " searchable"
	diaryToday, _, err := s.UpsertDiary(user.ID, today, oldContent, "focused", "sunny")
	if err != nil {
		t.Fatalf("UpsertDiary today: %v", err)
	}
	if _, _, err := s.UpsertDiary(user.ID, yesterday, "yesterday searchable", "calm", "cloudy"); err != nil {
		t.Fatalf("UpsertDiary yesterday: %v", err)
	}
	otherDiary, _, err := s.UpsertDiary(other.ID, today, "other diary", "private", "rain")
	if err != nil {
		t.Fatalf("UpsertDiary other: %v", err)
	}

	rec := performRequest(t, e, http.MethodGet, "/api/v1/diaries/exists", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /diaries/exists default status = %d body=%s", rec.Code, rec.Body.String())
	}
	payload := decodeJSONBody(t, rec)
	if len(payload["dates"].([]any)) < 2 {
		t.Fatalf("exists dates = %#v", payload)
	}

	rec = performRequest(t, e, http.MethodGet, "/api/v1/diaries/stats?tz=UTC", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /diaries/stats status = %d body=%s", rec.Code, rec.Body.String())
	}
	payload = decodeJSONBody(t, rec)
	if payload["total"].(float64) < 2 || payload["streak"].(float64) < 2 {
		t.Fatalf("stats payload = %#v", payload)
	}

	rec = performRequest(t, e, http.MethodGet, "/api/v1/diaries/search", nil, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("GET /diaries/search missing q status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/diaries/search?q=searchable", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /diaries/search status = %d body=%s", rec.Code, rec.Body.String())
	}
	payload = decodeJSONBody(t, rec)
	results := payload["results"].([]any)
	if len(results) == 0 || !strings.HasSuffix(results[0].(map[string]any)["snippet"].(string), "...") {
		t.Fatalf("search payload = %#v", payload)
	}

	rec = performRequest(t, e, http.MethodPost, "/api/v1/diaries/by-ids", strings.NewReader(`{"ids":["`+diaryToday.ID+`","`+otherDiary.ID+`","missing"]}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /diaries/by-ids status = %d body=%s", rec.Code, rec.Body.String())
	}
	payload = decodeJSONBody(t, rec)
	if diaries := payload["diaries"].([]any); len(diaries) != 1 {
		t.Fatalf("by-ids payload = %#v", payload)
	}
	rec = performRequest(t, e, http.MethodPost, "/api/v1/diaries/by-ids", strings.NewReader(`{`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /diaries/by-ids invalid status = %d", rec.Code)
	}

	rec = performRequest(t, e, http.MethodGet, "/api/v1/diaries/recent?limit=500", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /diaries/recent status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = performRequest(t, e, http.MethodGet, "/api/v1/diaries/"+otherDiary.ID, nil, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("GET /diaries/:id forbidden status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodDelete, "/api/v1/diaries/missing", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE /diaries/missing status = %d", rec.Code)
	}

	rec = performRequest(t, e, http.MethodPost, "/api/v1/diaries/upsert", strings.NewReader(`{"date":"`+today+`","content":"changed"}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /diaries/upsert status = %d body=%s", rec.Code, rec.Body.String())
	}
	if changeCount != 1 {
		t.Fatalf("changeCount = %d, want 1", changeCount)
	}
}

func TestImageUploadAndCheveretoRoutes(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	RegisterImageUploadRoutes(e, s, authMiddlewareFor(user))
	RegisterCheveretoRoutes(e, s, authMiddlewareFor(user))

	configService := config.NewConfigService(s)
	settings, err := loadImageUploadSettings(configService, s, user.ID)
	if err != nil {
		t.Fatalf("loadImageUploadSettings: %v", err)
	}
	if settings.Provider != "local" || settings.Local.Path == "" {
		t.Fatalf("default image upload settings = %#v", settings)
	}

	if got := normalizeImageUploadProvider(" S3 "); got != "s3" {
		t.Fatalf("normalizeImageUploadProvider = %q, want s3", got)
	}
	if got := normalizeImageUploadProvider("invalid"); got != "" {
		t.Fatalf("normalizeImageUploadProvider invalid = %q, want empty", got)
	}
	if got := normalizeLocalPath(s, " "); got != s.DefaultLocalMediaDir() {
		t.Fatalf("normalizeLocalPath blank = %q", got)
	}
	if _, err := normalizeImageUploadSettings(imageUploadSettingsResponse{Provider: "invalid"}, s); err == nil {
		t.Fatal("normalizeImageUploadSettings should reject invalid provider")
	}
	if _, err := normalizeImageUploadSettings(imageUploadSettingsResponse{Provider: "s3"}, s); err == nil {
		t.Fatal("normalizeImageUploadSettings should require S3 fields")
	}
	if _, err := normalizeImageUploadSettings(imageUploadSettingsResponse{Provider: "chevereto"}, s); err == nil {
		t.Fatal("normalizeImageUploadSettings should require Chevereto fields")
	}

	rec := performRequest(t, e, http.MethodGet, "/api/v1/image-upload/settings", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET image-upload/settings status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPut, "/api/v1/image-upload/settings", strings.NewReader(`{"provider":"invalid"}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT image-upload/settings invalid status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPut, "/api/v1/image-upload/settings", strings.NewReader(`{"provider":"local","local":{"path":"media-root"}}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT image-upload/settings status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = performRequest(t, e, http.MethodGet, "/api/v1/chevereto/settings", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET chevereto/settings status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPut, "/api/v1/chevereto/settings", strings.NewReader(`{"enabled":true}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT chevereto/settings invalid status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPut, "/api/v1/chevereto/settings", strings.NewReader(`{"enabled":true,"domain":"https://img.example.com/","api_key":"abc","album_id":"42"}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT chevereto/settings valid status = %d body=%s", rec.Code, rec.Body.String())
	}

	withMockTransport(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/1/upload" {
			return httpResponse(http.StatusNotFound, "not found"), nil
		}
		switch req.Method {
		case http.MethodGet:
			return httpResponse(http.StatusOK, "ok"), nil
		case http.MethodPost:
			return httpResponse(http.StatusOK, `{"image":{"url":"https://img.example.com/uploaded.png"}}`), nil
		default:
			return httpResponse(http.StatusMethodNotAllowed, "bad method"), nil
		}
	})

	rec = performRequest(t, e, http.MethodPost, "/api/v1/chevereto/test", strings.NewReader(`{"domain":"https://img.example.com","api_key":"abc"}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST chevereto/test status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPost, "/api/v1/chevereto/test", strings.NewReader(`{"domain":"","api_key":""}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST chevereto/test missing creds status = %d", rec.Code)
	}

	withMockTransport(t, func(req *http.Request) (*http.Response, error) {
		return httpResponse(http.StatusNotFound, "not found"), nil
	})
	rec = performRequest(t, e, http.MethodPost, "/api/v1/chevereto/test", strings.NewReader(`{"domain":"https://img.example.com","api_key":"abc"}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"success":false`) {
		t.Fatalf("POST chevereto/test 404 body = %s", rec.Body.String())
	}

	withMockTransport(t, func(req *http.Request) (*http.Response, error) {
		return httpResponse(http.StatusUnauthorized, "unauthorized"), nil
	})
	rec = performRequest(t, e, http.MethodPost, "/api/v1/chevereto/test", strings.NewReader(`{"domain":"https://img.example.com","api_key":"abc"}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"success":false`) {
		t.Fatalf("POST chevereto/test 401 body = %s", rec.Body.String())
	}

	withMockTransport(t, func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPost {
			return httpResponse(http.StatusOK, `{"image":{"url":"https://img.example.com/uploaded.png"}}`), nil
		}
		return httpResponse(http.StatusOK, "ok"), nil
	})

	body, contentType := multipartRequestBody(t, "source", "photo.png", pngBytes(), nil)
	rec = performRequest(t, e, http.MethodPost, "/api/v1/chevereto/upload", body, map[string]string{"Content-Type": contentType})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST chevereto/upload status = %d body=%s", rec.Code, rec.Body.String())
	}
	if payload := decodeJSONBody(t, rec); payload["url"] != "https://img.example.com/uploaded.png" {
		t.Fatalf("POST chevereto/upload payload = %#v", payload)
	}
}

func TestSingleSettingHandlers(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	configService := config.NewConfigService(s)
	e := echo.New()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/ai.chat_model", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec).(*echo.DefaultContext)
	c.Set(iauth.ContextUserKey, user)
	c.SetPathParams(echo.PathParams{{Name: "key", Value: "ai.chat_model"}})

	if err := getSettingHandler(configService)(c); err != nil {
		t.Fatalf("getSettingHandler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("getSettingHandler status = %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/v1/settings/ai.chat_model", strings.NewReader(`{"value":"gpt-test"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec).(*echo.DefaultContext)
	c.Set(iauth.ContextUserKey, user)
	c.SetPathParams(echo.PathParams{{Name: "key", Value: "ai.chat_model"}})
	if err := putSettingHandler(configService)(c); err != nil {
		t.Fatalf("putSettingHandler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("putSettingHandler status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/settings/ai.chat_model", nil)
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec).(*echo.DefaultContext)
	c.Set(iauth.ContextUserKey, user)
	c.SetPathParams(echo.PathParams{{Name: "key", Value: "ai.chat_model"}})
	if err := getSettingHandler(configService)(c); err != nil {
		t.Fatalf("getSettingHandler after put: %v", err)
	}
	if payload := decodeJSONBody(t, rec); payload["value"] != "gpt-test" {
		t.Fatalf("getSettingHandler payload = %#v", payload)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/settings/ai.chat_model", nil)
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec).(*echo.DefaultContext)
	c.Set(iauth.ContextUserKey, user)
	c.SetPathParams(echo.PathParams{{Name: "key", Value: "ai.chat_model"}})
	if err := deleteSettingHandler(configService)(c); err != nil {
		t.Fatalf("deleteSettingHandler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("deleteSettingHandler status = %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/settings/unknown", nil)
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec).(*echo.DefaultContext)
	c.Set(iauth.ContextUserKey, user)
	c.SetPathParams(echo.PathParams{{Name: "key", Value: "unknown"}})
	if err := getSettingHandler(configService)(c); err == nil {
		t.Fatal("getSettingHandler should reject unknown key")
	}

	req = httptest.NewRequest(http.MethodPut, "/api/v1/settings/ai.chat_model", strings.NewReader(`{"value":`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec).(*echo.DefaultContext)
	c.Set(iauth.ContextUserKey, user)
	c.SetPathParams(echo.PathParams{{Name: "key", Value: "ai.chat_model"}})
	if err := putSettingHandler(configService)(c); err == nil {
		t.Fatal("putSettingHandler should reject malformed JSON")
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/settings/unknown", nil)
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec).(*echo.DefaultContext)
	c.Set(iauth.ContextUserKey, user)
	c.SetPathParams(echo.PathParams{{Name: "key", Value: "unknown"}})
	if err := deleteSettingHandler(configService)(c); err == nil {
		t.Fatal("deleteSettingHandler should reject unknown key")
	}
}
