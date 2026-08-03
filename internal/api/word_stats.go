package api

import (
	"html"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/songtianlun/diarum/internal/store"
)

// DayWordCount is the word count of a single diary entry, keyed by its date.
type DayWordCount struct {
	Date  string `json:"date"`
	Words int    `json:"words"`
}

// YearWordCount aggregates one calendar year of writing.
type YearWordCount struct {
	Year    int `json:"year"`
	Words   int `json:"words"`
	Entries int `json:"entries"`
}

type wordStatsCacheEntry struct {
	fingerprint string
	daily       []DayWordCount
}

// wordStatsCache avoids re-scanning and re-parsing every diary body on each
// request. The cached value is invalidated by a cheap fingerprint query that
// changes whenever the user's diaries change.
var wordStatsCache = struct {
	mu      sync.RWMutex
	entries map[string]wordStatsCacheEntry
}{entries: make(map[string]wordStatsCacheEntry)}

// dailyWordCounts returns one entry per diary, in ascending date order.
func dailyWordCounts(s *store.Store, owner string) ([]DayWordCount, error) {
	fingerprint := s.DiaryFingerprint(owner)

	wordStatsCache.mu.RLock()
	cached, ok := wordStatsCache.entries[owner]
	wordStatsCache.mu.RUnlock()
	if ok && cached.fingerprint == fingerprint {
		return cached.daily, nil
	}

	daily := make([]DayWordCount, 0, 512)
	err := s.ScanDiaryContents(owner, func(date, content string) {
		daily = append(daily, DayWordCount{Date: store.DateOnly(date), Words: CountWords(content)})
	})
	if err != nil {
		return nil, err
	}

	wordStatsCache.mu.Lock()
	wordStatsCache.entries[owner] = wordStatsCacheEntry{fingerprint: fingerprint, daily: daily}
	wordStatsCache.mu.Unlock()
	return daily, nil
}

const (
	dateLayout = "2006-01-02"
	// The adjustable daily chart never looks back further than this, which
	// keeps the response small no matter how long the diary has been kept.
	// Longer horizons are served by the per-year series instead.
	maxDailySeriesDays = 1096
)

// parseDateParam reads a "YYYY-MM-DD" query parameter as UTC midnight. The
// client passes its own local date so that window boundaries line up with what
// it displays, without needing a tz database on the server.
func parseDateParam(raw string, fallback time.Time) time.Time {
	if parsed, err := time.ParseInLocation(dateLayout, strings.TrimSpace(raw), time.UTC); err == nil {
		return parsed
	}
	return time.Date(fallback.Year(), fallback.Month(), fallback.Day(), 0, 0, 0, 0, time.UTC)
}

// buildDailySeries turns sparse per-entry counts into a dense day-by-day array
// ending today, so the client can slice any range without extra requests.
func buildDailySeries(daily []DayWordCount, today time.Time) (string, []int) {
	start := today.AddDate(0, 0, -(maxDailySeriesDays - 1))
	if len(daily) > 0 {
		if first, err := time.ParseInLocation(dateLayout, daily[0].Date, time.UTC); err == nil && first.After(start) {
			start = first
		}
	} else {
		start = today.AddDate(0, 0, -29)
	}
	if start.After(today) {
		start = today
	}

	length := int(today.Sub(start).Hours()/24) + 1
	series := make([]int, length)
	for _, day := range daily {
		parsed, err := time.ParseInLocation(dateLayout, day.Date, time.UTC)
		if err != nil {
			continue
		}
		offset := int(parsed.Sub(start).Hours() / 24)
		if offset < 0 || offset >= length {
			continue
		}
		series[offset] += day.Words
	}
	return start.Format(dateLayout), series
}

// fillMissingYears makes the yearly chart continuous from the first year on
// record through the current year, so silent years show as zero rather than
// being skipped over.
func fillMissingYears(yearly []YearWordCount, currentYear int) []YearWordCount {
	if len(yearly) == 0 {
		return []YearWordCount{{Year: currentYear}}
	}
	sort.Slice(yearly, func(i, j int) bool { return yearly[i].Year < yearly[j].Year })

	lastYear := yearly[len(yearly)-1].Year
	if currentYear > lastYear {
		lastYear = currentYear
	}
	byYear := make(map[int]YearWordCount, len(yearly))
	for _, item := range yearly {
		byYear[item.Year] = item
	}
	filled := make([]YearWordCount, 0, lastYear-yearly[0].Year+1)
	for year := yearly[0].Year; year <= lastYear; year++ {
		if item, ok := byYear[year]; ok {
			filled = append(filled, item)
			continue
		}
		filled = append(filled, YearWordCount{Year: year})
	}
	return filled
}

// CountWords measures diary length the way a mixed CJK/Latin writer expects:
// every CJK character counts as one, and each run of Latin letters or digits
// counts as a single word. HTML markup is never counted.
func CountWords(content string) int {
	text := stripHTMLToText(content)
	count := 0
	inWord := false
	for _, r := range text {
		switch {
		case isCJK(r):
			count++
			inWord = false
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '\'' || r == '’':
			if !inWord {
				count++
				inWord = true
			}
		default:
			inWord = false
		}
	}
	return count
}

func isCJK(r rune) bool {
	switch {
	case r >= 0x3040 && r <= 0x30FF, // hiragana + katakana
		r >= 0x3400 && r <= 0x4DBF, // CJK extension A
		r >= 0x4E00 && r <= 0x9FFF, // CJK unified ideographs
		r >= 0xAC00 && r <= 0xD7AF, // hangul syllables
		r >= 0xF900 && r <= 0xFAFF, // CJK compatibility ideographs
		r >= 0x20000 && r <= 0x2FA1F: // CJK extension B and beyond
		return true
	}
	return false
}

var rawTextTags = []string{"script", "style"}

// stripHTMLToText replaces every tag with a space (so adjacent blocks do not
// merge into one word) and decodes character references.
func stripHTMLToText(input string) string {
	if input == "" {
		return ""
	}
	var builder strings.Builder
	builder.Grow(len(input))
	for i := 0; i < len(input); {
		if input[i] != '<' {
			builder.WriteByte(input[i])
			i++
			continue
		}
		if next, ok := skipRawTextElement(input, i); ok {
			builder.WriteByte(' ')
			i = next
			continue
		}
		gt := strings.IndexByte(input[i:], '>')
		if gt < 0 {
			break // unterminated tag: nothing countable remains
		}
		builder.WriteByte(' ')
		i += gt + 1
	}
	return html.UnescapeString(builder.String())
}

// skipRawTextElement reports the offset just past a <script>/<style> element
// starting at i, so its body is never mistaken for diary text.
func skipRawTextElement(input string, i int) (int, bool) {
	for _, tag := range rawTextTags {
		if !hasTagPrefix(input, i, tag) {
			continue
		}
		after := i + 1 + len(tag)
		if after < len(input) && !isTagNameBoundary(input[after]) {
			continue
		}
		closeIdx := indexCloseTag(input, after, tag)
		if closeIdx < 0 {
			return len(input), true
		}
		gt := strings.IndexByte(input[closeIdx:], '>')
		if gt < 0 {
			return len(input), true
		}
		return closeIdx + gt + 1, true
	}
	return 0, false
}

// hasTagPrefix reports whether input[i:] opens the given tag, ignoring case.
func hasTagPrefix(input string, i int, tag string) bool {
	if input[i] != '<' || i+1+len(tag) > len(input) {
		return false
	}
	return strings.EqualFold(input[i+1:i+1+len(tag)], tag)
}

// indexCloseTag finds the next "</tag" at or after `from`, ignoring case.
func indexCloseTag(input string, from int, tag string) int {
	needle := "</" + tag
	for i := from; i+len(needle) <= len(input); i++ {
		if input[i] != '<' {
			continue
		}
		if strings.EqualFold(input[i:i+len(needle)], needle) {
			return i
		}
	}
	return -1
}

func isTagNameBoundary(c byte) bool {
	return c == '>' || c == '/' || c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// yearFromDate parses the leading year of a "YYYY-MM-DD" string.
func yearFromDate(date string) (int, bool) {
	if len(date) < 4 {
		return 0, false
	}
	year, err := strconv.Atoi(date[:4])
	if err != nil {
		return 0, false
	}
	return year, true
}
