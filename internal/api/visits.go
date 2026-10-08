package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/audit"
	"github.com/songtianlun/diarum/internal/auth"
	"github.com/songtianlun/diarum/internal/backup"
	"github.com/songtianlun/diarum/internal/store"
	"github.com/songtianlun/diarum/internal/visits"
)

const (
	visitContextKey = "diarum_visits"
	// DeviceHeader carries the web app's per-browser device ID.
	DeviceHeader = "X-Diarum-Device"
)

// Routes whose reads are recorded. failures marks those where a refused
// attempt is recorded too (the others need the request body to know what
// was asked for).
var visitRoutes = map[string]struct {
	kind     string
	failures bool
}{
	"/api/v1/diaries/by-date/:date": {visits.KindView, true},
	"/api/v1/diaries/:id":           {visits.KindView, true},
	"/api/v1/diaries/revisions/:id": {visits.KindRevision, true},
	"/api/v1/diaries/by-ids":        {visits.KindView, false},
	"/api/v1/diaries":               {visits.KindView, true},
	"/api/v1/mcp":                   {visits.KindView, false},
}

// visitRequest collects the reads a handler served.
type visitRequest struct {
	visits []visits.Visit
}

// VisitMiddleware records diary reads at the API layer: the reads handlers
// report through trackVisit, and refused attempts at the read routes (signed
// out, someone else's entry, an invalid token), which never reach a handler
// able to report them. A nil or disabled tracker records nothing.
func VisitMiddleware(t *visits.Tracker, s *store.Store) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			route, tracked := visitRoutes[c.Path()]
			if !tracked || !t.Enabled() {
				return next(c)
			}
			vr := &visitRequest{}
			c.Set(visitContextKey, vr)
			err := next(c)
			status, _ := responseStatus(c, err)

			req := c.Request()
			base := visits.Visit{
				Time:   time.Now(),
				Route:  c.Path(),
				Status: status,
				IP:     c.RealIP(),
				UA:     req.UserAgent(),
				Device: visits.CleanDeviceID(req.Header.Get(DeviceHeader)),
			}
			if len(vr.visits) == 0 {
				if status >= 400 && route.failures {
					t.Record(failedVisit(c, s, base, route.kind))
				}
				return err
			}
			for _, v := range vr.visits {
				v.Time, v.Route, v.Status, v.IP, v.UA, v.Device = base.Time, base.Route, base.Status, base.IP, base.UA, base.Device
				t.Record(v)
			}
			return err
		}
	}
}

// responseStatus is the status a handler's result turns into.
func responseStatus(c echo.Context, err error) (int, string) {
	status := c.Response().Status
	message := ""
	if err != nil {
		var httpErr *echo.HTTPError
		if errors.As(err, &httpErr) {
			status = httpErr.Code
			message = fmt.Sprint(httpErr.Message)
		} else {
			status = http.StatusInternalServerError
			message = err.Error()
		}
	}
	if status == 0 {
		status = http.StatusOK
	}
	return status, message
}

// failedVisit describes a refused read as precisely as the request allows.
func failedVisit(c echo.Context, s *store.Store, v visits.Visit, kind string) visits.Visit {
	v.Kind = kind
	v.Source = visits.SourceWeb
	if user := auth.CurrentUser(c); user != nil {
		v.VisitorID, v.Visitor = user.ID, user.Username
	}
	switch c.Path() {
	case "/api/v1/diaries/:id":
		id := c.PathParam("id")
		v.DiaryID = id
		// Tie the attempt to the entry's owner, so they see who tried.
		if diary, err := s.GetDiaryByID(id); err == nil {
			v.OwnerID, v.DiaryDate = diary.Owner, store.DateOnly(diary.Date)
		}
	case "/api/v1/diaries/by-date/:date":
		v.DiaryDate = c.PathParam("date")
		v.OwnerID = v.VisitorID
	case "/api/v1/diaries/revisions/:id":
		v.OwnerID = v.VisitorID
	case "/api/v1/diaries":
		v.Source = visits.SourceAPI
		if date := c.QueryParam("date"); date != "" {
			v.DiaryDate = date
		} else if start, end := c.QueryParam("start"), c.QueryParam("end"); start != "" || end != "" {
			v.Kind, v.DiaryDate = visits.KindRange, start+".."+end
		}
	}
	return v
}

// trackVisit reports that the request read a diary entry. visitorID is the
// account reading (the owner itself for API tokens and MCP).
func trackVisit(c echo.Context, diary *store.Diary, kind, source, visitorID, visitor string) {
	if diary == nil {
		return
	}
	trackVisitDate(c, diary.Owner, diary.ID, store.DateOnly(diary.Date), kind, source, visitorID, visitor)
}

func trackVisitDate(c echo.Context, owner, diaryID, date, kind, source, visitorID, visitor string) {
	vr, _ := c.Get(visitContextKey).(*visitRequest)
	if vr == nil || len(vr.visits) >= visits.MaxPerRequest {
		return
	}
	vr.visits = append(vr.visits, visits.Visit{OwnerID: owner, DiaryID: diaryID, DiaryDate: date, Kind: kind, Source: source, VisitorID: visitorID, Visitor: visitor})
}

// trackMCPVisits reports the entries an MCP tool returned, from what the
// tool told the audit trail.
func trackMCPVisits(c echo.Context, userID, target string, detail map[string]any) {
	if dates, ok := detail["dates"].([]string); ok {
		for _, date := range dates {
			trackVisitDate(c, userID, "", date, visits.KindRange, visits.SourceMCP, userID, "")
		}
		return
	}
	id, _ := detail["id"].(string)
	trackVisitDate(c, userID, id, target, visits.KindView, visits.SourceMCP, userID, "")
}

// trackWebVisit reports a read by the signed-in user.
func trackWebVisit(c echo.Context, diary *store.Diary, kind string) {
	user := auth.CurrentUser(c)
	if user == nil {
		return
	}
	trackVisit(c, diary, kind, visits.SourceWeb, user.ID, user.Username)
}

// ----- Shared query parsing -----

func parseVisitFilter(c echo.Context) (visits.Filter, error) {
	f := visits.Filter{
		DiaryDate: strings.TrimSpace(c.QueryParam("diary")),
		VisitorID: strings.TrimSpace(c.QueryParam("visitor")),
		Device:    strings.TrimSpace(c.QueryParam("device")),
		Result:    c.QueryParam("result"),
		Source:    strings.TrimSpace(c.QueryParam("source")),
		Others:    isTrue(c.QueryParam("others")),
		Text:      truncate(c.QueryParam("q"), 100),
	}
	switch f.Result {
	case "", "ok", "failed":
	default:
		return f, errors.New("invalid result")
	}
	if len(f.DiaryDate) > 32 || len(f.VisitorID) > 64 || len(f.Device) > 64 || len(f.Source) > 16 {
		return f, errors.New("invalid filter")
	}
	var err error
	if f.Start, err = parseVisitTime(c.QueryParam("start"), false); err != nil {
		return f, errors.New("invalid start")
	}
	if f.End, err = parseVisitTime(c.QueryParam("end"), true); err != nil {
		return f, errors.New("invalid end")
	}
	if !f.Start.IsZero() && !f.End.IsZero() && f.Start.After(f.End) {
		f.Start, f.End = f.End, f.Start
	}
	return f, nil
}

// parseVisitTime accepts RFC 3339 or a plain date (start or end of that UTC
// day).
func parseVisitTime(raw string, endOfDay bool) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	t, err := time.Parse(dateLayout, raw)
	if err != nil {
		return time.Time{}, err
	}
	if endOfDay {
		t = t.Add(24*time.Hour - time.Millisecond)
	}
	return t, nil
}

func isTrue(raw string) bool {
	return raw == "1" || raw == "true"
}

func intParam(c echo.Context, name string, fallback int) (int, error) {
	raw := c.QueryParam(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return value, nil
}

type visitHandlers struct {
	tracker *visits.Tracker
	store   *store.Store
	// filter scopes a request to the owners it may see.
	filter func(c echo.Context) (visits.Filter, error)
}

func (h *visitHandlers) summary(c echo.Context) error {
	f, err := h.filter(c)
	if err != nil {
		return badRequest(err.Error(), nil)
	}
	days, err := intParam(c, "days", 30)
	if err != nil {
		return badRequest(err.Error(), nil)
	}
	offset, _ := strconv.Atoi(c.QueryParam("tz_offset"))
	h.tracker.Flush()
	summary, err := h.tracker.Summarize(f, days, offset)
	if err != nil {
		return serverError("Failed to load visitor statistics", err)
	}
	names := h.names()
	for i := range summary.TopVisitors {
		fillVisitorName(&summary.TopVisitors[i], names)
	}
	fillVisitNames(summary.RecentFails, names)
	return c.JSON(http.StatusOK, summary)
}

func (h *visitHandlers) diaries(c echo.Context) error {
	f, err := h.filter(c)
	if err != nil {
		return badRequest(err.Error(), nil)
	}
	limit, err := intParam(c, "limit", 50)
	if err != nil {
		return badRequest(err.Error(), nil)
	}
	offset, err := intParam(c, "offset", 0)
	if err != nil {
		return badRequest(err.Error(), nil)
	}
	rows, total, err := h.tracker.Diaries(f, c.QueryParam("sort"), limit, offset)
	if err != nil {
		return serverError("Failed to load visitor statistics", err)
	}
	return c.JSON(http.StatusOK, map[string]any{"diaries": rows, "total": total})
}

func (h *visitHandlers) diary(c echo.Context) error {
	f, err := h.filter(c)
	if err != nil {
		return badRequest(err.Error(), nil)
	}
	f.DiaryDate = c.PathParam("date")
	if len(f.DiaryDate) > 32 {
		return badRequest("invalid date", nil)
	}
	stats, _, err := h.tracker.Diaries(f, "views", 1, 0)
	if err != nil {
		return serverError("Failed to load visitor statistics", err)
	}
	visitors, total, err := h.tracker.Visitors(f, 100, 0)
	if err != nil {
		return serverError("Failed to load visitor statistics", err)
	}
	names := h.names()
	for i := range visitors {
		fillVisitorName(&visitors[i], names)
	}
	var stat *visits.DiaryStat
	if len(stats) > 0 {
		stat = &stats[0]
	}
	return c.JSON(http.StatusOK, map[string]any{"date": f.DiaryDate, "stats": stat, "visitors": visitors, "visitors_total": total})
}

func (h *visitHandlers) visitors(c echo.Context) error {
	f, err := h.filter(c)
	if err != nil {
		return badRequest(err.Error(), nil)
	}
	limit, err := intParam(c, "limit", 50)
	if err != nil {
		return badRequest(err.Error(), nil)
	}
	offset, err := intParam(c, "offset", 0)
	if err != nil {
		return badRequest(err.Error(), nil)
	}
	rows, total, err := h.tracker.Visitors(f, limit, offset)
	if err != nil {
		return serverError("Failed to load visitor statistics", err)
	}
	names := h.names()
	for i := range rows {
		fillVisitorName(&rows[i], names)
	}
	return c.JSON(http.StatusOK, map[string]any{"visitors": rows, "total": total})
}

func (h *visitHandlers) logs(c echo.Context) error {
	f, err := h.filter(c)
	if err != nil {
		return badRequest(err.Error(), nil)
	}
	limit, err := intParam(c, "limit", 50)
	if err != nil {
		return badRequest(err.Error(), nil)
	}
	offset, err := intParam(c, "offset", 0)
	if err != nil {
		return badRequest(err.Error(), nil)
	}
	h.tracker.Flush()
	rows, total, err := h.tracker.Logs(f, limit, offset, true)
	if err != nil {
		return serverError("Failed to load visit logs", err)
	}
	fillVisitNames(rows, h.names())
	return c.JSON(http.StatusOK, map[string]any{"visits": rows, "total": total})
}

func (h *visitHandlers) names() map[string]string {
	names, err := h.store.UserNames()
	if err != nil {
		return map[string]string{}
	}
	return names
}

func fillVisitorName(v *visits.VisitorStat, names map[string]string) {
	if v.VisitorID != "" {
		if name := names[v.VisitorID]; name != "" {
			v.Visitor = name
		}
	}
}

func fillVisitNames(rows []visits.Visit, names map[string]string) {
	for i := range rows {
		if rows[i].VisitorID != "" {
			if name := names[rows[i].VisitorID]; name != "" {
				rows[i].Visitor = name
			}
		}
	}
}

// ----- User routes -----

// RegisterVisitRoutes serves each user the visit statistics of their own
// diaries, while an administrator has visit tracking switched on.
func RegisterVisitRoutes(e *echo.Echo, s *store.Store, authMiddleware echo.MiddlewareFunc, t *visits.Tracker) {
	h := &visitHandlers{tracker: t, store: s, filter: func(c echo.Context) (visits.Filter, error) {
		f, err := parseVisitFilter(c)
		f.Owner = auth.CurrentUser(c).ID
		return f, err
	}}
	group := e.Group("/api/v1/visits", authMiddleware)
	group.GET("/status", func(c echo.Context) error {
		settings := t.Settings()
		return c.JSON(http.StatusOK, map[string]any{
			"enabled":        t.Enabled(),
			"retention_days": settings.RetentionDays,
			"dedupe_seconds": settings.DedupeSeconds,
		})
	})
	enabled := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !t.Enabled() {
				return forbidden("Visitor statistics are turned off by the administrator")
			}
			return next(c)
		}
	}
	group.GET("/summary", h.summary, enabled)
	group.GET("/diaries", h.diaries, enabled)
	group.GET("/diaries/:date", h.diary, enabled)
	group.GET("/visitors", h.visitors, enabled)
	group.GET("/logs", h.logs, enabled)
}

// ----- Admin routes -----

const visitPullTimeout = 30 * time.Minute

type adminVisitRoutes struct {
	tracker *visits.Tracker
	store   *store.Store
}

// RegisterAdminVisitRoutes serves the admin console's visitor statistics:
// settings, every owner's statistics and logs, and the S3 archive.
func RegisterAdminVisitRoutes(e *echo.Echo, s *store.Store, authMiddleware echo.MiddlewareFunc, t *visits.Tracker) {
	r := &adminVisitRoutes{tracker: t, store: s}
	h := &visitHandlers{tracker: t, store: s, filter: adminVisitFilter}
	available := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if t == nil {
				return serverError("Visit tracking is unavailable on this server", nil)
			}
			return next(c)
		}
	}
	group := e.Group("/api/v1/admin/visits", authMiddleware, RequireAdmin, available)
	group.GET("/settings", r.settings)
	group.PUT("/settings", r.putSettings)
	group.POST("/settings/test", r.test)
	group.GET("/overview", r.overview)
	group.GET("/owners", r.owners)
	group.GET("/summary", h.summary)
	group.GET("/diaries", h.diaries)
	group.GET("/diaries/:date", h.diary)
	group.GET("/visitors", h.visitors)
	group.GET("/logs", h.logs)
	group.GET("/archives", r.archives)
	group.POST("/archives/run", r.archiveNow)
	group.POST("/cleanup", r.cleanup)
	group.POST("/archives/pull", r.pull)
	group.DELETE("/pulled", r.unload)
}

// adminVisitFilter reads the owner to look at: a user ID, "-" for attempts
// that could not be tied to an owner, or "*" for everyone.
func adminVisitFilter(c echo.Context) (visits.Filter, error) {
	f, err := parseVisitFilter(c)
	if err != nil {
		return f, err
	}
	switch owner := strings.TrimSpace(c.QueryParam("owner")); owner {
	case "":
		return f, errors.New("owner is required")
	case "*":
		f.AllOwners = true
	case visits.UnattributedOwner:
		f.Owner = ""
	default:
		if len(owner) > 64 {
			return f, errors.New("invalid owner")
		}
		f.Owner = owner
	}
	return f, nil
}

type visitSettingsResponse struct {
	visits.Settings
	SecretSet      bool           `json:"secret_set"`
	SharedS3       bool           `json:"shared_s3_available"`
	SharedBucket   string         `json:"shared_s3_bucket"`
	State          visits.State   `json:"state"`
	ServerTimezone string         `json:"server_timezone"`
	Limits         map[string]int `json:"limits"`
}

func (r *adminVisitRoutes) settingsResponse() visitSettingsResponse {
	settings := r.tracker.Settings()
	resp := visitSettingsResponse{
		Settings:       settings,
		SecretSet:      settings.Archive.S3.Secret != "",
		SharedS3:       r.tracker.SharedS3Available(),
		SharedBucket:   r.tracker.SharedS3Bucket(),
		State:          r.tracker.State(),
		ServerTimezone: time.Local.String(),
		Limits: map[string]int{
			"retention_max":         visits.MaxRetentionDays,
			"retention_default":     visits.DefaultRetentionDays,
			"archive_retention_max": visits.MaxArchiveRetentionDays,
			"archive_retention_def": visits.DefaultArchiveRetentionDays,
			"dedupe_max":            visits.MaxDedupeSeconds,
			"dedupe_default":        visits.DefaultDedupeSeconds,
			"max_records_min":       visits.MinMaxRecords,
			"max_records_max":       visits.MaxMaxRecords,
			"max_records_default":   visits.DefaultMaxRecords,
			"rate_max":              visits.MaxRateLimit,
			"pulled_keep_days":      visits.PulledKeepDays,
		},
	}
	// The secret never leaves the server.
	resp.Archive.S3.Secret = ""
	return resp
}

func (r *adminVisitRoutes) settings(c echo.Context) error {
	return c.JSON(http.StatusOK, r.settingsResponse())
}

func (r *adminVisitRoutes) putSettings(c echo.Context) error {
	var body visits.Settings
	if err := c.Bind(&body); err != nil {
		return badRequest("Invalid request body", err)
	}
	previous := r.tracker.Settings()
	next, err := r.tracker.UpdateSettings(body)
	if err != nil {
		return badRequest(err.Error(), nil)
	}
	recordAudit(c, audit.ActionAdminVisits, "", map[string]any{
		"enabled":                [2]bool{previous.Enabled, next.Enabled},
		"retention_days":         [2]int{previous.RetentionDays, next.RetentionDays},
		"dedupe_seconds":         [2]int{previous.DedupeSeconds, next.DedupeSeconds},
		"max_records":            [2]int{previous.MaxRecords, next.MaxRecords},
		"archive_enabled":        [2]bool{previous.Archive.Enabled, next.Archive.Enabled},
		"archive_source":         next.Archive.Source,
		"archive_retention_days": [2]int{previous.Archive.RetentionDays, next.Archive.RetentionDays},
		"secret_changed":         body.Archive.S3.Secret != "",
	})
	return c.JSON(http.StatusOK, r.settingsResponse())
}

func (r *adminVisitRoutes) test(c echo.Context) error {
	var body struct {
		Source string          `json:"source"`
		S3     backup.S3Config `json:"s3"`
		Prefix string          `json:"prefix"`
	}
	if err := c.Bind(&body); err != nil {
		return badRequest("Invalid request body", err)
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), adminS3Timeout)
	defer cancel()
	if err := r.tracker.TestArchive(ctx, body.Source, body.S3, body.Prefix); err != nil {
		return badRequest("Connection test failed: "+err.Error(), nil)
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true})
}

func (r *adminVisitRoutes) overview(c echo.Context) error {
	r.tracker.Flush()
	storage, err := r.tracker.Storage()
	if err != nil {
		return serverError("Failed to load visit storage", err)
	}
	settings := r.tracker.Settings()
	return c.JSON(http.StatusOK, map[string]any{
		"enabled":     settings.Enabled,
		"storage":     storage,
		"max_records": settings.MaxRecords,
		"writer":      r.tracker.WriterStats(),
		"state":       r.tracker.State(),
	})
}

func (r *adminVisitRoutes) owners(c echo.Context) error {
	limit, err := intParam(c, "limit", 50)
	if err != nil {
		return badRequest(err.Error(), nil)
	}
	offset, err := intParam(c, "offset", 0)
	if err != nil {
		return badRequest(err.Error(), nil)
	}
	r.tracker.Flush()
	rows, total, err := r.tracker.Owners(c.QueryParam("sort"), limit, offset)
	if err != nil {
		return serverError("Failed to load visitor statistics", err)
	}
	names, _ := r.store.UserNames()
	type ownerRow struct {
		visits.OwnerStat
		Username string `json:"username"`
	}
	out := make([]ownerRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, ownerRow{OwnerStat: row, Username: names[row.Owner]})
	}
	return c.JSON(http.StatusOK, map[string]any{"owners": out, "total": total})
}

func (r *adminVisitRoutes) archives(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), adminS3Timeout)
	defer cancel()
	archives, err := r.tracker.ListArchives(ctx)
	if errors.Is(err, visits.ErrArchiveDisabled) {
		return c.JSON(http.StatusOK, map[string]any{"enabled": false, "archives": []visits.ArchiveInfo{}})
	}
	if err != nil {
		return serverError("Failed to list archives", err)
	}
	return c.JSON(http.StatusOK, map[string]any{"enabled": true, "archives": archives})
}

func (r *adminVisitRoutes) archiveNow(c echo.Context) error {
	report, err := r.tracker.ArchiveNow("manual")
	if errors.Is(err, visits.ErrArchiveDisabled) {
		return badRequest(err.Error(), nil)
	}
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{"message": err.Error(), "report": report})
	}
	return c.JSON(http.StatusOK, map[string]any{"report": report})
}

func (r *adminVisitRoutes) cleanup(c echo.Context) error {
	report := r.tracker.Cleanup("manual")
	status := http.StatusOK
	if report.Error != "" {
		status = http.StatusBadGateway
	}
	return c.JSON(status, map[string]any{"report": report, "message": report.Error})
}

func (r *adminVisitRoutes) pull(c echo.Context) error {
	var body struct {
		Days  []string `json:"days"`
		Start string   `json:"start"`
		End   string   `json:"end"`
	}
	if err := c.Bind(&body); err != nil {
		return badRequest("Invalid request body", err)
	}
	if len(body.Days) > 4000 {
		return badRequest("too many days", nil)
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), visitPullTimeout)
	defer cancel()
	report, err := r.tracker.Pull(ctx, body.Days, body.Start, body.End)
	switch {
	case errors.Is(err, visits.ErrInvalidDay):
		return badRequest(err.Error(), nil)
	case errors.Is(err, visits.ErrArchiveDisabled), errors.Is(err, visits.ErrCapacity), errors.Is(err, visits.ErrBusy):
		return c.JSON(http.StatusConflict, map[string]any{"message": err.Error(), "report": report})
	case errors.Is(err, visits.ErrArchiveNotFound):
		return notFound(err.Error())
	case err != nil:
		return serverError("Failed to pull archives", err)
	}
	recordAudit(c, audit.ActionAdminVisitPull, body.Start+".."+body.End, map[string]any{"days": len(report.Days), "records": report.Records, "skipped": len(report.Skipped), "errors": len(report.Errors)})
	return c.JSON(http.StatusOK, map[string]any{"report": report})
}

func (r *adminVisitRoutes) unload(c echo.Context) error {
	day := c.QueryParam("day")
	n, err := r.tracker.Unload(day)
	if errors.Is(err, visits.ErrInvalidDay) {
		return badRequest(err.Error(), nil)
	}
	if err != nil {
		return serverError("Failed to unload pulled visits", err)
	}
	recordAudit(c, audit.ActionAdminVisitDrop, day, map[string]any{"records": n})
	return c.JSON(http.StatusOK, map[string]any{"removed": n})
}
