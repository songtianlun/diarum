package api

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/auth"
	"github.com/songtianlun/diarum/internal/store"
)

// RegisterAuthRoutes registers auth business API endpoints.
func RegisterAuthRoutes(e *echo.Echo, store *store.Store, authService *auth.Service) {
	e.POST("/api/v1/auth/login", func(c echo.Context) error {
		var body struct {
			UsernameOrEmail string `json:"usernameOrEmail"`
			Password        string `json:"password"`
		}
		if err := c.Bind(&body); err != nil {
			return badRequest("Invalid request body", err)
		}

		if body.UsernameOrEmail == "" || body.Password == "" {
			return badRequest("usernameOrEmail and password are required", nil)
		}

		user, err := store.GetUserByIdentity(body.UsernameOrEmail)
		if err != nil || !authService.VerifyPassword(user.PasswordHash, body.Password) {
			return unauthorized("Invalid login credentials")
		}

		token, err := authService.IssueToken(user)
		if err != nil {
			return serverError("Failed to issue token", err)
		}

		return c.JSON(http.StatusOK, map[string]any{
			"token":  token,
			"record": user,
		})
	})

	e.POST("/api/v1/auth/register", func(c echo.Context) error {
		var body struct {
			Username        string `json:"username"`
			Email           string `json:"email"`
			Password        string `json:"password"`
			PasswordConfirm string `json:"passwordConfirm"`
		}
		if err := c.Bind(&body); err != nil {
			return badRequest("Invalid request body", err)
		}

		body.Username = strings.TrimSpace(body.Username)
		body.Email = strings.TrimSpace(body.Email)
		if body.Username == "" || body.Email == "" || body.Password == "" || body.PasswordConfirm == "" {
			return badRequest("username, email, password, and passwordConfirm are required", nil)
		}
		if body.Password != body.PasswordConfirm {
			return badRequest("passwords do not match", nil)
		}

		hash, err := authService.HashPassword(body.Password)
		if err != nil {
			return serverError("Failed to hash password", err)
		}
		user, err := store.CreateUser(body.Username, body.Email, hash)
		if err != nil {
			return badRequest("Failed to create user", err)
		}

		return c.JSON(http.StatusOK, user)
	})
}
