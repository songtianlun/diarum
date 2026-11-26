package holidays

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/songtianlun/diarum/internal/logger"
	"github.com/songtianlun/diarum/internal/store"
)

//go:embed data/*.json
var embeddedFS embed.FS

const (
	RegionCN = "cn"

	// SettingCNEnabledKey stores the global admin switch for China mainland holidays.
	SettingCNEnabledKey = "holidays.cn.enabled"

	MinYear = 2000
	MaxYear = 2100

	maxOverrideNameRunes = 40

	SourceEmbedded = "embedded"

	defaultHTTPTimeout    = 10 * time.Second
	maxPayloadBytes       = 512 * 1024
	currentYearTTL        = 7 * 24 * time.Hour
	nextYearTTL           = 7 * 24 * time.Hour
	policyWindowTTL       = 24 * time.Hour
	notPublishedCooldown  = 24 * time.Hour
	schedulerTickInterval = 6 * time.Hour
	defaultStartupDelay   = 10 * time.Second
)

var (
	ErrInvalidYear = errors.New("year out of range")
	ErrInvalidData = errors.New("invalid holiday data")
	ErrInvalidDate = errors.New("invalid date")
	ErrInvalidName = errors.New("invalid holiday name")
)

// DefaultSources lists holiday-cn mirrors in fallback order.
var DefaultSources = []string{
	"https://fastly.jsdelivr.net/gh/NateScarlet/holiday-cn@master/%d.json",
	"https://cdn.jsdelivr.net/gh/NateScarlet/holiday-cn@master/%d.json",
	"https://raw.githubusercontent.com/NateScarlet/holiday-cn/master/%d.json",
}

// Day represents a statutory holiday or lieu workday adjustment.
type Day struct {
	Name     string `json:"name"`
	Date     string `json:"date"`
	IsOffDay bool   `json:"isOffDay"`
	Custom   bool   `json:"custom,omitempty"`
}

// YearData matches the NateScarlet/holiday-cn yearly JSON schema.
type YearData struct {
	Year   int      `json:"year"`
	Papers []string `json:"papers"`
	Days   []Day    `json:"days"`
}

// YearResponse is returned by the holiday query API for a single year.
type YearResponse struct {
	Region    string   `json:"region"`
	Year      int      `json:"year"`
	Published bool     `json:"published"`
	Days      []Day    `json:"days"`
	Papers    []string `json:"papers"`
	Source    string   `json:"source"`
	FetchedAt string   `json:"fetched_at,omitempty"`
}

// YearSummary summarizes cached/embedded availability for one year.
type YearSummary struct {
	Year      int      `json:"year"`
	Published bool     `json:"published"`
	DayCount  int      `json:"day_count"`
	Source    string   `json:"source"`
	FetchedAt string   `json:"fetched_at,omitempty"`
	Papers    []string `json:"papers,omitempty"`
}

// StatusResponse summarizes China holiday data status.
type StatusResponse struct {
	Region        string        `json:"region"`
	Enabled       bool          `json:"enabled"`
	Years         []YearSummary `json:"years"`
	LastFetchedAt string        `json:"last_fetched_at,omitempty"`
}

// Options allows tests to override clocks, HTTP clients, and mirror URLs.
type Options struct {
	Client       *http.Client
	Sources      []string
	Now          func() time.Time
	StartupDelay time.Duration
	TickInterval time.Duration
}

// Service manages China mainland statutory holiday and lieu-day data with
// embedded baselines, SQLite caching, multi-mirror syncing, and background refresh.
type Service struct {
	store        *store.Store
	client       *http.Client
	sources      []string
	now          func() time.Time
	startupDelay time.Duration
	tickInterval time.Duration

	embedded map[int]YearData
	inFlight sync.Map // year(int) -> struct{}
}

// NewService creates a holiday service backed by embedded snapshots and SQLite.
func NewService(s *store.Store, opts ...Options) *Service {
	var opt Options
	if len(opts) > 0 {
		opt = opts[0]
	}
	client := opt.Client
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	sources := opt.Sources
	if len(sources) == 0 {
		sources = append([]string(nil), DefaultSources...)
	}
	nowFn := opt.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	startupDelay := opt.StartupDelay
	if startupDelay == 0 {
		startupDelay = defaultStartupDelay
	}
	tickInterval := opt.TickInterval
	if tickInterval == 0 {
		tickInterval = schedulerTickInterval
	}

	return &Service{
		store:        s,
		client:       client,
		sources:      sources,
		now:          nowFn,
		startupDelay: startupDelay,
		tickInterval: tickInterval,
		embedded:     loadEmbedded(),
	}
}

func loadEmbedded() map[int]YearData {
	out := make(map[int]YearData)
	entries, err := embeddedFS.ReadDir("data")
	if err != nil {
		return out
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		yearStr := strings.TrimSuffix(entry.Name(), ".json")
		year, err := strconv.Atoi(yearStr)
		if err != nil {
			continue
		}
		raw, err := embeddedFS.ReadFile(path.Join("data", entry.Name()))
		if err != nil {
			continue
		}
		var data YearData
		if err := json.Unmarshal(raw, &data); err != nil {
			continue
		}
		if err := ValidateYearData(year, &data); err != nil {
			continue
		}
		out[year] = data
	}
	return out
}

// ValidateYearData checks that parsed holiday JSON is well-formed and matches expectedYear.
func ValidateYearData(expectedYear int, data *YearData) error {
	if data == nil {
		return fmt.Errorf("%w: nil payload", ErrInvalidData)
	}
	if data.Year != expectedYear {
		return fmt.Errorf("%w: year mismatch (got %d, want %d)", ErrInvalidData, data.Year, expectedYear)
	}
	if len(data.Days) == 0 || len(data.Days) > 366 {
		return fmt.Errorf("%w: unexpected day count %d", ErrInvalidData, len(data.Days))
	}
	seen := make(map[string]struct{}, len(data.Days))
	prefix := fmt.Sprintf("%04d-", expectedYear)
	for _, d := range data.Days {
		if strings.TrimSpace(d.Name) == "" {
			return fmt.Errorf("%w: empty holiday name on %s", ErrInvalidData, d.Date)
		}
		if !strings.HasPrefix(d.Date, prefix) {
			return fmt.Errorf("%w: date %q does not belong to %d", ErrInvalidData, d.Date, expectedYear)
		}
		parsed, err := time.Parse("2006-01-02", d.Date)
		if err != nil || parsed.Format("2006-01-02") != d.Date {
			return fmt.Errorf("%w: invalid date %q", ErrInvalidData, d.Date)
		}
		if _, dup := seen[d.Date]; dup {
			return fmt.Errorf("%w: duplicate date %q", ErrInvalidData, d.Date)
		}
		seen[d.Date] = struct{}{}
	}
	return nil
}

// Enabled reports whether an administrator has turned on China mainland holidays globally.
func (s *Service) Enabled() bool {
	if s == nil || s.store == nil {
		return false
	}
	raw, ok, err := s.store.GetSystemSetting(SettingCNEnabledKey)
	if err != nil || !ok {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(raw), "true")
}

// SetEnabled persists the global admin switch for China mainland holidays.
func (s *Service) SetEnabled(enabled bool) error {
	if s.store == nil {
		return errors.New("store unavailable")
	}
	val := "false"
	if enabled {
		val = "true"
	}
	return s.store.SetSystemSetting(SettingCNEnabledKey, val)
}

// SetOverride adds or updates an admin manual holiday/workday override for a single date.
func (s *Service) SetOverride(date, name string, isOffDay bool) (*Day, error) {
	if s.store == nil {
		return nil, errors.New("store unavailable")
	}
	date = strings.TrimSpace(date)
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil || parsed.Format("2006-01-02") != date {
		return nil, ErrInvalidDate
	}
	year := parsed.Year()
	if year < MinYear || year > MaxYear {
		return nil, ErrInvalidYear
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxOverrideNameRunes {
		return nil, ErrInvalidName
	}

	rec := &store.HolidayOverrideRecord{
		Region:   RegionCN,
		Date:     date,
		Year:     year,
		Name:     name,
		IsOffDay: isOffDay,
	}
	if err := s.store.UpsertHolidayOverride(rec); err != nil {
		return nil, err
	}
	return &Day{
		Name:     name,
		Date:     date,
		IsOffDay: isOffDay,
		Custom:   true,
	}, nil
}

// DeleteOverride removes a manual override by date.
func (s *Service) DeleteOverride(date string) error {
	if s.store == nil {
		return errors.New("store unavailable")
	}
	date = strings.TrimSpace(date)
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil || parsed.Format("2006-01-02") != date {
		return ErrInvalidDate
	}
	return s.store.DeleteHolidayOverride(RegionCN, date)
}

// ListOverrides returns admin manual overrides. Pass year == 0 for all years.
func (s *Service) ListOverrides(year int) ([]Day, error) {
	if s.store == nil {
		return []Day{}, nil
	}
	records, err := s.store.ListHolidayOverrides(RegionCN, year)
	if err != nil {
		return nil, err
	}
	out := make([]Day, 0, len(records))
	for _, r := range records {
		out = append(out, Day{
			Name:     r.Name,
			Date:     r.Date,
			IsOffDay: r.IsOffDay,
			Custom:   true,
		})
	}
	return out, nil
}

// GetYear returns China mainland holiday data for the requested year, merged with
// any admin manual overrides. It serves immediately from SQLite cache or embedded
// baseline and triggers an asynchronous background refresh if stale.
func (s *Service) GetYear(ctx context.Context, year int) (*YearResponse, error) {
	if year < MinYear || year > MaxYear {
		return nil, ErrInvalidYear
	}

	now := s.now()
	var rec *store.HolidayYearRecord
	if s.store != nil {
		var err error
		rec, err = s.store.GetHolidayYear(RegionCN, year)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}

	var resp *YearResponse
	if rec != nil && !rec.NotPublished && rec.Payload != "" {
		var data YearData
		if err := json.Unmarshal([]byte(rec.Payload), &data); err == nil && ValidateYearData(year, &data) == nil {
			if s.Enabled() && s.needsRefresh(year, rec, now) {
				s.triggerBackgroundRefresh(year)
			}
			resp = buildResponse(year, &data, rec.Source, rec.FetchedAt)
		}
	}

	if resp == nil {
		if emb, ok := s.embedded[year]; ok {
			if s.Enabled() && s.needsEmbeddedRefresh(year, rec, now) {
				s.triggerBackgroundRefresh(year)
			}
			resp = buildResponse(year, &emb, SourceEmbedded, "")
		} else if rec != nil && rec.NotPublished {
			if s.Enabled() && s.needsNotPublishedRetry(rec, now) {
				s.triggerBackgroundRefresh(year)
			}
			resp = &YearResponse{
				Region:    RegionCN,
				Year:      year,
				Published: false,
				Days:      []Day{},
				Papers:    []string{},
				Source:    rec.Source,
				FetchedAt: rec.FetchedAt,
			}
		} else {
			if s.Enabled() && year >= 2007 && year <= now.Year()+2 {
				s.triggerBackgroundRefresh(year)
			}
			resp = &YearResponse{
				Region:    RegionCN,
				Year:      year,
				Published: false,
				Days:      []Day{},
				Papers:    []string{},
				Source:    "",
			}
		}
	}

	return s.applyOverrides(year, resp)
}

func (s *Service) applyOverrides(year int, resp *YearResponse) (*YearResponse, error) {
	overrides, err := s.ListOverrides(year)
	if err != nil || len(overrides) == 0 {
		return resp, err
	}
	byDate := make(map[string]Day, len(resp.Days)+len(overrides))
	for _, d := range resp.Days {
		byDate[d.Date] = d
	}
	for _, ov := range overrides {
		byDate[ov.Date] = ov
	}
	merged := make([]Day, 0, len(byDate))
	for _, d := range byDate {
		merged = append(merged, d)
	}
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Date < merged[j].Date
	})
	resp.Days = merged
	return resp, nil
}

// RefreshYear synchronously fetches the latest data for a single year from mirrors.
func (s *Service) RefreshYear(ctx context.Context, year int) (*YearResponse, error) {
	if year < MinYear || year > MaxYear {
		return nil, ErrInvalidYear
	}

	var existing *store.HolidayYearRecord
	if s.store != nil {
		rec, err := s.store.GetHolidayYear(RegionCN, year)
		if err == nil {
			existing = rec
		}
	}

	etag := ""
	if existing != nil && !existing.NotPublished {
		etag = existing.ETag
	}

	nowStr := s.now().UTC().Format(time.RFC3339)
	notFoundCount := 0
	var lastErr error

	for _, tmpl := range s.sources {
		url := fmt.Sprintf(tmpl, year)
		data, newETag, statusCode, err := s.fetchFromURL(ctx, year, url, etag)
		if err != nil {
			lastErr = err
			continue
		}

		switch statusCode {
		case http.StatusNotModified:
			if existing != nil && existing.Payload != "" {
				updated := *existing
				updated.FetchedAt = nowStr
				updated.Updated = nowStr
				if s.store != nil {
					_ = s.store.UpsertHolidayYear(&updated)
				}
				var parsed YearData
				if err := json.Unmarshal([]byte(existing.Payload), &parsed); err == nil {
					return buildResponse(year, &parsed, updated.Source, nowStr), nil
				}
			}
			// ETag matched something we can no longer decode; retry without ETag next source.
			etag = ""
			continue

		case http.StatusOK:
			normalized, err := json.Marshal(data)
			if err != nil {
				lastErr = err
				continue
			}
			rec := &store.HolidayYearRecord{
				Region:       RegionCN,
				Year:         year,
				Payload:      string(normalized),
				ETag:         newETag,
				Source:       url,
				NotPublished: false,
				FetchedAt:    nowStr,
				Updated:      nowStr,
			}
			if s.store != nil {
				if err := s.store.UpsertHolidayYear(rec); err != nil {
					return nil, err
				}
			}
			return buildResponse(year, data, url, nowStr), nil

		case http.StatusNotFound:
			notFoundCount++
			continue
		}
	}

	if notFoundCount == len(s.sources) && len(s.sources) > 0 {
		// Never overwrite an already-published DB record with not_published.
		if existing != nil && !existing.NotPublished && existing.Payload != "" {
			var parsed YearData
			if err := json.Unmarshal([]byte(existing.Payload), &parsed); err == nil {
				return buildResponse(year, &parsed, existing.Source, existing.FetchedAt), nil
			}
		}
		if s.store != nil {
			_ = s.store.UpsertHolidayYear(&store.HolidayYearRecord{
				Region:       RegionCN,
				Year:         year,
				NotPublished: true,
				Source:       "not_published",
				FetchedAt:    nowStr,
				Updated:      nowStr,
			})
		}
		return &YearResponse{
			Region:    RegionCN,
			Year:      year,
			Published: false,
			Days:      []Day{},
			Papers:    []string{},
			Source:    "not_published",
			FetchedAt: nowStr,
		}, nil
	}

	if lastErr == nil {
		lastErr = errors.New("no holiday source succeeded")
	}
	return nil, lastErr
}

// RefreshPolicyYears refreshes the current year and next year synchronously.
// A 404 on the next year (not yet published by the State Council) is treated as non-fatal.
func (s *Service) RefreshPolicyYears(ctx context.Context) (*StatusResponse, error) {
	currentYear := s.now().Year()
	years := []int{currentYear, currentYear + 1}

	var firstErr error
	succeeded := 0
	for _, y := range years {
		if _, err := s.RefreshYear(ctx, y); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			logger.Warn("[Holidays] refresh %d failed: %v", y, err)
		} else {
			succeeded++
		}
	}
	status, err := s.Status()
	if err != nil {
		return nil, err
	}
	if succeeded == 0 && firstErr != nil {
		return status, firstErr
	}
	return status, nil
}

// Status returns an overview of available embedded and cached years.
func (s *Service) Status() (*StatusResponse, error) {
	byYear := make(map[int]YearSummary, len(s.embedded))
	for y, data := range s.embedded {
		byYear[y] = YearSummary{
			Year:      y,
			Published: true,
			DayCount:  len(data.Days),
			Source:    SourceEmbedded,
			Papers:    cloneStrings(data.Papers),
		}
	}

	lastFetched := ""
	if s.store != nil {
		records, err := s.store.ListHolidayYears(RegionCN)
		if err != nil {
			return nil, err
		}
		for _, rec := range records {
			if rec.FetchedAt > lastFetched {
				lastFetched = rec.FetchedAt
			}
			if rec.NotPublished || rec.Payload == "" {
				if _, hasEmb := byYear[rec.Year]; !hasEmb {
					byYear[rec.Year] = YearSummary{
						Year:      rec.Year,
						Published: false,
						Source:    rec.Source,
						FetchedAt: rec.FetchedAt,
					}
				}
				continue
			}
			var data YearData
			if err := json.Unmarshal([]byte(rec.Payload), &data); err != nil {
				continue
			}
			byYear[rec.Year] = YearSummary{
				Year:      rec.Year,
				Published: true,
				DayCount:  len(data.Days),
				Source:    rec.Source,
				FetchedAt: rec.FetchedAt,
				Papers:    cloneStrings(data.Papers),
			}
		}
	}

	years := make([]YearSummary, 0, len(byYear))
	for _, item := range byYear {
		years = append(years, item)
	}
	sort.Slice(years, func(i, j int) bool {
		return years[i].Year < years[j].Year
	})

	return &StatusResponse{
		Region:        RegionCN,
		Enabled:       s.Enabled(),
		Years:         years,
		LastFetchedAt: lastFetched,
	}, nil
}

// StartScheduler runs periodic background checks for current and next year policy updates.
func (s *Service) StartScheduler(ctx context.Context) {
	go s.runScheduler(ctx)
}

func (s *Service) runScheduler(ctx context.Context) {
	if s.startupDelay > 0 {
		timer := time.NewTimer(s.startupDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}

	s.syncDuePolicyYears(ctx)

	ticker := time.NewTicker(s.tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syncDuePolicyYears(ctx)
		}
	}
}

func (s *Service) syncDuePolicyYears(ctx context.Context) {
	if !s.Enabled() {
		return
	}
	now := s.now()
	for _, y := range []int{now.Year(), now.Year() + 1} {
		var rec *store.HolidayYearRecord
		if s.store != nil {
			if r, err := s.store.GetHolidayYear(RegionCN, y); err == nil {
				rec = r
			}
		}
		if rec == nil || (rec.NotPublished && s.needsNotPublishedRetry(rec, now)) || (!rec.NotPublished && s.needsRefresh(y, rec, now)) {
			reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			if _, err := s.RefreshYear(reqCtx, y); err != nil {
				logger.Debug("[Holidays] scheduled refresh %d skipped: %v", y, err)
			}
			cancel()
		}
	}
}

func (s *Service) fetchFromURL(ctx context.Context, expectedYear int, url, etag string) (*YearData, string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Diarum-Holidays/1.0")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified || resp.StatusCode == http.StatusNotFound {
		return nil, resp.Header.Get("ETag"), resp.StatusCode, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", resp.StatusCode, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPayloadBytes))
	if err != nil {
		return nil, "", resp.StatusCode, err
	}
	var data YearData
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, "", resp.StatusCode, fmt.Errorf("decode %s: %w", url, err)
	}
	if err := ValidateYearData(expectedYear, &data); err != nil {
		return nil, "", resp.StatusCode, fmt.Errorf("validate %s: %w", url, err)
	}
	return &data, resp.Header.Get("ETag"), resp.StatusCode, nil
}

func (s *Service) triggerBackgroundRefresh(year int) {
	if _, loaded := s.inFlight.LoadOrStore(year, struct{}{}); loaded {
		return
	}
	go func() {
		defer s.inFlight.Delete(year)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, err := s.RefreshYear(ctx, year); err != nil {
			logger.Debug("[Holidays] background refresh %d failed: %v", year, err)
		}
	}()
}

func (s *Service) needsRefresh(year int, rec *store.HolidayYearRecord, now time.Time) bool {
	currentYear := now.Year()
	if year < currentYear {
		return false
	}
	fetchedAt, err := time.Parse(time.RFC3339, rec.FetchedAt)
	if err != nil {
		return true
	}
	ttl := currentYearTTL
	if year > currentYear {
		if now.Month() >= time.October {
			ttl = policyWindowTTL
		} else {
			ttl = nextYearTTL
		}
	}
	return now.Sub(fetchedAt) >= ttl
}

func (s *Service) needsEmbeddedRefresh(year int, rec *store.HolidayYearRecord, now time.Time) bool {
	if year < now.Year() {
		return false
	}
	if rec == nil {
		return true
	}
	return s.needsRefresh(year, rec, now)
}

func (s *Service) needsNotPublishedRetry(rec *store.HolidayYearRecord, now time.Time) bool {
	fetchedAt, err := time.Parse(time.RFC3339, rec.FetchedAt)
	if err != nil {
		return true
	}
	return now.Sub(fetchedAt) >= notPublishedCooldown
}

func buildResponse(year int, data *YearData, source, fetchedAt string) *YearResponse {
	days := make([]Day, len(data.Days))
	copy(days, data.Days)
	return &YearResponse{
		Region:    RegionCN,
		Year:      year,
		Published: true,
		Days:      days,
		Papers:    cloneStrings(data.Papers),
		Source:    source,
		FetchedAt: fetchedAt,
	}
}

func cloneStrings(src []string) []string {
	if len(src) == 0 {
		return []string{}
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}
