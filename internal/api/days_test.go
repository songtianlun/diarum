package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
)

func TestDayRoutes(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	other := newTestUser(t, s)
	e := echo.New()
	RegisterDayRoutes(e, s, authMiddlewareFor(user))
	otherEcho := echo.New()
	RegisterDayRoutes(otherEcho, s, authMiddlewareFor(other))

	json := map[string]string{"Content-Type": "application/json"}
	do := func(e *echo.Echo, method, path, body string) (int, map[string]any) {
		t.Helper()
		rec := performRequest(t, e, method, path, strings.NewReader(body), json)
		if rec.Body.Len() == 0 || rec.Code == http.StatusNoContent {
			return rec.Code, nil
		}
		return rec.Code, decodeJSONBody(t, rec)
	}

	for _, body := range []string{
		`{`,
		`{"name":"  ","date":"2026-01-01"}`,
		`{"name":"` + strings.Repeat("x", 101) + `","date":"2026-01-01"}`,
		`{"name":"A","date":"2026-13-01"}`,
		`{"name":"A","date":"2026-01-01","note":"` + strings.Repeat("n", 2001) + `"}`,
		`{"name":"A","date":"2026-01-01","repeat":"day"}`,
		`{"name":"A","date":"2026-01-01","emoji":"` + strings.Repeat("🎉", 9) + `"}`,
	} {
		if code, _ := do(e, http.MethodPost, "/api/v1/days", body); code != http.StatusBadRequest {
			t.Fatalf("POST %s: got %d, want 400", body, code)
		}
	}

	code, created := do(e, http.MethodPost, "/api/v1/days", `{"name":" Birthday ","date":"2026-12-01","emoji":"🎂","note":" cake ","repeat":"year","include_start":true,"show_on_home":true}`)
	if code != http.StatusOK || created["name"] != "Birthday" || created["note"] != "cake" || created["repeat"] != "year" || created["show_on_home"] != true || created["include_start"] != true {
		t.Fatalf("create: %d %v", code, created)
	}
	id := created["id"].(string)
	if code, _ := do(e, http.MethodPost, "/api/v1/days", `{"name":"Earlier","date":"2020-01-01"}`); code != http.StatusOK {
		t.Fatalf("create second: %d", code)
	}

	code, updated := do(e, http.MethodPut, "/api/v1/days/"+id, `{"name":"Birthday!","date":"2026-12-02","show_on_home":false}`)
	if code != http.StatusOK || updated["name"] != "Birthday!" || updated["date"] != "2026-12-02" || updated["show_on_home"] != false || updated["repeat"] != "" {
		t.Fatalf("update: %d %v", code, updated)
	}
	if code, _ := do(e, http.MethodPut, "/api/v1/days/"+id, `{"name":""}`); code != http.StatusBadRequest {
		t.Fatalf("invalid update: %d", code)
	}
	if code, _ := do(otherEcho, http.MethodPut, "/api/v1/days/"+id, `{"name":"Mine","date":"2026-01-01"}`); code != http.StatusNotFound {
		t.Fatalf("foreign update: %d", code)
	}

	// Milestones.
	if code, _ := do(e, http.MethodPost, "/api/v1/days/"+id+"/milestones", `{"name":"","date":"2026-01-01"}`); code != http.StatusBadRequest {
		t.Fatalf("invalid milestone: %d", code)
	}
	if code, _ := do(e, http.MethodPost, "/api/v1/days/"+id+"/milestones", `{`); code != http.StatusBadRequest {
		t.Fatalf("malformed milestone: %d", code)
	}
	if code, _ := do(otherEcho, http.MethodPost, "/api/v1/days/"+id+"/milestones", `{"name":"x","date":"2026-01-01"}`); code != http.StatusNotFound {
		t.Fatalf("foreign milestone: %d", code)
	}
	code, late := do(e, http.MethodPost, "/api/v1/days/"+id+"/milestones", `{"name":"Party","date":"2027-01-05","note":"friends"}`)
	if code != http.StatusOK || late["name"] != "Party" || late["day"] != id {
		t.Fatalf("milestone: %d %v", code, late)
	}
	code, early := do(e, http.MethodPost, "/api/v1/days/"+id+"/milestones", `{"name":"Invite","date":"2026-11-01"}`)
	if code != http.StatusOK {
		t.Fatalf("milestone 2: %d", code)
	}
	code, edited := do(e, http.MethodPut, "/api/v1/days/"+id+"/milestones/"+late["id"].(string), `{"name":"Big party","date":"2027-01-06"}`)
	if code != http.StatusOK || edited["name"] != "Big party" || edited["note"] != "" {
		t.Fatalf("edit milestone: %d %v", code, edited)
	}
	if code, _ := do(e, http.MethodPut, "/api/v1/days/"+id+"/milestones/missing", `{"name":"x","date":"2026-01-01"}`); code != http.StatusNotFound {
		t.Fatalf("missing milestone: %d", code)
	}

	code, detail := do(e, http.MethodGet, "/api/v1/days/"+id, "")
	milestones, _ := detail["milestones"].([]any)
	if code != http.StatusOK || len(milestones) != 2 || milestones[0].(map[string]any)["id"] != early["id"] {
		t.Fatalf("detail: %d %v", code, detail)
	}
	if code, _ := do(otherEcho, http.MethodGet, "/api/v1/days/"+id, ""); code != http.StatusNotFound {
		t.Fatalf("foreign detail: %d", code)
	}

	code, list := do(e, http.MethodGet, "/api/v1/days", "")
	days, _ := list["days"].([]any)
	if code != http.StatusOK || len(days) != 2 || days[0].(map[string]any)["name"] != "Earlier" || days[1].(map[string]any)["milestones"] != float64(2) {
		t.Fatalf("list: %d %v", code, list)
	}
	if _, list := do(otherEcho, http.MethodGet, "/api/v1/days", ""); len(list["days"].([]any)) != 0 {
		t.Fatalf("foreign list: %v", list)
	}

	if code, _ := do(otherEcho, http.MethodDelete, "/api/v1/days/"+id+"/milestones/"+early["id"].(string), ""); code != http.StatusNotFound {
		t.Fatalf("foreign milestone delete: %d", code)
	}
	if code, _ := do(e, http.MethodDelete, "/api/v1/days/"+id+"/milestones/"+early["id"].(string), ""); code != http.StatusNoContent {
		t.Fatalf("milestone delete: %d", code)
	}
	if code, _ := do(otherEcho, http.MethodDelete, "/api/v1/days/"+id, ""); code != http.StatusNotFound {
		t.Fatalf("foreign delete: %d", code)
	}
	if code, _ := do(e, http.MethodDelete, "/api/v1/days/"+id, ""); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	var remaining int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM day_milestones`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("milestones left after delete: %d %v", remaining, err)
	}
	if code, _ := do(e, http.MethodGet, "/api/v1/days/"+id, ""); code != http.StatusNotFound {
		t.Fatalf("deleted detail: %d", code)
	}

	// Database failures surface as 500s.
	_ = s.Close()
	for _, req := range [][3]string{
		{http.MethodGet, "/api/v1/days", ""},
		{http.MethodPost, "/api/v1/days", `{"name":"A","date":"2026-01-01"}`},
		{http.MethodPut, "/api/v1/days/x", `{"name":"A","date":"2026-01-01"}`},
		{http.MethodGet, "/api/v1/days/x", ""},
		{http.MethodDelete, "/api/v1/days/x", ""},
		{http.MethodPost, "/api/v1/days/x/milestones", `{"name":"A","date":"2026-01-01"}`},
		{http.MethodDelete, "/api/v1/days/x/milestones/y", ""},
	} {
		if code, _ := do(e, req[0], req[1], req[2]); code != http.StatusInternalServerError {
			t.Fatalf("%s %s on closed store: %d", req[0], req[1], code)
		}
	}
}
