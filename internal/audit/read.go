package audit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ErrInvalidDate is returned for dates that do not name a log file.
var ErrInvalidDate = errors.New("invalid date")

// FileInfo describes one daily log file.
type FileInfo struct {
	Date    string `json:"date"`
	Size    int64  `json:"size"`
	Entries int    `json:"entries"`
}

// Query selects entries from a user's trail.
type Query struct {
	// Start and End bound entry times (inclusive); zero means unbounded.
	Start time.Time
	End   time.Time
	// Text is matched case-insensitively against every field of the entry
	// and against the action's human-readable names.
	Text string
	// Action keeps entries whose action equals it or starts with it plus a
	// dot, so "diary" matches every diary action.
	Action string
	// Limit caps the number of entries returned, newest first.
	Limit int
}

// Result is a page of entries, newest first.
type Result struct {
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated"`
	// Scanned is how many daily files were read.
	Scanned int `json:"scanned"`
}

// Files lists a user's daily log files, newest first.
func (l *Logger) Files(userID string) ([]FileInfo, error) {
	if l == nil || !validUserID(userID) {
		return []FileInfo{}, nil
	}
	l.Flush(userID)
	files, err := l.listFiles(userID)
	if err != nil {
		return nil, err
	}
	for i := range files {
		files[i].Entries = countLines(filepath.Join(l.userDir(userID), files[i].Date+fileSuffix))
	}
	return files, nil
}

func (l *Logger) listFiles(userID string) ([]FileInfo, error) {
	entries, err := os.ReadDir(l.userDir(userID))
	if errors.Is(err, os.ErrNotExist) {
		return []FileInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	files := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, fileSuffix) {
			continue
		}
		date := strings.TrimSuffix(name, fileSuffix)
		if _, err := time.Parse(fileDateLayout, date); err != nil {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, FileInfo{Date: date, Size: info.Size()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Date > files[j].Date })
	return files, nil
}

// OpenFile opens one raw daily log file for download.
func (l *Logger) OpenFile(userID, date string) (*os.File, error) {
	if l == nil || !validUserID(userID) {
		return nil, os.ErrNotExist
	}
	if _, err := time.Parse(fileDateLayout, date); err != nil {
		return nil, ErrInvalidDate
	}
	l.Flush(userID)
	return os.Open(filepath.Join(l.userDir(userID), date+fileSuffix))
}

// Search returns the newest entries matching q.
func (l *Logger) Search(userID string, q Query) (*Result, error) {
	result := &Result{Entries: []Entry{}}
	if l == nil || !validUserID(userID) {
		return result, nil
	}
	if q.Limit <= 0 {
		q.Limit = 100
	}
	l.Flush(userID)
	files, err := l.listFiles(userID)
	if err != nil {
		return nil, err
	}

	// A daily file only holds entries from its own local day, so files
	// outside the requested range are skipped without being opened.
	firstDate, lastDate := "", ""
	if !q.Start.IsZero() {
		firstDate = q.Start.In(l.loc).Format(fileDateLayout)
	}
	if !q.End.IsZero() {
		lastDate = q.End.In(l.loc).Format(fileDateLayout)
	}
	text := strings.ToLower(strings.TrimSpace(q.Text))
	action := strings.TrimSpace(q.Action)

	for _, file := range files {
		if (firstDate != "" && file.Date < firstDate) || (lastDate != "" && file.Date > lastDate) {
			continue
		}
		result.Scanned++
		entries := l.readFile(userID, file.Date)
		// Files are appended in time order; walk each one backwards.
		for i := len(entries) - 1; i >= 0; i-- {
			entry := entries[i]
			if !q.Start.IsZero() && entry.Time.Before(q.Start) {
				continue
			}
			if !q.End.IsZero() && entry.Time.After(q.End) {
				continue
			}
			if action != "" && entry.Action != action && !strings.HasPrefix(entry.Action, action+".") {
				continue
			}
			if text != "" && !matchesText(entry, text) {
				continue
			}
			if len(result.Entries) >= q.Limit {
				result.Truncated = true
				return result, nil
			}
			result.Entries = append(result.Entries, entry)
		}
	}
	return result, nil
}

// readFile parses one daily file, skipping lines that do not decode (for
// example a line cut short by a crash).
func (l *Logger) readFile(userID, date string) []Entry {
	f, err := os.Open(filepath.Join(l.userDir(userID), date+fileSuffix))
	if err != nil {
		return nil
	}
	defer f.Close()
	entries := make([]Entry, 0, 64)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), maxLineBytes)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var entry Entry
		if err := json.Unmarshal(line, &entry); err != nil || entry.Action == "" {
			continue
		}
		entries = append(entries, entry)
	}
	// Entries are appended under a lock, but merged autosaves carry the time
	// of their last save; keep the file order stable by time.
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Time.Before(entries[j].Time) })
	return entries
}

func matchesText(entry Entry, text string) bool {
	fields := []string{entry.Action, entry.Target, entry.Actor, entry.Source, entry.IP, entry.UA}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), text) {
			return true
		}
	}
	for _, label := range actionLabels[entry.Action] {
		if strings.Contains(strings.ToLower(label), text) {
			return true
		}
	}
	if len(entry.Detail) > 0 {
		if raw, err := json.Marshal(entry.Detail); err == nil && strings.Contains(strings.ToLower(string(raw)), text) {
			return true
		}
	}
	return false
}

func countLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	count := 0
	buf := make([]byte, 32*1024)
	for {
		n, err := f.Read(buf)
		count += bytes.Count(buf[:n], []byte{'\n'})
		if err == io.EOF {
			return count
		}
		if err != nil {
			return count
		}
	}
}

// actionLabels lets a search for "删除" or "delete" find diary.delete. They
// mirror the labels the settings page shows.
var actionLabels = map[string][]string{
	ActionDiaryCreate:    {"create diary", "created", "创建日记", "创建"},
	ActionDiaryUpdate:    {"update diary", "updated", "edit", "更新日记", "更新", "编辑", "修改"},
	ActionDiaryDelete:    {"delete diary", "deleted", "删除日记", "删除"},
	ActionDiaryRestore:   {"restore version", "restored", "恢复历史版本", "恢复"},
	ActionDiaryView:      {"view diary", "viewed", "read", "查看日记", "查看", "阅读"},
	ActionDiarySearch:    {"search diary", "searched", "搜索日记", "搜索"},
	ActionConvDelete:     {"delete conversation", "删除对话"},
	ActionMediaUpload:    {"upload image", "uploaded", "上传图片", "上传"},
	ActionMediaDelete:    {"delete image", "deleted", "删除图片", "删除"},
	ActionDataImport:     {"import data", "imported", "导入数据", "导入"},
	ActionDataExport:     {"export data", "exported", "导出数据", "导出"},
	ActionSettingsUpdate: {"change settings", "settings", "修改设置", "设置"},
	ActionTokenUpdate:    {"token", "api token", "令牌", "密钥"},
	ActionAuthLogin:      {"sign in", "login", "登录"},
	ActionAuthLoginFail:  {"failed sign in", "login failed", "登录失败"},
	ActionAuthRegister:   {"register", "sign up", "注册"},
}
