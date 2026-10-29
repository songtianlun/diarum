package audit

import (
	"context"
	"errors"
	"testing"

	"github.com/songtianlun/diarum/internal/backup"
)

func systemLogger(t *testing.T, available *bool) (*Logger, string, *backup.MemoryStore, *memSettings) {
	t.Helper()
	bucket := backup.NewMemoryStore()
	settings := &memSettings{}
	l, dir, _ := newTestLogger(t, Options{
		Settings:       settings,
		NewObjectStore: func(backup.S3Config) (backup.ObjectStore, error) { return bucket, nil },
		SystemS3: func() (backup.S3Config, bool) {
			return backup.S3Config{Bucket: "sys", Region: "r", AccessKey: "ak", Secret: "sk", Prefix: "diarum/audit"}, *available
		},
	})
	return l, dir, bucket, settings
}

func TestArchiveToSystemS3(t *testing.T) {
	available := false
	l, dir, bucket, _ := systemLogger(t, &available)
	if got := l.Settings().Archive.Source; got != ArchiveSourceSystem {
		t.Fatalf("default source = %q", got)
	}
	if _, ok := l.SystemS3Path(); ok {
		t.Fatal("system path while unconfigured")
	}
	next := DefaultSettings()
	next.Archive.Enabled = true
	if _, err := l.UpdateSettings(next); !errors.Is(err, ErrSystemS3Unavailable) {
		t.Fatalf("unavailable = %v", err)
	}
	if err := l.TestArchive(context.Background(), ArchiveSourceSystem, backup.S3Config{}); !errors.Is(err, ErrSystemS3Unavailable) {
		t.Fatalf("test unavailable = %v", err)
	}

	available = true
	if path, ok := l.SystemS3Path(); !ok || path != "diarum/audit/" {
		t.Fatalf("path = %q %v", path, ok)
	}
	if _, err := l.UpdateSettings(next); err != nil {
		t.Fatal(err)
	}
	if !l.UsesSystemS3() {
		t.Fatal("system use not reported")
	}
	if cfg, ok := l.ArchiveTarget(); !ok || cfg.Bucket != "sys" {
		t.Fatalf("target = %+v %v", cfg, ok)
	}
	writeDays(t, dir, false, "2026-09-27")
	if _, err := l.ArchiveNow("test"); err != nil {
		t.Fatal(err)
	}
	if _, ok := bucket.Object("diarum/audit/2026-09-27.log.gz"); !ok {
		t.Fatalf("keys = %v", bucket.Keys())
	}
	archives, err := l.ListArchives(context.Background())
	if err != nil || len(archives) != 1 {
		t.Fatalf("archives = %v %v", archives, err)
	}
	if err := l.TestArchive(context.Background(), ArchiveSourceSystem, backup.S3Config{}); err != nil {
		t.Fatal(err)
	}
	if err := l.TestArchive(context.Background(), "ftp", backup.S3Config{}); err == nil {
		t.Fatal("unknown source accepted")
	}

	// When the system S3 goes away, the archive stops rather than failing.
	available = false
	if _, err := l.ArchiveNow("test"); !errors.Is(err, ErrArchiveDisabled) {
		t.Fatalf("after removal = %v", err)
	}
	if _, err := l.Pull(context.Background(), "2026-09-01"); !errors.Is(err, ErrArchiveDisabled) {
		t.Fatalf("pull after removal = %v", err)
	}
}

func TestLegacySettingsKeepTheirSource(t *testing.T) {
	available := true
	for raw, want := range map[string]string{
		`{"retention_days":3,"archive":{"enabled":true,"s3":{"bucket":"b","region":"r","access_key":"a","secret":"s"}}}`: ArchiveSourceCustom,
		`{"retention_days":3,"archive":{"enabled":false}}`:                                                               ArchiveSourceSystem,
		`{"retention_days":3,"archive":{"source":"system"}}`:                                                             ArchiveSourceSystem,
	} {
		store := &memSettings{values: map[string]string{SettingsKey: raw}}
		l, _, _ := newTestLogger(t, Options{Settings: store, SystemS3: func() (backup.S3Config, bool) { return backup.S3Config{}, available }})
		if got := l.Settings().Archive.Source; got != want {
			t.Fatalf("%s: source = %q, want %q", raw, got, want)
		}
	}

	var l *Logger
	if l.UsesSystemS3() {
		t.Fatal("nil logger uses system S3")
	}
	if _, ok := l.ArchiveTarget(); ok {
		t.Fatal("nil logger has a target")
	}
	if _, ok := l.SystemS3Path(); ok {
		t.Fatal("nil logger has a system path")
	}
	if _, err := (Settings{RetentionDays: 3, Archive: ArchiveSettings{RetentionDays: 30, Source: "ftp"}}).Validate(); err == nil {
		t.Fatal("unknown source accepted")
	}
}
