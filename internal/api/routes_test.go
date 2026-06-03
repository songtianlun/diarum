package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
		"badRequest": {err: io.EOF, code: http.StatusBadRequest, fn: badRequest},
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

	rec = performRequest(t, e, http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"usernameOrEmail":"alice","password":"wrong"}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("login wrong password status = %d", rec.Code)
	}
	rec = performRequest(t, e, http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"usernameOrEmail":"","password":""}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("login missing fields status = %d", rec.Code)
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

	rec = performRequest(t, e, http.MethodGet, "/api/v1/settings", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET settings status = %d", rec.Code)
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
