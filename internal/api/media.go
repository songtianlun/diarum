package api

import (
	"net/http"
	"os"
	"strconv"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/auth"
	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/store"
)

func RegisterMediaRoutes(e *echo.Echo, s *store.Store, authMiddleware echo.MiddlewareFunc) {
	group := e.Group("/api/v1/media", authMiddleware)

	group.GET("", func(c echo.Context) error {
		user := auth.CurrentUser(c)
		page := parsePositiveInt(c.QueryParam("page"), 1)
		perPage := parsePositiveInt(c.QueryParam("perPage"), 50)
		items, total, err := s.ListMedia(user.ID, page, perPage)
		if err != nil {
			return serverError("Failed to fetch media", err)
		}
		return c.JSON(http.StatusOK, map[string]any{
			"page":       page,
			"perPage":    perPage,
			"totalItems": total,
			"totalPages": store.TotalPages(total, perPage),
			"items":      items,
		})
	})

	group.GET("/:id", func(c echo.Context) error {
		user := auth.CurrentUser(c)
		media, err := s.GetMedia(c.PathParam("id"), user.ID)
		if err != nil {
			return notFound("Media not found")
		}
		return c.JSON(http.StatusOK, media)
	})

	group.POST("", func(c echo.Context) error {
		user := auth.CurrentUser(c)
		file, header, err := c.Request().FormFile("file")
		if err != nil {
			return badRequest("No file provided", err)
		}
		defer file.Close()

		buffer := make([]byte, 512)
		n, _ := file.Read(buffer)
		if seeker, ok := file.(interface {
			Seek(int64, int) (int64, error)
		}); ok {
			_, _ = seeker.Seek(0, 0)
		}
		if detected, allowed := config.IsAllowedMediaType(buffer[:n]); !allowed {
			return badRequest("Invalid file type: "+detected, nil)
		}
		if header.Size > 50*1024*1024 {
			return badRequest("File size exceeds 50MB limit", nil)
		}

		filename := store.SafeFilename(header.Filename)
		name := c.FormValue("name")
		if name == "" {
			name = filename
		}
		alt := c.FormValue("alt")
		diary := c.Request().Form["diary"]
		media, err := s.CreateMedia(user.ID, filename, name, alt, diary)
		if err != nil {
			return badRequest("Failed to create media", err)
		}
		if err := s.SaveUploadedFile(s.NewMediaFilePath(media.ID, filename), file); err != nil {
			_ = s.DeleteMedia(media.ID, user.ID)
			return serverError("Failed to save media file", err)
		}
		return c.JSON(http.StatusOK, media)
	})

	group.PATCH("/:id", func(c echo.Context) error {
		user := auth.CurrentUser(c)
		var body struct {
			Diary []string `json:"diary"`
		}
		if err := c.Bind(&body); err != nil {
			return badRequest("Invalid request body", err)
		}
		media, err := s.UpdateMediaDiary(c.PathParam("id"), user.ID, body.Diary)
		if err != nil {
			return notFound("Media not found")
		}
		return c.JSON(http.StatusOK, media)
	})

	group.DELETE("/:id", func(c echo.Context) error {
		user := auth.CurrentUser(c)
		media, err := s.GetMedia(c.PathParam("id"), user.ID)
		if err != nil {
			return notFound("Media not found")
		}
		path := s.MediaFilePath(media)
		if err := s.DeleteMedia(media.ID, user.ID); err != nil {
			return notFound("Media not found")
		}
		_ = os.Remove(path)
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	e.GET("/api/v1/files/media/:id/:filename", func(c echo.Context) error {
		media, err := s.GetMedia(c.PathParam("id"), "")
		if err != nil || media.File != c.PathParam("filename") {
			return notFound("File not found")
		}
		return c.File(s.MediaFilePath(media))
	})
}

func parsePositiveInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
