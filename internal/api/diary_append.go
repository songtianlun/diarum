package api

import (
	"html"
	"regexp"
	"strings"
)

var (
	// blankLineSplitRegexp splits plain text into paragraphs on blank lines,
	// tolerating spaces/tabs on otherwise empty lines.
	blankLineSplitRegexp = regexp.MustCompile(`\n[ \t]*\n+`)

	// trailingEmptyParagraphRegexp matches a single empty TipTap paragraph at
	// the end of HTML content (whitespace, &nbsp;/&#160;/&#xa0;, zero-width
	// spaces, and <br> tags only).
	trailingEmptyParagraphRegexp = regexp.MustCompile(`(?is)<p(?:\s[^>]*)?>(?:\s|&nbsp;|&#160;|&#x0*a0;|\x{00a0}|\x{200b}|<br(?:\s[^>]*)?/?>)*</p>\s*$`)

	// htmlCommentRegexp removes HTML comments when testing whether a diary has
	// any real content left after trimming empty paragraphs.
	htmlCommentRegexp = regexp.MustCompile(`(?s)<!--.*?-->`)

	// trailingDividerRegexp matches an <hr> at the end of trimmed content,
	// allowing trailing HTML comments (such as a Memos END marker) and spaces.
	trailingDividerRegexp = regexp.MustCompile(`(?is)<hr(?:\s[^>]*)?/?>\s*(?:<!--.*?-->\s*)*$`)
)

// appendQuickNoteHTML appends a plain-text quick note (with optional HH:MM:SS
// timestamp) to the bottom of an existing diary's HTML content.
//
// Smart divider rules:
//   - Trailing empty paragraphs and whitespace are cleared from existing content.
//   - If existing content is empty, no leading <hr> is inserted.
//   - If existing content already ends with <hr> after clearing trailing empty
//     space, no duplicate leading <hr> is inserted.
//   - Otherwise a leading <hr> separates existing content from the new note.
//   - The result always ends with a trailing <hr> followed by an empty
//     paragraph (<p></p>) so the editor has a ready blank line beneath it.
func appendQuickNoteHTML(existing, noteText, timestamp string) string {
	noteBlock := renderQuickNoteHTML(noteText, timestamp)
	trimmed := trimTrailingEmptyDiaryHTML(existing)

	if isEffectivelyEmptyDiaryHTML(trimmed) {
		return noteBlock + "\n<hr>\n<p></p>"
	}
	if trailingDividerRegexp.MatchString(trimmed) {
		return trimmed + "\n" + noteBlock + "\n<hr>\n<p></p>"
	}
	return trimmed + "\n<hr>\n" + noteBlock + "\n<hr>\n<p></p>"
}

// renderQuickNoteHTML renders plain text (and optional timestamp) into TipTap-
// compatible HTML paragraphs.
func renderQuickNoteHTML(noteText, timestamp string) string {
	normalized := strings.ReplaceAll(noteText, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	normalized = strings.TrimSpace(normalized)

	parts := make([]string, 0, 4)
	if ts := strings.TrimSpace(timestamp); ts != "" {
		parts = append(parts, "<p><code>"+html.EscapeString(ts)+"</code></p>")
	}

	rawParagraphs := blankLineSplitRegexp.Split(normalized, -1)
	for _, raw := range rawParagraphs {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		escaped := html.EscapeString(trimmed)
		escaped = strings.ReplaceAll(escaped, "\n", "<br>")
		parts = append(parts, "<p>"+escaped+"</p>")
	}

	return strings.Join(parts, "\n")
}

// trimTrailingEmptyDiaryHTML removes trailing whitespace and trailing empty
// paragraphs (including <p></p>, <p><br></p>, <p>&nbsp;</p>) without stripping
// meaningful nodes or HTML comments.
func trimTrailingEmptyDiaryHTML(content string) string {
	out := strings.TrimRight(content, " \t\r\n")
	for {
		loc := trailingEmptyParagraphRegexp.FindStringIndex(out)
		if loc == nil {
			break
		}
		out = strings.TrimRight(out[:loc[0]], " \t\r\n")
	}
	return out
}

func isEffectivelyEmptyDiaryHTML(trimmed string) bool {
	if trimmed == "" {
		return true
	}
	withoutComments := htmlCommentRegexp.ReplaceAllString(trimmed, "")
	return strings.TrimSpace(withoutComments) == ""
}
