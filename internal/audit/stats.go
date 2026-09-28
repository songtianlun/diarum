package audit

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// aggregator builds Stats in one pass over the matches.
type aggregator struct {
	loc    *time.Location
	hourly bool
	from   time.Time
	to     time.Time

	stats     Stats
	actions   map[string]int
	status    map[string]int
	methods   map[string]int
	sources   map[string]int
	users     map[string]*Count
	ips       map[string]*ipAgg
	routes    map[string]*routeAgg
	failed    map[string]int
	buckets   map[int64]*Bucket
	durations []int64
	durSum    int64
	durCount  int
	first     time.Time
	last      time.Time
}

type ipAgg struct {
	IPCount
	users map[string]struct{}
	last  time.Time
}

type routeAgg struct {
	RouteCount
	sum int64
}

func newAggregator(q Query, files []FileInfo, fileLoc *time.Location) *aggregator {
	a := &aggregator{
		loc:     q.Location,
		actions: map[string]int{},
		status:  map[string]int{},
		methods: map[string]int{},
		sources: map[string]int{},
		users:   map[string]*Count{},
		ips:     map[string]*ipAgg{},
		routes:  map[string]*routeAgg{},
		failed:  map[string]int{},
		buckets: map[int64]*Bucket{},
	}
	// The timeline spans the requested range, or the days read.
	a.from, a.to = q.Start, q.End
	if len(files) > 0 {
		if a.from.IsZero() {
			a.from, _ = time.ParseInLocation(fileDateLayout, files[0].Date, fileLoc)
		}
		if a.to.IsZero() {
			last, _ := time.ParseInLocation(fileDateLayout, files[len(files)-1].Date, fileLoc)
			a.to = last.AddDate(0, 0, 1).Add(-time.Nanosecond)
		}
	}
	a.hourly = !a.from.IsZero() && !a.to.IsZero() && a.to.Sub(a.from) <= 72*time.Hour
	return a
}

func (a *aggregator) bucketStart(t time.Time) time.Time {
	t = t.In(a.loc)
	if a.hourly {
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, a.loc)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, a.loc)
}

func statusClass(status int) string {
	if status <= 0 {
		return "-"
	}
	return strconv.Itoa(status/100) + "xx"
}

func (a *aggregator) add(e *Entry) {
	s := &a.stats
	s.Total++
	if a.first.IsZero() || e.Time.Before(a.first) {
		a.first = e.Time
	}
	if e.Time.After(a.last) {
		a.last = e.Time
	}
	isError := e.Status >= 400
	switch {
	case e.Status >= 500:
		s.ServerErrors++
	case e.Status >= 400:
		s.ClientErrors++
	}
	if e.Status == 401 || e.Status == 403 {
		s.Denied++
	}
	switch e.Action {
	case ActionAuthLogin:
		s.LoginOK++
	case ActionAuthLoginFail:
		s.LoginFailed++
		identity, _ := e.Detail["identity"].(string)
		if identity == "" {
			identity = e.User
		}
		if identity != "" {
			a.failed[identity]++
		}
	}
	switch e.Method {
	case "POST", "PUT", "PATCH", "DELETE":
		s.Writes++
	}

	action := e.Action
	if action == "" {
		action = "-"
	}
	a.actions[action]++
	a.status[statusClass(e.Status)]++
	if e.Method != "" {
		a.methods[e.Method]++
	}
	source := e.Source
	if source == "" {
		source = "-"
	}
	a.sources[source]++

	if e.UserID == "" {
		s.Anonymous++
	} else {
		user := a.users[e.UserID]
		if user == nil {
			user = &Count{Key: e.UserID}
			a.users[e.UserID] = user
		}
		user.Count++
		if e.User != "" {
			user.Label = e.User
		}
	}

	if e.IP != "" {
		ip := a.ips[e.IP]
		if ip == nil {
			ip = &ipAgg{IPCount: IPCount{IP: e.IP}, users: map[string]struct{}{}}
			a.ips[e.IP] = ip
		}
		ip.Count++
		if isError {
			ip.Errors++
		}
		if e.Action == ActionAuthLoginFail {
			ip.Failed++
		}
		if e.UserID != "" {
			ip.users[e.UserID] = struct{}{}
		}
		if e.Time.After(ip.last) {
			ip.last = e.Time
		}
	}

	if e.Route != "" || e.Method != "" {
		key := e.Method + " " + e.Route
		route := a.routes[key]
		if route == nil {
			route = &routeAgg{RouteCount: RouteCount{Method: e.Method, Route: e.Route}}
			a.routes[key] = route
		}
		route.Count++
		route.sum += e.Duration
		if isError {
			route.Errors++
		}
		if e.Duration > route.MaxMS {
			route.MaxMS = e.Duration
		}
		a.durSum += e.Duration
		a.durCount++
		if len(a.durations) < maxDurSamples {
			a.durations = append(a.durations, e.Duration)
		}
		if e.Duration > s.MaxMS {
			s.MaxMS = e.Duration
		}
	}

	start := a.bucketStart(e.Time)
	bucket := a.buckets[start.Unix()]
	if bucket == nil {
		bucket = &Bucket{Start: start}
		a.buckets[start.Unix()] = bucket
	}
	bucket.Count++
	if isError {
		bucket.Errors++
	}
}

func (a *aggregator) finish() *Stats {
	s := &a.stats
	s.Users = len(a.users)
	s.IPs = len(a.ips)
	if !a.first.IsZero() {
		s.First = a.first.Format(time.RFC3339)
		s.Last = a.last.Format(time.RFC3339)
	}
	if a.durCount > 0 {
		s.AvgMS = float64(a.durSum) / float64(a.durCount)
		sort.Slice(a.durations, func(i, j int) bool { return a.durations[i] < a.durations[j] })
		// Nearest-rank percentile.
		s.P95MS = a.durations[(len(a.durations)*95+99)/100-1]
	}

	s.Actions = topCounts(a.actions, 0)
	s.Status = topCounts(a.status, 0)
	s.Methods = topCounts(a.methods, 0)
	s.Sources = topCounts(a.sources, 0)
	s.FailedLogins = topCounts(a.failed, topN)

	users := make([]Count, 0, len(a.users))
	for _, user := range a.users {
		users = append(users, *user)
	}
	sortCounts(users)
	s.TopUsers = users[:min(len(users), topN)]

	ips := make([]IPCount, 0, len(a.ips))
	for _, ip := range a.ips {
		ip.Users = len(ip.users)
		ip.LastSeen = ip.last.Format(time.RFC3339)
		ips = append(ips, ip.IPCount)
	}
	sort.Slice(ips, func(i, j int) bool {
		if ips[i].Count != ips[j].Count {
			return ips[i].Count > ips[j].Count
		}
		return ips[i].IP < ips[j].IP
	})
	s.TopIPs = ips[:min(len(ips), topN)]

	routes := make([]RouteCount, 0, len(a.routes))
	for _, route := range a.routes {
		if route.Count > 0 {
			route.AvgMS = float64(route.sum) / float64(route.Count)
		}
		routes = append(routes, route.RouteCount)
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Count != routes[j].Count {
			return routes[i].Count > routes[j].Count
		}
		return routes[i].Method+routes[i].Route < routes[j].Method+routes[j].Route
	})
	s.Routes = routes[:min(len(routes), topRoutes)]

	s.Bucket = "day"
	if a.hourly {
		s.Bucket = "hour"
	}
	s.Timeline = a.timeline()
	return s
}

// timeline lists every bucket of the range, including empty ones, so charts
// show gaps as zero rather than skipping them.
func (a *aggregator) timeline() []Bucket {
	from, to := a.from, a.to
	if from.IsZero() || to.IsZero() {
		from, to = a.first, a.last
	}
	out := make([]Bucket, 0)
	if from.IsZero() || to.Before(from) {
		return out
	}
	const maxBuckets = 400
	for start := a.bucketStart(from); !start.After(to) && len(out) < maxBuckets; {
		if bucket, ok := a.buckets[start.Unix()]; ok {
			out = append(out, *bucket)
		} else {
			out = append(out, Bucket{Start: start})
		}
		if a.hourly {
			start = start.Add(time.Hour)
		} else {
			start = start.AddDate(0, 0, 1)
		}
	}
	return out
}

func topCounts(counts map[string]int, limit int) []Count {
	out := make([]Count, 0, len(counts))
	for key, count := range counts {
		out = append(out, Count{Key: key, Count: count})
	}
	sortCounts(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func sortCounts(counts []Count) {
	sort.Slice(counts, func(i, j int) bool {
		if counts[i].Count != counts[j].Count {
			return counts[i].Count > counts[j].Count
		}
		return counts[i].Key < counts[j].Key
	})
}

// actionsForText returns the actions whose readable names contain text, so
// a search for "删除" or "sign in" finds them.
func actionsForText(text string) map[string]bool {
	out := map[string]bool{}
	for action, labels := range actionLabels {
		for _, label := range labels {
			if strings.Contains(strings.ToLower(label), text) {
				out[action] = true
				break
			}
		}
	}
	return out
}

// actionLabels mirror the labels the admin console shows.
var actionLabels = map[string][]string{
	ActionDiaryCreate:    {"create diary", "created", "创建日记", "创建", "新建"},
	ActionDiaryUpdate:    {"update diary", "updated", "edit", "更新日记", "更新", "编辑", "修改"},
	ActionDiaryDelete:    {"delete diary", "deleted", "删除日记", "删除"},
	ActionDiaryRestore:   {"restore version", "restored", "恢复历史版本", "恢复"},
	ActionDiaryView:      {"view diary", "viewed", "read", "查看日记", "查看", "阅读"},
	ActionDiarySearch:    {"search diary", "searched", "搜索日记", "搜索"},
	ActionConvDelete:     {"delete conversation", "删除对话"},
	ActionMediaUpload:    {"upload image", "uploaded", "上传图片", "上传"},
	ActionMediaDelete:    {"delete image", "deleted", "删除图片", "删除"},
	ActionDataImport:     {"import data", "imported", "导入数据", "导入"},
	ActionDataExport:     {"export data", "exported", "导出数据", "导出"},
	ActionSettingsUpdate: {"change settings", "settings", "修改设置", "设置"},
	ActionTokenUpdate:    {"token", "api token", "令牌", "密钥"},
	ActionAuthLogin:      {"sign in", "login", "登录"},
	ActionAuthLoginFail:  {"failed sign in", "login failed", "登录失败"},
	ActionAuthLogout:     {"sign out", "logout", "退出", "登出"},
	ActionAuthRegister:   {"register", "sign up", "注册"},
	ActionAuthDenied:     {"unauthorized", "denied", "未授权", "拒绝"},
	ActionAuthForbidden:  {"forbidden", "denied", "禁止", "拒绝"},
	ActionAdminRole:      {"role", "admin", "身份", "角色", "管理员"},
	ActionAdminAudit:     {"audit settings", "审计设置"},
	ActionAdminPull:      {"pull archive", "拉取归档", "拉取"},
}
