package backup

import (
	"errors"
	"fmt"
	"strings"
	"time"
	// Embed the zone database so IANA time zones resolve even on minimal
	// container images without /usr/share/zoneinfo.
	_ "time/tzdata"

	"github.com/robfig/cron/v3"
)

// MinScheduleInterval is the shortest gap allowed between two scheduled runs.
const MinScheduleInterval = 15 * time.Minute

// Standard five-field cron (minute hour day-of-month month day-of-week) plus
// the @hourly/@daily/@weekly/@monthly/@yearly shorthands.
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// Schedule is a parsed cron expression bound to a time zone.
type Schedule struct {
	spec cron.Schedule
	Loc  *time.Location
	Expr string
	Zone string
}

// ParseSchedule validates a cron expression and time zone. An empty zone
// means the server's local time.
func ParseSchedule(expr, zone string) (*Schedule, error) {
	expr = strings.Join(strings.Fields(expr), " ")
	if expr == "" {
		return nil, errors.New("schedule is required")
	}
	if strings.HasPrefix(expr, "TZ=") || strings.HasPrefix(expr, "CRON_TZ=") {
		return nil, errors.New("set the time zone separately instead of in the expression")
	}
	if strings.HasPrefix(expr, "@every") {
		return nil, errors.New("@every is not supported; use a cron expression")
	}
	loc, err := LoadLocation(zone)
	if err != nil {
		return nil, err
	}
	spec, err := cronParser.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression: %v", err)
	}
	sched := &Schedule{spec: spec, Loc: loc, Expr: expr, Zone: strings.TrimSpace(zone)}

	// The expression has to fire at all (e.g. not "0 0 30 2 *") and not so
	// often that runs pile up on each other.
	runs := sched.NextRuns(time.Now(), 24)
	if len(runs) == 0 {
		return nil, errors.New("schedule never runs")
	}
	for i := 1; i < len(runs); i++ {
		if runs[i].Sub(runs[i-1]) < MinScheduleInterval {
			return nil, fmt.Errorf("backups must be at least %d minutes apart", int(MinScheduleInterval.Minutes()))
		}
	}
	return sched, nil
}

// LoadLocation resolves an IANA zone name; empty means server local time.
func LoadLocation(zone string) (*time.Location, error) {
	zone = strings.TrimSpace(zone)
	if zone == "" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, fmt.Errorf("unknown time zone %q", zone)
	}
	return loc, nil
}

// Next returns the first run strictly after t, or the zero time if none.
func (s *Schedule) Next(t time.Time) time.Time {
	return s.spec.Next(t.In(s.Loc))
}

// NextRuns returns up to n upcoming runs after t.
func (s *Schedule) NextRuns(t time.Time, n int) []time.Time {
	runs := make([]time.Time, 0, n)
	for len(runs) < n {
		t = s.Next(t)
		if t.IsZero() {
			break
		}
		runs = append(runs, t)
	}
	return runs
}
