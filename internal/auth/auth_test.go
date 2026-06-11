package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/store"
)

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

func TestHashVerifyIssueAndParseToken(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	service := NewService(s)

	hash, err := service.HashPassword("super-secret")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "super-secret" {
		t.Fatal("HashPassword should not return the raw password")
	}
	if !service.VerifyPassword(hash, "super-secret") {
		t.Fatal("VerifyPassword should accept correct password")
	}
	if service.VerifyPassword(hash, "nope") {
		t.Fatal("VerifyPassword should reject wrong password")
	}

	token, err := service.IssueToken(user)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	parsedUser, err := service.ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}
	if parsedUser.ID != user.ID {
		t.Fatalf("ParseToken user ID = %q, want %q", parsedUser.ID, user.ID)
	}

	if _, err := service.ParseToken("invalid-token"); err == nil {
		t.Fatal("ParseToken should reject invalid token")
	}
}

func TestMiddlewareAndCurrentUser(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	service := NewService(s)

	token, err := service.IssueToken(user)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	var gotUser *store.User
	handler := service.Middleware(func(c echo.Context) error {
		gotUser = CurrentUser(c)
		return c.String(http.StatusOK, "ok")
	})
	if err := handler(c); err != nil {
		t.Fatalf("Middleware valid token: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("response code = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotUser == nil || gotUser.ID != user.ID {
		t.Fatalf("CurrentUser = %#v, want user %q", gotUser, user.ID)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	if err := service.Middleware(func(c echo.Context) error { return nil })(c); err == nil {
		t.Fatal("Middleware should reject missing Authorization header")
	}

	if CurrentUser(c) != nil {
		t.Fatal("CurrentUser should be nil when context has no user")
	}
}

func TestParseTokenUserNotFound(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	service := NewService(s)

	token, err := service.IssueToken(user)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	deleteUser(t, s, user.ID)

	if _, err := service.ParseToken(token); err == nil {
		t.Fatal("ParseToken should fail when user no longer exists")
	}
}

func TestMiddlewareWrongPrefix(t *testing.T) {
	s := newTestStore(t)
	service := NewService(s)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic dGVzdDp0ZXN0")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := service.Middleware(func(c echo.Context) error { return nil })(c); err == nil {
		t.Fatal("Middleware should reject non-Bearer Authorization header")
	}
}

func deleteUser(t *testing.T, s *store.Store, userID string) {
	t.Helper()
	if _, err := s.DB.Exec(`DELETE FROM users WHERE id = ?`, userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
}
