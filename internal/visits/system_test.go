package visits

import (
	"context"
	"errors"
	"testing"

	"github.com/songtianlun/diarum/internal/backup"
)

func TestArchiveToSystemS3(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, ok := f.t.SystemS3Path(); ok {
		t.Fatal("system path while unconfigured")
	}
	next := DefaultSettings()
	next.Archive.Enabled = true
	if next.Archive.Source != S3SourceSystem {
		t.Fatalf("default source = %q", next.Archive.Source)
	}
	if _, err := f.t.UpdateSettings(next); !errors.Is(err, ErrSystemS3Unavailable) {
		t.Fatalf("unavailable = %v", err)
	}
	if err := f.t.TestArchive(ctx, S3SourceSystem, backup.S3Config{}, ""); !errors.Is(err, ErrSystemS3Unavailable) {
		t.Fatalf("test unavailable = %v", err)
	}

	f.system = backup.S3Config{Bucket: "sys", Region: "r", AccessKey: "ak", Secret: "sk", Prefix: "diarum/visits"}
	if path, ok := f.t.SystemS3Path(); !ok || path != "diarum/visits/" {
		t.Fatalf("path = %q %v", path, ok)
	}
	f.enable(t, func(s *Settings) {
		s.DedupeSeconds = 0
		s.Archive.Enabled = true
		// The own prefix only applies to the custom and shared sources.
		s.Archive.Prefix = "ignored"
	})
	if !f.t.UsesSystemS3() {
		t.Fatal("system use not reported")
	}
	recordOn(t, f, "2026-09-27")
	if _, err := f.t.ArchiveNow("test"); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.bucket.Object("diarum/visits/2026/2026-09-27.jsonl.gz"); !ok {
		t.Fatalf("keys = %v", f.bucket.Keys())
	}
	archives, err := f.t.ListArchives(ctx)
	if err != nil || len(archives) != 1 {
		t.Fatalf("archives = %v %v", archives, err)
	}
	if err := f.t.TestArchive(ctx, "", backup.S3Config{}, ""); err != nil {
		t.Fatal(err)
	}

	var nilTracker *Tracker
	if nilTracker.UsesSystemS3() {
		t.Fatal("nil tracker uses system S3")
	}
	if _, ok := nilTracker.SystemS3Path(); ok {
		t.Fatal("nil tracker has a system path")
	}
}

func TestLegacySourcesAreKept(t *testing.T) {
	for raw, want := range map[string]string{
		`{"enabled":true,"archive":{"enabled":true}}`:                 S3SourceShared,
		`{"enabled":true,"archive":{"source":"audit"}}`:               S3SourceShared,
		`{"enabled":true,"archive":{"source":"custom"}}`:              S3SourceCustom,
		`{"enabled":true,"archive":{"source":"system","prefix":"x"}}`: S3SourceSystem,
	} {
		store := &memSettings{values: map[string]string{SettingsKey: raw}}
		tr, err := New(t.TempDir(), Options{Settings: store})
		if err != nil {
			t.Fatal(err)
		}
		if got := tr.Settings().Archive.Source; got != want {
			t.Fatalf("%s: source = %q, want %q", raw, got, want)
		}
		tr.Close()
	}
}
