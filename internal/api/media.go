package api

import (
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
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
		if err := s.SaveUploadedMedia(media, file); err != nil {
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
		if err := s.DeleteMedia(media.ID, user.ID); err != nil {
			return notFound("Media not found")
		}
		if err := s.DeleteMediaFile(media); err != nil && !os.IsNotExist(err) {
			return serverError("Failed to delete media file", err)
		}
		return c.JSON(http.StatusOK, map[string]any{"success": true})
	})

	e.GET("/api/v1/files/media/:id/:filename", func(c echo.Context) error {
		media, err := s.GetMedia(c.PathParam("id"), "")
		if err != nil || media.File != c.PathParam("filename") {
			return notFound("File not found")
		}
		path := s.MediaFilePath(media)
		if _, err := os.Stat(path); err == nil {
			return c.File(path)
		}

		reader, err := s.OpenMediaFile(media)
		if err != nil {
			return notFound("File not found")
		}
		defer reader.Close()

		head := make([]byte, 512)
		n, readErr := reader.Read(head)
		if readErr != nil && readErr != io.EOF {
			return serverError("Failed to read media file", readErr)
		}

		contentType := http.DetectContentType(head[:n])
		if guessed := mime.TypeByExtension(filepath.Ext(media.File)); guessed != "" {
			contentType = guessed
		}
		c.Response().Header().Set(echo.HeaderContentType, contentType)
		c.Response().WriteHeader(http.StatusOK)
		if n > 0 {
			if _, err := c.Response().Write(head[:n]); err != nil {
				return err
			}
		}
		_, err = io.Copy(c.Response().Writer, reader)
		return err
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
