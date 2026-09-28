package api

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/audit"
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
			// Failed attempts are recorded with the identity tried, whether or
			// not it exists, so guessing and credential stuffing stand out.
			detail := map[string]any{"identity": truncate(strings.TrimSpace(body.UsernameOrEmail), 100), "known": err == nil}
			if err == nil {
				recordAuditFor(c, user.ID, user.Username, audit.SourceWeb, audit.ActionAuthLoginFail, "", detail)
			} else {
				recordAuditFor(c, "", "", audit.SourceWeb, audit.ActionAuthLoginFail, "", detail)
			}
			return unauthorized("Invalid login credentials")
		}

		token, err := authService.IssueToken(user)
		if err != nil {
			return serverError("Failed to issue token", err)
		}

		recordAuditFor(c, user.ID, user.Username, audit.SourceWeb, audit.ActionAuthLogin, "", nil)
		return c.JSON(http.StatusOK, map[string]any{
			"token":  token,
			"record": user,
		})
	})

	// Sessions are stateless tokens, so signing out only needs recording;
	// the client discards its token.
	e.POST("/api/v1/auth/logout", func(c echo.Context) error {
		recordAudit(c, audit.ActionAuthLogout, "", nil)
		return c.NoContent(http.StatusNoContent)
	}, authService.Middleware)

	// The signed-in account, including its role.
	e.GET("/api/v1/auth/me", func(c echo.Context) error {
		return c.JSON(http.StatusOK, auth.CurrentUser(c))
	}, authService.Middleware)

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
			recordAuditFor(c, "", "", audit.SourceWeb, audit.ActionAuthRegister, truncate(body.Username, 100), map[string]any{"failed": true})
			return badRequest("Failed to create user", err)
		}
		recordAuditFor(c, user.ID, user.Username, audit.SourceWeb, audit.ActionAuthRegister, user.Username, map[string]any{"role": user.Role})

		return c.JSON(http.StatusOK, user)
	})
}
