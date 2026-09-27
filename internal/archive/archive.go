// Package archive reads and writes Diarum export archives: a ZIP holding a
// diarum_export.json manifest, a Markdown copy of every diary and the media
// files. The manual export/import endpoints and the automatic backups share
// it.
//
// Both directions stream. Exports read the database in short keyset batches
// and copy media straight into the ZIP; imports decode the manifest element
// by element and read media lazily from the ZIP. Memory use therefore stays
// flat no matter how many diaries or how much media a user has.
package archive

import (
	"errors"
	"strings"
	"time"
)

const (
	// FormatVersion is written to every manifest.
	FormatVersion = 1
	// ManifestName is the manifest's path inside the archive.
	ManifestName = "diarum_export.json"
	// MaxMediaFileSize caps a single media file accepted on import.
	MaxMediaFileSize = 100 << 20

	dateLayout = "2006-01-02"
	batchSize  = 500
)

var (
	// ErrMissingManifest reports an archive without diarum_export.json.
	ErrMissingManifest = errors.New("ZIP missing " + ManifestName)
	// ErrInvalidManifest reports a manifest that is not valid export JSON.
	ErrInvalidManifest = errors.New("failed to parse " + ManifestName)
)

// Data is the manifest layout. Exports stream it rather than build it; the
// type documents the format and is handy for building archives in tests.
type Data struct {
	Version       int            `json:"version"`
	ExportedAt    string         `json:"exported_at"`
	Diaries       []Diary        `json:"diaries"`
	Media         []Media        `json:"media"`
	Conversations []Conversation `json:"conversations"`
}

type Diary struct {
	ID      string `json:"id"`
	Date    string `json:"date"`
	Content string `json:"content"`
	Mood    string `json:"mood,omitempty"`
	Weather string `json:"weather,omitempty"`
}

type Media struct {
	ID   string `json:"id"`
	File string `json:"file"`
	// Path is the file's entry in the archive. Archives written before it
	// existed keep files at media/<file>, where equal names collide.
	Path  string   `json:"path,omitempty"`
	Name  string   `json:"name,omitempty"`
	Alt   string   `json:"alt,omitempty"`
	Diary []string `json:"diary,omitempty"`
	Owner string   `json:"-"`
}

type Conversation struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Messages []Message `json:"messages"`
}

type Message struct {
	ID                string   `json:"id"`
	Role              string   `json:"role"`
	Content           string   `json:"content"`
	ReferencedDiaries []string `json:"referenced_diaries,omitempty"`
}

type ExportStats struct {
	DateRangeType string       `json:"date_range_type"`
	StartDate     string       `json:"start_date"`
	EndDate       string       `json:"end_date"`
	Diaries       CountDetail  `json:"diaries"`
	Media         CountDetail  `json:"media"`
	Conversations CountDetail  `json:"conversations"`
	Messages      int          `json:"messages"`
	FailedItems   []FailedItem `json:"failed_items,omitempty"`
}

type CountDetail struct {
	TotalInSystem  int `json:"total_in_system"`
	ShouldExport   int `json:"should_export"`
	ActualExported int `json:"actual_exported"`
}

type FailedItem struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type ImportStats struct {
	Diaries       ImportCounters `json:"diaries"`
	Media         ImportCounters `json:"media"`
	Conversations ImportCounters `json:"conversations"`
}

type ImportCounters struct {
	Total    int `json:"total"`
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
	Failed   int `json:"failed"`
}

// ValidPath reports whether an archive entry name is safe to use.
func ValidPath(name string) bool {
	return !strings.Contains(name, "..") && !strings.HasPrefix(name, "/") && !strings.HasPrefix(name, "\\")
}

// InRange reports whether a YYYY-MM-DD date lies within [start, end].
func InRange(date string, start, end time.Time) bool {
	if date == "" {
		return false
	}
	day, err := time.Parse(dateLayout, date)
	if err != nil {
		return false
	}
	return !day.Before(start) && !day.After(end)
}

// Markdown renders a diary as the Markdown file stored next to the manifest.
func Markdown(d Diary) string {
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
