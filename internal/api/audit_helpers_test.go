package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/store"
)

func TestRecordDiarySaveBranches(t *testing.T) {
	e := echo.New()
	c := e.NewContext(httptest.NewRequest(http.MethodPost, "/", nil), httptest.NewRecorder())
	after := &store.Diary{ID: "d", Date: "2026-01-01 00:00:00.000Z", Content: "<p>new words</p>", Mood: "😊", Weather: "☀️"}
	// Without an audit trail on the request these only build the detail;
	// every branch must still run without panicking.
	recordDiarySave(c, "u", "user", "web", nil, nil)
	recordDiarySave(c, "u", "user", "web", nil, after)
	recordDiarySave(c, "u", "user", "web", &store.Diary{Content: "<p>old</p>", Mood: "😢", Weather: "🌧"}, after)
	recordDiarySave(c, "u", "user", "web", after, after)
	if detail := diaryAuditDetail(nil); len(detail) != 0 {
		t.Fatalf("nil diary detail: %v", detail)
	}
}

func TestSettingAuditDetailValues(t *testing.T) {
	cases := []struct {
		key   string
		value any
		keep  bool
	}{
		{"api.token", "secret", false},
		{"ui.flag", true, true},
		{"ui.count", float64(3), true},
		{"ui.name", "short", true},
		{"ui.long", strings.Repeat("x", 65), false},
		{"ui.list", []any{1}, false},
	}
	for _, tc := range cases {
		got := settingAuditDetail(tc.key, tc.value)
		if (got != nil) != tc.keep {
			t.Fatalf("%s: got %v, keep=%v", tc.key, got, tc.keep)
		}
	}
}
