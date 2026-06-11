package api

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/auth"
	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/store"
)

const (
	memosBeginPrefix = "<!-- DIARUM:MEMOS:BEGIN"
	memosEndPrefix   = "<!-- DIARUM:MEMOS:END"
)

type memosSettings struct {
	Enabled     bool   `json:"enabled"`
	BaseURL     string `json:"base_url"`
	WebhookURL  string `json:"webhook_url"`
	TokenExists bool   `json:"token_exists"`
}

type memosMemo struct {
	ID         string
	Name       string
	Content    string
	CreateTime string
	UpdateTime string
	URL        string
}

type memosWebhookEvent struct {
	Action string
	Memo   memosMemo
}

// RegisterMemosRoutes registers Memos webhook sync endpoints.
func RegisterMemosRoutes(e *echo.Echo, s *store.Store, authMiddleware echo.MiddlewareFunc, onDiaryChanged func(string)) {
	configService := config.NewConfigService(s)

	e.POST("/api/v1/memos/webhook/:token", func(c echo.Context) error {
		userID, err := validateMemosWebhookToken(s, c.PathParam("token"))
		if err != nil {
			return serverError("Failed to validate webhook token", err)
		}
		if userID == "" {
			return echo.ErrUnauthorized
		}
		enabled, _ := configService.GetBool(userID, "memos.enabled")
		if !enabled {
			return echo.NewHTTPError(http.StatusForbidden, "Memos sync is disabled")
		}

		var payload map[string]any
		if err := json.NewDecoder(c.Request().Body).Decode(&payload); err != nil {
			return badRequest("Invalid request body", err)
		}
		event := parseMemosWebhookEvent(payload)
		if event.Memo.ID == "" {
			return badRequest("Missing memo id", nil)
		}
		if event.Memo.URL == "" {
			baseURL, _ := configService.GetString(userID, "memos.base_url")
			event.Memo.URL = buildMemosURL(baseURL, event.Memo.ID)
		}

		changed, err := syncMemosMemo(s, userID, event)
		if err != nil {
			return serverError("Failed to sync memo", err)
		}
		if changed && onDiaryChanged != nil {
			onDiaryChanged(userID)
		}
		return c.JSON(http.StatusOK, map[string]any{"success": true, "changed": changed})
	})

	group := e.Group("/api/v1/memos", authMiddleware)
	group.GET("/settings", func(c echo.Context) error {
		userID := auth.CurrentUser(c).ID
		settings, err := loadMemosSettings(c, configService, userID)
		if err != nil {
			return serverError("Failed to load Memos settings", err)
		}
		return c.JSON(http.StatusOK, settings)
	})

	group.PUT("/settings", func(c echo.Context) error {
		userID := auth.CurrentUser(c).ID
		var body struct {
			Enabled bool   `json:"enabled"`
			BaseURL string `json:"base_url"`
		}
		if err := c.Bind(&body); err != nil {
			return badRequest("Invalid request body", err)
		}
		if body.Enabled {
			token, _ := configService.GetString(userID, "memos.webhook_token")
			if token == "" {
				newToken, err := generateToken()
				if err != nil {
					return serverError("Failed to generate webhook token", err)
				}
				if err := configService.Set(userID, "memos.webhook_token", newToken); err != nil {
					return serverError("Failed to save webhook token", err)
				}
			}
		}
		if err := configService.SetBatch(userID, map[string]any{
			"memos.enabled":  body.Enabled,
			"memos.base_url": strings.TrimSpace(body.BaseURL),
		}); err != nil {
			return serverError("Failed to save Memos settings", err)
		}
		settings, err := loadMemosSettings(c, configService, userID)
		if err != nil {
			return serverError("Failed to load Memos settings", err)
		}
		return c.JSON(http.StatusOK, settings)
	})

	group.POST("/settings/reset-token", func(c echo.Context) error {
		userID := auth.CurrentUser(c).ID
		newToken, err := generateToken()
		if err != nil {
			return serverError("Failed to generate webhook token", err)
		}
		if err := configService.Set(userID, "memos.webhook_token", newToken); err != nil {
			return serverError("Failed to save webhook token", err)
		}
		settings, err := loadMemosSettings(c, configService, userID)
		if err != nil {
			return serverError("Failed to load Memos settings", err)
		}
		return c.JSON(http.StatusOK, settings)
	})
}

func loadMemosSettings(c echo.Context, configService *config.ConfigService, userID string) (*memosSettings, error) {
	enabled, _ := configService.GetBool(userID, "memos.enabled")
	baseURL, _ := configService.GetString(userID, "memos.base_url")
	token, _ := configService.GetString(userID, "memos.webhook_token")
	return &memosSettings{
		Enabled:     enabled,
		BaseURL:     baseURL,
		WebhookURL:  absoluteURL(c, "/api/v1/memos/webhook/"+token),
		TokenExists: token != "",
	}, nil
}

func validateMemosWebhookToken(s *store.Store, token string) (string, error) {
	if token == "" {
		return "", nil
	}
	rows, err := s.DB.Query(`SELECT user, value FROM user_settings WHERE key = 'memos.webhook_token'`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var userID string
		var raw sql.NullString
		if err := rows.Scan(&userID, &raw); err != nil {
			return "", err
		}
		var stored string
		if raw.Valid {
			_ = json.Unmarshal([]byte(raw.String), &stored)
		}
		if stored != "" && subtle.ConstantTimeCompare([]byte(stored), []byte(token)) == 1 {
			return userID, nil
		}
	}
	return "", rows.Err()
}

func absoluteURL(c echo.Context, path string) string {
	if strings.HasSuffix(path, "/") {
		path = strings.TrimRight(path, "/")
	}
	if path == "/api/v1/memos/webhook" || strings.HasSuffix(path, "/webhook/") {
		return ""
	}
	req := c.Request()
	scheme := req.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		scheme = "http"
		if req.TLS != nil {
			scheme = "https"
		}
	}
	host := req.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = req.Host
	}
	return scheme + "://" + host + path
}

func parseMemosWebhookEvent(payload map[string]any) memosWebhookEvent {
	action := normalizeMemosAction(firstString(payload, "action", "event", "type", "activityType"))
	memoMap := firstMap(payload, "memo", "data", "resource")
	if nested := firstMap(memoMap, "memo", "resource"); len(nested) > 0 {
		memoMap = nested
	}
	if len(memoMap) == 0 {
		memoMap = payload
	}
	memo := memosMemo{
		ID:         memoID(memoMap),
		Name:       firstString(memoMap, "name"),
		Content:    firstString(memoMap, "content"),
		CreateTime: firstString(memoMap, "createTime", "create_time", "createdTime", "created_time", "createdTs", "created_ts", "createTs", "create_ts", "createdAt", "created_at", "created"),
		UpdateTime: firstString(memoMap, "updateTime", "update_time", "updatedTime", "updated_time", "updatedTs", "updated_ts", "updateTs", "update_ts", "updatedAt", "updated_at", "updated"),
		URL:        firstString(memoMap, "url", "link", "htmlUrl", "html_url"),
	}
	if action == "" {
		if strings.EqualFold(firstString(memoMap, "state", "rowStatus", "row_status"), "ARCHIVED") {
			action = "delete"
		} else {
			action = "upsert"
		}
	}
	return memosWebhookEvent{Action: action, Memo: memo}
}

func normalizeMemosAction(action string) string {
	action = strings.ToLower(strings.TrimSpace(action))
	switch {
	case strings.Contains(action, "delete"), strings.Contains(action, "archive"):
		return "delete"
	case strings.Contains(action, "create"), strings.Contains(action, "update"), strings.Contains(action, "upsert"):
		return "upsert"
	default:
		return action
	}
}

func firstMap(source map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if value, ok := source[key]; ok {
			if item, ok := value.(map[string]any); ok {
				return item
			}
		}
	}
	return nil
}

func firstString(source map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := source[key]; ok {
			if str := stringValue(value); str != "" {
				return str
			}
		}
	}
	return ""
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strings.TrimSpace(fmt.Sprintf("%.0f", v))
	case map[string]any:
		if formatted := protobufTimestampString(v); formatted != "" {
			return formatted
		}
	}
	return ""
}

func protobufTimestampString(value map[string]any) string {
	secondsRaw, ok := value["seconds"]
	if !ok {
		return ""
	}
	seconds, ok := int64Value(secondsRaw)
	if !ok {
		return ""
	}
	var nanos int64
	if nanosRaw, ok := value["nanos"]; ok {
		nanos, _ = int64Value(nanosRaw)
	}
	return time.Unix(seconds, nanos).UTC().Format(time.RFC3339)
}

func int64Value(value any) (int64, bool) {
	switch v := value.(type) {
	case float64:
		return int64(v), true
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func memoID(memo map[string]any) string {
	for _, key := range []string{"id", "uid", "memoId", "memo_id", "memo", "resource"} {
		if value := firstString(memo, key); value != "" {
			return lastPathPart(value)
		}
	}
	name := firstString(memo, "name")
	if name == "" {
		return ""
	}
	return lastPathPart(name)
}

func lastPathPart(value string) string {
	parts := strings.Split(strings.Trim(value, "/"), "/")
	return parts[len(parts)-1]
}

func syncMemosMemo(s *store.Store, userID string, event memosWebhookEvent) (bool, error) {
	date := memoDate(event.Memo)
	if event.Action == "delete" {
		return removeMemosBlock(s, userID, event.Memo.ID, date)
	}
	if date == "" {
		return false, fmt.Errorf("missing or invalid memo create_time/update_time for memo %s", event.Memo.ID)
	}
	block := renderMemosBlock(event.Memo, date)
	return upsertMemosBlock(s, userID, event.Memo.ID, date, block)
}

func memoDate(memo memosMemo) string {
	if date := memoDateFromValue(memo.CreateTime); date != "" {
		return date
	}
	return memoDateFromValue(memo.UpdateTime)
}

func memoDateFromValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if unixDate := memoDateFromUnixTimestamp(value); unixDate != "" {
		return unixDate
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC().Format("2006-01-02")
		}
	}
	return ""
}

func memoDateFromUnixTimestamp(value string) string {
	if !regexp.MustCompile(`^\d{10,19}$`).MatchString(value) {
		return ""
	}
	timestamp, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return ""
	}
	switch {
	case len(value) >= 19:
		return time.Unix(0, timestamp).UTC().Format("2006-01-02")
	case len(value) >= 16:
		return time.UnixMicro(timestamp).UTC().Format("2006-01-02")
	case len(value) >= 13:
		return time.UnixMilli(timestamp).UTC().Format("2006-01-02")
	default:
		return time.Unix(timestamp, 0).UTC().Format("2006-01-02")
	}
}

func renderMemosBlock(memo memosMemo, date string) string {
	lines := []string{
		fmt.Sprintf(`%s id="%s" date="%s" -->`, memosBeginPrefix, markerEscape(memo.ID), date),
		"<hr>",
		renderMemosMetadataHTML(memo),
	}
	lines = append(lines, renderMemoContentHTML(memo.Content), "<hr>", fmt.Sprintf(`%s id="%s" -->`, memosEndPrefix, markerEscape(memo.ID)))
	return strings.Join(lines, "\n")
}

func renderMemosMetadataHTML(memo memosMemo) string {
	metadata := []string{
		"Source: Memos",
		"Memo ID: " + memo.ID,
	}
	if memo.URL != "" {
		metadata = append(metadata, "Memo URL: "+memo.URL)
	}
	if memo.CreateTime != "" {
		metadata = append(metadata, "Created: "+memo.CreateTime)
	}
	if memo.UpdateTime != "" {
		metadata = append(metadata, "Updated: "+memo.UpdateTime)
	}
	return "<pre><code>" + html.EscapeString(strings.Join(metadata, "\n")) + "</code></pre>"
}

func renderMemoContentHTML(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return "<p></p>"
	}
	paragraphs := strings.Split(content, "\n\n")
	for i, paragraph := range paragraphs {
		escaped := html.EscapeString(strings.TrimSpace(paragraph))
		escaped = strings.ReplaceAll(escaped, "\n", "<br>")
		paragraphs[i] = "<p>" + escaped + "</p>"
	}
	return strings.Join(paragraphs, "\n")
}

func markerEscape(value string) string {
	value = strings.ReplaceAll(value, `"`, `&quot;`)
	return strings.ReplaceAll(value, "-->", "--&gt;")
}

func buildMemosURL(baseURL, id string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || id == "" {
		return ""
	}
	return baseURL + "/m/" + id
}

func upsertMemosBlock(s *store.Store, userID, memoID, date, block string) (bool, error) {
	diary, err := findMemosDiary(s, userID, memoID, date)
	if err != nil && !isNotFound(err) {
		return false, err
	}
	if diary == nil {
		_, _, err := s.UpsertDiary(userID, date, block, "", "")
		return err == nil, err
	}
	content, replaced := replaceMemosBlock(diary.Content, memoID, block)
	if !replaced {
		content = appendMemosBlock(diary.Content, block)
	}
	_, _, err = s.UpsertDiary(userID, store.DateOnly(diary.Date), content, diary.Mood, diary.Weather)
	return err == nil, err
}

func removeMemosBlock(s *store.Store, userID, memoID, date string) (bool, error) {
	diary, err := findMemosDiary(s, userID, memoID, date)
	if err != nil && !isNotFound(err) {
		return false, err
	}
	if diary == nil {
		return false, nil
	}
	content, removed := removeMemosBlockFromContent(diary.Content, memoID)
	if !removed {
		return false, nil
	}
	_, _, err = s.UpsertDiary(userID, store.DateOnly(diary.Date), strings.TrimSpace(content), diary.Mood, diary.Weather)
	return err == nil, err
}

func findMemosDiary(s *store.Store, userID, memoID, date string) (*store.Diary, error) {
	var dateDiary *store.Diary
	if date != "" {
		diary, err := s.GetDiaryByDate(userID, date+" 00:00:00.000Z", date+" 23:59:59.999Z")
		if err == nil && strings.Contains(diary.Content, memosMarker(memoID)) {
			return diary, nil
		}
		if err != nil && !isNotFound(err) {
			return nil, err
		}
		if err == nil && diary != nil {
			dateDiary = diary
		}
	}
	diaries, err := s.ListDiaries(userID, "", "", "-date", 0)
	if err != nil {
		return nil, err
	}
	for _, diary := range diaries {
		if strings.Contains(diary.Content, memosMarker(memoID)) {
			return diary, nil
		}
	}
	if dateDiary != nil {
		return dateDiary, nil
	}
	return nil, sql.ErrNoRows
}

func appendMemosBlock(content, block string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return block
	}
	if endsWithHorizontalRule(content) {
		block = trimLeadingMemosHorizontalRule(block)
	}
	return content + "\n\n" + block
}

func endsWithHorizontalRule(content string) bool {
	content = strings.TrimSpace(content)
	return regexp.MustCompile(`(?i)<hr\s*/?>$`).MatchString(content)
}

func trimLeadingMemosHorizontalRule(block string) string {
	return regexp.MustCompile(`(?i)^\s*<!-- DIARUM:MEMOS:BEGIN([^>]*)-->\s*<hr\s*/?>\s*`).ReplaceAllString(block, "<!-- DIARUM:MEMOS:BEGIN$1 -->\n")
}

func replaceMemosBlock(content, memoID, block string) (string, bool) {
	pattern := memosBlockRegexp(memoID)
	if pattern.MatchString(content) {
		return pattern.ReplaceAllString(content, block), true
	}
	pattern = memosHTMLBlockRegexp(memoID)
	if pattern.MatchString(content) {
		return pattern.ReplaceAllString(content, block), true
	}
	return content, false
}

func removeMemosBlockFromContent(content, memoID string) (string, bool) {
	pattern := memosBlockRegexp(memoID)
	if pattern.MatchString(content) {
		return pattern.ReplaceAllString(content, ""), true
	}
	pattern = memosHTMLBlockRegexp(memoID)
	if pattern.MatchString(content) {
		return pattern.ReplaceAllString(content, ""), true
	}
	return content, false
}

func memosBlockRegexp(memoID string) *regexp.Regexp {
	quoted := regexp.QuoteMeta(memosMarker(memoID))
	return regexp.MustCompile(`(?s)\n*<!-- DIARUM:MEMOS:BEGIN[^>]*` + quoted + `[^>]*-->.*?<!-- DIARUM:MEMOS:END[^>]*` + quoted + `[^>]*-->\n*`)
}

func memosHTMLBlockRegexp(memoID string) *regexp.Regexp {
	quotedID := regexp.QuoteMeta(html.EscapeString(memoID))
	return regexp.MustCompile(`(?s)\n*<hr>.*?Memo ID:\s*` + quotedID + `.*?<hr>\n*`)
}

func memosMarker(memoID string) string {
	return `id="` + markerEscape(memoID) + `"`
}

func isNotFound(err error) bool {
	return err == sql.ErrNoRows || err == store.ErrNotFound
}
