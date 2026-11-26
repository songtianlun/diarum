package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/labstack/echo/v5"
)

func TestAppendQuickNoteHTML(t *testing.T) {
	tests := []struct {
		name      string
		existing  string
		note      string
		timestamp string
		want      string
	}{
		{
			name:      "empty diary skips leading divider and ends with divider plus newline",
			existing:  "",
			note:      "今天天气很好",
			timestamp: "09:15:30",
			want:      "<p><code>09:15:30</code></p>\n<p>今天天气很好</p>\n<hr>\n<p></p>",
		},
		{
			name:      "only empty paragraphs treated as empty diary",
			existing:  "<p></p><p>  &nbsp; </p><p><br class=\"ProseMirror-trailingBreak\"></p>\n",
			note:      "第一条记录",
			timestamp: "10:00:00",
			want:      "<p><code>10:00:00</code></p>\n<p>第一条记录</p>\n<hr>\n<p></p>",
		},
		{
			name:      "existing text gets leading and trailing dividers",
			existing:  "<p>早上写的内容</p>",
			note:      "下午的想法",
			timestamp: "14:22:05",
			want:      "<p>早上写的内容</p>\n<hr>\n<p><code>14:22:05</code></p>\n<p>下午的想法</p>\n<hr>\n<p></p>",
		},
		{
			name:      "existing text with trailing empty paragraphs strips blanks before divider",
			existing:  "<p>早上写的内容</p><p></p><p><br></p><p>&nbsp;</p>   ",
			note:      "午休后",
			timestamp: "13:30:00",
			want:      "<p>早上写的内容</p>\n<hr>\n<p><code>13:30:00</code></p>\n<p>午休后</p>\n<hr>\n<p></p>",
		},
		{
			name:      "existing trailing divider plus trailing empty paragraph avoids duplicate divider",
			existing:  "<p>早上写的内容</p>\n<hr>\n<p><code>14:22:05</code></p>\n<p>下午的想法</p>\n<hr>\n<p></p>",
			note:      "晚上散步",
			timestamp: "21:05:18",
			want:      "<p>早上写的内容</p>\n<hr>\n<p><code>14:22:05</code></p>\n<p>下午的想法</p>\n<hr>\n<p><code>21:05:18</code></p>\n<p>晚上散步</p>\n<hr>\n<p></p>",
		},
		{
			name:      "existing trailing self-closing hr with nbsp paragraph avoids duplicate divider",
			existing:  "<p>记录</p><hr /><p> \u00a0 <br/></p>",
			note:      "继续写",
			timestamp: "22:00:00",
			want:      "<p>记录</p><hr />\n<p><code>22:00:00</code></p>\n<p>继续写</p>\n<hr>\n<p></p>",
		},
		{
			name:      "existing memos block ending with hr and end comment avoids duplicate divider",
			existing:  "<!-- DIARUM:MEMOS:BEGIN id=\"1\" date=\"2026-11-24\" -->\n<hr>\n<p>memo</p>\n<hr>\n<!-- DIARUM:MEMOS:END id=\"1\" -->\n<p></p>",
			note:      "随手记",
			timestamp: "23:10:00",
			want:      "<!-- DIARUM:MEMOS:BEGIN id=\"1\" date=\"2026-11-24\" -->\n<hr>\n<p>memo</p>\n<hr>\n<!-- DIARUM:MEMOS:END id=\"1\" -->\n<p><code>23:10:00</code></p>\n<p>随手记</p>\n<hr>\n<p></p>",
		},
		{
			name:      "comment only content treated as empty",
			existing:  "<!-- empty -->   ",
			note:      "hello",
			timestamp: "",
			want:      "<p>hello</p>\n<hr>\n<p></p>",
		},
		{
			name:      "multiline paragraphs and HTML escaping without timestamp",
			existing:  "<p>top</p>",
			note:      "  line 1\r\nline 2 & <script>\r\n\r\n  \r\npara 2  ",
			timestamp: "",
			want:      "<p>top</p>\n<hr>\n<p>line 1<br>line 2 &amp; &lt;script&gt;</p>\n<p>para 2</p>\n<hr>\n<p></p>",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := appendQuickNoteHTML(tc.existing, tc.note, tc.timestamp)
			if got != tc.want {
				t.Fatalf("appendQuickNoteHTML() mismatch:\ngot:  %q\nwant: %q", got, tc.want)
			}
		})
	}
}

func TestDiaryAppendEndpoint(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	changedCount := 0
	e := echo.New()
	RegisterDiaryRoutes(e, s, authMiddlewareFor(user), func(userID string) {
		if userID == user.ID {
			changedCount++
		}
	})

	postAppend := func(payload string) *http.Response {
		rec := performRequest(t, e, http.MethodPost, "/api/v1/diaries/append", strings.NewReader(payload), map[string]string{
			"Content-Type": "application/json",
		})
		return rec.Result()
	}

	// Validation error paths.
	if resp := postAppend("{bad-json"); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid json status = %d, want 400", resp.StatusCode)
	}
	if resp := postAppend(`{"date":"","content":"hi"}`); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing date status = %d, want 400", resp.StatusCode)
	}
	if resp := postAppend(`{"date":"not-a-date","content":"hi"}`); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid date status = %d, want 400", resp.StatusCode)
	}
	if resp := postAppend(`{"date":"2026-11-24","content":"   \n  "}`); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("blank content status = %d, want 400", resp.StatusCode)
	}

	// 1. Append to a brand-new day.
	rec1 := performRequest(t, e, http.MethodPost, "/api/v1/diaries/append", bytes.NewBufferString(`{"date":"2026-11-24","content":"第一条随手记","timestamp":"09:10:11"}`), map[string]string{
		"Content-Type": "application/json",
	})
	if rec1.Code != http.StatusOK {
		t.Fatalf("first append status = %d, body = %s", rec1.Code, rec1.Body.String())
	}
	body1 := decodeJSONBody(t, rec1)
	want1 := "<p><code>09:10:11</code></p>\n<p>第一条随手记</p>\n<hr>\n<p></p>"
	if body1["content"] != want1 {
		t.Fatalf("first content = %q, want %q", body1["content"], want1)
	}

	// Set mood & weather on the entry, then append again to verify they are preserved
	// and the smart divider avoids inserting a duplicate <hr>.
	if _, _, err := s.UpsertDiary(user.ID, "2026-11-24", want1, "😊", "☀️"); err != nil {
		t.Fatalf("UpsertDiary mood/weather: %v", err)
	}

	rec2 := performRequest(t, e, http.MethodPost, "/api/v1/diaries/append", bytes.NewBufferString(`{"date":"2026-11-24","content":"第二条随手记","timestamp":"18:20:30"}`), map[string]string{
		"Content-Type": "application/json",
	})
	if rec2.Code != http.StatusOK {
		t.Fatalf("second append status = %d, body = %s", rec2.Code, rec2.Body.String())
	}
	body2 := decodeJSONBody(t, rec2)
	want2 := "<p><code>09:10:11</code></p>\n<p>第一条随手记</p>\n<hr>\n<p><code>18:20:30</code></p>\n<p>第二条随手记</p>\n<hr>\n<p></p>"
	if body2["content"] != want2 {
		t.Fatalf("second content = %q, want %q", body2["content"], want2)
	}
	if body2["mood"] != "😊" || body2["weather"] != "☀️" {
		t.Fatalf("mood/weather not preserved: mood=%v weather=%v", body2["mood"], body2["weather"])
	}
	if changedCount != 2 {
		t.Fatalf("onDiaryChanged count = %d, want 2", changedCount)
	}

	// Verify history snapshot was archived on update.
	revs, err := s.ListDiaryRevisions(user.ID, "2026-11-24")
	if err != nil {
		t.Fatalf("ListDiaryRevisions: %v", err)
	}
	if len(revs) == 0 {
		t.Fatalf("expected archived diary revision after append")
	}

	// 3. Concurrent appends should never lose any note.
	var wg sync.WaitGroup
	notes := []string{"并发A", "并发B", "并发C"}
	for _, note := range notes {
		wg.Add(1)
		go func(text string) {
			defer wg.Done()
			payload, _ := json.Marshal(map[string]string{
				"date":      "2026-11-25",
				"content":   text,
				"timestamp": "12:00:00",
			})
			rec := performRequest(t, e, http.MethodPost, "/api/v1/diaries/append", bytes.NewReader(payload), map[string]string{
				"Content-Type": "application/json",
			})
			if rec.Code != http.StatusOK {
				t.Errorf("concurrent append %s failed: %d %s", text, rec.Code, rec.Body.String())
			}
		}(note)
	}
	wg.Wait()

	diary, err := s.GetDiaryByDate(user.ID, "2026-11-25 00:00:00.000Z", "2026-11-25 23:59:59.999Z")
	if err != nil {
		t.Fatalf("GetDiaryByDate 2026-11-25: %v", err)
	}
	for _, note := range notes {
		if !strings.Contains(diary.Content, note) {
			t.Fatalf("concurrent diary missing %q; content=%q", note, diary.Content)
		}
	}
	if strings.Contains(diary.Content, "<hr>\n<hr>") {
		t.Fatalf("unexpected consecutive <hr> in %q", diary.Content)
	}
	if !strings.HasSuffix(diary.Content, "<hr>\n<p></p>") {
		t.Fatalf("diary should end with <hr>\\n<p></p>, got %q", diary.Content)
	}
}

func TestDiaryAppendStoreError(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	RegisterDiaryRoutes(e, s, authMiddlewareFor(user), nil)

	// Closing the DB forces AppendDiary to fail so the 500 path is exercised.
	_ = s.Close()
	rec := performRequest(t, e, http.MethodPost, "/api/v1/diaries/append", bytes.NewBufferString(`{"date":"2026-11-24","content":"hello","timestamp":"10:00:00"}`), map[string]string{
		"Content-Type": "application/json",
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when store closed, got %d", rec.Code)
	}
}
