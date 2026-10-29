package backup

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func systemS3(ok *bool) SystemS3Func {
	return func() (S3Config, bool) {
		return S3Config{Bucket: "sys", Region: "r", AccessKey: "ak", Secret: "sk", Prefix: "diarum/user-backups"}, *ok
	}
}

func TestBackupToSystemS3(t *testing.T) {
	f := newFixture(t)
	available := false
	var opened S3Config
	f.svc.OpenStore = func(cfg S3Config) (ObjectStore, error) { opened = cfg; return f.mem, nil }
	f.configure(Settings{Enabled: true, Source: SourceSystem, S3: S3Config{Prefix: "own"}})

	if f.svc.SystemAvailable() {
		t.Fatal("system S3 offered")
	}
	if _, ok := f.svc.SystemPath(f.user.ID); ok {
		t.Fatal("system path without system S3")
	}
	if _, err := f.svc.StartBackup(f.user.ID, TriggerManual); !errors.Is(err, ErrSystemUnavailable) {
		t.Fatalf("unavailable = %v", err)
	}
	f.svc.SystemS3 = systemS3(&available)
	if _, err := f.svc.List(context.Background(), f.user.ID); !errors.Is(err, ErrSystemUnavailable) {
		t.Fatalf("not allowed = %v", err)
	}

	available = true
	want := "diarum/user-backups/" + f.user.ID + "/"
	if path, ok := f.svc.SystemPath(f.user.ID); !ok || path != want {
		t.Fatalf("path = %q %v", path, ok)
	}
	job := f.backup()
	if job.Status != StatusSuccess {
		t.Fatalf("job = %#v", job)
	}
	if opened.Bucket != "sys" {
		t.Fatalf("opened %+v", opened)
	}
	keys := f.mem.Keys()
	if len(keys) != 2 {
		t.Fatalf("keys = %v", keys)
	}
	for _, key := range keys {
		if !strings.HasPrefix(key, want) {
			t.Fatalf("key %q outside %q", key, want)
		}
	}
	log, err := f.svc.Log(context.Background(), f.user.ID, job.BackupID)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range log.Entries {
		if strings.Contains(entry.Message, "sys") && strings.Contains(entry.Message, "s3://") {
			t.Fatalf("system bucket shown to the user: %s", entry.Message)
		}
	}
	entries, err := f.svc.List(context.Background(), f.user.ID)
	if err != nil || len(entries) != 1 {
		t.Fatalf("list = %v %v", entries, err)
	}

	// The custom fields stay saved and are not used.
	settings, err := LoadSettings(f.svc.config, f.user.ID)
	if err != nil || settings.Source != SourceSystem || settings.S3.Prefix != "own" {
		t.Fatalf("settings = %+v %v", settings, err)
	}

	ctx := context.Background()
	if err := f.svc.Test(ctx, f.user.ID, SourceSystem, S3Config{}); err != nil {
		t.Fatal(err)
	}
	available = false
	if err := f.svc.Test(ctx, f.user.ID, SourceSystem, testS3()); !errors.Is(err, ErrSystemUnavailable) {
		t.Fatalf("test unavailable = %v", err)
	}
}

func TestNormalizeSource(t *testing.T) {
	for _, c := range []struct {
		source string
		s3     S3Config
		want   string
	}{
		{"system", testS3(), SourceSystem},
		{" custom ", S3Config{}, SourceCustom},
		{"", testS3(), SourceCustom},
		{"", S3Config{}, SourceSystem},
		{"ftp", S3Config{}, SourceSystem},
	} {
		if got := NormalizeSource(c.source, c.s3); got != c.want {
			t.Fatalf("NormalizeSource(%q) = %q", c.source, got)
		}
	}
	if JoinPrefix("/a/", "", "b//c") != "a/b/c" {
		t.Fatal("JoinPrefix")
	}

	// Settings saved before sources existed keep their own bucket.
	f := newFixture(t)
	f.enable(3)
	settings, err := LoadSettings(f.svc.config, f.user.ID)
	if err != nil || settings.Source != SourceCustom {
		t.Fatalf("legacy = %+v %v", settings, err)
	}
}

func TestSchedulerKeepsSystemSourceUsers(t *testing.T) {
	f := newFixture(t)
	f.configure(Settings{Enabled: true, Source: SourceSystem, AutoEnabled: true})
	sc, _ := f.scheduler(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	sc.Reload(f.user.ID)
	if _, ok := sc.NextRun(f.user.ID); !ok {
		t.Fatal("system source user not scheduled")
	}
}
