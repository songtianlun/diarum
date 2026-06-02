package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/auth"
	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/store"
)

type imageUploadLocalSettings struct {
	Path string `json:"path"`
}

type imageUploadS3Settings struct {
	Bucket         string `json:"bucket"`
	Region         string `json:"region"`
	Endpoint       string `json:"endpoint"`
	AccessKey      string `json:"access_key"`
	Secret         string `json:"secret"`
	ForcePathStyle bool   `json:"force_path_style"`
}

type imageUploadCheveretoSettings struct {
	Domain  string `json:"domain"`
	APIKey  string `json:"api_key"`
	AlbumID string `json:"album_id"`
}

type imageUploadSettingsResponse struct {
	Provider  string                       `json:"provider"`
	Local     imageUploadLocalSettings     `json:"local"`
	S3        imageUploadS3Settings        `json:"s3"`
	Chevereto imageUploadCheveretoSettings `json:"chevereto"`
}

func RegisterImageUploadRoutes(e *echo.Echo, s *store.Store, authMiddleware echo.MiddlewareFunc) {
	configService := config.NewConfigService(s)
	group := e.Group("/api/v1/image-upload", authMiddleware)

	group.GET("/settings", func(c echo.Context) error {
		userID := auth.CurrentUser(c).ID
		settings, err := loadImageUploadSettings(configService, s, userID)
		if err != nil {
			return serverError("Failed to load image upload settings", err)
		}
		return c.JSON(http.StatusOK, settings)
	})

	group.PUT("/settings", func(c echo.Context) error {
		userID := auth.CurrentUser(c).ID
		var body imageUploadSettingsResponse
		if err := c.Bind(&body); err != nil {
			return badRequest("Invalid request body", err)
		}

		settings, err := normalizeImageUploadSettings(body, s)
		if err != nil {
			var httpErr *echo.HTTPError
			if errors.As(err, &httpErr) {
				return httpErr
			}
			return badRequest("Invalid image upload settings", err)
		}

		payload := map[string]any{
			"image_upload.provider":            settings.Provider,
			"image_upload.local.path":          settings.Local.Path,
			"image_upload.s3.bucket":           settings.S3.Bucket,
			"image_upload.s3.region":           settings.S3.Region,
			"image_upload.s3.endpoint":         settings.S3.Endpoint,
			"image_upload.s3.access_key":       settings.S3.AccessKey,
			"image_upload.s3.secret":           settings.S3.Secret,
			"image_upload.s3.force_path_style": settings.S3.ForcePathStyle,
			"chevereto.enabled":                settings.Provider == "chevereto",
			"chevereto.domain":                 settings.Chevereto.Domain,
			"chevereto.api_key":                settings.Chevereto.APIKey,
			"chevereto.album_id":               settings.Chevereto.AlbumID,
		}
		if err := configService.SetBatch(userID, payload); err != nil {
			return badRequest("Failed to save image upload settings", err)
		}

		updated, err := loadImageUploadSettings(configService, s, userID)
		if err != nil {
			return serverError("Failed to reload image upload settings", err)
		}
		return c.JSON(http.StatusOK, map[string]any{"success": true, "settings": updated})
	})
}

func loadImageUploadSettings(configService *config.ConfigService, s *store.Store, userID string) (*imageUploadSettingsResponse, error) {
	provider, err := configService.GetString(userID, "image_upload.provider")
	if err != nil {
		return nil, err
	}
	provider = normalizeImageUploadProvider(provider)
	if provider == "" {
		provider = "local"
	}

	localPath, err := configService.GetString(userID, "image_upload.local.path")
	if err != nil {
		return nil, err
	}
	bucket, err := configService.GetString(userID, "image_upload.s3.bucket")
	if err != nil {
		return nil, err
	}
	region, err := configService.GetString(userID, "image_upload.s3.region")
	if err != nil {
		return nil, err
	}
	endpoint, err := configService.GetString(userID, "image_upload.s3.endpoint")
	if err != nil {
		return nil, err
	}
	accessKey, err := configService.GetString(userID, "image_upload.s3.access_key")
	if err != nil {
		return nil, err
	}
	secret, err := configService.GetString(userID, "image_upload.s3.secret")
	if err != nil {
		return nil, err
	}
	forcePathStyle, err := configService.GetBool(userID, "image_upload.s3.force_path_style")
	if err != nil {
		return nil, err
	}
	domain, err := configService.GetString(userID, "chevereto.domain")
	if err != nil {
		return nil, err
	}
	apiKey, err := configService.GetString(userID, "chevereto.api_key")
	if err != nil {
		return nil, err
	}
	albumID, err := configService.GetString(userID, "chevereto.album_id")
	if err != nil {
		return nil, err
	}

	return &imageUploadSettingsResponse{
		Provider: provider,
		Local: imageUploadLocalSettings{
			Path: normalizeLocalPath(s, localPath),
		},
		S3: imageUploadS3Settings{
			Bucket:         bucket,
			Region:         region,
			Endpoint:       endpoint,
			AccessKey:      accessKey,
			Secret:         secret,
			ForcePathStyle: forcePathStyle,
		},
		Chevereto: imageUploadCheveretoSettings{
			Domain:  strings.TrimRight(strings.TrimSpace(domain), "/"),
			APIKey:  apiKey,
			AlbumID: albumID,
		},
	}, nil
}

func normalizeImageUploadSettings(settings imageUploadSettingsResponse, s *store.Store) (*imageUploadSettingsResponse, error) {
	settings.Provider = normalizeImageUploadProvider(settings.Provider)
	if settings.Provider == "" {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "Provider must be one of local, s3, chevereto")
	}

	settings.Local.Path = normalizeLocalPath(s, settings.Local.Path)
	settings.S3.Bucket = strings.TrimSpace(settings.S3.Bucket)
	settings.S3.Region = strings.TrimSpace(settings.S3.Region)
	settings.S3.Endpoint = strings.TrimSpace(settings.S3.Endpoint)
	settings.S3.AccessKey = strings.TrimSpace(settings.S3.AccessKey)
	settings.S3.Secret = strings.TrimSpace(settings.S3.Secret)
	settings.Chevereto.Domain = strings.TrimRight(strings.TrimSpace(settings.Chevereto.Domain), "/")
	settings.Chevereto.APIKey = strings.TrimSpace(settings.Chevereto.APIKey)
	settings.Chevereto.AlbumID = strings.TrimSpace(settings.Chevereto.AlbumID)

	if settings.Provider == "s3" {
		if settings.S3.Bucket == "" || settings.S3.Region == "" || settings.S3.AccessKey == "" || settings.S3.Secret == "" {
			return nil, echo.NewHTTPError(http.StatusBadRequest, "Bucket, region, access key and secret are required for S3")
		}
	}
	if settings.Provider == "chevereto" {
		if settings.Chevereto.Domain == "" || settings.Chevereto.APIKey == "" {
			return nil, echo.NewHTTPError(http.StatusBadRequest, "Domain and API Key are required for Chevereto")
		}
	}

	return &settings, nil
}

func normalizeImageUploadProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "local", "s3", "chevereto":
		return strings.ToLower(strings.TrimSpace(provider))
	default:
		return ""
	}
}

func normalizeLocalPath(s *store.Store, path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return s.DefaultLocalMediaDir()
	}
	return trimmed
}
