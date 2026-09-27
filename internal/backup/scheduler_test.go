package backup

import (
	"context"
	"strings"
	"testing"
	"time"
)

func (f *fixture) scheduler(at time.Time) (*Scheduler, *time.Time) {
	sc := NewScheduler(f.svc)
	now := at
	sc.now = func() time.Time { return now }
	return sc, &now
}

func TestSchedulerRunsDueBackups(t *testing.T) {
	f := newFixture(t)
	f.configure(Settings{Enabled: true, S3: testS3(), AutoEnabled: true, Schedule: "0 2 * * *", Timezone: "UTC"})
	sc, now := f.scheduler(time.Date(2026, 5, 1, 1, 0, 0, 0, time.UTC))

	sc.refreshAll()
	next, ok := sc.NextRun(f.user.ID)
	if !ok || !next.Equal(time.Date(2026, 5, 1, 2, 0, 0, 0, time.UTC)) {
		t.Fatalf("NextRun = %v, %v", next, ok)
	}
	if d := sc.sleep(); d != maxSchedulerSleep {
		t.Fatalf("sleep = %v, want the cap", d)
	}
	*now = next.Add(-10 * time.Second)
	if d := sc.sleep(); d != 10*time.Second {
		t.Fatalf("sleep = %v", d)
	}

	// Nothing is due yet.
	sc.runDue()
	f.svc.Wait()
	if f.svc.Status(f.user.ID) != nil {
		t.Fatal("backup ran early")
	}

	*now = next.Add(3 * time.Second)
	if d := sc.sleep(); d != 0 {
		t.Fatalf("overdue sleep = %v", d)
	}
	sc.runDue()
	f.svc.Wait()
	job := f.svc.Status(f.user.ID)
	if job == nil || job.Trigger != TriggerScheduled || job.Status != StatusSuccess {
		t.Fatalf("scheduled job = %#v", job)
	}
	if next, _ := sc.NextRun(f.user.ID); !next.Equal(time.Date(2026, 5, 2, 2, 0, 0, 0, time.UTC)) {
		t.Fatalf("next after run = %v", next)
	}

	// Reloading unchanged settings keeps the pending slot.
	*now = time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	sc.Reload(f.user.ID)
	if next, _ := sc.NextRun(f.user.ID); !next.Equal(time.Date(2026, 5, 2, 2, 0, 0, 0, time.UTC)) {
		t.Fatalf("next after reload = %v", next)
	}
	// A new time zone reschedules.
	f.configure(Settings{Enabled: true, S3: testS3(), AutoEnabled: true, Schedule: "0 2 * * *", Timezone: "Asia/Shanghai"})
	sc.Reload(f.user.ID)
	if next, _ := sc.NextRun(f.user.ID); !next.Equal(time.Date(2026, 5, 1, 18, 0, 0, 0, time.UTC)) {
		t.Fatalf("next in Shanghai = %v", next.UTC())
	}

	// Turning automatic backups off unschedules the user.
	f.configure(Settings{Enabled: true, S3: testS3(), AutoEnabled: false})
	sc.refreshAll()
	if _, ok := sc.NextRun(f.user.ID); ok {
		t.Fatal("user should no longer be scheduled")
	}
}

func TestSchedulerSkipsBusyAndBrokenUsers(t *testing.T) {
	f := newFixture(t)
	f.configure(Settings{Enabled: true, S3: testS3(), AutoEnabled: true, Schedule: "0 * * * *"})
	sc, now := f.scheduler(time.Date(2026, 5, 1, 1, 30, 0, 0, time.Local))
	sc.Reload(f.user.ID)
	next, _ := sc.NextRun(f.user.ID)

	// A job still running: the slot is skipped, not queued.
	if err := f.svc.claim(f.user.ID, &Job{Kind: KindBackup, Status: StatusRunning}); err != nil {
		t.Fatalf("claim: %v", err)
	}
	*now = next
	sc.runDue()
	if job := f.svc.Status(f.user.ID); job.BackupID != "" {
		t.Fatalf("busy slot started a backup: %#v", job)
	}
	f.svc.update(f.user.ID, func(j *Job) { j.Status = StatusSuccess })

	// Settings changed behind the scheduler's back: the start fails cleanly.
	f.configure(Settings{Enabled: false, S3: testS3(), AutoEnabled: true, Schedule: "0 * * * *"})
	*now = now.Add(time.Hour)
	sc.runDue()
	f.svc.Wait()

	// An invalid stored schedule is ignored.
	if err := f.store.SetSetting(f.user.ID, KeyEnabled, true, false); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := f.store.SetSetting(f.user.ID, KeySchedule, "not cron", false); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	sc.Reload(f.user.ID)
	if _, ok := sc.NextRun(f.user.ID); ok {
		t.Fatal("invalid schedule should not be scheduled")
	}

	// A schedule that never fires again is dropped.
	never, err := cronParser.Parse("0 0 30 2 *")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	sc.entries[f.user.ID] = &scheduleEntry{schedule: &Schedule{spec: never, Loc: time.UTC, Expr: "0 0 30 2 *"}, next: *now}
	sc.runDue()
	if _, ok := sc.NextRun(f.user.ID); ok {
		t.Fatal("exhausted schedule should be dropped")
	}

	_ = f.store.Close()
	sc.entries[f.user.ID] = &scheduleEntry{schedule: &Schedule{spec: never, Loc: time.UTC}, next: now.Add(time.Hour)}
	sc.refreshAll() // logs and keeps what it has
	if _, ok := sc.NextRun(f.user.ID); !ok {
		t.Fatal("a failed refresh should not drop schedules")
	}
}

func TestSchedulerLoop(t *testing.T) {
	f := newFixture(t)
	f.configure(Settings{Enabled: true, S3: testS3(), AutoEnabled: true, Schedule: "0 2 * * *", Timezone: "UTC"})
	sc := NewScheduler(f.svc)
	var clock time.Time = time.Date(2026, 5, 1, 1, 59, 59, 0, time.UTC)
	tick := make(chan struct{}, 16)
	sc.now = func() time.Time {
		select {
		case tick <- struct{}{}:
		default:
		}
		return clock
	}
	ctx, cancel := context.WithCancel(context.Background())
	sc.Start(ctx)
	<-tick

	// Move past the slot and wake the loop; it starts the backup.
	sc.mu.Lock()
	clock = time.Date(2026, 5, 1, 2, 0, 1, 0, time.UTC).Add(schedulerRefresh)
	sc.mu.Unlock()
	sc.Reload(f.user.ID)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if job := f.svc.Status(f.user.ID); job != nil && job.Trigger == TriggerScheduled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("scheduled backup did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	f.svc.Wait()
	for _, key := range f.mem.Keys() {
		if strings.HasSuffix(key, archiveSuffix) {
			return
		}
	}
	t.Fatalf("no archive written: %v", f.mem.Keys())
}
