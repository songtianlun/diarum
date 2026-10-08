package visits

import (
	"database/sql"
	"os"
	"strings"
	"time"
)

const (
	// AnonymousVisitor filters for visitors that were not signed in.
	AnonymousVisitor = "-"
	// UnattributedOwner selects attempts that could not be tied to an owner.
	UnattributedOwner = "-"

	maxLimit        = 200
	defaultLimit    = 50
	maxSummaryDays  = 36600
	maxTimelineDays = 400
)

// Filter narrows a query. Owner is required unless AllOwners is set.
type Filter struct {
	Owner     string
	AllOwners bool
	DiaryDate string
	DiaryID   string
	// VisitorID is an account ID or AnonymousVisitor.
	VisitorID string
	// Device narrows to one device.
	Device string
	// Result is "ok", "failed" or empty for both.
	Result string
	Source string
	// Others hides the owner's own reads.
	Others bool
	Start  time.Time
	End    time.Time
	// Text matches IP, user agent, visitor name or device.
	Text string
}

func (f Filter) where() (string, []any) {
	clauses := []string{}
	args := []any{}
	if !f.AllOwners {
		clauses = append(clauses, "owner = ?")
		args = append(args, f.Owner)
	}
	if f.DiaryDate != "" {
		clauses = append(clauses, "diary_date = ?")
		args = append(args, f.DiaryDate)
	}
	if f.DiaryID != "" {
		clauses = append(clauses, "diary_id = ?")
		args = append(args, f.DiaryID)
	}
	switch f.VisitorID {
	case "":
	case AnonymousVisitor:
		clauses = append(clauses, "visitor_id = ''")
	default:
		clauses = append(clauses, "visitor_id = ?")
		args = append(args, f.VisitorID)
	}
	if f.Device != "" {
		clauses = append(clauses, "device = ?")
		args = append(args, f.Device)
	}
	switch f.Result {
	case "ok":
		clauses = append(clauses, "success = 1")
	case "failed":
		clauses = append(clauses, "success = 0")
	}
	if f.Source != "" {
		clauses = append(clauses, "source = ?")
		args = append(args, f.Source)
	}
	if f.Others {
		clauses = append(clauses, "self = 0")
	}
	if !f.Start.IsZero() {
		clauses = append(clauses, "ts >= ?")
		args = append(args, f.Start.UnixMilli())
	}
	if !f.End.IsZero() {
		clauses = append(clauses, "ts <= ?")
		args = append(args, f.End.UnixMilli())
	}
	if text := strings.TrimSpace(f.Text); text != "" {
		like := "%" + escapeLike(text) + "%"
		clauses = append(clauses, `(ip LIKE ? ESCAPE '\' OR ua LIKE ? ESCAPE '\' OR visitor LIKE ? ESCAPE '\' OR device LIKE ? ESCAPE '\' OR diary_date LIKE ? ESCAPE '\')`)
		args = append(args, like, like, like, like, like)
	}
	if len(clauses) == 0 {
		return "1 = 1", args
	}
	return strings.Join(clauses, " AND "), args
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	return min(limit, maxLimit)
}

// ----- Summary -----

// Totals are the headline numbers of a query.
type Totals struct {
	Views     int64      `json:"views"`
	OK        int64      `json:"ok"`
	Failed    int64      `json:"failed"`
	Visitors  int64      `json:"visitors"`
	Devices   int64      `json:"devices"`
	Anonymous int64      `json:"anonymous"`
	Others    int64      `json:"others"`
	Diaries   int64      `json:"diaries"`
	IPs       int64      `json:"ips"`
	Pulled    int64      `json:"pulled"`
	First     *time.Time `json:"first,omitempty"`
	Last      *time.Time `json:"last,omitempty"`
}

// DayCount is one day of the timeline.
type DayCount struct {
	Day    string `json:"day"`
	OK     int64  `json:"ok"`
	Failed int64  `json:"failed"`
}

// KeyCount is a count by some key.
type KeyCount struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// Summary is the overview of a filtered set of visits.
type Summary struct {
	Totals      Totals        `json:"totals"`
	Timeline    []DayCount    `json:"timeline"`
	TopDiaries  []DiaryStat   `json:"top_diaries"`
	TopVisitors []VisitorStat `json:"top_visitors"`
	Sources     []KeyCount    `json:"sources"`
	Reasons     []KeyCount    `json:"reasons"`
	RecentFails []Visit       `json:"recent_failures"`
}

func (t *Tracker) totals(f Filter) (Totals, error) {
	where, args := f.where()
	var out Totals
	var first, last sql.NullInt64
	err := t.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(success),0), COUNT(DISTINCT vkey), COUNT(DISTINCT device),
		COALESCE(SUM(CASE WHEN visitor_id = '' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN self = 0 THEN 1 ELSE 0 END),0),
		COUNT(DISTINCT CASE WHEN diary_date != '' THEN diary_date END), COUNT(DISTINCT ip), COALESCE(SUM(pulled),0), MIN(ts), MAX(ts)
		FROM visits WHERE `+where, args...).Scan(&out.Views, &out.OK, &out.Visitors, &out.Devices, &out.Anonymous, &out.Others, &out.Diaries, &out.IPs, &out.Pulled, &first, &last)
	if err != nil {
		return out, err
	}
	out.Failed = out.Views - out.OK
	if first.Valid {
		v := time.UnixMilli(first.Int64).UTC()
		out.First = &v
	}
	if last.Valid {
		v := time.UnixMilli(last.Int64).UTC()
		out.Last = &v
	}
	return out, nil
}

// Summarize computes totals, a daily timeline of the last `days` days
// (shifted by tzOffsetMinutes so days match the reader's clock) and the top
// diaries and visitors.
func (t *Tracker) Summarize(f Filter, days, tzOffsetMinutes int) (*Summary, error) {
	if t == nil {
		return nil, errClosed
	}
	if days <= 0 {
		days = 30
	}
	days = min(days, maxSummaryDays)
	tzOffsetMinutes = max(-14*60, min(14*60, tzOffsetMinutes))
	offset := time.Duration(tzOffsetMinutes) * time.Minute

	now := t.now().UTC()
	localNow := now.Add(offset)
	startDay := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(days - 1))
	if f.Start.IsZero() {
		f.Start = startDay.Add(-offset)
	}

	totals, err := t.totals(f)
	if err != nil {
		return nil, err
	}
	out := &Summary{Totals: totals, Timeline: []DayCount{}, TopDiaries: []DiaryStat{}, TopVisitors: []VisitorStat{}, Sources: []KeyCount{}, Reasons: []KeyCount{}, RecentFails: []Visit{}}

	where, args := f.where()
	if days <= maxTimelineDays {
		rows, err := t.db.Query(`SELECT strftime('%Y-%m-%d', (ts / 1000) + ?, 'unixepoch') AS d, SUM(success), SUM(1 - success)
			FROM visits WHERE `+where+` GROUP BY d ORDER BY d`, append([]any{int64(offset.Seconds())}, args...)...)
		if err != nil {
			return nil, err
		}
		counts := map[string]DayCount{}
		for rows.Next() {
			var c DayCount
			if err := rows.Scan(&c.Day, &c.OK, &c.Failed); err != nil {
				rows.Close()
				return nil, err
			}
			counts[c.Day] = c
		}
		rows.Close()
		for i := 0; i < days; i++ {
			day := startDay.AddDate(0, 0, i).Format(dayLayout)
			c := counts[day]
			c.Day = day
			out.Timeline = append(out.Timeline, c)
		}
	}

	if out.TopDiaries, _, err = t.Diaries(f, "views", 10, 0); err != nil {
		return nil, err
	}
	if out.TopVisitors, _, err = t.Visitors(f, 10, 0); err != nil {
		return nil, err
	}
	if out.Sources, err = t.countBy(f, "source", ""); err != nil {
		return nil, err
	}
	if out.Reasons, err = t.countBy(f, "reason", "success = 0"); err != nil {
		return nil, err
	}
	failed := f
	failed.Result = "failed"
	if out.RecentFails, _, err = t.Logs(failed, 10, 0, false); err != nil {
		return nil, err
	}
	return out, nil
}

func (t *Tracker) countBy(f Filter, column, extra string) ([]KeyCount, error) {
	where, args := f.where()
	if extra != "" {
		where += " AND " + extra
	}
	rows, err := t.db.Query(`SELECT `+column+`, COUNT(*) AS n FROM visits WHERE `+where+` GROUP BY `+column+` ORDER BY n DESC LIMIT 20`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []KeyCount{}
	for rows.Next() {
		var c KeyCount
		if err := rows.Scan(&c.Key, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ----- Per diary -----

// DiaryStat is the ranking row of one diary entry.
type DiaryStat struct {
	Owner     string    `json:"owner,omitempty"`
	DiaryDate string    `json:"diary_date"`
	DiaryID   string    `json:"diary_id,omitempty"`
	Views     int64     `json:"views"`
	OK        int64     `json:"ok"`
	Failed    int64     `json:"failed"`
	Visitors  int64     `json:"visitors"`
	Others    int64     `json:"others"`
	First     time.Time `json:"first"`
	Last      time.Time `json:"last"`
}

var diarySorts = map[string]string{
	"views":    "views DESC, last DESC",
	"last":     "last DESC",
	"failed":   "failed DESC, last DESC",
	"visitors": "visitors DESC, views DESC",
	"others":   "others DESC, views DESC",
	"date":     "diary_date DESC",
}

// Diaries ranks diary entries by visits.
func (t *Tracker) Diaries(f Filter, sort string, limit, offset int) ([]DiaryStat, int64, error) {
	if t == nil {
		return nil, 0, errClosed
	}
	order, ok := diarySorts[sort]
	if !ok {
		order = diarySorts["views"]
	}
	where, args := f.where()
	var total int64
	if err := t.db.QueryRow(`SELECT COUNT(*) FROM (SELECT 1 FROM visits WHERE `+where+` GROUP BY owner, diary_date)`, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := t.db.Query(`SELECT owner, diary_date, MAX(diary_id), COUNT(*) AS views, SUM(success), SUM(1 - success) AS failed,
		COUNT(DISTINCT vkey) AS visitors, SUM(CASE WHEN self = 0 THEN 1 ELSE 0 END) AS others, MIN(ts), MAX(ts) AS last
		FROM visits WHERE `+where+` GROUP BY owner, diary_date ORDER BY `+order+` LIMIT ? OFFSET ?`, append(args, clampLimit(limit), max(offset, 0))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []DiaryStat{}
	for rows.Next() {
		var d DiaryStat
		var first, last int64
		if err := rows.Scan(&d.Owner, &d.DiaryDate, &d.DiaryID, &d.Views, &d.OK, &d.Failed, &d.Visitors, &d.Others, &first, &last); err != nil {
			return nil, 0, err
		}
		d.First, d.Last = time.UnixMilli(first).UTC(), time.UnixMilli(last).UTC()
		out = append(out, d)
	}
	return out, total, rows.Err()
}

// ----- Per visitor -----

// VisitorStat ranks one visitor: an account, or an anonymous device.
type VisitorStat struct {
	Key       string    `json:"key"`
	VisitorID string    `json:"visitor_id,omitempty"`
	Visitor   string    `json:"visitor,omitempty"`
	Self      bool      `json:"self"`
	Views     int64     `json:"views"`
	OK        int64     `json:"ok"`
	Failed    int64     `json:"failed"`
	Diaries   int64     `json:"diaries"`
	Devices   int64     `json:"devices"`
	IPs       int64     `json:"ips"`
	LastIP    string    `json:"last_ip,omitempty"`
	LastUA    string    `json:"last_ua,omitempty"`
	Device    string    `json:"device,omitempty"`
	First     time.Time `json:"first"`
	Last      time.Time `json:"last"`
}

// Visitors ranks who read the most.
func (t *Tracker) Visitors(f Filter, limit, offset int) ([]VisitorStat, int64, error) {
	if t == nil {
		return nil, 0, errClosed
	}
	where, args := f.where()
	var total int64
	if err := t.db.QueryRow(`SELECT COUNT(DISTINCT vkey) FROM visits WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := t.db.Query(`SELECT vkey, MAX(visitor_id), MAX(visitor), MAX(self), COUNT(*) AS views, SUM(success), SUM(1 - success),
		COUNT(DISTINCT diary_date), COUNT(DISTINCT device), COUNT(DISTINCT ip), MIN(ts), MAX(ts) AS last
		FROM visits WHERE `+where+` GROUP BY vkey ORDER BY views DESC, last DESC LIMIT ? OFFSET ?`, append(args, clampLimit(limit), max(offset, 0))...)
	if err != nil {
		return nil, 0, err
	}
	out := []VisitorStat{}
	for rows.Next() {
		var v VisitorStat
		var self int
		var first, last int64
		if err := rows.Scan(&v.Key, &v.VisitorID, &v.Visitor, &self, &v.Views, &v.OK, &v.Failed, &v.Diaries, &v.Devices, &v.IPs, &first, &last); err != nil {
			rows.Close()
			return nil, 0, err
		}
		v.Self = self == 1
		v.First, v.Last = time.UnixMilli(first).UTC(), time.UnixMilli(last).UTC()
		if strings.HasPrefix(v.Key, "a:") {
			v.Device = strings.TrimPrefix(v.Key, "a:")
		}
		out = append(out, v)
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	// The most recent IP and client of each visitor, for display.
	for i := range out {
		vf := f
		vf.VisitorID = out[i].VisitorID
		if vf.VisitorID == "" {
			vf.VisitorID = AnonymousVisitor
			vf.Device = out[i].Device
		}
		w, a := vf.where()
		_ = t.db.QueryRow(`SELECT ip, ua FROM visits WHERE `+w+` ORDER BY ts DESC LIMIT 1`, a...).Scan(&out[i].LastIP, &out[i].LastUA)
	}
	return out, total, nil
}

// ----- Logs -----

const selectColumns = `id, ts, owner, diary_id, diary_date, visitor_id, visitor, self, source, kind, route, status, success, reason, ip, ua, device, pulled`

func scanVisit(row interface{ Scan(...any) error }) (*Visit, error) {
	var v Visit
	var ts int64
	var self, success, pulled int
	if err := row.Scan(&v.ID, &ts, &v.OwnerID, &v.DiaryID, &v.DiaryDate, &v.VisitorID, &v.Visitor, &self, &v.Source, &v.Kind, &v.Route, &v.Status, &success, &v.Reason, &v.IP, &v.UA, &v.Device, &pulled); err != nil {
		return nil, err
	}
	v.Time = time.UnixMilli(ts).UTC()
	v.Self, v.Success, v.Pulled = self == 1, success == 1, pulled == 1
	return &v, nil
}

// Logs lists visits newest first. countTotal also reports how many match.
func (t *Tracker) Logs(f Filter, limit, offset int, countTotal bool) ([]Visit, int64, error) {
	if t == nil {
		return nil, 0, errClosed
	}
	where, args := f.where()
	var total int64
	if countTotal {
		if err := t.db.QueryRow(`SELECT COUNT(*) FROM visits WHERE `+where, args...).Scan(&total); err != nil {
			return nil, 0, err
		}
	}
	rows, err := t.db.Query(`SELECT `+selectColumns+` FROM visits WHERE `+where+` ORDER BY ts DESC, id LIMIT ? OFFSET ?`, append(args, clampLimit(limit), max(offset, 0))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Visit{}
	for rows.Next() {
		v, err := scanVisit(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *v)
	}
	return out, total, rows.Err()
}

// ----- Admin -----

// OwnerStat summarises the visits of one owner's diaries.
type OwnerStat struct {
	Owner    string    `json:"owner"`
	Views    int64     `json:"views"`
	Failed   int64     `json:"failed"`
	Others   int64     `json:"others"`
	Visitors int64     `json:"visitors"`
	Diaries  int64     `json:"diaries"`
	Last     time.Time `json:"last"`
}

var ownerSorts = map[string]string{
	"views":  "views DESC",
	"failed": "failed DESC, views DESC",
	"others": "others DESC, views DESC",
	"last":   "last DESC",
}

// Owners ranks diary owners by visits; attempts without an owner appear
// with an empty owner.
func (t *Tracker) Owners(sort string, limit, offset int) ([]OwnerStat, int64, error) {
	if t == nil {
		return nil, 0, errClosed
	}
	order, ok := ownerSorts[sort]
	if !ok {
		order = ownerSorts["views"]
	}
	var total int64
	if err := t.db.QueryRow(`SELECT COUNT(DISTINCT owner) FROM visits`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := t.db.Query(`SELECT owner, COUNT(*) AS views, SUM(1 - success) AS failed, SUM(CASE WHEN self = 0 THEN 1 ELSE 0 END) AS others,
		COUNT(DISTINCT vkey), COUNT(DISTINCT diary_date), MAX(ts) AS last FROM visits GROUP BY owner ORDER BY `+order+` LIMIT ? OFFSET ?`, clampLimit(limit), max(offset, 0))
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []OwnerStat{}
	for rows.Next() {
		var o OwnerStat
		var last int64
		if err := rows.Scan(&o.Owner, &o.Views, &o.Failed, &o.Others, &o.Visitors, &o.Diaries, &last); err != nil {
			return nil, 0, err
		}
		o.Last = time.UnixMilli(last).UTC()
		out = append(out, o)
	}
	return out, total, rows.Err()
}

// Storage describes the database.
type Storage struct {
	Records    int64      `json:"records"`
	Pulled     int64      `json:"pulled"`
	Bytes      int64      `json:"bytes"`
	Oldest     *time.Time `json:"oldest,omitempty"`
	Newest     *time.Time `json:"newest,omitempty"`
	Suppressed int64      `json:"suppressed"`
	Path       string     `json:"path"`
}

// Storage reports the size of the database and its contents.
func (t *Tracker) Storage() (Storage, error) {
	if t == nil {
		return Storage{}, errClosed
	}
	var out Storage
	var oldest, newest sql.NullInt64
	if err := t.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(pulled),0), MIN(ts), MAX(ts) FROM visits`).Scan(&out.Records, &out.Pulled, &oldest, &newest); err != nil {
		return out, err
	}
	if oldest.Valid {
		v := time.UnixMilli(oldest.Int64).UTC()
		out.Oldest = &v
	}
	if newest.Valid {
		v := time.UnixMilli(newest.Int64).UTC()
		out.Newest = &v
	}
	_ = t.db.QueryRow(`SELECT COALESCE(SUM(count),0) FROM suppressed`).Scan(&out.Suppressed)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if info, err := os.Stat(t.path + suffix); err == nil {
			out.Bytes += info.Size()
		}
	}
	out.Path = t.path
	return out, nil
}
