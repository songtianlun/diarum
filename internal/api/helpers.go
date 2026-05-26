package api

import (
	"net/http"

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
