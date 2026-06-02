package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// RegisterAuthRoutes registers auth business API endpoints.
func RegisterAuthRoutes(app *pocketbase.PocketBase, e *core.ServeEvent) {
	client := &http.Client{Timeout: 10 * time.Second}

	e.Router.POST("/api/v1/auth/login", func(c echo.Context) error {
		var body struct {
			UsernameOrEmail string `json:"usernameOrEmail"`
			Password        string `json:"password"`
		}
		if err := c.Bind(&body); err != nil {
			return apis.NewBadRequestError("Invalid request body", err)
		}

		if body.UsernameOrEmail == "" || body.Password == "" {
			return apis.NewBadRequestError("usernameOrEmail and password are required", nil)
		}

		return proxyJSON(c, client, http.MethodPost, "/api/collections/users/auth-with-password", map[string]any{
			"identity": body.UsernameOrEmail,
			"password": body.Password,
		})
	}, apis.ActivityLogger(app))

	e.Router.POST("/api/v1/auth/register", func(c echo.Context) error {
		var body struct {
			Username        string `json:"username"`
			Email           string `json:"email"`
			Password        string `json:"password"`
			PasswordConfirm string `json:"passwordConfirm"`
		}
		if err := c.Bind(&body); err != nil {
			return apis.NewBadRequestError("Invalid request body", err)
		}

		if body.Username == "" || body.Email == "" || body.Password == "" || body.PasswordConfirm == "" {
			return apis.NewBadRequestError("username, email, password, and passwordConfirm are required", nil)
		}

		return proxyJSON(c, client, http.MethodPost, "/api/collections/users/records", map[string]any{
			"username":        body.Username,
			"email":           body.Email,
			"password":        body.Password,
			"passwordConfirm": body.PasswordConfirm,
		})
	}, apis.ActivityLogger(app))
}

func proxyJSON(c echo.Context, client *http.Client, method, path string, payload any) error {
	baseURL := c.Scheme() + "://" + c.Request().Host

	requestBody, err := json.Marshal(payload)
	if err != nil {
		return apis.NewBadRequestError("Failed to encode request", err)
	}

	req, err := http.NewRequest(method, baseURL+path, bytes.NewReader(requestBody))
	if err != nil {
		return apis.NewApiError(http.StatusInternalServerError, "Failed to create upstream request", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return apis.NewApiError(http.StatusBadGateway, "Upstream auth service request failed", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return apis.NewApiError(http.StatusBadGateway, "Failed to read upstream response", err)
	}

	return c.Blob(resp.StatusCode, echo.MIMEApplicationJSON, respBody)
}
