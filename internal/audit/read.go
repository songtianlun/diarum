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
	"strconv"
	"strings"
	"time"
)

// ErrInvalidDate is returned for dates that do not name a log file.
var ErrInvalidDate = errors.New("invalid date")

// FileInfo describes one daily log file.
type FileInfo struct {
	Date string `json:"date"`
	Size int64  `json:"size"`
	// Pulled marks a day pulled back from the S3 archive; it is removed at
	// the next cleanup.
	Pulled bool `json:"pulled"`
	// PulledAt is when it was pulled.
	PulledAt *time.Time `json:"pulled_at,omitempty"`
}

// Query selects entries. Every set field must match.
type Query struct {
	// Start and End bound entry times (inclusive); zero means unbounded.
	Start time.Time
	End   time.Time
	// UserID keeps entries of this account; "-" keeps anonymous requests.
	UserID string
	// Actions keeps entries whose action equals one of them or starts with
	// it plus a dot ("diary" matches diary.update). "-" matches entries
	// without an action.
	Actions []string
	// Status is "2xx", "3xx", "4xx", "5xx" or "error" (400 and above).
	Status string
	Method string
	Source string
	IP     string
	// Text is matched case-insensitively against the whole line and against
	// the actions' human-readable names.
	Text string
	// IncludePulled also reads days pulled back from the archive.
	IncludePulled bool
	// Offset and Limit page through the matches, newest first.
	Offset int
	Limit  int
	// Stats asks for aggregate statistics over every match.
	Stats bool
	// Location buckets the timeline; defaults to the logger's.
	Location *time.Location
}

// Result is a page of matching entries, newest first.
type Result struct {
	Entries []Entry `json:"entries"`
	// Total is how many entries matched.
	Total int `json:"total"`
	// Files and Bytes describe what was read.
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
	Stats *Stats `json:"stats,omitempty"`
	// TookMS is how long the scan took.
	TookMS int64 `json:"took_ms"`
}

// Stats aggregates the matching entries.
type Stats struct {
	Total        int     `json:"total"`
	Users        int     `json:"users"`
	IPs          int     `json:"ips"`
	Anonymous    int     `json:"anonymous"`
	ClientErrors int     `json:"client_errors"`
	ServerErrors int     `json:"server_errors"`
	LoginOK      int     `json:"login_ok"`
	LoginFailed  int     `json:"login_failed"`
	Denied       int     `json:"denied"`
	Writes       int     `json:"writes"`
	AvgMS        float64 `json:"avg_ms"`
	P95MS        int64   `json:"p95_ms"`
	MaxMS        int64   `json:"max_ms"`
	First        string  `json:"first,omitempty"`
	Last         string  `json:"last,omitempty"`

	Actions  []Count      `json:"actions"`
	Status   []Count      `json:"status"`
	Methods  []Count      `json:"methods"`
	Sources  []Count      `json:"sources"`
	TopUsers []Count      `json:"top_users"`
	TopIPs   []IPCount    `json:"top_ips"`
	Routes   []RouteCount `json:"routes"`
	// FailedLogins lists the identities most often used in failed sign-ins.
	FailedLogins []Count `json:"failed_logins"`
	// Timeline counts entries per bucket; Bucket is "hour" or "day".
	Bucket   string   `json:"bucket"`
	Timeline []Bucket `json:"timeline"`
}

// Count is one row of a breakdown.
type Count struct {
	Key   string `json:"key"`
	Label string `json:"label,omitempty"`
	Count int    `json:"count"`
}

// IPCount is how busy one client address was.
type IPCount struct {
	IP       string `json:"ip"`
	Count    int    `json:"count"`
	Errors   int    `json:"errors"`
	Failed   int    `json:"failed_logins"`
	Users    int    `json:"users"`
	LastSeen string `json:"last_seen"`
}

// RouteCount summarises one API endpoint.
type RouteCount struct {
	Method string  `json:"method"`
	Route  string  `json:"route"`
	Count  int     `json:"count"`
	Errors int     `json:"errors"`
	AvgMS  float64 `json:"avg_ms"`
	MaxMS  int64   `json:"max_ms"`
}

// Bucket is one step of the timeline.
type Bucket struct {
	Start  time.Time `json:"start"`
	Count  int       `json:"count"`
	Errors int       `json:"errors"`
}

const (
	defaultLimit  = 100
	MaxLimit      = 1000
	MaxOffset     = 20000
	topN          = 10
	topRoutes     = 20
	maxDurSamples = 100000
)

// Files lists the daily files on disk, newest first.
func (l *Logger) Files(includePulled bool) ([]FileInfo, error) {
	if l == nil {
		return []FileInfo{}, nil
	}
	l.Flush()
	files, err := l.listDir(l.root, false)
	if err != nil {
		return nil, err
	}
	if includePulled {
		pulled, err := l.listDir(filepath.Join(l.root, PulledDirName), true)
		if err != nil {
			return nil, err
		}
		files = append(files, pulled...)
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Date > files[j].Date })
	return files, nil
}

func (l *Logger) listDir(dir string, pulled bool) ([]FileInfo, error) {
	entries, err := os.ReadDir(dir)
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
		if !validDate(date) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		file := FileInfo{Date: date, Size: info.Size(), Pulled: pulled}
		if pulled {
			mod := info.ModTime()
			file.PulledAt = &mod
		}
		files = append(files, file)
	}
	return files, nil
}

func validDate(date string) bool {
	_, err := time.Parse(fileDateLayout, date)
	return err == nil
}

// filePath returns where a day's file lives.
func (l *Logger) filePath(date string, pulled bool) string {
	if pulled {
		return filepath.Join(l.root, PulledDirName, date+fileSuffix)
	}
	return filepath.Join(l.root, date+fileSuffix)
}

// OpenFile opens one raw daily file for download.
func (l *Logger) OpenFile(date string, pulled bool) (*os.File, error) {
	if l == nil {
		return nil, os.ErrNotExist
	}
	if !validDate(date) {
		return nil, ErrInvalidDate
	}
	l.Flush()
	return os.Open(l.filePath(date, pulled))
}

// matcher holds a query prepared for fast line filtering: most lines are
// rejected by byte comparisons before any JSON is decoded.
type matcher struct {
	q            Query
	text         []byte
	textActions  map[string]bool
	userNeedle   []byte
	ipNeedle     []byte
	statusLo     int
	statusHi     int
	actions      []string
	noActionOnly bool
}

func newMatcher(q Query) *matcher {
	m := &matcher{q: q}
	if text := strings.ToLower(strings.TrimSpace(q.Text)); text != "" {
		m.text = []byte(text)
		m.textActions = actionsForText(text)
	}
	if q.UserID != "" && q.UserID != "-" {
		m.userNeedle = []byte(`"user_id":` + strconv.Quote(q.UserID))
	}
	if q.IP != "" {
		m.ipNeedle = []byte(`"ip":` + strconv.Quote(q.IP))
	}
	switch q.Status {
	case "2xx":
		m.statusLo, m.statusHi = 200, 299
	case "3xx":
		m.statusLo, m.statusHi = 300, 399
	case "4xx":
		m.statusLo, m.statusHi = 400, 499
	case "5xx":
		m.statusLo, m.statusHi = 500, 599
	case "error":
		m.statusLo, m.statusHi = 400, 999
	}
	for _, action := range q.Actions {
		action = strings.TrimSpace(action)
		if action != "" {
			m.actions = append(m.actions, action)
		}
	}
	return m
}

// prefilter rejects lines that cannot match without decoding them.
func (m *matcher) prefilter(line []byte) bool {
	if m.userNeedle != nil && !bytes.Contains(line, m.userNeedle) {
		return false
	}
	if m.ipNeedle != nil && !bytes.Contains(line, m.ipNeedle) {
		return false
	}
	if m.text != nil && !containsFold(line, m.text) {
		if len(m.textActions) == 0 {
			return false
		}
		found := false
		for action := range m.textActions {
			if bytes.Contains(line, []byte(`"action":"`+action+`"`)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (m *matcher) match(e *Entry) bool {
	q := m.q
	if !q.Start.IsZero() && e.Time.Before(q.Start) {
		return false
	}
	if !q.End.IsZero() && e.Time.After(q.End) {
		return false
	}
	if q.UserID == "-" && e.UserID != "" {
		return false
	}
	if q.UserID != "" && q.UserID != "-" && e.UserID != q.UserID {
		return false
	}
	if q.IP != "" && e.IP != q.IP {
		return false
	}
	if q.Method != "" && !strings.EqualFold(e.Method, q.Method) {
		return false
	}
	if q.Source != "" && e.Source != q.Source {
		return false
	}
	if m.statusHi > 0 && (e.Status < m.statusLo || e.Status > m.statusHi) {
		return false
	}
	if len(m.actions) > 0 {
		ok := false
		for _, action := range m.actions {
			if action == "-" && e.Action == "" || e.Action == action || strings.HasPrefix(e.Action, action+".") {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func containsFold(line, lowerNeedle []byte) bool {
	if bytes.Contains(line, lowerNeedle) {
		return true
	}
	return bytes.Contains(bytes.ToLower(line), lowerNeedle)
}

// Search scans the retained days (and pulled days when asked) and returns
// one page of matches plus, optionally, statistics over all of them.
func (l *Logger) Search(q Query) (*Result, error) {
	started := time.Now()
	result := &Result{Entries: []Entry{}}
	if l == nil {
		return result, nil
	}
	if q.Limit < 0 {
		q.Limit = 0
	} else if q.Limit == 0 {
		q.Limit = defaultLimit
	}
	q.Limit = min(q.Limit, MaxLimit)
	q.Offset = min(max(q.Offset, 0), MaxOffset)
	if q.Location == nil {
		q.Location = l.loc
	}
	files, err := l.Files(q.IncludePulled)
	if err != nil {
		return nil, err
	}

	// A daily file only holds entries from its own server-local day, so
	// files outside the requested range are skipped without being opened.
	firstDate, lastDate := "", ""
	if !q.Start.IsZero() {
		firstDate = q.Start.In(l.loc).Format(fileDateLayout)
	}
	if !q.End.IsZero() {
		lastDate = q.End.In(l.loc).Format(fileDateLayout)
	}
	selected := make([]FileInfo, 0, len(files))
	for _, file := range files {
		if (firstDate != "" && file.Date < firstDate) || (lastDate != "" && file.Date > lastDate) {
			continue
		}
		selected = append(selected, file)
	}
	// Oldest first so the newest matches end up last in the window.
	sort.SliceStable(selected, func(i, j int) bool { return selected[i].Date < selected[j].Date })

	m := newMatcher(q)
	window := newRing(q.Offset + q.Limit)
	var agg *aggregator
	if q.Stats {
		agg = newAggregator(q, selected, l.loc)
	}
	for _, file := range selected {
		result.Files++
		result.Bytes += file.Size
		err := l.scanFile(l.filePath(file.Date, file.Pulled), m, func(e *Entry) {
			result.Total++
			window.push(*e)
			if agg != nil {
				agg.add(e)
			}
		})
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	all := window.newestFirst()
	if q.Offset < len(all) {
		result.Entries = all[q.Offset:]
	}
	if agg != nil {
		result.Stats = agg.finish()
	}
	result.TookMS = time.Since(started).Milliseconds()
	return result, nil
}

// scanFile feeds every matching entry of one file, in file order, to fn.
// Lines that do not decode (for example cut short by a crash) are skipped.
func (l *Logger) scanFile(path string, m *matcher, fn func(*Entry)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	reader := bufio.NewReaderSize(f, 256<<10)
	for {
		line, err := reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			// An oversized line: skip the rest of it.
			for errors.Is(err, bufio.ErrBufferFull) {
				_, err = reader.ReadSlice('\n')
			}
			continue
		}
		line = bytes.TrimSpace(line)
		if len(line) > 0 && len(line) <= maxLineBytes && m.prefilter(line) {
			var entry Entry
			if json.Unmarshal(line, &entry) == nil && !entry.Time.IsZero() && m.match(&entry) {
				fn(&entry)
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// ring keeps the last n entries pushed.
type ring struct {
	items []Entry
	max   int
	next  int
}

func newRing(n int) *ring {
	return &ring{items: make([]Entry, 0, min(n, 4096)), max: n}
}

func (r *ring) push(e Entry) {
	if r.max == 0 {
		return
	}
	if len(r.items) < r.max {
		r.items = append(r.items, e)
		return
	}
	r.items[r.next] = e
	r.next = (r.next + 1) % r.max
}

func (r *ring) newestFirst() []Entry {
	n := len(r.items)
	out := make([]Entry, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, r.items[(r.next-1-i+2*n)%n])
	}
	// Entries within a file are appended in completion order, which can
	// differ slightly from their start times; present them strictly by time.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out
}
