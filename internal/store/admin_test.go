package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openAdminTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestFirstUserBecomesAdmin(t *testing.T) {
	s := openAdminTestStore(t)
	first, err := s.CreateUser("first", "first@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateUser("second", "second@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if first.Role != RoleAdmin || second.Role != RoleUser {
		t.Fatalf("roles = %s, %s", first.Role, second.Role)
	}
	if n, err := s.CountAdmins(); err != nil || n != 1 {
		t.Fatalf("admins = %d (%v)", n, err)
	}
}

func TestMigrationAddsRoleAndPromotesEarliestUser(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"old", "newer"} {
		if _, err := s.CreateUser(name, name+"@example.com", "hash"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetSetting(mustUserID(t, s, "old"), legacyAuditRetentionKey, 7, false); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Simulate a database from before roles: drop the column.
	db, err := sql.Open("sqlite", filepath.Join(dir, DatabaseName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE users DROP COLUMN role`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE system_settings`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	old, _ := s.GetUserByIdentity("old")
	newer, _ := s.GetUserByIdentity("newer")
	if old.Role != RoleAdmin || newer.Role != RoleUser {
		t.Fatalf("roles after migration = %s, %s", old.Role, newer.Role)
	}
	if _, err := s.GetSetting(old.ID, legacyAuditRetentionKey); err == nil {
		t.Fatal("legacy per-user audit setting kept")
	}
	if err := s.SetSystemSetting("k", "v"); err != nil {
		t.Fatal(err)
	}
}

func mustUserID(t *testing.T, s *Store, name string) string {
	t.Helper()
	user, err := s.GetUserByIdentity(name)
	if err != nil {
		t.Fatal(err)
	}
	return user.ID
}

func TestSystemSettings(t *testing.T) {
	s := openAdminTestStore(t)
	if _, ok, err := s.GetSystemSetting("audit.settings"); ok || err != nil {
		t.Fatalf("missing setting: ok=%v err=%v", ok, err)
	}
	for _, value := range []string{`{"a":1}`, `{"a":2}`} {
		if err := s.SetSystemSetting("audit.settings", value); err != nil {
			t.Fatal(err)
		}
	}
	if value, ok, err := s.GetSystemSetting("audit.settings"); !ok || err != nil || value != `{"a":2}` {
		t.Fatalf("setting = %q %v %v", value, ok, err)
	}
	s.Close()
	if _, _, err := s.GetSystemSetting("x"); err == nil {
		t.Fatal("closed store should fail")
	}
}

func TestFindUserAndSetRole(t *testing.T) {
	s := openAdminTestStore(t)
	user, _ := s.CreateUser("alice", "alice@example.com", "hash")
	for _, identity := range []string{user.ID, "alice", "alice@example.com", "  alice  "} {
		found, err := s.FindUser(identity)
		if err != nil || found.ID != user.ID {
			t.Fatalf("FindUser(%q) = %v, %v", identity, found, err)
		}
	}
	if _, err := s.FindUser(" "); err == nil {
		t.Fatal("blank identity found a user")
	}
	if _, err := s.FindUser("nobody"); err == nil {
		t.Fatal("unknown identity found a user")
	}
	if err := s.SetUserRole(user.ID, "root"); err != ErrInvalidRole {
		t.Fatalf("invalid role err = %v", err)
	}
	if err := s.SetUserRole("missing", RoleAdmin); err != ErrNotFound {
		t.Fatalf("missing user err = %v", err)
	}
	if err := s.SetUserRole(user.ID, RoleUser); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetUserByID(user.ID); got.Role != RoleUser {
		t.Fatalf("role = %s", got.Role)
	}
	s.Close()
	if err := s.SetUserRole(user.ID, RoleAdmin); err == nil {
		t.Fatal("closed store should fail")
	}
}

func TestListUsers(t *testing.T) {
	s := openAdminTestStore(t)
	alice, _ := s.CreateUser("alice", "alice@example.com", "hash")
	bob, _ := s.CreateUser("bob", "bob@example.com", "hash")
	s.CreateUser("carol_x", "carol@example.com", "hash")
	for _, date := range []string{"2026-09-01", "2026-09-02"} {
		if _, _, err := s.UpsertDiary(bob.ID, date+" 00:00:00.000Z", "hello", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreateMedia(alice.ID, "a.png", "a.png", "", nil); err != nil {
		t.Fatal(err)
	}

	users, total, err := s.ListUsers(UserListOptions{})
	if err != nil || total != 3 || len(users) != 3 || users[0].Username != "alice" || users[0].Role != RoleAdmin || users[0].Media != 1 {
		t.Fatalf("list = %+v total=%d err=%v", users, total, err)
	}
	busiest, _, _ := s.ListUsers(UserListOptions{Sort: "-diaries"})
	if busiest[0].ID != bob.ID || busiest[0].Diaries != 2 || busiest[0].LastDiaryAt == "" {
		t.Fatalf("-diaries = %+v", busiest[0])
	}
	for _, sort := range []string{"-created", "username", "diaries", "last_active", "bogus"} {
		if _, n, err := s.ListUsers(UserListOptions{Sort: sort}); err != nil || n != 3 {
			t.Fatalf("sort %s: %d %v", sort, n, err)
		}
	}
	page, total, _ := s.ListUsers(UserListOptions{Limit: 1, Offset: 1, Sort: "username"})
	if total != 3 || len(page) != 1 || page[0].Username != "bob" {
		t.Fatalf("page = %+v", page)
	}
	// Search escapes LIKE wildcards: "_" only matches a literal underscore.
	matched, n, _ := s.ListUsers(UserListOptions{Query: "_X"})
	if n != 1 || matched[0].Username != "carol_x" {
		t.Fatalf("search = %+v", matched)
	}
	admins, n, _ := s.ListUsers(UserListOptions{Role: RoleAdmin})
	if n != 1 || admins[0].ID != alice.ID {
		t.Fatalf("admins = %+v", admins)
	}

	names, err := s.UserNames()
	if err != nil || names[bob.ID] != "bob" || len(names) != 3 {
		t.Fatalf("names = %v %v", names, err)
	}

	s.Close()
	if _, _, err := s.ListUsers(UserListOptions{}); err == nil {
		t.Fatal("closed store should fail")
	}
	if _, err := s.UserNames(); err == nil {
		t.Fatal("closed store should fail")
	}
	if _, err := s.CountAdmins(); err == nil {
		t.Fatal("closed store should fail")
	}
}

func TestSystemStats(t *testing.T) {
	s := openAdminTestStore(t)
	user, _ := s.CreateUser("alice", "alice@example.com", "hash")
	s.CreateUser("bob", "bob@example.com", "hash")
	today := time.Now().UTC().Format("2006-01-02")
	if _, _, err := s.UpsertDiary(user.ID, today+" 00:00:00.000Z", "hi", "", ""); err != nil {
		t.Fatal(err)
	}
	stats, err := s.SystemStats(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Users != 2 || stats.Admins != 1 || stats.NewUsers30d != 2 || stats.ActiveUsers7d != 1 || stats.Diaries != 1 || stats.DiariesToday != 1 || stats.Diaries7d != 1 || stats.DatabaseBytes == 0 {
		t.Fatalf("stats = %+v", stats)
	}
	if len(stats.DailyDiaries) != statsSeriesDays || stats.DailyDiaries[statsSeriesDays-1].Date != today || stats.DailyDiaries[statsSeriesDays-1].Count != 1 {
		t.Fatalf("daily diaries = %+v", stats.DailyDiaries[statsSeriesDays-1])
	}
	if stats.DailyUsers[statsSeriesDays-1].Count != 2 {
		t.Fatalf("daily users = %+v", stats.DailyUsers[statsSeriesDays-1])
	}
	if _, err := s.dailySeries(`SELECT 1, 2, 3`, today, time.Now()); err == nil {
		t.Fatal("bad scan should fail")
	}
	s.Close()
	if _, err := s.SystemStats(time.Now()); err == nil {
		t.Fatal("closed store should fail")
	}
	if _, err := s.dailySeries(`SELECT 1`, today, time.Now()); err == nil {
		t.Fatal("closed store should fail")
	}
}

func TestColumnExistsErrors(t *testing.T) {
	s := openAdminTestStore(t)
	if ok, err := columnExists(s.DB, "users", "ROLE"); !ok || err != nil {
		t.Fatalf("role column: %v %v", ok, err)
	}
	s.Close()
	if _, err := columnExists(s.DB, "users", "role"); err == nil {
		t.Fatal("closed db should fail")
	}
	if err := migrateAdminSchema(s.DB); err == nil {
		t.Fatal("closed db should fail")
	}
}
