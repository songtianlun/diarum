package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/logger"
	"github.com/songtianlun/diarum/internal/store"
)

// mcpProtocolVersion is the MCP specification revision this server speaks.
const mcpProtocolVersion = "2025-06-18"

// mcpServerName and mcpServerVersion identify this server during initialize.
const mcpServerName = "diarum"

// JSON-RPC 2.0 error codes used by the MCP endpoint.
const (
	jsonRPCParseError     = -32700
	jsonRPCInvalidRequest = -32600
	jsonRPCMethodNotFound = -32601
	jsonRPCInvalidParams  = -32602
	jsonRPCInternalError  = -32603
)

// jsonRPCRequest is a single JSON-RPC 2.0 request. A request without an ID is
// a notification and gets no response body.
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

func rpcResult(id json.RawMessage, result any) *jsonRPCResponse {
	return &jsonRPCResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func rpcError(id json.RawMessage, code int, message string) *jsonRPCResponse {
	return &jsonRPCResponse{JSONRPC: "2.0", ID: id, Error: &jsonRPCError{Code: code, Message: message}}
}

// mcpToolDefinitions lists the read-only tools exposed to MCP clients. The
// permission scope matches the public /api/v1/diaries endpoint: a token can
// read its owner's diaries and nothing else.
func mcpToolDefinitions() []map[string]any {
	return []map[string]any{
		{
			"name":        "get_diary",
			"description": "Get the diary entry for a single date. Returns the content, mood and weather, or exists=false when nothing was written that day.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"date": map[string]any{
						"type":        "string",
						"description": "Date in YYYY-MM-DD format.",
					},
				},
				"required": []string{"date"},
			},
		},
		{
			"name":        "list_diaries",
			"description": "List diary entries within an inclusive date range, newest first.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"start": map[string]any{
						"type":        "string",
						"description": "Range start date in YYYY-MM-DD format.",
					},
					"end": map[string]any{
						"type":        "string",
						"description": "Range end date in YYYY-MM-DD format.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of entries to return (default 50, max 200).",
					},
				},
				"required": []string{"start", "end"},
			},
		},
		{
			"name":        "search_diaries",
			"description": "Full-text search across diary content, newest first.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Text to search for in diary content.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of entries to return (default 20, max 200).",
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

// RegisterMCPRoutes registers the MCP Streamable HTTP endpoint. It reuses the
// existing API token for authentication and the api.mcp_enabled setting as an
// independent on/off switch layered on top of api.enabled.
func RegisterMCPRoutes(e *echo.Echo, s *store.Store, version string) {
	configService := config.NewConfigService(s)

	handler := func(c echo.Context) error {
		userId, err := authenticateMCP(c, configService)
		if err != nil {
			return err
		}

		body, err := parseRPCBody(c)
		if err != nil {
			return c.JSON(http.StatusOK, rpcError(nil, jsonRPCParseError, "Invalid JSON payload"))
		}

		// A batch request is answered with an array; a single request with an
		// object. Notifications are dropped from the response either way.
		responses := make([]*jsonRPCResponse, 0, len(body.requests))
		for _, req := range body.requests {
			resp := dispatchMCP(s, userId, version, req)
			if resp != nil {
				responses = append(responses, resp)
			}
		}

		if len(responses) == 0 {
			// Every message was a notification: MCP expects 202 with no body.
			return c.NoContent(http.StatusAccepted)
		}
		if body.batch {
			return c.JSON(http.StatusOK, responses)
		}
		return c.JSON(http.StatusOK, responses[0])
	}

	e.POST("/api/v1/mcp", handler)

	// Streamable HTTP clients may probe GET/DELETE for a resumable SSE stream.
	// This server is stateless, so both are politely declined.
	e.GET("/api/v1/mcp", func(c echo.Context) error {
		return echo.NewHTTPError(http.StatusMethodNotAllowed, "This MCP server is stateless; use POST")
	})
	e.DELETE("/api/v1/mcp", func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})
}

// authenticateMCP resolves the API token from the Authorization header or the
// token query parameter, then verifies both the API and MCP switches.
func authenticateMCP(c echo.Context, configService *config.ConfigService) (string, error) {
	token := strings.TrimSpace(c.QueryParam("token"))
	if header := c.Request().Header.Get("Authorization"); token == "" && header != "" {
		if len(header) > 7 && strings.EqualFold(header[:7], "Bearer ") {
			token = strings.TrimSpace(header[7:])
		}
	}
	if token == "" {
		return "", unauthorized("API token is required")
	}

	userId, err := configService.ValidateTokenAndGetUser(token)
	if err == config.ErrAPIDisabled {
		return "", unauthorized("API is disabled for this user")
	}
	if err != nil || userId == "" {
		return "", unauthorized("Invalid API token")
	}

	enabled, err := configService.GetBool(userId, "api.mcp_enabled")
	if err != nil {
		logger.Debug("[MCP] error checking mcp enabled: %v", err)
		return "", serverError("Failed to check MCP status", err)
	}
	if !enabled {
		return "", unauthorized("MCP is disabled for this user")
	}

	return userId, nil
}

type rpcBody struct {
	requests []jsonRPCRequest
	batch    bool
}

func parseRPCBody(c echo.Context) (*rpcBody, error) {
	var raw json.RawMessage
	if err := json.NewDecoder(c.Request().Body).Decode(&raw); err != nil {
		return nil, err
	}

	trimmed := strings.TrimLeft(string(raw), " \t\r\n")
	if strings.HasPrefix(trimmed, "[") {
		var requests []jsonRPCRequest
		if err := json.Unmarshal(raw, &requests); err != nil {
			return nil, err
		}
		return &rpcBody{requests: requests, batch: true}, nil
	}

	var request jsonRPCRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	return &rpcBody{requests: []jsonRPCRequest{request}, batch: false}, nil
}

// dispatchMCP routes one JSON-RPC message. It returns nil for notifications,
// which carry no ID and must not be answered.
func dispatchMCP(s *store.Store, userId, version string, req jsonRPCRequest) *jsonRPCResponse {
	isNotification := len(req.ID) == 0

	switch req.Method {
	case "initialize":
		if isNotification {
			return nil
		}
		return rpcResult(req.ID, map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    mcpServerName,
				"version": version,
			},
		})

	case "notifications/initialized", "notifications/cancelled":
		return nil

	case "ping":
		if isNotification {
			return nil
		}
		return rpcResult(req.ID, map[string]any{})

	case "tools/list":
		if isNotification {
			return nil
		}
		return rpcResult(req.ID, map[string]any{"tools": mcpToolDefinitions()})

	case "tools/call":
		if isNotification {
			return nil
		}
		return callMCPTool(s, userId, req)

	default:
		if isNotification {
			return nil
		}
		return rpcError(req.ID, jsonRPCMethodNotFound, "Unknown method: "+req.Method)
	}
}

type toolCallParams struct {
	Name      string `json:"name"`
	Arguments struct {
		Date  string `json:"date"`
		Start string `json:"start"`
		End   string `json:"end"`
		Query string `json:"query"`
		Limit int    `json:"limit"`
	} `json:"arguments"`
}

func callMCPTool(s *store.Store, userId string, req jsonRPCRequest) *jsonRPCResponse {
	var params toolCallParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return rpcError(req.ID, jsonRPCInvalidParams, "Invalid tool arguments")
		}
	}

	args := params.Arguments

	switch params.Name {
	case "get_diary":
		if args.Date == "" {
			return toolError(req.ID, "The 'date' argument is required (YYYY-MM-DD).")
		}
		diary, err := s.GetDiaryByDate(userId, args.Date+" 00:00:00.000Z", args.Date+" 23:59:59.999Z")
		if err != nil {
			return toolSuccess(req.ID, map[string]any{"date": args.Date, "content": "", "exists": false})
		}
		return toolSuccess(req.ID, map[string]any{
			"id":      diary.ID,
			"date":    args.Date,
			"content": diary.Content,
			"mood":    diary.Mood,
			"weather": diary.Weather,
			"exists":  true,
		})

	case "list_diaries":
		if args.Start == "" || args.End == "" {
			return toolError(req.ID, "Both 'start' and 'end' arguments are required (YYYY-MM-DD).")
		}
		diaries, err := s.ListDiaries(userId, args.Start+" 00:00:00.000Z", args.End+" 23:59:59.999Z", "-date", clampLimit(args.Limit, 50))
		if err != nil {
			return rpcError(req.ID, jsonRPCInternalError, "Failed to query diaries")
		}
		return toolSuccess(req.ID, map[string]any{
			"diaries": summarizeDiaries(diaries),
			"total":   len(diaries),
		})

	case "search_diaries":
		if strings.TrimSpace(args.Query) == "" {
			return toolError(req.ID, "The 'query' argument is required.")
		}
		diaries, err := s.SearchDiaries(userId, args.Query, clampLimit(args.Limit, 20))
		if err != nil {
			return rpcError(req.ID, jsonRPCInternalError, "Failed to search diaries")
		}
		return toolSuccess(req.ID, map[string]any{
			"diaries": summarizeDiaries(diaries),
			"total":   len(diaries),
		})

	default:
		return rpcError(req.ID, jsonRPCInvalidParams, "Unknown tool: "+params.Name)
	}
}

func summarizeDiaries(diaries []*store.Diary) []map[string]any {
	results := make([]map[string]any, 0, len(diaries))
	for _, diary := range diaries {
		results = append(results, map[string]any{
			"id":      diary.ID,
			"date":    store.DateOnly(diary.Date),
			"content": diary.Content,
			"mood":    diary.Mood,
			"weather": diary.Weather,
		})
	}
	return results
}

func clampLimit(limit, fallback int) int {
	if limit <= 0 {
		return fallback
	}
	if limit > 200 {
		return 200
	}
	return limit
}

// toolSuccess wraps a payload in an MCP tool result. The JSON is returned as
// text content, which every MCP client understands.
func toolSuccess(id json.RawMessage, payload any) *jsonRPCResponse {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return rpcError(id, jsonRPCInternalError, "Failed to encode tool result")
	}
	return rpcResult(id, map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": string(encoded)},
		},
	})
}

// toolError reports a tool-level failure, which MCP models as a successful
// response carrying isError rather than a protocol error.
func toolError(id json.RawMessage, message string) *jsonRPCResponse {
	return rpcResult(id, map[string]any{
		"isError": true,
		"content": []map[string]any{
			{"type": "text", "text": message},
		},
	})
}
