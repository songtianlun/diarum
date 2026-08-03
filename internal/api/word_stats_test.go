package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
)

func TestCountWords(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    int
	}{
		{"empty", "", 0},
		{"plain latin", "hello brave new world", 4},
		{"markup is not counted", "<p>hello <strong>world</strong></p>", 2},
		{"adjacent blocks do not merge", "<p>one</p><p>two</p>", 2},
		{"entities are decoded", "caf&eacute; &amp; cream", 2},
		{"nbsp is a separator", "one&nbsp;two", 2},
		{"cjk counts per character", "<p>今天天气很好</p>", 6},
		// 9 CJK characters, plus "Go" and "3".
		{"mixed cjk and latin", "今天用了 Go 写了 3 个函数", 11},
		{"punctuation is skipped", "Hi, there! Yes.", 3},
		{"contractions stay one word", "it's fine", 2},
		{"script bodies are ignored", `<p>hi</p><script>var a = 1;</script>`, 1},
		{"style bodies are ignored", `<style>p{color:red}</style><p>hi</p>`, 1},
		{"unterminated tag stops counting", "<p>hi</p><div", 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CountWords(tc.content); got != tc.want {
				t.Fatalf("CountWords(%q) = %d, want %d", tc.content, got, tc.want)
			}
		})
	}
}

func utcDay(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestBuildSeriesBetweenIsDense(t *testing.T) {
	daily := []DayWordCount{
		{Date: "2026-08-01", Words: 10},
		{Date: "2026-08-03", Words: 5},
	}

	series := buildSeriesBetween(daily, utcDay(2026, 8, 1), utcDay(2026, 8, 3))
	want := []int{10, 0, 5}
	if len(series) != len(want) {
		t.Fatalf("series length = %d, want %d", len(series), len(want))
	}
	for i := range want {
		if series[i] != want[i] {
			t.Fatalf("series[%d] = %d, want %d", i, series[i], want[i])
		}
	}
}

func TestBuildSeriesBetweenExcludesOutsideWindow(t *testing.T) {
	daily := []DayWordCount{
		{Date: "2026-07-31", Words: 99},
		{Date: "2026-08-02", Words: 7},
		{Date: "2026-08-04", Words: 99},
	}

	series := buildSeriesBetween(daily, utcDay(2026, 8, 1), utcDay(2026, 8, 3))
	want := []int{0, 7, 0}
	for i := range want {
		if series[i] != want[i] {
			t.Fatalf("series[%d] = %d, want %d", i, series[i], want[i])
		}
	}
}

func TestBuildSeriesBetweenIsBounded(t *testing.T) {
	daily := []DayWordCount{{Date: "2005-01-01", Words: 42}}

	series := buildSeriesBetween(daily, utcDay(2000, 1, 1), utcDay(2026, 8, 3))
	if len(series) != maxDailySeriesDays {
		t.Fatalf("series length = %d, want %d", len(series), maxDailySeriesDays)
	}
}

func TestBuildSeriesBetweenRejectsInvertedRange(t *testing.T) {
	series := buildSeriesBetween(nil, utcDay(2026, 8, 3), utcDay(2026, 8, 1))
	if len(series) != 0 {
		t.Fatalf("series length = %d, want 0", len(series))
	}
}

func TestClampSeriesDays(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{"", defaultDailySeriesDays},
		{"nonsense", defaultDailySeriesDays},
		{"90", 90},
		{"0", 1},
		{"-5", 1},
		{"999999", maxDailySeriesDays},
	}
	for _, tc := range cases {
		if got := clampSeriesDays(tc.raw, defaultDailySeriesDays); got != tc.want {
			t.Fatalf("clampSeriesDays(%q) = %d, want %d", tc.raw, got, tc.want)
		}
	}
}

func TestFillMissingYears(t *testing.T) {
	filled := fillMissingYears([]YearWordCount{
		{Year: 2022, Words: 100, Entries: 3},
		{Year: 2025, Words: 50, Entries: 1},
	}, 2026)

	wantYears := []int{2022, 2023, 2024, 2025, 2026}
	if len(filled) != len(wantYears) {
		t.Fatalf("got %d years, want %d", len(filled), len(wantYears))
	}
	for i, year := range wantYears {
		if filled[i].Year != year {
			t.Fatalf("filled[%d].Year = %d, want %d", i, filled[i].Year, year)
		}
	}
	if filled[1].Words != 0 || filled[1].Entries != 0 {
		t.Fatalf("gap year should be zeroed, got %+v", filled[1])
	}
	if filled[3].Words != 50 {
		t.Fatalf("filled[3].Words = %d, want 50", filled[3].Words)
	}
}

func TestFillMissingYearsEmpty(t *testing.T) {
	filled := fillMissingYears(nil, 2026)
	if len(filled) != 1 || filled[0].Year != 2026 || filled[0].Words != 0 {
		t.Fatalf("unexpected result: %+v", filled)
	}
}

func jsonNumber(t *testing.T, payload map[string]any, key string) float64 {
	t.Helper()

	value, ok := payload[key].(float64)
	if !ok {
		t.Fatalf("payload[%q] = %#v, want a number", key, payload[key])
	}
	return value
}

func jsonArray(t *testing.T, payload map[string]any, key string) []any {
	t.Helper()

	value, ok := payload[key].([]any)
	if !ok {
		t.Fatalf("payload[%q] = %#v, want an array", key, payload[key])
	}
	return value
}

func TestWordStatsRoute(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	RegisterDiaryRoutes(e, s, authMiddlewareFor(user), nil)

	seed := []struct{ date, content string }{
		{"2024-05-10", "<p>五月十日</p>"},          // 4 words, two years back
		{"2026-07-20", "hello brave new world"}, // 4 words, inside every window
		{"2026-08-01", "今天写了字"},                // 5 words, inside every window
	}
	for _, item := range seed {
		if _, err := s.InsertImportedDiary(user.ID, "", item.date, item.content, "", ""); err != nil {
			t.Fatalf("InsertImportedDiary(%s): %v", item.date, err)
		}
	}

	rec := performRequest(t, e, http.MethodGet, "/api/v1/diaries/word-stats?today=2026-08-03&series_days=30", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("word-stats status = %d body=%s", rec.Code, rec.Body.String())
	}
	payload := decodeJSONBody(t, rec)

	if got := jsonNumber(t, payload, "total_entries"); got != 3 {
		t.Fatalf("total_entries = %v, want 3", got)
	}
	if got := jsonNumber(t, payload, "total_words"); got != 13 {
		t.Fatalf("total_words = %v, want 13", got)
	}
	// The 2024 entry falls outside every rolling window.
	for _, key := range []string{"last_month_words", "last_six_months_words", "last_twelve_months_words"} {
		if got := jsonNumber(t, payload, key); got != 9 {
			t.Fatalf("%s = %v, want 9", key, got)
		}
	}
	if payload["first_date"] != "2024-05-10" || payload["last_date"] != "2026-08-01" {
		t.Fatalf("span = %v..%v, want 2024-05-10..2026-08-01", payload["first_date"], payload["last_date"])
	}
	if payload["series_start"] != "2026-07-05" || payload["series_end"] != "2026-08-03" {
		t.Fatalf("series window = %v..%v, want 2026-07-05..2026-08-03", payload["series_start"], payload["series_end"])
	}
	if series := jsonArray(t, payload, "series"); len(series) != 30 {
		t.Fatalf("series length = %d, want 30", len(series))
	}

	// Silent years are filled in so the yearly chart stays continuous.
	yearly := jsonArray(t, payload, "yearly")
	if len(yearly) != 3 {
		t.Fatalf("yearly length = %d, want 3 (2024-2026)", len(yearly))
	}
	gap, ok := yearly[1].(map[string]any)
	if !ok {
		t.Fatalf("yearly[1] = %#v, want an object", yearly[1])
	}
	if gap["year"].(float64) != 2025 || gap["words"].(float64) != 0 {
		t.Fatalf("yearly[1] = %#v, want 2025 with zero words", gap)
	}

	// No parameters: server date and the default window.
	rec = performRequest(t, e, http.MethodGet, "/api/v1/diaries/word-stats", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("bare word-stats status = %d body=%s", rec.Code, rec.Body.String())
	}
	if series := jsonArray(t, decodeJSONBody(t, rec), "series"); len(series) != defaultDailySeriesDays {
		t.Fatalf("default series length = %d, want %d", len(series), defaultDailySeriesDays)
	}
}

func TestWordStatsRouteSkipsMalformedDates(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	RegisterDiaryRoutes(e, s, authMiddlewareFor(user), nil)

	// A row whose date cannot be parsed must be skipped, not fatal.
	if _, err := s.DB.Exec(
		`INSERT INTO diaries(content, created, date, id, mood, owner, updated, weather, tags)
		 VALUES('broken', 'now', 'bad-date', 'broken-id', '', ?, 'now', '', '[]')`,
		user.ID,
	); err != nil {
		t.Fatalf("insert malformed diary: %v", err)
	}

	rec := performRequest(t, e, http.MethodGet, "/api/v1/diaries/word-stats?today=2026-08-03", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("word-stats status = %d body=%s", rec.Code, rec.Body.String())
	}
	payload := decodeJSONBody(t, rec)
	yearly := jsonArray(t, payload, "yearly")
	if len(yearly) != 1 {
		t.Fatalf("yearly length = %d, want 1 (current year only)", len(yearly))
	}
	if year := yearly[0].(map[string]any)["year"].(float64); year != 2026 {
		t.Fatalf("yearly[0].year = %v, want 2026", year)
	}
}

func TestWordSeriesRoute(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	RegisterDiaryRoutes(e, s, authMiddlewareFor(user), nil)

	if _, err := s.InsertImportedDiary(user.ID, "", "2026-08-01", "今天写了字", "", ""); err != nil {
		t.Fatalf("InsertImportedDiary: %v", err)
	}

	rec := performRequest(t, e, http.MethodGet, "/api/v1/diaries/word-series?start=2026-07-30&end=2026-08-03", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("word-series status = %d body=%s", rec.Code, rec.Body.String())
	}
	payload := decodeJSONBody(t, rec)
	series := jsonArray(t, payload, "series")
	if len(series) != 5 {
		t.Fatalf("series length = %d, want 5", len(series))
	}
	if series[2].(float64) != 5 {
		t.Fatalf("series[2] = %v, want 5 (2026-08-01)", series[2])
	}
	if got := jsonNumber(t, payload, "total"); got != 5 {
		t.Fatalf("total = %v, want 5", got)
	}

	// An inverted range is swapped rather than rejected.
	rec = performRequest(t, e, http.MethodGet, "/api/v1/diaries/word-series?start=2026-08-03&end=2026-07-30", nil, nil)
	payload = decodeJSONBody(t, rec)
	if payload["start"] != "2026-07-30" || payload["end"] != "2026-08-03" {
		t.Fatalf("inverted range = %v..%v, want 2026-07-30..2026-08-03", payload["start"], payload["end"])
	}

	// Missing parameters fall back to the default window ending today.
	rec = performRequest(t, e, http.MethodGet, "/api/v1/diaries/word-series", nil, nil)
	if series := jsonArray(t, decodeJSONBody(t, rec), "series"); len(series) != defaultDailySeriesDays {
		t.Fatalf("default series length = %d, want %d", len(series), defaultDailySeriesDays)
	}

	// An absurd span is trimmed to the cap instead of erroring.
	rec = performRequest(t, e, http.MethodGet, "/api/v1/diaries/word-series?start=1900-01-01&end=2026-08-03", nil, nil)
	if series := jsonArray(t, decodeJSONBody(t, rec), "series"); len(series) != maxDailySeriesDays {
		t.Fatalf("capped series length = %d, want %d", len(series), maxDailySeriesDays)
	}
}

func TestWordStatsRoutesDatabaseError(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	RegisterDiaryRoutes(e, s, authMiddlewareFor(user), nil)

	if err := s.DB.Close(); err != nil {
		t.Fatalf("close DB: %v", err)
	}
	for _, path := range []string{"/api/v1/diaries/word-stats", "/api/v1/diaries/word-series"} {
		rec := performRequest(t, e, http.MethodGet, path, nil, nil)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("%s status = %d, want 500", path, rec.Code)
		}
	}
}

func TestYearFromDate(t *testing.T) {
	cases := []struct {
		date string
		want int
		ok   bool
	}{
		{"2026-08-03", 2026, true},
		{"1999-01-01", 1999, true},
		{"", 0, false},
		{"abc", 0, false},
		{"20x6-01-01", 0, false},
	}
	for _, tc := range cases {
		got, ok := yearFromDate(tc.date)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("yearFromDate(%q) = (%d, %v), want (%d, %v)", tc.date, got, ok, tc.want, tc.ok)
		}
	}
}

func TestDailyWordCountsCachesUntilDataChanges(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)

	if _, err := s.InsertImportedDiary(user.ID, "", "2026-08-01", "<p>hello world</p>", "", ""); err != nil {
		t.Fatalf("InsertImportedDiary: %v", err)
	}

	first, err := dailyWordCounts(s, user.ID)
	if err != nil {
		t.Fatalf("dailyWordCounts: %v", err)
	}
	if len(first) != 1 || first[0].Words != 2 {
		t.Fatalf("first = %+v, want one entry of 2 words", first)
	}

	second, err := dailyWordCounts(s, user.ID)
	if err != nil {
		t.Fatalf("dailyWordCounts (cached): %v", err)
	}
	// Same backing array means the scan was skipped entirely.
	if &first[0] != &second[0] {
		t.Fatal("unchanged diaries should be served from cache")
	}

	if _, err := s.InsertImportedDiary(user.ID, "", "2026-08-02", "今天很好", "", ""); err != nil {
		t.Fatalf("InsertImportedDiary: %v", err)
	}
	third, err := dailyWordCounts(s, user.ID)
	if err != nil {
		t.Fatalf("dailyWordCounts (after change): %v", err)
	}
	if len(third) != 2 {
		t.Fatalf("got %d entries after adding a diary, want 2", len(third))
	}
	if third[1].Words != 4 {
		t.Fatalf("third[1].Words = %d, want 4", third[1].Words)
	}
}

func TestDailyWordCountsScanError(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)

	if err := s.DB.Close(); err != nil {
		t.Fatalf("close DB: %v", err)
	}
	if _, err := dailyWordCounts(s, user.ID); err == nil {
		t.Fatal("dailyWordCounts should fail after DB close")
	}
}

func TestParseDateParamFallsBackToServerDate(t *testing.T) {
	fallback := time.Date(2026, 8, 3, 17, 30, 0, 0, time.UTC)

	if got := parseDateParam("2024-02-29", fallback); got.Format(dateLayout) != "2024-02-29" {
		t.Fatalf("got %s, want 2024-02-29", got.Format(dateLayout))
	}
	if got := parseDateParam("not-a-date", fallback); got.Format(dateLayout) != "2026-08-03" {
		t.Fatalf("got %s, want 2026-08-03", got.Format(dateLayout))
	}
	if got := parseDateParam("", fallback); !got.Equal(time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("got %v, want midnight UTC", got)
	}
}
