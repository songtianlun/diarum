package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/auth"
	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/embedding"
	"github.com/songtianlun/diarum/internal/logger"
	"github.com/songtianlun/diarum/internal/store"
)

const maxImportSize = 200 << 20
const maxSingleFileSize = 100 << 20

type ExportRequest struct {
	DateRange            string `json:"date_range"`
	StartDate            string `json:"start_date,omitempty"`
	EndDate              string `json:"end_date,omitempty"`
	IncludeDiaries       bool   `json:"include_diaries"`
	IncludeMedia         bool   `json:"include_media"`
	IncludeConversations bool   `json:"include_conversations"`
}

type exportData struct {
	Version       int                  `json:"version"`
	ExportedAt    string               `json:"exported_at"`
	Diaries       []exportDiary        `json:"diaries"`
	Media         []exportMedia        `json:"media"`
	Conversations []exportConversation `json:"conversations"`
}

type exportDiary struct {
	ID      string `json:"id"`
	Date    string `json:"date"`
	Content string `json:"content"`
	Mood    string `json:"mood,omitempty"`
	Weather string `json:"weather,omitempty"`
}

type exportMedia struct {
	ID    string   `json:"id"`
	File  string   `json:"file"`
	Name  string   `json:"name,omitempty"`
	Alt   string   `json:"alt,omitempty"`
	Diary []string `json:"diary,omitempty"`
	Owner string   `json:"-"`
}

type exportConversation struct {
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	Messages []exportMessage `json:"messages"`
}

type exportMessage struct {
	ID                string   `json:"id"`
	Role              string   `json:"role"`
	Content           string   `json:"content"`
	ReferencedDiaries []string `json:"referenced_diaries,omitempty"`
}

type exportStats struct {
	DateRangeType string             `json:"date_range_type"`
	StartDate     string             `json:"start_date"`
	EndDate       string             `json:"end_date"`
	Diaries       exportCountDetail  `json:"diaries"`
	Media         exportCountDetail  `json:"media"`
	Conversations exportCountDetail  `json:"conversations"`
	Messages      int                `json:"messages"`
	FailedItems   []exportFailedItem `json:"failed_items,omitempty"`
}

type exportCountDetail struct {
	TotalInSystem  int `json:"total_in_system"`
	ShouldExport   int `json:"should_export"`
	ActualExported int `json:"actual_exported"`
}

type exportFailedItem struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type importStats struct {
	Diaries       importCounters `json:"diaries"`
	Media         importCounters `json:"media"`
	Conversations importCounters `json:"conversations"`
}

type importCounters struct {
	Total    int `json:"total"`
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
	Failed   int `json:"failed"`
}

func RegisterExportImportRoutes(e *echo.Echo, s *store.Store, authMiddleware echo.MiddlewareFunc, embeddingService *embedding.EmbeddingService) {
	group := e.Group("/api/v1", authMiddleware)
	group.POST("/export", func(c echo.Context) error { return handleExport(c, s) })
	group.POST("/import", func(c echo.Context) error { return handleImport(c, s, embeddingService) })
}

func handleExport(c echo.Context, s *store.Store) error {
	userID := auth.CurrentUser(c).ID
	var req ExportRequest
	if err := c.Bind(&req); err != nil {
		req = ExportRequest{DateRange: "3m", IncludeDiaries: true, IncludeMedia: true, IncludeConversations: true}
	}
	if req.DateRange == "" {
		req.DateRange = "3m"
	}
	startDate, endDate, err := calculateDateRange(req)
	if err != nil {
		return badRequest(err.Error(), nil)
	}
	stats := exportStats{DateRangeType: req.DateRange, StartDate: startDate.Format("2006-01-02"), EndDate: endDate.Format("2006-01-02"), FailedItems: make([]exportFailedItem, 0)}

	allDiaries, _ := s.ListDiaries(userID, "", "", "-date", 0)
	allMedia, mediaTotal, _ := s.ListMedia(userID, 1, 1000000)
	allConversations, _ := s.ListConversations(userID, 1000000)
	stats.Diaries.TotalInSystem = len(allDiaries)
	stats.Media.TotalInSystem = mediaTotal
	stats.Conversations.TotalInSystem = len(allConversations)

	exportDiaries := make([]exportDiary, 0)
	if req.IncludeDiaries {
		for _, d := range allDiaries {
			date := store.DateOnly(d.Date)
			if isDateInRange(date, startDate, endDate) {
				exportDiaries = append(exportDiaries, exportDiary{ID: d.ID, Date: date, Content: d.Content, Mood: d.Mood, Weather: d.Weather})
			}
		}
	}
	stats.Diaries.ShouldExport = len(exportDiaries)
	stats.Diaries.ActualExported = len(exportDiaries)

	exportMediaList := make([]exportMedia, 0)
	if req.IncludeMedia {
		for _, m := range allMedia {
			if isDateInRange(store.DateOnly(m.Created), startDate, endDate) {
				exportMediaList = append(exportMediaList, exportMedia{ID: m.ID, File: m.File, Name: m.Name, Alt: m.Alt, Diary: m.Diary, Owner: m.Owner})
			}
		}
	}
	stats.Media.ShouldExport = len(exportMediaList)

	exportConvs := make([]exportConversation, 0)
	if req.IncludeConversations {
		for _, conv := range allConversations {
			if !isDateInRange(store.DateOnly(conv.Updated), startDate, endDate) {
				continue
			}
			messages, err := s.ListMessages(conv.ID, 0)
			if err != nil {
				stats.FailedItems = append(stats.FailedItems, exportFailedItem{Type: "conversation", ID: conv.ID, Reason: err.Error()})
				continue
			}
			msgs := make([]exportMessage, 0, len(messages))
			for _, msg := range messages {
				msgs = append(msgs, exportMessage{ID: msg.ID, Role: msg.Role, Content: msg.Content, ReferencedDiaries: msg.ReferencedDiaries})
			}
			stats.Messages += len(msgs)
			exportConvs = append(exportConvs, exportConversation{ID: conv.ID, Title: conv.Title, Messages: msgs})
		}
	}
	stats.Conversations.ShouldExport = len(exportConvs)
	stats.Conversations.ActualExported = len(exportConvs)

	data := exportData{Version: 1, ExportedAt: time.Now().UTC().Format(time.RFC3339), Diaries: exportDiaries, Media: exportMediaList, Conversations: exportConvs}
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return badRequest("Failed to serialize export data", err)
	}

	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)
	if w, err := zipWriter.Create("diarum_export.json"); err == nil {
		_, _ = w.Write(jsonBytes)
	}
	for _, d := range exportDiaries {
		filename := d.Date + ".md"
		if d.Mood != "" {
			filename = d.Date + "_" + d.Mood + ".md"
		}
		if w, err := zipWriter.Create("markdown/" + filename); err == nil {
			_, _ = w.Write([]byte(generateMarkdown(d)))
		}
	}
	mediaExportedCount := 0
	for _, m := range exportMediaList {
		media := &store.Media{ID: m.ID, File: m.File, Owner: m.Owner}
		reader, err := s.OpenMediaFile(media)
		if err != nil {
			stats.FailedItems = append(stats.FailedItems, exportFailedItem{Type: "media", ID: m.ID, Reason: err.Error()})
			continue
		}
		content, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			stats.FailedItems = append(stats.FailedItems, exportFailedItem{Type: "media", ID: m.ID, Reason: err.Error()})
			continue
		}
		if w, err := zipWriter.Create("media/" + m.File); err == nil {
			_, _ = w.Write(content)
			mediaExportedCount++
		}
	}
	stats.Media.ActualExported = mediaExportedCount
	if err := zipWriter.Close(); err != nil {
		return badRequest("Failed to create ZIP", err)
	}
	statsJSON, _ := json.Marshal(stats)
	c.Response().Header().Set("Content-Type", "application/zip")
	c.Response().Header().Set("Content-Disposition", "attachment; filename=diarum_export.zip")
	c.Response().Header().Set("X-Export-Stats", string(statsJSON))
	c.Response().Header().Set("Access-Control-Expose-Headers", "X-Export-Stats")
	c.Response().WriteHeader(http.StatusOK)
	_, _ = c.Response().Write(buf.Bytes())
	return nil
}

func handleImport(c echo.Context, s *store.Store, embeddingService *embedding.EmbeddingService) error {
	userID := auth.CurrentUser(c).ID
	fh, err := c.FormFile("file")
	if err != nil {
		return badRequest("Missing upload file", err)
	}
	if fh.Size > maxImportSize {
		return badRequest("File too large (max 200MB)", nil)
	}
	f, err := fh.Open()
	if err != nil {
		return badRequest("Failed to open upload", err)
	}
	defer f.Close()
	zipBytes, err := io.ReadAll(io.LimitReader(f, maxImportSize+1))
	if err != nil || int64(len(zipBytes)) > maxImportSize {
		return badRequest("Failed to read upload", err)
	}
	zipReader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return badRequest("Failed to read ZIP file", err)
	}
	var exportJSON []byte
	mediaFiles := make(map[string][]byte)
	for _, zf := range zipReader.File {
		if !isValidZipPath(zf.Name) || zf.UncompressedSize64 > maxSingleFileSize {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxSingleFileSize+1))
		rc.Close()
		if err != nil || int64(len(data)) > maxSingleFileSize {
			continue
		}
		switch {
		case zf.Name == "diarum_export.json":
			exportJSON = data
		case strings.HasPrefix(zf.Name, "media/"):
			name := strings.TrimPrefix(zf.Name, "media/")
			if name != "" {
				mediaFiles[name] = data
			}
		}
	}
	if exportJSON == nil {
		return badRequest("ZIP missing diarum_export.json", nil)
	}
	var data exportData
	if err := json.Unmarshal(exportJSON, &data); err != nil {
		return badRequest("Failed to parse diarum_export.json", err)
	}
	stats := importStats{Diaries: importCounters{Total: len(data.Diaries)}, Media: importCounters{Total: len(data.Media)}, Conversations: importCounters{Total: len(data.Conversations)}}
	diaryIDMap := make(map[string]string)
	for _, d := range data.Diaries {
		if d.Date == "" {
			stats.Diaries.Failed++
			continue
		}
		if s.DiaryExistsByDate(userID, d.Date) {
			stats.Diaries.Skipped++
			diaryIDMap[d.ID] = ""
			continue
		}
		diary, err := s.InsertImportedDiary(userID, "", d.Date, d.Content, d.Mood, d.Weather)
		if err != nil {
			stats.Diaries.Failed++
			continue
		}
		diaryIDMap[d.ID] = diary.ID
		stats.Diaries.Imported++
	}
	for _, m := range data.Media {
		fileBytes, ok := mediaFiles[m.File]
		if m.File == "" || !ok {
			stats.Media.Failed++
			continue
		}
		if detected, allowed := config.IsAllowedMediaType(fileBytes); !allowed {
			logger.Warn("[Import] media file %s has disallowed MIME type: %s", m.File, detected)
			stats.Media.Failed++
			continue
		}
		newDiaryIDs := make([]string, 0)
		for _, oldID := range m.Diary {
			if newID := diaryIDMap[oldID]; newID != "" {
				newDiaryIDs = append(newDiaryIDs, newID)
			}
		}
		media, err := s.CreateMedia(userID, m.File, m.Name, m.Alt, newDiaryIDs)
		if err != nil {
			stats.Media.Failed++
			continue
		}
		if err := os.MkdirAll(filepath.Dir(s.NewMediaFilePath(media.ID, media.File)), 0o755); err != nil {
			stats.Media.Failed++
			continue
		}
		if err := os.WriteFile(s.NewMediaFilePath(media.ID, media.File), fileBytes, 0o600); err != nil {
			_ = s.DeleteMedia(media.ID, userID)
			stats.Media.Failed++
			continue
		}
		stats.Media.Imported++
	}
	for _, conv := range data.Conversations {
		convRecord, err := s.CreateConversation(userID, conv.Title)
		if err != nil {
			stats.Conversations.Failed++
			continue
		}
		for _, msg := range conv.Messages {
			refs := make([]string, 0)
			for _, oldID := range msg.ReferencedDiaries {
				if newID := diaryIDMap[oldID]; newID != "" {
					refs = append(refs, newID)
				}
			}
			_, _ = s.CreateMessage(userID, convRecord.ID, msg.Role, msg.Content, refs)
		}
		stats.Conversations.Imported++
	}
	if embeddingService != nil {
		configService := config.NewConfigService(s)
		enabled, _ := configService.GetBool(userID, "ai.enabled")
		if enabled {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				_, _ = embeddingService.BuildIncrementalVectors(ctx, userID)
			}()
		}
	}
	return c.JSON(http.StatusOK, stats)
}

func isValidZipPath(name string) bool {
	return !strings.Contains(name, "..") && !strings.HasPrefix(name, "/") && !strings.HasPrefix(name, "\\")
}

func generateMarkdown(d exportDiary) string {
	var sb strings.Builder
	sb.WriteString("# " + d.Date + "\n\n")
	if d.Mood != "" {
		sb.WriteString("**Mood:** " + d.Mood + "\n")
	}
	if d.Weather != "" {
		sb.WriteString("**Weather:** " + d.Weather + "\n")
	}
	if d.Mood != "" || d.Weather != "" {
		sb.WriteString("\n")
	}
	sb.WriteString(d.Content)
	return sb.String()
}

func calculateDateRange(req ExportRequest) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	endDate := now
	switch req.DateRange {
	case "1m":
		return now.AddDate(0, -1, 0), endDate, nil
	case "3m":
		return now.AddDate(0, -3, 0), endDate, nil
	case "6m":
		return now.AddDate(0, -6, 0), endDate, nil
	case "1y":
		return now.AddDate(-1, 0, 0), endDate, nil
	case "all":
		return time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), endDate, nil
	case "custom":
		if req.StartDate == "" || req.EndDate == "" {
			return time.Time{}, time.Time{}, fmt.Errorf("start_date and end_date are required for custom date range")
		}
		start, err := time.Parse("2006-01-02", req.StartDate)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid start_date format, expected YYYY-MM-DD")
		}
		end, err := time.Parse("2006-01-02", req.EndDate)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end_date format, expected YYYY-MM-DD")
		}
		if start.After(end) {
			return time.Time{}, time.Time{}, fmt.Errorf("start_date cannot be after end_date")
		}
		return start, end.Add(24*time.Hour - time.Second), nil
	default:
		return now.AddDate(0, -3, 0), endDate, nil
	}
}

func isDateInRange(dateStr string, start, end time.Time) bool {
	if dateStr == "" {
		return false
	}
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return false
	}
	return !date.Before(start) && !date.After(end)
}
