package store

import (
	"fmt"
	"math"
	"testing"
	"time"
)

// fakeClock lets tests step through the snapshot coalescing window.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }
func useFakeClock(s *Store) *fakeClock {
	c := &fakeClock{t: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	s.now = c.now
	return c
}
func mustUpsert(t *testing.T, s *Store, owner, date, content, mood, weather string) *Diary {
	t.Helper()
	diary, _, err := s.UpsertDiary(owner, date, content, mood, weather)
	if err != nil {
		t.Fatalf("UpsertDiary(%q): %v", content, err)
	}
	return diary
}

func mustRevisions(t *testing.T, s *Store, owner, date string) []*DiaryRevision {
	t.Helper()
	revisions, err := s.ListDiaryRevisions(owner, date)
	if err != nil {
		t.Fatalf("ListDiaryRevisions: %v", err)
	}
	return revisions
}

func revisionContents(revisions []*DiaryRevision) []string {
	contents := make([]string, 0, len(revisions))
	for _, revision := range revisions {
		contents = append(contents, revision.Content)
	}
	return contents
}

func assertContents(t *testing.T, got []*DiaryRevision, want ...string) {
	t.Helper()
	contents := revisionContents(got)
	if fmt.Sprint(contents) != fmt.Sprint(want) {
		t.Fatalf("revision contents = %q, want %q", contents, want)
	}
}

func TestUpsertDiaryArchivesPreviousStateAndKeepsLatestInDiaries(t *testing.T) {
	s := newTestStore(t)
	s.SnapshotInterval = 0
	useFakeClock(s)
	user := newTestUser(t, s)

	first, created, err := s.UpsertDiary(user.ID, "2026-01-02", "v1", "😊", "☀️")
	if err != nil || !created {
		t.Fatalf("first upsert created=%v err=%v", created, err)
	}
	if revisions := mustRevisions(t, s, user.ID, "2026-01-02"); len(revisions) != 0 {
		t.Fatalf("a new entry must not create history, got %d", len(revisions))
	}

	second, created, err := s.UpsertDiary(user.ID, "2026-01-02", "v2", "😔", "🌧️")
	if err != nil || created {
		t.Fatalf("second upsert created=%v err=%v", created, err)
	}
	if second.ID != first.ID || second.Content != "v2" {
		t.Fatalf("diaries row should hold the latest state in place, got %+v", second)
	}

	revisions := mustRevisions(t, s, user.ID, "2026-01-02")
	assertContents(t, revisions, "v1")
	revision := revisions[0]
	if revision.Mood != "😊" || revision.Weather != "☀️" || revision.DiaryID != first.ID || revision.Owner != user.ID {
		t.Fatalf("revision fields = %+v", revision)
	}
	if revision.Date != "2026-01-02" || revision.Saved != first.Updated || revision.Created != "2026-01-02 03:04:05.000Z" {
		t.Fatalf("revision timestamps/date = %+v (first.Updated=%s)", revision, first.Updated)
	}

	// Reads never see history: the by-date lookup returns the latest state.
	start, end := dayRange("2026-01-02")
	latest, err := s.GetDiaryByDate(user.ID, start, end)
	if err != nil || latest.Content != "v2" {
		t.Fatalf("GetDiaryByDate = %+v, %v", latest, err)
	}
	if count := s.CountDiaries(user.ID); count != 1 {
		t.Fatalf("CountDiaries = %d, want 1", count)
	}
}

func TestUpsertDiarySkipsUnchangedSavesAndDuplicateSnapshots(t *testing.T) {
	s := newTestStore(t)
	s.SnapshotInterval = 0
	useFakeClock(s)
	user := newTestUser(t, s)

	mustUpsert(t, s, user.ID, "2026-01-02", "same", "", "")
	mustUpsert(t, s, user.ID, "2026-01-02", "same", "", "")
	if revisions := mustRevisions(t, s, user.ID, "2026-01-02"); len(revisions) != 0 {
		t.Fatalf("an unchanged save must not snapshot, got %q", revisionContents(revisions))
	}

	// Mood-only and weather-only changes are changes too.
	mustUpsert(t, s, user.ID, "2026-01-02", "same", "😊", "")
	mustUpsert(t, s, user.ID, "2026-01-02", "same", "😊", "☀️")
	revisions := mustRevisions(t, s, user.ID, "2026-01-02")
	if len(revisions) != 2 || revisions[0].Mood != "😊" || revisions[0].Weather != "" || revisions[1].Mood != "" {
		t.Fatalf("mood/weather snapshots = %+v", revisions)
	}
}

func TestUpsertDiaryCoalescesAutosavesWithinInterval(t *testing.T) {
	s := newTestStore(t)
	clock := useFakeClock(s)
	if s.SnapshotInterval != DefaultDiarySnapshotInterval {
		t.Fatalf("Open should set the default interval, got %v", s.SnapshotInterval)
	}
	user := newTestUser(t, s)

	mustUpsert(t, s, user.ID, "2026-01-02", "before session", "", "")
	clock.advance(time.Hour)
	// The first save of a session always keeps the state it replaces.
	mustUpsert(t, s, user.ID, "2026-01-02", "typing 1", "", "")
	for i := 2; i <= 5; i++ {
		clock.advance(30 * time.Second)
		mustUpsert(t, s, user.ID, "2026-01-02", fmt.Sprintf("typing %d", i), "", "")
	}
	assertContents(t, mustRevisions(t, s, user.ID, "2026-01-02"), "before session")

	// Once the window has passed, a checkpoint of the in-progress state is taken.
	clock.advance(DefaultDiarySnapshotInterval)
	mustUpsert(t, s, user.ID, "2026-01-02", "typing 6", "", "")
	assertContents(t, mustRevisions(t, s, user.ID, "2026-01-02"), "typing 5", "before session")
}

func TestUpsertDiaryPrunesOldestBeyondConfiguredLimit(t *testing.T) {
	s := newTestStore(t)
	s.SnapshotInterval = 0
	clock := useFakeClock(s)
	user := newTestUser(t, s)

	for i := 0; i <= DefaultDiarySnapshots+3; i++ {
		mustUpsert(t, s, user.ID, "2026-01-02", fmt.Sprintf("v%d", i), "", "")
		clock.advance(time.Second)
	}
	revisions := mustRevisions(t, s, user.ID, "2026-01-02")
	if len(revisions) != DefaultDiarySnapshots {
		t.Fatalf("kept %d snapshots, want %d", len(revisions), DefaultDiarySnapshots)
	}
	if revisions[0].Content != "v12" || revisions[len(revisions)-1].Content != "v3" {
		t.Fatalf("expected newest v12..oldest v3, got %q", revisionContents(revisions))
	}

	var stored int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM diary_revisions WHERE owner = ?`, user.ID).Scan(&stored); err != nil || stored != DefaultDiarySnapshots {
		t.Fatalf("stored rows = %d (%v), want pruned to %d", stored, err, DefaultDiarySnapshots)
	}

	// Lowering the limit hides extra snapshots at once and prunes on next save.
	if err := s.SetSetting(user.ID, SettingDiaryMaxSnapshots, 3, false); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	assertContents(t, mustRevisions(t, s, user.ID, "2026-01-02"), "v12", "v11", "v10")
	mustUpsert(t, s, user.ID, "2026-01-02", "v14", "", "")
	assertContents(t, mustRevisions(t, s, user.ID, "2026-01-02"), "v13", "v12", "v11")
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM diary_revisions WHERE owner = ?`, user.ID).Scan(&stored); err != nil || stored != 3 {
		t.Fatalf("stored rows after lowering limit = %d (%v), want 3", stored, err)
	}
}

func TestDiaryHistoryIsScopedPerEntryAndOwner(t *testing.T) {
	s := newTestStore(t)
	s.SnapshotInterval = 0
	useFakeClock(s)
	alice := newTestUser(t, s)
	bob := newTestUser(t, s)

	mustUpsert(t, s, alice.ID, "2026-01-02", "a1", "", "")
	mustUpsert(t, s, alice.ID, "2026-01-02", "a2", "", "")
	mustUpsert(t, s, alice.ID, "2026-01-03", "b1", "", "")
	mustUpsert(t, s, alice.ID, "2026-01-03", "b2", "", "")
	mustUpsert(t, s, bob.ID, "2026-01-02", "c1", "", "")
	mustUpsert(t, s, bob.ID, "2026-01-02", "c2", "", "")

	assertContents(t, mustRevisions(t, s, alice.ID, "2026-01-02"), "a1")
	assertContents(t, mustRevisions(t, s, alice.ID, "2026-01-03"), "b1")
	bobRevisions := mustRevisions(t, s, bob.ID, "2026-01-02")
	assertContents(t, bobRevisions, "c1")

	if _, err := s.GetDiaryRevision(alice.ID, bobRevisions[0].ID); !IsNoRows(err) {
		t.Fatalf("reading another user's revision must fail with no rows, got %v", err)
	}
	if _, err := s.RestoreDiaryRevision(alice.ID, bobRevisions[0].ID); !IsNoRows(err) {
		t.Fatalf("restoring another user's revision must fail with no rows, got %v", err)
	}
	got, err := s.GetDiaryRevision(bob.ID, bobRevisions[0].ID)
	if err != nil || got.Content != "c1" {
		t.Fatalf("GetDiaryRevision = %+v, %v", got, err)
	}

	// Accepts a full timestamp as the date, too.
	assertContents(t, mustRevisions(t, s, alice.ID, "2026-01-02 00:00:00.000Z"), "a1")
}

func TestRestoreDiaryRevisionArchivesCurrentAndCanBeUndone(t *testing.T) {
	s := newTestStore(t)
	clock := useFakeClock(s)
	user := newTestUser(t, s)

	mustUpsert(t, s, user.ID, "2026-01-02", "original", "😊", "☀️")
	clock.advance(time.Hour)
	mustUpsert(t, s, user.ID, "2026-01-02", "rewritten", "😔", "")
	original := mustRevisions(t, s, user.ID, "2026-01-02")[0]

	// Restoring is deliberate: it snapshots even inside the coalescing window.
	clock.advance(time.Second)
	restored, err := s.RestoreDiaryRevision(user.ID, original.ID)
	if err != nil {
		t.Fatalf("RestoreDiaryRevision: %v", err)
	}
	if restored.Content != "original" || restored.Mood != "😊" || restored.Weather != "☀️" {
		t.Fatalf("restored diary = %+v", restored)
	}
	revisions := mustRevisions(t, s, user.ID, "2026-01-02")
	assertContents(t, revisions, "rewritten", "original")

	// Undo the restore.
	clock.advance(time.Second)
	back, err := s.RestoreDiaryRevision(user.ID, revisions[0].ID)
	if err != nil || back.Content != "rewritten" {
		t.Fatalf("undo restore = %+v, %v", back, err)
	}
	assertContents(t, mustRevisions(t, s, user.ID, "2026-01-02"), "original", "rewritten", "original")

	// Restoring the version that is already current changes nothing.
	clock.advance(time.Second)
	current := mustRevisions(t, s, user.ID, "2026-01-02")[1]
	if _, err := s.RestoreDiaryRevision(user.ID, current.ID); err != nil {
		t.Fatalf("restore current: %v", err)
	}
	assertContents(t, mustRevisions(t, s, user.ID, "2026-01-02"), "original", "rewritten", "original")

	if _, err := s.RestoreDiaryRevision(user.ID, "missing"); !IsNoRows(err) {
		t.Fatalf("restoring a missing revision must fail with no rows, got %v", err)
	}
}

func TestDeleteDiaryArchivesFinalStateAndRestoreRecreatesEntry(t *testing.T) {
	s := newTestStore(t)
	clock := useFakeClock(s)
	user := newTestUser(t, s)

	diary := mustUpsert(t, s, user.ID, "2026-01-02", "keep me", "😊", "")
	clock.advance(time.Second)
	if err := s.DeleteDiary(diary.ID, user.ID); err != nil {
		t.Fatalf("DeleteDiary: %v", err)
	}
	if s.DiaryExistsByDate(user.ID, "2026-01-02") {
		t.Fatal("entry should be gone after delete")
	}
	revisions := mustRevisions(t, s, user.ID, "2026-01-02")
	assertContents(t, revisions, "keep me")

	restored, err := s.RestoreDiaryRevision(user.ID, revisions[0].ID)
	if err != nil || restored.Content != "keep me" || restored.Mood != "😊" {
		t.Fatalf("restore deleted entry = %+v, %v", restored, err)
	}
	if !s.DiaryExistsByDate(user.ID, "2026-01-02") {
		t.Fatal("restore should recreate the deleted entry")
	}
	// Nothing to archive when recreating, and the snapshot is still there.
	assertContents(t, mustRevisions(t, s, user.ID, "2026-01-02"), "keep me")

	// Deleting a missing or foreign entry still reports no rows.
	other := newTestUser(t, s)
	if err := s.DeleteDiary(restored.ID, other.ID); !IsNoRows(err) {
		t.Fatalf("DeleteDiary foreign owner err = %v", err)
	}
	if err := s.DeleteDiary("missing", user.ID); !IsNoRows(err) {
		t.Fatalf("DeleteDiary missing err = %v", err)
	}
}

func TestArchiveSkipsBlankEntries(t *testing.T) {
	s := newTestStore(t)
	s.SnapshotInterval = 0
	useFakeClock(s)
	user := newTestUser(t, s)

	blank := mustUpsert(t, s, user.ID, "2026-01-02", "", "", "")
	mustUpsert(t, s, user.ID, "2026-01-02", "now with text", "", "")
	if revisions := mustRevisions(t, s, user.ID, "2026-01-02"); len(revisions) != 0 {
		t.Fatalf("blank states must not be archived, got %q", revisionContents(revisions))
	}
	if err := s.DeleteDiary(blank.ID, user.ID); err != nil {
		t.Fatalf("DeleteDiary: %v", err)
	}
	assertContents(t, mustRevisions(t, s, user.ID, "2026-01-02"), "now with text")
}

func TestDiaryHistoryRemovedWithUser(t *testing.T) {
	s := newTestStore(t)
	s.SnapshotInterval = 0
	user := newTestUser(t, s)
	mustUpsert(t, s, user.ID, "2026-01-02", "v1", "", "")
	mustUpsert(t, s, user.ID, "2026-01-02", "v2", "", "")
	if _, err := s.DB.Exec(`DELETE FROM users WHERE id = ?`, user.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM diary_revisions`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("revisions after user delete = %d (%v)", count, err)
	}
}

func TestDiarySnapshotLimitParsingAndClamping(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)

	if got := s.DiarySnapshotLimit(user.ID); got != DefaultDiarySnapshots {
		t.Fatalf("unset limit = %d", got)
	}
	cases := []struct {
		value any
		want  int
	}{
		{5, 5},
		{20, 20},
		{50, MaxDiarySnapshots},
		{1e300, MaxDiarySnapshots},
		{5.9, 5},
		{0, MinDiarySnapshots},
		{-3, MinDiarySnapshots},
		{"7", 7},
		{" 8 ", 8},
		{"lots", DefaultDiarySnapshots},
		{true, DefaultDiarySnapshots},
		{nil, DefaultDiarySnapshots},
	}
	for _, tc := range cases {
		if err := s.SetSetting(user.ID, SettingDiaryMaxSnapshots, tc.value, false); err != nil {
			t.Fatalf("SetSetting(%v): %v", tc.value, err)
		}
		if got := s.DiarySnapshotLimit(user.ID); got != tc.want {
			t.Fatalf("limit for %#v = %d, want %d", tc.value, got, tc.want)
		}
	}
	if got := ClampDiarySnapshots(math.MaxInt); got != MaxDiarySnapshots {
		t.Fatalf("ClampDiarySnapshots(max) = %d", got)
	}
}

func TestDiaryHistoryTimeHelpersAndCorruptTimestamps(t *testing.T) {
	s := newTestStore(t)
	if got := s.currentTime(); time.Since(got) > time.Minute || got.Location() != time.UTC {
		t.Fatalf("currentTime without clock = %v", got)
	}
	clock := useFakeClock(s)
	user := newTestUser(t, s)

	mustUpsert(t, s, user.ID, "2026-01-02", "v1", "", "")
	mustUpsert(t, s, user.ID, "2026-01-02", "v2", "", "")
	// An unparseable snapshot time must not block future snapshots.
	if _, err := s.DB.Exec(`UPDATE diary_revisions SET created = 'garbage' WHERE owner = ?`, user.ID); err != nil {
		t.Fatalf("corrupt created: %v", err)
	}
	clock.advance(time.Second)
	mustUpsert(t, s, user.ID, "2026-01-02", "v3", "", "")
	if revisions := mustRevisions(t, s, user.ID, "2026-01-02"); len(revisions) != 2 {
		t.Fatalf("expected a new snapshot despite a corrupt timestamp, got %q", revisionContents(revisions))
	}
}

func TestDiaryHistoryDatabaseErrors(t *testing.T) {
	s := newTestStore(t)
	s.SnapshotInterval = 0
	user := newTestUser(t, s)
	diary := mustUpsert(t, s, user.ID, "2026-01-02", "v1", "", "")

	// Break the history table: saves that need to archive must fail as a
	// whole, leaving the latest state untouched.
	if _, err := s.DB.Exec(`ALTER TABLE diary_revisions RENAME TO diary_revisions_broken`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, _, err := s.UpsertDiary(user.ID, "2026-01-02", "v2", "", ""); err == nil {
		t.Fatal("UpsertDiary should fail when history cannot be written")
	}
	start, end := dayRange("2026-01-02")
	if latest, err := s.GetDiaryByDate(user.ID, start, end); err != nil || latest.Content != "v1" {
		t.Fatalf("failed save must roll back, got %+v, %v", latest, err)
	}
	if err := s.DeleteDiary(diary.ID, user.ID); err == nil {
		t.Fatal("DeleteDiary should fail when history cannot be written")
	}
	if !s.DiaryExistsByDate(user.ID, "2026-01-02") {
		t.Fatal("failed delete must roll back")
	}
	if _, err := s.ListDiaryRevisions(user.ID, "2026-01-02"); err == nil {
		t.Fatal("ListDiaryRevisions should fail without the table")
	}

	// Insert and prune failures are reported too.
	if _, err := s.DB.Exec(`ALTER TABLE diary_revisions_broken RENAME TO diary_revisions`); err != nil {
		t.Fatalf("rename back: %v", err)
	}
	if _, err := s.DB.Exec(`CREATE TRIGGER fail_revision_insert BEFORE INSERT ON diary_revisions BEGIN SELECT RAISE(ABORT, 'insert blocked'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if _, _, err := s.UpsertDiary(user.ID, "2026-01-02", "v2", "", ""); err == nil {
		t.Fatal("UpsertDiary should fail when the snapshot insert fails")
	}
	if _, err := s.DB.Exec(`DROP TRIGGER fail_revision_insert`); err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	if _, err := s.DB.Exec(`CREATE TRIGGER fail_revision_delete BEFORE DELETE ON diary_revisions BEGIN SELECT RAISE(ABORT, 'delete blocked'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if err := s.SetSetting(user.ID, SettingDiaryMaxSnapshots, 1, false); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	mustUpsert(t, s, user.ID, "2026-01-02", "v2", "", "")
	if _, _, err := s.UpsertDiary(user.ID, "2026-01-02", "v3", "", ""); err == nil {
		t.Fatal("UpsertDiary should fail when pruning fails")
	}

	// A broken diaries lookup inside the transaction is reported.
	if _, err := s.DB.Exec(`DROP TRIGGER fail_revision_delete`); err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	if _, err := s.DB.Exec(`ALTER TABLE diaries RENAME TO diaries_broken`); err != nil {
		t.Fatalf("rename diaries: %v", err)
	}
	if _, _, err := s.UpsertDiary(user.ID, "2026-01-02", "v4", "", ""); err == nil {
		t.Fatal("UpsertDiary should fail without the diaries table")
	}
	revisions, err := s.ListDiaryRevisions(user.ID, "2026-01-02")
	if err != nil || len(revisions) == 0 {
		t.Fatalf("ListDiaryRevisions = %v, %v", revisions, err)
	}
	if _, err := s.RestoreDiaryRevision(user.ID, revisions[0].ID); err == nil {
		t.Fatal("RestoreDiaryRevision should fail without the diaries table")
	}
}

func TestDiaryHistoryRowScanError(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	// A NULL in a NOT NULL-less copy of the table makes scanning fail.
	if _, err := s.DB.Exec(`DROP TABLE diary_revisions`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := s.DB.Exec(`CREATE TABLE diary_revisions (content TEXT, created TEXT, date TEXT, diary TEXT, id TEXT, mood TEXT, owner TEXT, saved TEXT, weather TEXT)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.DB.Exec(`INSERT INTO diary_revisions(id, owner, date, content) VALUES('r1', ?, '2026-01-02', NULL)`, user.ID); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := s.ListDiaryRevisions(user.ID, "2026-01-02"); err == nil {
		t.Fatal("ListDiaryRevisions should surface scan errors")
	}
	// The latest-snapshot lookup inside archiving surfaces them too.
	diary := &Diary{ID: "d1", Owner: user.ID, Date: "2026-01-02 00:00:00.000Z", Content: "x"}
	tx, err := s.DB.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	if err := s.archiveDiary(tx, diary, 10, false); err == nil {
		t.Fatal("archiveDiary should surface scan errors")
	}
}
