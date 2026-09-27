package backup

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/songtianlun/diarum/internal/logger"
)

const (
	// The scheduler never sleeps longer than this, so clock jumps and
	// settings saved outside the backup API are picked up quickly.
	maxSchedulerSleep = time.Minute
	// Every so often all users' settings are re-read from the database.
	schedulerRefresh = 5 * time.Minute
)

type scheduleEntry struct {
	schedule *Schedule
	next     time.Time
}

// Scheduler starts automatic backups. It keeps the next run of every user
// with automatic backups enabled and wakes when the earliest one is due. A
// run missed while the server was down is not made up; the next one happens
// at its regular time.
type Scheduler struct {
	svc *Service
	now func() time.Time

	mu      sync.Mutex
	entries map[string]*scheduleEntry
	wake    chan struct{}
}

// NewScheduler creates a scheduler for svc. Call Start to run it.
func NewScheduler(svc *Service) *Scheduler {
	return &Scheduler{svc: svc, now: time.Now, entries: make(map[string]*scheduleEntry), wake: make(chan struct{}, 1)}
}

// Start runs the scheduler until ctx ends.
func (sc *Scheduler) Start(ctx context.Context) {
	go sc.loop(ctx)
}

func (sc *Scheduler) loop(ctx context.Context) {
	sc.refreshAll()
	lastRefresh := sc.now()
	for {
		timer := time.NewTimer(sc.sleep())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-sc.wake:
			timer.Stop()
		case <-timer.C:
		}
		if sc.now().Sub(lastRefresh) >= schedulerRefresh {
			sc.refreshAll()
			lastRefresh = sc.now()
		}
		sc.runDue()
	}
}

// sleep is how long to wait before the next check.
func (sc *Scheduler) sleep() time.Duration {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	wait := maxSchedulerSleep
	now := sc.now()
	for _, entry := range sc.entries {
		if d := entry.next.Sub(now); d < wait {
			wait = d
		}
	}
	if wait < 0 {
		wait = 0
	}
	return wait
}

// runDue starts every backup whose time has come and schedules its next run.
func (sc *Scheduler) runDue() {
	now := sc.now()
	due := make([]string, 0)
	sc.mu.Lock()
	for userID, entry := range sc.entries {
		if entry.next.After(now) {
			continue
		}
		due = append(due, userID)
		// Next strictly after the slot that fired, never before now, so an
		// early timer cannot fire the same slot twice.
		from := entry.next
		if now.After(from) {
			from = now
		}
		entry.next = entry.schedule.Next(from)
		if entry.next.IsZero() {
			delete(sc.entries, userID)
		}
	}
	sc.mu.Unlock()

	for _, userID := range due {
		if _, err := sc.svc.StartBackup(userID, TriggerScheduled); err != nil {
			if errors.Is(err, ErrBusy) {
				logger.Warn("[Backup] scheduled backup for %s skipped: a job is still running", userID)
			} else {
				logger.Error("[Backup] scheduled backup for %s could not start: %v", userID, err)
			}
		}
	}
}

// refreshAll re-reads the settings of every user with automatic backups on.
func (sc *Scheduler) refreshAll() {
	users, err := sc.svc.store.UsersWithSetting(KeyAutoEnabled, true)
	if err != nil {
		logger.Error("[Backup] loading scheduled users: %v", err)
		return
	}
	active := make(map[string]bool, len(users))
	for _, userID := range users {
		active[userID] = true
		sc.load(userID)
	}
	sc.mu.Lock()
	for userID := range sc.entries {
		if !active[userID] {
			delete(sc.entries, userID)
		}
	}
	sc.mu.Unlock()
}

// Reload picks up a user's changed settings immediately.
func (sc *Scheduler) Reload(userID string) {
	sc.load(userID)
	select {
	case sc.wake <- struct{}{}:
	default:
	}
}

func (sc *Scheduler) load(userID string) {
	settings, err := LoadSettings(sc.svc.config, userID)
	var schedule *Schedule
	if err == nil && settings.Enabled && settings.AutoEnabled && settings.S3.Complete() {
		schedule, err = ParseSchedule(settings.Schedule, settings.Timezone)
		if err != nil {
			logger.Warn("[Backup] ignoring invalid schedule of %s: %v", userID, err)
		}
	}

	sc.mu.Lock()
	defer sc.mu.Unlock()
	if schedule == nil {
		delete(sc.entries, userID)
		return
	}
	if current := sc.entries[userID]; current != nil && current.schedule.Expr == schedule.Expr && current.schedule.Loc.String() == schedule.Loc.String() {
		return
	}
	next := schedule.Next(sc.now())
	if next.IsZero() {
		delete(sc.entries, userID)
		return
	}
	sc.entries[userID] = &scheduleEntry{schedule: schedule, next: next}
}

// NextRun returns the user's next automatic backup, if one is scheduled.
func (sc *Scheduler) NextRun(userID string) (time.Time, bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	entry := sc.entries[userID]
	if entry == nil {
		return time.Time{}, false
	}
	return entry.next, true
}
