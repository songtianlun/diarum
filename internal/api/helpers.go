package api

import (
	"net/http"
	"unicode/utf8"

	"github.com/labstack/echo/v5"
)

func badRequest(message string, err error) error {
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, message+": "+err.Error())
	}
	return echo.NewHTTPError(http.StatusBadRequest, message)
}

func unauthorized(message string) error {
	return echo.NewHTTPError(http.StatusUnauthorized, message)
}

func forbidden(message string) error {
	return echo.NewHTTPError(http.StatusForbidden, message)
}

func notFound(message string) error {
	return echo.NewHTTPError(http.StatusNotFound, message)
}

func serverError(message string, err error) error {
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, message+": "+err.Error())
	}
	return echo.NewHTTPError(http.StatusInternalServerError, message)
}

// truncate cuts s to at most n bytes without splitting a UTF-8 character.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
