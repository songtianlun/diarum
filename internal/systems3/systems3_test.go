package systems3

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/songtianlun/diarum/internal/backup"
)

type memSettings struct {
	mu     sync.Mutex
	values map[string]string
	getErr error
	setErr error
}

func (m *memSettings) GetSystemSetting(key string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return "", false, m.getErr
	}
	v, ok := m.values[key]
	return v, ok, nil
}

func (m *memSettings) SetSystemSetting(key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setErr != nil {
		return m.setErr
	}
	if m.values == nil {
		m.values = map[string]string{}
	}
	m.values[key] = value
	return nil
}

func conn() backup.S3Config {
	return backup.S3Config{Bucket: "b", Region: "r", AccessKey: "ak", Secret: "sk"}
}

func memFactory(bucket *backup.MemoryStore) ObjectStoreFactory {
	return func(backup.S3Config) (backup.ObjectStore, error) { return bucket, nil }
}

func TestUnconfiguredDefaults(t *testing.T) {
	s := New(nil, nil)
	if got := s.Settings(); got.Configured() || got.S3.Prefix != DefaultPrefix || got.AllowUserBackups {
		t.Fatalf("settings = %+v", got)
	}
	if _, ok := s.For(FolderAudit); ok {
		t.Fatal("unconfigured storage offered")
	}
	if _, ok := s.Func(FolderVisits)(); ok {
		t.Fatal("unconfigured storage offered")
	}
	if _, ok := s.UserBackups(); ok {
		t.Fatal("unconfigured storage offered for backups")
	}
	if got := s.Path(FolderUserBackups); got != "diarum/user-backups/" {
		t.Fatalf("path = %q", got)
	}
}

func TestUpdatePersistsAndResolvesFolders(t *testing.T) {
	store := &memSettings{}
	s := New(store, nil)
	next := Settings{S3: conn(), AllowUserBackups: true}
	next.S3.Prefix = " /team//diarum/ "
	saved, err := s.Update(next)
	if err != nil || saved.S3.Prefix != "team/diarum" || !saved.Configured() {
		t.Fatalf("update = %+v %v", saved, err)
	}

	reopened := New(store, nil)
	cfg, ok := reopened.For(FolderAudit)
	if !ok || cfg.Bucket != "b" || cfg.Secret != "sk" || cfg.Prefix != "team/diarum/audit" {
		t.Fatalf("audit = %+v %v", cfg, ok)
	}
	cfg, ok = reopened.UserBackups()
	if !ok || cfg.Prefix != "team/diarum/user-backups" {
		t.Fatalf("user backups = %+v %v", cfg, ok)
	}
	if got := backup.UserPrefix(cfg, "user1"); got != "team/diarum/user-backups/user1/" {
		t.Fatalf("user prefix = %q", got)
	}

	// Backups can be withheld from users while archives keep using it.
	next.AllowUserBackups = false
	if _, err := reopened.Update(next); err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.UserBackups(); ok {
		t.Fatal("backups offered although not allowed")
	}
	if _, ok := reopened.For(FolderVisits); !ok {
		t.Fatal("archives lost the storage")
	}
}

func TestUpdateKeepsSecretAndValidates(t *testing.T) {
	s := New(&memSettings{}, nil)
	if _, err := s.Update(Settings{S3: conn()}); err != nil {
		t.Fatal(err)
	}
	keep := Settings{S3: conn()}
	keep.S3.Secret = ""
	got, err := s.Update(keep)
	if err != nil || got.S3.Secret != "sk" {
		t.Fatalf("secret lost: %+v %v", got, err)
	}
	if _, err := s.Update(Settings{S3: backup.S3Config{Bucket: "b", Region: "r"}}); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("partial = %v", err)
	}
	if !s.Settings().Configured() {
		t.Fatal("failed update applied")
	}
	// Clearing every field removes the storage, secret included.
	cleared, err := s.Update(Settings{})
	if err != nil || cleared.Configured() || cleared.S3.Secret != "" || cleared.S3.Prefix != DefaultPrefix {
		t.Fatalf("clear = %+v %v", cleared, err)
	}
}

func TestStoreErrors(t *testing.T) {
	store := &memSettings{setErr: errors.New("disk full")}
	s := New(store, nil)
	if _, err := s.Update(Settings{S3: conn()}); err == nil {
		t.Fatal("store error ignored")
	}
	if s.Settings().Configured() {
		t.Fatal("unsaved settings applied")
	}
	for _, bad := range []*memSettings{{getErr: errors.New("boom")}, {values: map[string]string{SettingsKey: "{"}}} {
		if New(bad, nil).Settings().Configured() {
			t.Fatal("unreadable settings used")
		}
	}
}

func TestNilService(t *testing.T) {
	var s *Service
	if s.Settings().Configured() {
		t.Fatal("nil service configured")
	}
	if _, err := s.Update(Settings{}); err == nil {
		t.Fatal("nil update accepted")
	}
	if err := s.Test(context.Background(), Settings{}); err == nil {
		t.Fatal("nil test passed")
	}
	if _, ok := s.For(FolderAudit); ok {
		t.Fatal("nil service offered storage")
	}
}

func TestConnectionTest(t *testing.T) {
	bucket := backup.NewMemoryStore()
	s := New(&memSettings{}, memFactory(bucket))
	ctx := context.Background()
	if err := s.Test(ctx, Settings{}); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("empty = %v", err)
	}
	if err := s.Test(ctx, Settings{S3: backup.S3Config{Bucket: "b"}}); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("partial = %v", err)
	}
	if err := s.Test(ctx, Settings{S3: conn()}); err != nil {
		t.Fatal(err)
	}
	if keys := bucket.Keys(); len(keys) != 0 {
		t.Fatalf("probe left behind: %v", keys)
	}
	// An empty secret tests with the stored one.
	if _, err := s.Update(Settings{S3: conn()}); err != nil {
		t.Fatal(err)
	}
	keep := Settings{S3: conn()}
	keep.S3.Secret = ""
	var seen backup.S3Config
	s.factory = func(cfg backup.S3Config) (backup.ObjectStore, error) { seen = cfg; return bucket, nil }
	if err := s.Test(ctx, keep); err != nil || seen.Secret != "sk" || seen.Prefix != DefaultPrefix {
		t.Fatalf("stored secret = %+v %v", seen, err)
	}

	for name, mutate := range map[string]func(){
		"write":  func() { bucket.FailPut = func(string) error { return errors.New("x") } },
		"read":   func() { bucket.FailGet = func(string) error { return errors.New("x") } },
		"list":   func() { bucket.FailList = func(string) error { return errors.New("x") } },
		"delete": func() { bucket.FailDelete = func(string) error { return errors.New("x") } },
	} {
		bucket.FailPut, bucket.FailGet, bucket.FailList, bucket.FailDelete = nil, nil, nil, nil
		mutate()
		if err := s.Test(ctx, Settings{S3: conn()}); err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("%s failure = %v", name, err)
		}
	}
	s.factory = func(backup.S3Config) (backup.ObjectStore, error) { return nil, errors.New("bad endpoint") }
	if err := s.Test(ctx, Settings{S3: conn()}); err == nil || !strings.Contains(err.Error(), "connect") {
		t.Fatalf("connect = %v", err)
	}
	s.factory = func(backup.S3Config) (backup.ObjectStore, error) { return mismatch{backup.NewMemoryStore()}, nil }
	if err := s.Test(ctx, Settings{S3: conn()}); err == nil || !strings.Contains(err.Error(), "did not match") {
		t.Fatalf("mismatch = %v", err)
	}
}

// mismatch returns other content than was written.
type mismatch struct{ *backup.MemoryStore }

func (m mismatch) Get(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("other")), nil
}
