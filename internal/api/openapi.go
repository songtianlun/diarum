package api

import (
	"net/http"
	"sort"
	"strings"

	"github.com/labstack/echo/v5"
)

// RegisterOpenAPIRoutes registers debug-only OpenAPI routes.
// The spec is generated from currently registered Echo routes at request time.
func RegisterOpenAPIRoutes(e *echo.Echo, version, name string) {
	handler := func(c echo.Context) error {
		spec := buildOpenAPISpec(e.Router().Routes(), version, name)
		return c.JSON(http.StatusOK, spec)
	}

	docsHandler := func(c echo.Context) error {
		html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Diarum API Docs</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
  <style>
    body { margin: 0; background: #f7f8fa; }
    #swagger-ui { max-width: 1200px; margin: 0 auto; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.ui = SwaggerUIBundle({
      url: '/api/openapi.json',
      dom_id: '#swagger-ui',
      deepLinking: true,
      docExpansion: 'list',
      defaultModelsExpandDepth: 1
    });
  </script>
</body>
</html>`
		return c.HTML(http.StatusOK, html)
	}

	// Keep both v1 and legacy aliases for convenience in local dev.
	e.GET("/api/v1/openapi.json", handler)
	e.GET("/api/openapi.json", handler)
	e.GET("/api/v1/docs", docsHandler)
	e.GET("/api/docs", docsHandler)
}

func buildOpenAPISpec(routes echo.Routes, version, name string) map[string]any {
	paths := make(map[string]any)

	routeCopies := make([]echo.RouteInfo, 0, len(routes))
	for _, route := range routes {
		if route == nil {
			continue
		}
		if !strings.HasPrefix(route.Path(), "/api/") {
			continue
		}
		if route.Path() == "/api/openapi.json" || route.Path() == "/api/v1/openapi.json" || route.Path() == "/api/docs" || route.Path() == "/api/v1/docs" {
			continue
		}
		if strings.Contains(route.Path(), "/*") {
			continue
		}
		routeCopies = append(routeCopies, route)
	}

	sort.Slice(routeCopies, func(i, j int) bool {
		if routeCopies[i].Path() == routeCopies[j].Path() {
			return routeCopies[i].Method() < routeCopies[j].Method()
		}
		return routeCopies[i].Path() < routeCopies[j].Path()
	})

	for _, route := range routeCopies {
		path := toOpenAPIPath(route.Path())
		method := strings.ToLower(route.Method())
		if method == "" {
			continue
		}

		pathItem, ok := paths[path].(map[string]any)
		if !ok {
			pathItem = map[string]any{}
		}
		pathItem[method] = buildOperation(route.Method(), path)
		paths[path] = pathItem
	}

	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       name + " API",
			"version":     version,
			"description": "Auto-generated API document from registered routes (debug mode).",
		},
		"servers": []map[string]any{
			{"url": "/"},
		},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "JWT",
				},
				"apiTokenQuery": map[string]any{
					"type": "apiKey",
					"in":   "query",
					"name": "token",
				},
			},
		},
		"paths": paths,
	}
}

func buildOperation(method, path string) map[string]any {
	summary := strings.TrimSpace(method + " " + path)
	op := map[string]any{
		"summary":     summary,
		"operationId": strings.ToLower(method) + strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_").Replace(strings.TrimPrefix(path, "/")),
		"responses": map[string]any{
			"200": map[string]any{"description": "OK"},
			"400": map[string]any{"description": "Bad Request"},
			"401": map[string]any{"description": "Unauthorized"},
			"403": map[string]any{"description": "Forbidden"},
			"404": map[string]any{"description": "Not Found"},
		},
	}

	if strings.HasPrefix(path, "/api/v1/diaries") && method == "GET" && path == "/api/v1/diaries" {
		op["security"] = []map[string][]string{{"apiTokenQuery": {}}}
	} else if !strings.HasSuffix(path, "/version") {
		op["security"] = []map[string][]string{{"bearerAuth": {}}}
	}

	if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
		op["requestBody"] = map[string]any{
			"required": true,
			"content": map[string]any{
				"application/json": map[string]any{
					"schema": map[string]any{"type": "object"},
				},
			},
		}
	}

	return op
}

func toOpenAPIPath(path string) string {
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		if seg == "" {
			continue
		}
		if strings.HasPrefix(seg, ":") {
			segments[i] = "{" + strings.TrimPrefix(seg, ":") + "}"
		}
	}
	return strings.Join(segments, "/")
}
