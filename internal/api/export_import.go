package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/models"

	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/embedding"
	"github.com/songtianlun/diarum/internal/logger"
)

const maxImportSize = 200 << 20     // 200MB total upload
const maxSingleFileSize = 100 << 20 // 100MB per file (ZIP bomb protection)

// ---------- Export Request ----------

// ExportRequest defines the export options
type ExportRequest struct {
	// DateRange: "1m", "3m", "6m", "1y", "all", "custom"
	DateRange string `json:"date_range"`
	// StartDate: required when DateRange is "custom" (format: YYYY-MM-DD)
	StartDate string `json:"start_date,omitempty"`
	// EndDate: required when DateRange is "custom" (format: YYYY-MM-DD)
	EndDate string `json:"end_date,omitempty"`
	// Content types to export
	IncludeDiaries       bool `json:"include_diaries"`
	IncludeMedia         bool `json:"include_media"`
	IncludeConversations bool `json:"include_conversations"`
}

// ---------- Export/Import 数据结构 ----------

type exportData struct {
	Version      int                `json:"version"`
	ExportedAt   string             `json:"exported_at"`
	Diaries      []exportDiary      `json:"diaries"`
	Media        []exportMedia      `json:"media"`
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
}

type exportConversation struct {
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	Messages []exportMessage `json:"messages"`
}

type exportMessage struct {
	ID                 string   `json:"id"`
	Role               string   `json:"role"`
	Content            string   `json:"content"`
	ReferencedDiaries  []string `json:"referenced_diaries,omitempty"`
}

type exportStats struct {
	// Date range info
	DateRangeType string `json:"date_range_type"`
	StartDate     string `json:"start_date"`
	EndDate       string `json:"end_date"`
	// Counts: total in system, should export, actually exported
	Diaries       exportCountDetail `json:"diaries"`
	Media         exportCountDetail `json:"media"`
	Conversations exportCountDetail `json:"conversations"`
	Messages      int               `json:"messages"`
	// Failed items with reasons
	FailedItems []exportFailedItem `json:"failed_items,omitempty"`
}

type exportCountDetail struct {
	TotalInSystem  int `json:"total_in_system"`
	ShouldExport   int `json:"should_export"`
	ActualExported int `json:"actual_exported"`
}

type exportFailedItem struct {
	Type   string `json:"type"` // "diary", "media", "conversation"
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type importStats struct {
	Diaries       importCounters `json:"diaries"`
	Media         importCounters `json:"media"`
	Conversations importCounters `json:"conversations"`
}

type importCounters struct {
	Total   int `json:"total"`
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
	Failed   int `json:"failed"`
}

// ---------- Route Registration ----------

func RegisterExportImportRoutes(app *pocketbase.PocketBase, e *core.ServeEvent, embeddingService *embedding.EmbeddingService) {
	e.Router.POST("/api/export", func(c echo.Context) error {
		return handleExport(c, app)
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())

	e.Router.POST("/api/import", func(c echo.Context) error {
		return handleImport(c, app, embeddingService)
	}, apis.ActivityLogger(app), apis.RequireRecordAuth())
}

// ---------- Export Handler ----------

func handleExport(c echo.Context, app *pocketbase.PocketBase) error {
	authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
	if authRecord == nil {
		return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
	}
	userID := authRecord.Id

	// Parse export request
	var req ExportRequest
	if err := c.Bind(&req); err != nil {
		// Default values if no body provided
		req = ExportRequest{
			DateRange:            "3m",
			IncludeDiaries:       true,
			IncludeMedia:         true,
			IncludeConversations: true,
		}
	}

	// Apply defaults for empty values
	if req.DateRange == "" {
		req.DateRange = "3m"
	}

	// Calculate date range
	startDate, endDate, err := calculateDateRange(req)
	if err != nil {
		return apis.NewBadRequestError(err.Error(), nil)
	}

	stats := exportStats{
		DateRangeType: req.DateRange,
		StartDate:     startDate.Format("2006-01-02"),
		EndDate:       endDate.Format("2006-01-02"),
		FailedItems:   make([]exportFailedItem, 0),
	}

	// Get total counts in system first
	allDiaries, _ := app.Dao().FindRecordsByFilter(
		"diaries", "owner = {:owner}", "-date", -1, 0,
		map[string]any{"owner": userID},
	)
	stats.Diaries.TotalInSystem = len(allDiaries)

	allMedia, _ := app.Dao().FindRecordsByFilter(
		"media", "owner = {:owner}", "-created", -1, 0,
		map[string]any{"owner": userID},
	)
	stats.Media.TotalInSystem = len(allMedia)

	allConversations, _ := app.Dao().FindRecordsByFilter(
		"ai_conversations", "owner = {:owner}", "-updated", -1, 0,
		map[string]any{"owner": userID},
	)
	stats.Conversations.TotalInSystem = len(allConversations)

	// Build exported data with filtering
	var diaries []*models.Record
	var mediaRecords []*models.Record
	var conversations []*models.Record

	// Filter and export diaries
	if req.IncludeDiaries {
		for _, d := range allDiaries {
			diaryDate := extractExportDate(d.GetString("date"))
			if isDateInRange(diaryDate, startDate, endDate) {
				diaries = append(diaries, d)
			}
		}
	}
	stats.Diaries.ShouldExport = len(diaries)

	// Filter and export media (based on creation date)
	if req.IncludeMedia {
		for _, m := range allMedia {
			createdStr := m.GetString("created")
			if isDateInRange(extractExportDate(createdStr), startDate, endDate) {
				mediaRecords = append(mediaRecords, m)
			}
		}
	}
	stats.Media.ShouldExport = len(mediaRecords)

	// Filter and export conversations (based on update date)
	if req.IncludeConversations {
		for _, c := range allConversations {
			updatedStr := c.GetString("updated")
			if isDateInRange(extractExportDate(updatedStr), startDate, endDate) {
				conversations = append(conversations, c)
			}
		}
	}
	stats.Conversations.ShouldExport = len(conversations)

	// Build diary list
	exportDiaries := make([]exportDiary, 0, len(diaries))
	for _, d := range diaries {
		exportDiaries = append(exportDiaries, exportDiary{
			ID:      d.Id,
			Date:    extractExportDate(d.GetString("date")),
			Content: d.GetString("content"),
			Mood:    d.GetString("mood"),
			Weather: d.GetString("weather"),
		})
	}
	stats.Diaries.ActualExported = len(exportDiaries)

	// Build media list
	exportMediaList := make([]exportMedia, 0, len(mediaRecords))
	for _, m := range mediaRecords {
		diaryIDs := m.GetStringSlice("diary")
		exportMediaList = append(exportMediaList, exportMedia{
			ID:    m.Id,
			File:  m.GetString("file"),
			Name:  m.GetString("name"),
			Alt:   m.GetString("alt"),
			Diary: diaryIDs,
		})
	}
	stats.Media.ActualExported = len(exportMediaList)

	// 构建 conversations 列表
	exportConvs := make([]exportConversation, 0, len(conversations))
	for _, conv := range conversations {
		messages, err := app.Dao().FindRecordsByFilter(
			"ai_messages",
			"conversation = {:conv}",
			"created",
			-1, 0,
			map[string]any{"conv": conv.Id},
		)
		if err != nil {
			logger.Warn("[Export] failed to fetch messages for conversation %s: %v", conv.Id, err)
			continue
		}

		msgs := make([]exportMessage, 0, len(messages))
		for _, msg := range messages {
			var refDiaries []string
			// referenced_diaries 是 JSON 字段
			if raw := msg.Get("referenced_diaries"); raw != nil {
				if arr, ok := raw.([]string); ok {
					refDiaries = arr
				} else {
					// 尝试从 JSON 反序列化
					if jsonBytes, err2 := json.Marshal(raw); err2 == nil {
						json.Unmarshal(jsonBytes, &refDiaries)
					}
				}
			}
			msgs = append(msgs, exportMessage{
				ID:                msg.Id,
				Role:              msg.GetString("role"),
				Content:           msg.GetString("content"),
				ReferencedDiaries: refDiaries,
			})
		}
		stats.Messages += len(msgs)
		exportConvs = append(exportConvs, exportConversation{
			ID:       conv.Id,
			Title:    conv.GetString("title"),
			Messages: msgs,
		})
	}
	stats.Conversations.ActualExported = len(exportConvs)

	// 序列化 JSON
	data := exportData{
		Version:       1,
		ExportedAt:    time.Now().UTC().Format(time.RFC3339),
		Diaries:       exportDiaries,
		Media:         exportMediaList,
		Conversations: exportConvs,
	}
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return apis.NewBadRequestError("Failed to serialize export data", err)
	}

	// 初始化 filesystem（本地/S3 透明）
	fsys, err := app.NewFilesystem()
	if err != nil {
		logger.Error("[Export] failed to init filesystem: %v", err)
		return apis.NewBadRequestError("Failed to initialize filesystem", err)
	}
	defer fsys.Close()

	// 构建 ZIP
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	// 写入 diarum_export.json
	if w, err := zipWriter.Create("diarum_export.json"); err == nil {
		w.Write(jsonBytes)
	}

	// 写入 markdown/ 目录
	for _, d := range exportDiaries {
		filename := d.Date + ".md"
		if d.Mood != "" {
			filename = d.Date + "_" + d.Mood + ".md"
		}
		md := generateMarkdown(d)
		if w, err := zipWriter.Create("markdown/" + filename); err == nil {
			w.Write([]byte(md))
		}
	}

	// 写入 media/ 目录
	mediaExportedCount := 0
	for _, m := range exportMediaList {
		if m.File == "" {
			continue
		}
		// 找到对应的 record 来获取 BaseFilesPath
		record, err := app.Dao().FindRecordById("media", m.ID)
		if err != nil {
			logger.Warn("[Export] media record %s not found: %v", m.ID, err)
			stats.FailedItems = append(stats.FailedItems, exportFailedItem{
				Type:   "media",
				ID:     m.ID,
				Reason: fmt.Sprintf("record not found: %v", err),
			})
			continue
		}

		fileKey := record.BaseFilesPath() + "/" + m.File
		reader, err := fsys.GetFile(fileKey)
		if err != nil {
			logger.Warn("[Export] failed to read media file %s: %v", fileKey, err)
			stats.FailedItems = append(stats.FailedItems, exportFailedItem{
				Type:   "media",
				ID:     m.ID,
				Reason: fmt.Sprintf("failed to read file: %v", err),
			})
			continue
		}

		content, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			logger.Warn("[Export] failed to read media content %s: %v", fileKey, err)
			stats.FailedItems = append(stats.FailedItems, exportFailedItem{
				Type:   "media",
				ID:     m.ID,
				Reason: fmt.Sprintf("failed to read content: %v", err),
			})
			continue
		}

		if w, err := zipWriter.Create("media/" + m.File); err == nil {
			w.Write(content)
			mediaExportedCount++
		}
	}
	stats.Media.ActualExported = mediaExportedCount

	if err := zipWriter.Close(); err != nil {
		return apis.NewBadRequestError("Failed to create ZIP", err)
	}

	// 序列化 stats 放入 header
	statsJSON, _ := json.Marshal(stats)

	// 返回 ZIP 响应
	c.Response().Header().Set("Content-Type", "application/zip")
	c.Response().Header().Set("Content-Disposition", "attachment; filename=diarum_export.zip")
	c.Response().Header().Set("X-Export-Stats", string(statsJSON))
	c.Response().Header().Set("Access-Control-Expose-Headers", "X-Export-Stats")
	c.Response().WriteHeader(http.StatusOK)
	c.Response().Write(buf.Bytes())

	logger.Info("[Export] completed for user %s: %d diaries, %d media, %d conversations",
		userID, stats.Diaries.ActualExported, stats.Media.ActualExported, stats.Conversations.ActualExported)

	return nil
}

// ---------- Import Handler ----------

func handleImport(c echo.Context, app *pocketbase.PocketBase, embeddingService *embedding.EmbeddingService) error {
	authRecord, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
	if authRecord == nil {
		return apis.NewUnauthorizedError("The request requires valid authorization token.", nil)
	}
	userID := authRecord.Id

	// 读取上传的 ZIP 文件
	fh, err := c.FormFile("file")
	if err != nil {
		return apis.NewBadRequestError("Missing upload file", err)
	}
	if fh.Size > maxImportSize {
		return apis.NewBadRequestError("File too large (max 200MB). Please use segmented export with date range filters to create smaller export files, then import them separately.", nil)
	}

	f, err := fh.Open()
	if err != nil {
		return apis.NewBadRequestError("Failed to open upload", err)
	}
	defer f.Close()

	zipBytes, err := io.ReadAll(io.LimitReader(f, maxImportSize+1))
	if err != nil {
		return apis.NewBadRequestError("Failed to read upload", err)
	}
	if int64(len(zipBytes)) > maxImportSize {
		return apis.NewBadRequestError("File too large (max 200MB). Please use segmented export with date range filters to create smaller export files, then import them separately.", nil)
	}

	// 解压 ZIP
	zipReader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return apis.NewBadRequestError("Failed to read ZIP file", err)
	}

	// 读取 ZIP 中的各文件到内存
	var exportJSON []byte
	mediaFiles := make(map[string][]byte) // filename -> bytes

	for _, zf := range zipReader.File {
		// Path traversal protection
		if !isValidZipPath(zf.Name) {
			logger.Warn("[Import] skipping file with invalid path: %s", zf.Name)
			continue
		}

		// ZIP bomb protection - check uncompressed size
		if zf.UncompressedSize64 > maxSingleFileSize {
			logger.Warn("[Import] skipping file exceeding size limit: %s (%d bytes)", zf.Name, zf.UncompressedSize64)
			continue
		}

		rc, err := zf.Open()
		if err != nil {
			continue
		}

		// Read with size limit (defense in depth)
		limitedReader := io.LimitReader(rc, maxSingleFileSize+1)
		data, err := io.ReadAll(limitedReader)
		rc.Close()
		if err != nil {
			continue
		}
		if int64(len(data)) > maxSingleFileSize {
			logger.Warn("[Import] file exceeded size limit during read: %s", zf.Name)
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
		return apis.NewBadRequestError("ZIP missing diarum_export.json", nil)
	}

	// 解析 JSON
	var data exportData
	if err := json.Unmarshal(exportJSON, &data); err != nil {
		return apis.NewBadRequestError("Failed to parse diarum_export.json", err)
	}

	if data.Version < 1 {
		return apis.NewBadRequestError("Invalid export version", nil)
	}

	stats := importStats{}

	// 初始化 filesystem
	fsys, err := app.NewFilesystem()
	if err != nil {
		return apis.NewBadRequestError("Failed to initialize filesystem", err)
	}
	defer fsys.Close()

	// ---------- 导入日记 ----------
	// 维护旧 ID -> 新 ID 映射
	diaryIDMap := make(map[string]string)
	stats.Diaries.Total = len(data.Diaries)

	// 预先构建用户当前所有日记的 date set（用于快速去重查找）
	existingDates, err := app.Dao().FindRecordsByFilter(
		"diaries",
		"owner = {:owner}",
		"date",
		-1, 0,
		map[string]any{"owner": userID},
	)
	dateSet := make(map[string]bool)
	if err == nil {
		for _, r := range existingDates {
			dateSet[extractExportDate(r.GetString("date"))] = true
		}
	}

	diariesCollection, err := app.Dao().FindCollectionByNameOrId("diaries")
	if err != nil {
		return apis.NewBadRequestError("Failed to find diaries collection", err)
	}

	for _, d := range data.Diaries {
		if d.Date == "" {
			stats.Diaries.Failed++
			continue
		}

		// 基于日期去重
		if dateSet[d.Date] {
			stats.Diaries.Skipped++
			diaryIDMap[d.ID] = "" // 标记为已跳过
			continue
		}

		record := models.NewRecord(diariesCollection)
		record.Set("date", d.Date+" 00:00:00.000Z")
		record.Set("content", d.Content)
		record.Set("owner", userID)
		if d.Mood != "" {
			record.Set("mood", d.Mood)
		}
		if d.Weather != "" {
			record.Set("weather", d.Weather)
		}

		if err := app.Dao().SaveRecord(record); err != nil {
			logger.Error("[Import] failed to save diary %s: %v", d.Date, err)
			stats.Diaries.Failed++
			continue
		}

		diaryIDMap[d.ID] = record.Id
		dateSet[d.Date] = true // 更新 set 防止同一次导入中重复
		stats.Diaries.Imported++
	}

	// ---------- 导入媒体 ----------
	mediaCollection, err := app.Dao().FindCollectionByNameOrId("media")
	if err != nil {
		logger.Error("[Import] failed to find media collection: %v", err)
	}

	stats.Media.Total = len(data.Media)

	if mediaCollection != nil {
		for _, m := range data.Media {
			if m.File == "" {
				stats.Media.Failed++
				continue
			}

			// Check if file exists in ZIP
			fileBytes, ok := mediaFiles[m.File]
			if !ok {
				logger.Warn("[Import] media file %s not found in ZIP", m.File)
				stats.Media.Failed++
				continue
			}

			// Validate MIME type
			detectedMime, allowed := config.IsAllowedMediaType(fileBytes)
			if !allowed {
				logger.Warn("[Import] media file %s has disallowed MIME type: %s", m.File, detectedMime)
				stats.Media.Failed++
				continue
			}

			// Check if media with same ID already exists - skip if so
			if m.ID != "" {
				existing, _ := app.Dao().FindRecordById("media", m.ID)
				if existing != nil {
					logger.Info("[Import] media %s already exists, skipping", m.ID)
					stats.Media.Skipped++
					continue
				}
			}

			// Fix diary relations (old ID -> new ID)
			var newDiaryIDs []string
			for _, oldID := range m.Diary {
				if newID, exists := diaryIDMap[oldID]; exists && newID != "" {
					newDiaryIDs = append(newDiaryIDs, newID)
				}
			}

			// 创建 media 记录（先不设 file 字段）
			record := models.NewRecord(mediaCollection)
			record.Set("owner", userID)
			if m.Name != "" {
				record.Set("name", m.Name)
			}
			if m.Alt != "" {
				record.Set("alt", m.Alt)
			}
			if len(newDiaryIDs) > 0 {
				record.Set("diary", newDiaryIDs)
			}

			if err := app.Dao().SaveRecord(record); err != nil {
				logger.Error("[Import] failed to create media record: %v", err)
				stats.Media.Failed++
				continue
			}

			// 写入文件到存储
			fileKey := record.BaseFilesPath() + "/" + m.File
			if err := fsys.Upload(fileBytes, fileKey); err != nil {
				logger.Error("[Import] failed to upload media file %s: %v", m.File, err)
				stats.Media.Failed++
				// 清理已创建的空记录
				app.Dao().DeleteRecord(record)
				continue
			}

			// 更新 file 字段并保存
			record.Set("file", m.File)
			if err := app.Dao().SaveRecord(record); err != nil {
				logger.Error("[Import] failed to update media file field: %v", err)
				stats.Media.Failed++
				continue
			}

			stats.Media.Imported++
		}
	}

	// ---------- 导入 AI 对话 ----------
	convCollection, err := app.Dao().FindCollectionByNameOrId("ai_conversations")
	msgCollection, err2 := app.Dao().FindCollectionByNameOrId("ai_messages")

	stats.Conversations.Total = len(data.Conversations)

	if convCollection != nil && err == nil && msgCollection != nil && err2 == nil {
		for _, conv := range data.Conversations {
			// Check if conversation with same ID already exists - skip if so
			if conv.ID != "" {
				existing, _ := app.Dao().FindRecordById("ai_conversations", conv.ID)
				if existing != nil {
					logger.Info("[Import] conversation %s already exists, skipping", conv.ID)
					stats.Conversations.Skipped++
					continue
				}
			}

			// Create conversation record
			convRecord := models.NewRecord(convCollection)
			convRecord.Set("title", conv.Title)
			convRecord.Set("owner", userID)

			if err := app.Dao().SaveRecord(convRecord); err != nil {
				logger.Error("[Import] failed to create conversation: %v", err)
				stats.Conversations.Failed++
				continue
			}

			// Import messages
			for _, msg := range conv.Messages {
				// Check if message with same ID already exists - skip if so
				if msg.ID != "" {
					existing, _ := app.Dao().FindRecordById("ai_messages", msg.ID)
					if existing != nil {
						logger.Info("[Import] message %s already exists, skipping", msg.ID)
						continue
					}
				}

				msgRecord := models.NewRecord(msgCollection)
				msgRecord.Set("conversation", convRecord.Id)
				msgRecord.Set("role", msg.Role)
				msgRecord.Set("content", msg.Content)
				msgRecord.Set("owner", userID)

				// 修复 referenced_diaries 关联
				if len(msg.ReferencedDiaries) > 0 {
					var newRefs []string
					for _, oldID := range msg.ReferencedDiaries {
						if newID, exists := diaryIDMap[oldID]; exists && newID != "" {
							newRefs = append(newRefs, newID)
						}
					}
					if len(newRefs) > 0 {
						msgRecord.Set("referenced_diaries", newRefs)
					}
				}

				if err := app.Dao().SaveRecord(msgRecord); err != nil {
					logger.Error("[Import] failed to create message: %v", err)
					continue
				}
			}

			stats.Conversations.Imported++
		}
	}

	// ---------- 导入后异步触发向量重建 ----------
	if embeddingService != nil {
		configService := config.NewConfigService(app)
		enabled, _ := configService.GetBool(userID, "ai.enabled")
		if enabled {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				logger.Info("[Import] triggering incremental vector rebuild for user: %s", userID)
				result, err := embeddingService.BuildIncrementalVectors(ctx, userID)
				if err != nil {
					logger.Error("[Import] vector rebuild failed for user %s: %v", userID, err)
					return
				}
				logger.Info("[Import] vector rebuild completed for user %s: %d built, %d failed",
					userID, result.Success, result.Failed)
			}()
		}
	}

	logger.Info("[Import] completed for user %s: diaries=%+v, media=%+v, conversations=%+v",
		userID, stats.Diaries, stats.Media, stats.Conversations)

	return c.JSON(http.StatusOK, stats)
}

// ---------- Helpers ----------

// isValidZipPath checks for path traversal attacks
func isValidZipPath(name string) bool {
	if strings.Contains(name, "..") {
		return false
	}
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") {
		return false
	}
	return true
}

// extractExportDate extracts YYYY-MM-DD from a PocketBase timestamp string
func extractExportDate(dateTime string) string {
	if len(dateTime) >= 10 {
		return dateTime[:10]
	}
	return dateTime
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

// calculateDateRange calculates the start and end dates based on the export request
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
		// Use a very old date for "all"
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
		// Set end date to end of day
		end = end.Add(24*time.Hour - time.Second)
		return start, end, nil
	default:
		// Default to 3 months
		return now.AddDate(0, -3, 0), endDate, nil
	}
}

// isDateInRange checks if a date string (YYYY-MM-DD) is within the given range
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
