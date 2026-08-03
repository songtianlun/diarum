package api

import (
	"testing"
	"time"
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
