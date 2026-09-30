package medialib

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/songtianlun/diarum/internal/audit"
	"github.com/songtianlun/diarum/internal/store"
)

type testEnv struct {
	store *store.Store
	audit *audit.Logger
	svc   *Service
	user  *store.User
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	l, err := audit.New(s.DataDir, audit.Options{Location: time.UTC})
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(l.Close)
	user, err := s.CreateUser("alice", "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return &testEnv{store: s, audit: l, svc: New(s, l), user: user}
}

func (env *testEnv) entries(t *testing.T, action string) []audit.Entry {
	t.Helper()
	env.audit.Flush()
	result, err := env.audit.Search(audit.Query{Actions: []string{action}, Limit: 100})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	return result.Entries
}

func (env *testEnv) addMedia(t *testing.T, id string, diary []string) *store.Media {
	t.Helper()
	media, err := env.store.InsertImportedMedia(env.user.ID, id, id+".png", id, "", diary)
	if err != nil {
		t.Fatalf("InsertImportedMedia: %v", err)
	}
	return media
}

func (env *testEnv) later(days int) {
	env.svc.now = func() time.Time { return time.Now().AddDate(0, 0, days) }
}

func TestLoadSettings(t *testing.T) {
	env := newTestEnv(t)
	if got := env.svc.LoadSettings(env.user.ID); got.TrashRetentionDays != DefaultTrashRetentionDays || got.AutoCleanUnlinked {
		t.Fatalf("defaults = %+v", got)
	}
	_ = env.store.SetSetting(env.user.ID, SettingTrashRetentionDays, 0, false)
	_ = env.store.SetSetting(env.user.ID, SettingAutoCleanUnlinked, true, false)
	if got := env.svc.LoadSettings(env.user.ID); got.TrashRetentionDays != 0 || !got.AutoCleanUnlinked {
		t.Fatalf("saved = %+v", got)
	}
	for _, invalid := range []any{-1, MaxTrashRetentionDays + 1, 1.5, "7"} {
		_ = env.store.SetSetting(env.user.ID, SettingTrashRetentionDays, invalid, false)
		if got := env.svc.LoadSettings(env.user.ID); got.TrashRetentionDays != DefaultTrashRetentionDays {
			t.Errorf("retention %v -> %d, want default", invalid, got.TrashRetentionDays)
		}
	}
}

func TestToInt(t *testing.T) {
	cases := []struct {
		in   any
		want int
		ok   bool
	}{
		{float64(7), 7, true}, {7.5, 7, false}, {3, 3, true}, {int64(4), 4, true}, {"5", 0, false}, {nil, 0, false},
	}
	for _, c := range cases {
		if got, ok := toInt(c.in); got != c.want || ok != c.ok {
			t.Errorf("toInt(%v) = %d, %v", c.in, got, ok)
		}
	}
}

func TestRunUserTrashesUnlinkedAndPurgesExpired(t *testing.T) {
	env := newTestEnv(t)
	diary, _, err := env.store.UpsertDiary(env.user.ID, "2024-05-06", `<img src="/api/v1/files/media/used/used.png">`, "", "")
	if err != nil {
		t.Fatalf("UpsertDiary: %v", err)
	}
	env.addMedia(t, "used", []string{diary.ID})
	env.addMedia(t, "unused", nil)

	// Nothing to do by default: auto clean is off and nothing is in the trash.
	if got := env.svc.RunUser(env.user.ID); got != (Result{}) {
		t.Fatalf("default run = %+v", got)
	}

	_ = env.store.SetSetting(env.user.ID, SettingAutoCleanUnlinked, true, false)
	// Images uploaded within the grace period are left alone.
	if got := env.svc.RunUser(env.user.ID); got.Trashed != 0 {
		t.Fatalf("fresh upload trashed: %+v", got)
	}
	env.later(2)
	if got := env.svc.RunUser(env.user.ID); got.Trashed != 1 || got.Purged != 0 {
		t.Fatalf("second run = %+v", got)
	}
	trashed, err := env.store.GetMedia("unused", env.user.ID)
	if err != nil || !trashed.InTrash() || trashed.DeleteReason != store.MediaReasonUnlinked || trashed.DeleteTrigger != store.MediaTriggerAuto {
		t.Fatalf("unused image = %+v %v", trashed, err)
	}
	if used, _ := env.store.GetMedia("used", env.user.ID); used.InTrash() {
		t.Fatal("image used by an entry went to the trash")
	}

	env.later(DefaultTrashRetentionDays + 2)
	if got := env.svc.RunUser(env.user.ID); got.Purged != 1 {
		t.Fatalf("expired run = %+v", got)
	}
	if _, err := env.store.GetMedia("unused", env.user.ID); !store.IsNoRows(err) {
		t.Fatalf("expired image still there: %v", err)
	}

	trash := env.entries(t, audit.ActionMediaTrash)
	if len(trash) != 1 || trash[0].Source != audit.SourceSystem || trash[0].User != "alice" || trash[0].Detail["trigger"] != store.MediaTriggerAuto || trash[0].Detail["reason"] != store.MediaReasonUnlinked {
		t.Fatalf("trash audit = %+v", trash)
	}
	purge := env.entries(t, audit.ActionMediaPurge)
	if len(purge) != 1 || purge[0].Detail["reason"] != store.MediaReasonRetention || purge[0].Detail["retention_days"] != float64(DefaultTrashRetentionDays) {
		t.Fatalf("purge audit = %+v", purge)
	}
}

func TestRunUserKeepsTrashWhenRetentionIsOff(t *testing.T) {
	env := newTestEnv(t)
	env.addMedia(t, "m1", nil)
	if _, err := env.store.TrashMedia("m1", env.user.ID, store.TrashInfo{By: "alice", Reason: store.MediaReasonUser, Trigger: store.MediaTriggerManual}); err != nil {
		t.Fatalf("TrashMedia: %v", err)
	}
	_ = env.store.SetSetting(env.user.ID, SettingTrashRetentionDays, 0, false)
	env.later(5000)
	if got := env.svc.RunUser(env.user.ID); got != (Result{}) {
		t.Fatalf("run = %+v", got)
	}
	if media, err := env.store.GetMedia("m1", env.user.ID); err != nil || !media.InTrash() {
		t.Fatalf("trashed image = %+v %v", media, err)
	}
}

func TestRunUserRecordsFailedPurge(t *testing.T) {
	env := newTestEnv(t)
	env.addMedia(t, "m1", nil)
	if _, err := env.store.TrashMedia("m1", env.user.ID, store.TrashInfo{By: "alice", Reason: store.MediaReasonUser, Trigger: store.MediaTriggerManual}); err != nil {
		t.Fatalf("TrashMedia: %v", err)
	}
	// A non-empty directory where the file should be cannot be removed.
	path := env.store.NewMediaFilePath("m1", "m1.png")
	if err := os.MkdirAll(filepath.Join(path, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	env.later(DefaultTrashRetentionDays + 1)
	if got := env.svc.RunUser(env.user.ID); got.Failed != 1 || got.Purged != 0 {
		t.Fatalf("run = %+v", got)
	}
	if media, err := env.store.GetMedia("m1", env.user.ID); err != nil || !media.InTrash() {
		t.Fatalf("image should stay in the trash for a retry: %+v %v", media, err)
	}
	purge := env.entries(t, audit.ActionMediaPurge)
	if len(purge) != 1 || purge[0].Detail["error"] == nil {
		t.Fatalf("purge audit = %+v", purge)
	}
}

func TestRunUserSkipsConcurrentRun(t *testing.T) {
	env := newTestEnv(t)
	if !env.svc.begin(env.user.ID) {
		t.Fatal("begin should succeed")
	}
	_ = env.store.SetSetting(env.user.ID, SettingAutoCleanUnlinked, true, false)
	env.addMedia(t, "m1", nil)
	env.later(2)
	if got := env.svc.RunUser(env.user.ID); got != (Result{}) {
		t.Fatalf("concurrent run = %+v", got)
	}
	env.svc.end(env.user.ID)
	if got := env.svc.RunUser(env.user.ID); got.Trashed != 1 {
		t.Fatalf("run after end = %+v", got)
	}
}

func TestRunAllCoversEveryOwner(t *testing.T) {
	env := newTestEnv(t)
	other, err := env.store.CreateUser("bob", "bob@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	env.addMedia(t, "a1", nil)
	if _, err := env.store.InsertImportedMedia(other.ID, "b1", "b1.png", "b1", "", nil); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{env.user.ID, other.ID} {
		_ = env.store.SetSetting(id, SettingAutoCleanUnlinked, true, false)
	}
	env.later(2)
	env.svc.RunAll()
	for id, owner := range map[string]string{"a1": env.user.ID, "b1": other.ID} {
		if media, err := env.store.GetMedia(id, owner); err != nil || !media.InTrash() {
			t.Errorf("%s = %+v %v", id, media, err)
		}
	}

	// A broken database is logged, not fatal.
	_ = env.store.Close()
	env.svc.RunAll()
}

func TestRunAllRecoversFromPanic(t *testing.T) {
	svc := New(nil, nil)
	svc.RunAll()
}

func TestStartRunsUntilCancelled(t *testing.T) {
	env := newTestEnv(t)
	env.svc.startDelay, env.svc.interval = time.Millisecond, time.Millisecond
	_ = env.store.SetSetting(env.user.ID, SettingAutoCleanUnlinked, true, false)
	env.addMedia(t, "m1", nil)
	env.later(2)
	ctx, cancel := context.WithCancel(context.Background())
	env.svc.Start(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for {
		env.svc.mu.Lock()
		busy := env.svc.running[env.user.ID]
		env.svc.mu.Unlock()
		media, err := env.store.GetMedia("m1", env.user.ID)
		if !busy && err == nil && media.InTrash() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("housekeeping did not run")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	// Let the loop observe the cancellation before the store closes.
	time.Sleep(20 * time.Millisecond)
}

func TestOnStoreEventRecordsRestore(t *testing.T) {
	env := newTestEnv(t)
	env.store.MediaEvent = env.svc.OnStoreEvent
	env.svc.OnStoreEvent(audit.ActionMediaRestore, nil, nil)

	diary, _, err := env.store.UpsertDiary(env.user.ID, "2024-01-02", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	env.addMedia(t, "m1", nil)
	if _, err := env.store.TrashMedia("m1", env.user.ID, store.TrashInfo{By: "system", Reason: store.MediaReasonUnlinked, Trigger: store.MediaTriggerAuto}); err != nil {
		t.Fatal(err)
	}
	if err := env.store.LinkDiaryMedia(env.user.ID, diary.ID, `<img src="/api/v1/files/media/m1/m1.png">`); err != nil {
		t.Fatalf("LinkDiaryMedia: %v", err)
	}
	restores := env.entries(t, audit.ActionMediaRestore)
	if len(restores) != 1 || restores[0].Detail["reason"] != store.MediaReasonReferenced || restores[0].Detail["diary"] != diary.ID || restores[0].Detail["trigger"] != store.MediaTriggerAuto {
		t.Fatalf("restore audit = %+v", restores)
	}
}

func TestDetail(t *testing.T) {
	minimal := Detail(&store.Media{ID: "m", File: "f.png"}, store.MediaTriggerManual, "")
	if len(minimal) != 3 {
		t.Fatalf("minimal detail = %v", minimal)
	}
	full := Detail(&store.Media{
		ID: "m", File: "f.png", Date: "2024-01-02 00:00:00.000Z", Created: "2024-01-03 10:00:00.000Z",
		Storage: store.MediaStorageS3, Deleted: "2024-02-01 00:00:00.000Z", DeletedBy: "alice",
	}, store.MediaTriggerAuto, store.MediaReasonRetention)
	want := map[string]any{
		"reason": store.MediaReasonRetention, "image_date": "2024-01-02", "uploaded": "2024-01-03 10:00:00.000Z",
		"storage": store.MediaStorageS3, "trashed": "2024-02-01 00:00:00.000Z", "trashed_by": "alice",
	}
	for k, v := range want {
		if full[k] != v {
			t.Errorf("detail[%s] = %v, want %v", k, full[k], v)
		}
	}
}

func TestUsernameOfMissingUser(t *testing.T) {
	env := newTestEnv(t)
	if got := env.svc.username("missing"); got != "" {
		t.Fatalf("username = %q", got)
	}
}
