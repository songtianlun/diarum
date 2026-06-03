package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()

	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

func newTestUser(t *testing.T, s *Store) *User {
	t.Helper()

	id, err := GenerateID()
	if err != nil {
		t.Fatalf("generate id: %v", err)
	}
	user, err := s.CreateUser("user_"+id, id+"@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newMockS3Client(t *testing.T, fn roundTripFunc) *awss3.Client {
	t.Helper()

	cfg := aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("key", "secret", ""),
		HTTPClient:  &http.Client{Transport: fn},
		Retryer: func() aws.Retryer {
			return aws.NopRetryer{}
		},
	}
	return awss3.NewFromConfig(cfg, func(o *awss3.Options) {
		o.UsePathStyle = true
		o.BaseEndpoint = aws.String("https://mock-s3.local")
	})
}

func httpResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestOpenInitializesMetadata(t *testing.T) {
	s := newTestStore(t)

	if got := s.MediaCollectionID; got != DefaultMediaCollectionID {
		t.Fatalf("MediaCollectionID = %q, want %q", got, DefaultMediaCollectionID)
	}
	if len(s.AuthSecret) == 0 {
		t.Fatal("AuthSecret should be initialized")
	}
	if _, err := os.Stat(filepath.Join(s.DataDir, DatabaseName)); err != nil {
		t.Fatalf("database should exist: %v", err)
	}

	secret, err := getMeta(s.DB, "auth.secret")
	if err != nil {
		t.Fatalf("getMeta auth.secret: %v", err)
	}
	if secret == "" {
		t.Fatal("auth.secret should be persisted")
	}

	mediaCollectionID, err := getMeta(s.DB, "legacy.media_collection_id")
	if err != nil {
		t.Fatalf("getMeta legacy.media_collection_id: %v", err)
	}
	if mediaCollectionID != DefaultMediaCollectionID {
		t.Fatalf("legacy.media_collection_id = %q, want %q", mediaCollectionID, DefaultMediaCollectionID)
	}
}

func TestStoreUserDiarySettingsConversationFlows(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)

	gotUser, err := s.GetUserByID(user.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if gotUser.Username != user.Username {
		t.Fatalf("GetUserByID username = %q, want %q", gotUser.Username, user.Username)
	}

	gotByIdentity, err := s.GetUserByIdentity("  " + user.Email + " ")
	if err != nil {
		t.Fatalf("GetUserByIdentity: %v", err)
	}
	if gotByIdentity.ID != user.ID {
		t.Fatalf("GetUserByIdentity ID = %q, want %q", gotByIdentity.ID, user.ID)
	}

	err = s.Transaction(context.Background(), func(tx *sql.Tx) error {
		_, execErr := tx.Exec(`INSERT INTO user_settings(created, encrypted, id, key, updated, user, value) VALUES(?, ?, ?, ?, ?, ?, ?)`,
			nowString(), false, "tx-setting", "tx.key", nowString(), user.ID, `"ok"`)
		return execErr
	})
	if err != nil {
		t.Fatalf("Transaction commit: %v", err)
	}
	value, err := s.GetSetting(user.ID, "tx.key")
	if err != nil {
		t.Fatalf("GetSetting tx.key: %v", err)
	}
	if value != "ok" {
		t.Fatalf("GetSetting tx.key = %#v, want %q", value, "ok")
	}

	rollbackErr := s.Transaction(context.Background(), func(tx *sql.Tx) error {
		_, execErr := tx.Exec(`INSERT INTO user_settings(created, encrypted, id, key, updated, user, value) VALUES(?, ?, ?, ?, ?, ?, ?)`,
			nowString(), false, "tx-setting-rollback", "tx.rollback", nowString(), user.ID, `"nope"`)
		if execErr != nil {
			return execErr
		}
		return errors.New("rollback")
	})
	if rollbackErr == nil {
		t.Fatal("Transaction should return rollback error")
	}
	if _, err := s.GetSetting(user.ID, "tx.rollback"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("rolled back setting error = %v, want sql.ErrNoRows", err)
	}

	created, inserted, err := s.UpsertDiary(user.ID, "2024-05-20", "first entry", "happy", "sunny")
	if err != nil {
		t.Fatalf("UpsertDiary create: %v", err)
	}
	if !inserted {
		t.Fatal("expected first UpsertDiary to insert")
	}
	if created.Date != "2024-05-20 00:00:00.000Z" {
		t.Fatalf("created diary date = %q", created.Date)
	}

	updated, inserted, err := s.UpsertDiary(user.ID, "2024-05-20", "updated entry", "calm", "rain")
	if err != nil {
		t.Fatalf("UpsertDiary update: %v", err)
	}
	if inserted {
		t.Fatal("expected second UpsertDiary to update existing diary")
	}
	if updated.ID != created.ID || updated.Content != "updated entry" {
		t.Fatalf("updated diary = %#v", updated)
	}

	other, err := s.InsertImportedDiary(user.ID, "", "2024-05-21", "searchable content", "focused", "cloudy")
	if err != nil {
		t.Fatalf("InsertImportedDiary: %v", err)
	}

	byDate, err := s.GetDiaryByDate(user.ID, "2024-05-20 00:00:00.000Z", "2024-05-20 23:59:59.999Z")
	if err != nil {
		t.Fatalf("GetDiaryByDate: %v", err)
	}
	if byDate.ID != created.ID {
		t.Fatalf("GetDiaryByDate ID = %q, want %q", byDate.ID, created.ID)
	}

	listed, err := s.ListDiaries(user.ID, "2024-05-20 00:00:00.000Z", "2024-05-21 23:59:59.999Z", "-date", 1)
	if err != nil {
		t.Fatalf("ListDiaries: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != other.ID {
		t.Fatalf("ListDiaries result = %#v", listed)
	}

	searchResults, err := s.SearchDiaries(user.ID, "searchable", 0)
	if err != nil {
		t.Fatalf("SearchDiaries: %v", err)
	}
	if len(searchResults) != 1 || searchResults[0].ID != other.ID {
		t.Fatalf("SearchDiaries result = %#v", searchResults)
	}
	if got := s.CountDiaries(user.ID); got != 2 {
		t.Fatalf("CountDiaries = %d, want 2", got)
	}
	if !s.DiaryExistsByDate(user.ID, "2024-05-20") {
		t.Fatal("DiaryExistsByDate should be true")
	}

	if err := s.SetSetting(user.ID, "api.token", "secret-token", false); err != nil {
		t.Fatalf("SetSetting api.token: %v", err)
	}
	if err := s.SetSetting(user.ID, "api.enabled", true, false); err != nil {
		t.Fatalf("SetSetting api.enabled: %v", err)
	}
	if err := s.SetSetting(user.ID, "json.setting", map[string]any{"enabled": true}, false); err != nil {
		t.Fatalf("SetSetting json.setting: %v", err)
	}

	apiTokenUser, err := s.ValidateAPIToken("secret-token")
	if err != nil {
		t.Fatalf("ValidateAPIToken enabled: %v", err)
	}
	if apiTokenUser != user.ID {
		t.Fatalf("ValidateAPIToken user = %q, want %q", apiTokenUser, user.ID)
	}

	if err := s.SetSetting(user.ID, "api.enabled", false, false); err != nil {
		t.Fatalf("disable api.enabled: %v", err)
	}
	if _, err := s.ValidateAPIToken("secret-token"); err == nil || err.Error() != "api disabled" {
		t.Fatalf("ValidateAPIToken disabled error = %v", err)
	}

	settings, err := s.GetSettings(user.ID)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if settings["api.token"] != "secret-token" {
		t.Fatalf("GetSettings api.token = %#v", settings["api.token"])
	}
	if setting := settings["json.setting"].(map[string]any); setting["enabled"] != true {
		t.Fatalf("GetSettings json.setting = %#v", settings["json.setting"])
	}

	if err := s.DeleteSetting(user.ID, "json.setting"); err != nil {
		t.Fatalf("DeleteSetting: %v", err)
	}
	if _, err := s.GetSetting(user.ID, "json.setting"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted setting error = %v, want sql.ErrNoRows", err)
	}

	conv, err := s.CreateConversation(user.ID, "My Conversation")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if _, err := s.InsertImportedConversation(user.ID, "", "bad"); err == nil {
		t.Fatal("InsertImportedConversation should require ID")
	}
	conversations, err := s.ListConversations(user.ID, 0)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(conversations) != 1 || conversations[0].ID != conv.ID {
		t.Fatalf("ListConversations result = %#v", conversations)
	}

	msg, err := s.CreateMessage(user.ID, conv.ID, "user", "hello", []string{created.ID})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if _, err := s.InsertImportedMessage(user.ID, "", conv.ID, "assistant", "bad", nil); err == nil {
		t.Fatal("InsertImportedMessage should require ID")
	}
	gotMsg, err := s.GetMessage(msg.ID)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if len(gotMsg.ReferencedDiaries) != 1 || gotMsg.ReferencedDiaries[0] != created.ID {
		t.Fatalf("GetMessage referenced diaries = %#v", gotMsg.ReferencedDiaries)
	}
	messageCount, err := s.CountMessages(conv.ID)
	if err != nil {
		t.Fatalf("CountMessages: %v", err)
	}
	if messageCount != 1 {
		t.Fatalf("CountMessages = %d, want 1", messageCount)
	}

	msgs, err := s.ListMessages(conv.ID, 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].ID != msg.ID {
		t.Fatalf("ListMessages result = %#v", msgs)
	}

	renamed, err := s.UpdateConversationTitle(conv.ID, user.ID, "Renamed")
	if err != nil {
		t.Fatalf("UpdateConversationTitle: %v", err)
	}
	if renamed.Title != "Renamed" {
		t.Fatalf("updated title = %q, want Renamed", renamed.Title)
	}

	if err := s.DeleteConversation(conv.ID, user.ID); err != nil {
		t.Fatalf("DeleteConversation: %v", err)
	}
	if err := s.DeleteConversation(conv.ID, user.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("DeleteConversation second error = %v, want sql.ErrNoRows", err)
	}

	if err := s.DeleteDiary(created.ID, user.ID); err != nil {
		t.Fatalf("DeleteDiary: %v", err)
	}
	if err := s.DeleteDiary(created.ID, user.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("DeleteDiary second error = %v, want sql.ErrNoRows", err)
	}
}

func TestStoreMediaAndFileHelpers(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)

	if err := s.SetSetting(user.ID, "image_upload.local.path", "custom-media", false); err != nil {
		t.Fatalf("SetSetting local path: %v", err)
	}
	if err := s.SetSetting(user.ID, "image_upload.provider", "local", false); err != nil {
		t.Fatalf("SetSetting provider: %v", err)
	}

	diary, err := s.InsertImportedDiary(user.ID, "", "2024-06-01", "linked diary", "", "")
	if err != nil {
		t.Fatalf("InsertImportedDiary: %v", err)
	}

	media, err := s.CreateMedia(user.ID, "photo.png", "Photo", "Alt text", []string{diary.ID})
	if err != nil {
		t.Fatalf("CreateMedia: %v", err)
	}
	if got := s.NewMediaFilePath(media.ID, media.File); !strings.HasSuffix(got, filepath.Join(DefaultMediaCollectionID, media.ID, media.File)) {
		t.Fatalf("NewMediaFilePath = %q", got)
	}
	if got := s.userLocalMediaDir(user.ID); got != filepath.Join(s.DataDir, "custom-media") {
		t.Fatalf("userLocalMediaDir = %q", got)
	}
	if got := s.imageUploadProvider(user.ID); got != "local" {
		t.Fatalf("imageUploadProvider = %q, want local", got)
	}

	if err := s.SaveUploadedMedia(media, bytes.NewBufferString("media-body")); err != nil {
		t.Fatalf("SaveUploadedMedia: %v", err)
	}
	path := s.MediaFilePath(media)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("MediaFilePath should exist: %v", err)
	}

	reader, err := s.OpenMediaFile(media)
	if err != nil {
		t.Fatalf("OpenMediaFile: %v", err)
	}
	content, err := ioReadAllAndClose(reader)
	if err != nil {
		t.Fatalf("OpenMediaFile read: %v", err)
	}
	if string(content) != "media-body" {
		t.Fatalf("OpenMediaFile content = %q, want media-body", string(content))
	}

	items, total, err := s.ListMedia(user.ID, 0, 0)
	if err != nil {
		t.Fatalf("ListMedia: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("ListMedia total/items = %d/%d", total, len(items))
	}
	expand, ok := items[0].Expand["diary"].([]Diary)
	if !ok || len(expand) != 1 || expand[0].ID != diary.ID {
		t.Fatalf("ListMedia expand = %#v", items[0].Expand)
	}

	gotMedia, err := s.GetMedia(media.ID, user.ID)
	if err != nil {
		t.Fatalf("GetMedia: %v", err)
	}
	if gotMedia.Name != "Photo" {
		t.Fatalf("GetMedia name = %q, want Photo", gotMedia.Name)
	}
	if _, err := s.GetMedia(media.ID, "another-user"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetMedia wrong owner error = %v, want sql.ErrNoRows", err)
	}

	updatedMedia, err := s.UpdateMediaDiary(media.ID, user.ID, nil)
	if err != nil {
		t.Fatalf("UpdateMediaDiary: %v", err)
	}
	if len(updatedMedia.Diary) != 0 {
		t.Fatalf("UpdateMediaDiary diary = %#v, want empty", updatedMedia.Diary)
	}

	if err := s.DeleteMediaFile(media); err != nil {
		t.Fatalf("DeleteMediaFile: %v", err)
	}
	if _, err := s.OpenMediaFile(media); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OpenMediaFile after delete error = %v, want os.ErrNotExist", err)
	}

	if err := s.DeleteMedia(media.ID, user.ID); err != nil {
		t.Fatalf("DeleteMedia: %v", err)
	}
	if err := s.DeleteMedia(media.ID, user.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("DeleteMedia second error = %v, want sql.ErrNoRows", err)
	}

	dst := filepath.Join(t.TempDir(), "nested", "upload.txt")
	if err := s.SaveUploadedFile(dst, bytes.NewBufferString("payload")); err != nil {
		t.Fatalf("SaveUploadedFile: %v", err)
	}
	if data, err := os.ReadFile(dst); err != nil || string(data) != "payload" {
		t.Fatalf("SaveUploadedFile data/error = %q / %v", string(data), err)
	}

	if err := s.SaveUploadedMedia(nil, bytes.NewBuffer(nil)); err == nil {
		t.Fatal("SaveUploadedMedia should reject nil media")
	}
}

func TestStoreS3AndHelperFunctions(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)

	if client, err := newS3Client(nil); err != nil || client != nil {
		t.Fatalf("newS3Client(nil) = %v, %v; want nil, nil", client, err)
	}
	if err := s.initLegacyS3Client(); err != nil {
		t.Fatalf("initLegacyS3Client nil config: %v", err)
	}

	cfg, err := parseLegacyS3Config(`{"s3":{"enabled":true,"bucket":"bucket","region":"region","accessKey":"key","secret":"secret","forcePathStyle":true}}`)
	if err != nil {
		t.Fatalf("parseLegacyS3Config: %v", err)
	}
	if cfg == nil || cfg.Bucket != "bucket" || !cfg.ForcePathStyle {
		t.Fatalf("parseLegacyS3Config result = %#v", cfg)
	}
	if cfg, err := parseLegacyS3Config(`{"s3":{"enabled":false}}`); err != nil || cfg != nil {
		t.Fatalf("parseLegacyS3Config disabled = %#v, %v", cfg, err)
	}

	if err := s.SetSetting(user.ID, "image_upload.provider", "s3", false); err != nil {
		t.Fatalf("SetSetting provider s3: %v", err)
	}
	for key, value := range map[string]any{
		"image_upload.s3.bucket":           "bucket",
		"image_upload.s3.region":           "region",
		"image_upload.s3.endpoint":         "s3.example.com",
		"image_upload.s3.access_key":       "key",
		"image_upload.s3.secret":           "secret",
		"image_upload.s3.force_path_style": true,
	} {
		if err := s.SetSetting(user.ID, key, value, false); err != nil {
			t.Fatalf("SetSetting %s: %v", key, err)
		}
	}
	s3cfg := s.userS3Config(user.ID)
	if s3cfg == nil || s3cfg.Endpoint != "s3.example.com" {
		t.Fatalf("userS3Config = %#v", s3cfg)
	}
	if err := s.SaveUploadedMedia(&Media{ID: "mid", File: "photo.png", Owner: user.ID}, bytes.NewBufferString("x")); err == nil {
		t.Fatal("SaveUploadedMedia with s3 provider should fail without a working S3 endpoint")
	}

	if err := s.SetSetting(user.ID, "image_upload.provider", "chevereto", false); err != nil {
		t.Fatalf("SetSetting provider chevereto: %v", err)
	}
	if err := s.SaveUploadedMedia(&Media{ID: "mid", File: "photo.png", Owner: user.ID}, bytes.NewBufferString("x")); err == nil || !strings.Contains(err.Error(), "chevereto uploads") {
		t.Fatalf("SaveUploadedMedia chevereto error = %v", err)
	}

	if _, err := s.openMediaFromS3(nil, nil, &Media{}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("openMediaFromS3 nil error = %v, want os.ErrNotExist", err)
	}
	if err := s.deleteMediaFromS3(nil, nil, &Media{}); err != nil {
		t.Fatalf("deleteMediaFromS3 nil: %v", err)
	}

	if got := SafeFilename("../../unsafe.txt"); got != "unsafe.txt" {
		t.Fatalf("SafeFilename = %q, want unsafe.txt", got)
	}
	if got := s.DefaultLocalMediaDir(); got != filepath.Join(s.DataDir, "storage", DefaultMediaCollectionID) {
		t.Fatalf("DefaultLocalMediaDir = %q", got)
	}
	if keys := s.mediaObjectKeys(&Media{ID: "mid", File: "photo.png"}); len(keys) != 2 || keys[0] != "media/mid/photo.png" {
		t.Fatalf("mediaObjectKeys = %#v", keys)
	}
	if got := SafeFilename(""); got != "upload" {
		t.Fatalf("SafeFilename empty = %q, want upload", got)
	}
	if got := TotalPages(0, 10); got != 0 {
		t.Fatalf("TotalPages empty = %d, want 0", got)
	}
	if got := TotalPages(11, 10); got != 2 {
		t.Fatalf("TotalPages = %d, want 2", got)
	}
	if got := NormalizeDate("2024-01-02T03:04:05Z"); got != "2024-01-02" {
		t.Fatalf("NormalizeDate = %q, want 2024-01-02", got)
	}
	if got := DateOnly("2024-01-02 03:04:05.000Z"); got != "2024-01-02" {
		t.Fatalf("DateOnly = %q, want 2024-01-02", got)
	}
	start, end := dayRange("2024-01-02")
	if start != "2024-01-02 00:00:00.000Z" || end != "2024-01-02 23:59:59.999Z" {
		t.Fatalf("dayRange = %q, %q", start, end)
	}
	if got := decodeStringSlice(`["a","b"]`); len(got) != 2 || got[0] != "a" {
		t.Fatalf("decodeStringSlice array = %#v", got)
	}
	if got := decodeStringSlice(`"solo"`); len(got) != 1 || got[0] != "solo" {
		t.Fatalf("decodeStringSlice solo = %#v", got)
	}
	if got := decodeStringSlice(`{`); len(got) != 0 {
		t.Fatalf("decodeStringSlice invalid = %#v", got)
	}
	if got := encodeJSON(map[string]any{"ok": true}); !strings.Contains(got, `"ok":true`) {
		t.Fatalf("encodeJSON = %q", got)
	}
	if got := encodeJSON(make(chan int)); got != "null" {
		t.Fatalf("encodeJSON unsupported = %q, want null", got)
	}
	if !settingHasValue(`{"ok":true}`) {
		t.Fatal("settingHasValue should accept object JSON")
	}
	if settingHasValue(`"   "`) {
		t.Fatal("settingHasValue should reject blank string JSON")
	}
	if got := anyToString(123); got != "123" {
		t.Fatalf("anyToString = %q, want 123", got)
	}
	if got := anyToString(bytes.NewBufferString("buf")); got != "buf" {
		t.Fatalf("anyToString stringer = %q, want buf", got)
	}
	if !anyToBool("TRUE") || anyToBool(0) || !anyToBool(1) || !anyToBool(true) || anyToBool("false") {
		t.Fatalf("anyToBool results unexpected")
	}
	if got := uniqueStrings([]string{"a", "a", "", "b"}); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("uniqueStrings = %#v", got)
	}
	if !isMissingLegacyTableError(fmt.Errorf("no such table: demo")) {
		t.Fatal("isMissingLegacyTableError should match")
	}
	if !IsNoRows(sql.ErrNoRows) || IsNoRows(errors.New("other")) {
		t.Fatal("IsNoRows results unexpected")
	}
	if err := Errorf("wrapped %s", "error"); err == nil || err.Error() != "wrapped error" {
		t.Fatalf("Errorf = %v", err)
	}

	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "src.txt")
	dst := filepath.Join(srcDir, "dst.txt")
	if err := os.WriteFile(src, []byte("copy-me"), 0o600); err != nil {
		t.Fatalf("WriteFile src: %v", err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile dst: %v", err)
	}
	if string(data) != "copy-me" {
		t.Fatalf("copyFile content = %q, want copy-me", string(data))
	}
}

func TestLegacyMigrationAndRuntimeInitialization(t *testing.T) {
	dataDir := t.TempDir()
	oldPath := filepath.Join(dataDir, LegacyDatabaseName)

	legacyDB, err := openSQLite(oldPath)
	if err != nil {
		t.Fatalf("open legacy sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = legacyDB.Close()
	})

	statements := []string{
		`CREATE TABLE _collections (id TEXT PRIMARY KEY, name TEXT NOT NULL)`,
		`CREATE TABLE _migrations (id TEXT PRIMARY KEY)`,
		`CREATE TABLE _params (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE users (
			avatar TEXT, created TEXT, email TEXT, emailVisibility BOOLEAN, id TEXT PRIMARY KEY,
			lastLoginAlertSentAt TEXT, lastResetSentAt TEXT, lastVerificationSentAt TEXT, name TEXT,
			passwordHash TEXT, tokenKey TEXT, updated TEXT, username TEXT, verified BOOLEAN
		)`,
		`CREATE TABLE diaries (
			content TEXT, created TEXT, date TEXT, id TEXT PRIMARY KEY, mood TEXT, owner TEXT, updated TEXT, weather TEXT, tags JSON
		)`,
		`CREATE TABLE user_settings (
			created TEXT, encrypted BOOLEAN, id TEXT PRIMARY KEY, key TEXT, updated TEXT, user TEXT, value JSON
		)`,
	}
	for _, statement := range statements {
		if _, err := legacyDB.Exec(statement); err != nil {
			t.Fatalf("create legacy schema: %v", err)
		}
	}
	if _, err := legacyDB.Exec(`INSERT INTO _collections(id, name) VALUES('legacy-media', 'media')`); err != nil {
		t.Fatalf("insert _collections: %v", err)
	}
	if _, err := legacyDB.Exec(`INSERT INTO _migrations(id) VALUES('m1')`); err != nil {
		t.Fatalf("insert _migrations: %v", err)
	}
	legacySettings := `{"s3":{"enabled":true,"bucket":"legacy-bucket","region":"legacy-region","endpoint":"s3.example.com","accessKey":"legacy-key","secret":"legacy-secret","forcePathStyle":true}}`
	if _, err := legacyDB.Exec(`INSERT INTO _params(key, value) VALUES('settings', ?)`, legacySettings); err != nil {
		t.Fatalf("insert _params: %v", err)
	}
	now := nowString()
	if _, err := legacyDB.Exec(`INSERT INTO users(avatar, created, email, emailVisibility, id, lastLoginAlertSentAt, lastResetSentAt, lastVerificationSentAt, name, passwordHash, tokenKey, updated, username, verified)
		VALUES('', ?, 'legacy@example.com', false, 'legacy-user', '', '', '', '', 'hash', 'token', ?, 'legacy', false)`, now, now); err != nil {
		t.Fatalf("insert legacy user: %v", err)
	}
	if _, err := legacyDB.Exec(`INSERT INTO diaries(content, created, date, id, mood, owner, updated, weather, tags)
		VALUES('legacy diary', ?, '2024-02-01 00:00:00.000Z', 'legacy-diary', 'happy', 'legacy-user', ?, 'sunny', '[]')`, now, now); err != nil {
		t.Fatalf("insert legacy diary: %v", err)
	}
	if _, err := legacyDB.Exec(`INSERT INTO user_settings(created, encrypted, id, key, updated, user, value)
		VALUES(?, false, 'legacy-setting', 'chevereto.enabled', ?, 'legacy-user', 'true')`, now, now); err != nil {
		t.Fatalf("insert legacy user setting: %v", err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	if !isLegacyDataDB(oldPath) {
		t.Fatal("isLegacyDataDB should detect legacy schema")
	}
	legacyS3, err := loadLegacyS3ConfigFromPath(oldPath)
	if err != nil {
		t.Fatalf("loadLegacyS3ConfigFromPath: %v", err)
	}
	if legacyS3 == nil || legacyS3.Bucket != "legacy-bucket" {
		t.Fatalf("loadLegacyS3ConfigFromPath = %#v", legacyS3)
	}

	s, err := Open(dataDir)
	if err != nil {
		t.Fatalf("Open migrated store: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})

	if s.MediaCollectionID != "legacy-media" {
		t.Fatalf("MediaCollectionID = %q, want legacy-media", s.MediaCollectionID)
	}
	if s.LegacyS3 == nil || s.LegacyS3.Bucket != "legacy-bucket" {
		t.Fatalf("LegacyS3 = %#v", s.LegacyS3)
	}
	if got := s.CountDiaries("legacy-user"); got != 1 {
		t.Fatalf("CountDiaries migrated = %d, want 1", got)
	}
	if _, err := s.GetUserByID("legacy-user"); err != nil {
		t.Fatalf("GetUserByID migrated user: %v", err)
	}

	rawProvider, err := getUserSettingRaw(s.DB, "legacy-user", "image_upload.provider")
	if err != nil {
		t.Fatalf("getUserSettingRaw provider: %v", err)
	}
	if rawProvider != `"chevereto"` {
		t.Fatalf("image_upload.provider raw = %q, want chevereto", rawProvider)
	}
	if got := userSettingStringValue(s.DB, "legacy-user", "image_upload.s3.bucket"); got != "legacy-bucket" {
		t.Fatalf("userSettingStringValue bucket = %q, want legacy-bucket", got)
	}
	if !userSettingBoolValue(s.DB, "legacy-user", "chevereto.enabled") {
		t.Fatal("userSettingBoolValue should read chevereto.enabled")
	}

	defaultLocalPath := filepath.Join(dataDir, "storage", DefaultMediaCollectionID)
	if got := userSettingStringValue(s.DB, "legacy-user", "image_upload.local.path"); got != defaultLocalPath {
		t.Fatalf("image_upload.local.path = %q, want %q", got, defaultLocalPath)
	}
	if got := s.imageUploadProvider("legacy-user"); got != "chevereto" {
		t.Fatalf("imageUploadProvider migrated = %q, want chevereto", got)
	}

	if entries, err := os.ReadDir(filepath.Join(dataDir, "backups")); err != nil || len(entries) == 0 {
		t.Fatalf("backup directory entries = %v, err=%v", entries, err)
	}

	if cfg, err := getLegacyS3Config(s.DB); err != nil || cfg == nil || cfg.Bucket != "legacy-bucket" {
		t.Fatalf("getLegacyS3Config = %#v, %v", cfg, err)
	}

	reopened, err := Open(dataDir)
	if err != nil {
		t.Fatalf("reopen migrated store: %v", err)
	}
	if reopened.MediaCollectionID != "legacy-media" {
		t.Fatalf("reopened MediaCollectionID = %q, want legacy-media", reopened.MediaCollectionID)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("close reopened store: %v", err)
	}
}

func TestEnsureImageUploadSettingsBranches(t *testing.T) {
	s := newTestStore(t)

	userLocal := newTestUser(t, s)
	userS3 := newTestUser(t, s)
	userChevereto := newTestUser(t, s)

	if err := s.SetSetting(userLocal.ID, "image_upload.provider", "local", false); err != nil {
		t.Fatalf("Set local provider: %v", err)
	}
	if err := s.SetSetting(userChevereto.ID, "chevereto.domain", "https://img.example.com", false); err != nil {
		t.Fatalf("Set chevereto.domain: %v", err)
	}
	if err := s.SetSetting(userChevereto.ID, "chevereto.api_key", "key", false); err != nil {
		t.Fatalf("Set chevereto.api_key: %v", err)
	}

	legacyS3 := &LegacyS3Config{
		Enabled:        true,
		Bucket:         "bucket",
		Region:         "region",
		Endpoint:       "endpoint",
		AccessKey:      "access",
		Secret:         "secret",
		ForcePathStyle: true,
	}
	if err := ensureImageUploadSettings(s.DB, s.DataDir, legacyS3); err != nil {
		t.Fatalf("ensureImageUploadSettings: %v", err)
	}

	if got := s.imageUploadProvider(userLocal.ID); got != "local" {
		t.Fatalf("imageUploadProvider local = %q, want local", got)
	}
	if got := s.imageUploadProvider(userS3.ID); got != "s3" {
		t.Fatalf("imageUploadProvider s3 = %q, want s3", got)
	}
	if got := s.imageUploadProvider(userChevereto.ID); got != "chevereto" {
		t.Fatalf("imageUploadProvider chevereto = %q, want chevereto", got)
	}
	if err := s.SetSetting(userLocal.ID, "image_upload.provider", "invalid", false); err != nil {
		t.Fatalf("Set invalid provider: %v", err)
	}
	if got := s.imageUploadProvider(userLocal.ID); got != "local" {
		t.Fatalf("imageUploadProvider invalid = %q, want local fallback", got)
	}

	if got := userSettingStringValue(s.DB, userS3.ID, "image_upload.s3.bucket"); got != "bucket" {
		t.Fatalf("userSettingStringValue s3 bucket = %q, want bucket", got)
	}
	if !userSettingBoolValue(s.DB, userS3.ID, "image_upload.s3.force_path_style") {
		t.Fatal("userSettingBoolValue should read image_upload.s3.force_path_style")
	}

	if err := insertUserSettingIfMissing(s.DB, userLocal.ID, "image_upload.provider", "s3", false); err != nil {
		t.Fatalf("insertUserSettingIfMissing existing: %v", err)
	}
	if got := userSettingStringValue(s.DB, userLocal.ID, "image_upload.provider"); got != "invalid" {
		t.Fatalf("existing provider should not be overwritten, got %q", got)
	}

	if cfg, err := parseLegacyS3Config(`{"s3":{"enabled":true,"bucket":"","region":"region","accessKey":"key","secret":"secret"}}`); err != nil || cfg != nil {
		t.Fatalf("parseLegacyS3Config incomplete = %#v, %v", cfg, err)
	}
	if got := s.userLocalMediaDir(""); got != s.DefaultLocalMediaDir() {
		t.Fatalf("userLocalMediaDir blank user = %q", got)
	}
	absPath := filepath.Join(t.TempDir(), "absolute-media")
	if err := s.SetSetting(userLocal.ID, "image_upload.local.path", absPath, false); err != nil {
		t.Fatalf("Set absolute local path: %v", err)
	}
	if got := s.userLocalMediaDir(userLocal.ID); got != absPath {
		t.Fatalf("userLocalMediaDir absolute = %q, want %q", got, absPath)
	}
}

func TestS3MediaHelpers(t *testing.T) {
	s := newTestStore(t)
	s.MediaCollectionID = "legacy-media"
	cfg := &LegacyS3Config{
		Enabled: true,
		Bucket:  "bucket",
		Region:  "us-east-1",
	}
	media := &Media{ID: "mid", File: "photo.png"}

	client := newMockS3Client(t, func(req *http.Request) (*http.Response, error) {
		switch req.Method {
		case http.MethodGet:
			if strings.Contains(req.URL.Path, "/legacy-media/") {
				return httpResponse(http.StatusNotFound, `<?xml version="1.0"?><Error><Code>NoSuchKey</Code></Error>`), nil
			}
			return httpResponse(http.StatusOK, "remote-bytes"), nil
		case http.MethodDelete:
			return httpResponse(http.StatusNoContent, ""), nil
		default:
			return httpResponse(http.StatusMethodNotAllowed, ""), nil
		}
	})

	reader, err := s.openMediaFromS3(client, cfg, media)
	if err != nil {
		t.Fatalf("openMediaFromS3: %v", err)
	}
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatalf("read openMediaFromS3 body: %v", err)
	}
	if string(data) != "remote-bytes" {
		t.Fatalf("openMediaFromS3 data = %q, want remote-bytes", string(data))
	}

	if err := s.deleteMediaFromS3(client, cfg, media); err != nil {
		t.Fatalf("deleteMediaFromS3: %v", err)
	}

	failingClient := newMockS3Client(t, func(req *http.Request) (*http.Response, error) {
		return httpResponse(http.StatusInternalServerError, `<?xml version="1.0"?><Error><Code>InternalError</Code></Error>`), nil
	})
	if _, err := s.openMediaFromS3(failingClient, cfg, media); err == nil {
		t.Fatal("openMediaFromS3 should fail on non-NoSuchKey errors")
	}
	if err := s.deleteMediaFromS3(failingClient, cfg, media); err == nil {
		t.Fatal("deleteMediaFromS3 should fail on non-NoSuchKey errors")
	}

	s.LegacyS3 = cfg
	s.legacyS3Client = client
	reader, err = s.OpenMediaFile(media)
	if err != nil {
		t.Fatalf("OpenMediaFile legacy S3: %v", err)
	}
	data, err = io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(data) != "remote-bytes" {
		t.Fatalf("OpenMediaFile legacy S3 data/error = %q / %v", string(data), err)
	}
	if err := s.DeleteMediaFile(media); err != nil {
		t.Fatalf("DeleteMediaFile legacy S3: %v", err)
	}

	var nilStore *Store
	if err := nilStore.Close(); err != nil {
		t.Fatalf("nil store Close error = %v", err)
	}
}

func TestEnsureRuntimeMetadataValidation(t *testing.T) {
	dataDir := t.TempDir()
	db, err := openSQLite(filepath.Join(dataDir, "runtime.db"))
	if err != nil {
		t.Fatalf("openSQLite: %v", err)
	}
	defer db.Close()
	if err := createSchema(db); err != nil {
		t.Fatalf("createSchema: %v", err)
	}

	if err := ensureRuntimeMetadata(db, dataDir, filepath.Join(dataDir, "missing-legacy.db")); err != nil {
		t.Fatalf("ensureRuntimeMetadata fresh: %v", err)
	}
	if secret, err := getMeta(db, "auth.secret"); err != nil || secret == "" {
		t.Fatalf("auth.secret after ensureRuntimeMetadata = %q, %v", secret, err)
	}

	if err := setMeta(db, "legacy.s3", `not-json`); err != nil {
		t.Fatalf("setMeta invalid legacy.s3: %v", err)
	}
	if err := ensureRuntimeMetadata(db, dataDir, filepath.Join(dataDir, "missing-legacy.db")); err == nil {
		t.Fatal("ensureRuntimeMetadata should fail on invalid legacy.s3 metadata")
	}

	legacyPath := filepath.Join(dataDir, "legacy-meta.db")
	legacyDB, err := openSQLite(legacyPath)
	if err != nil {
		t.Fatalf("open legacy meta db: %v", err)
	}
	if _, err := legacyDB.Exec(`CREATE TABLE _collections (id TEXT PRIMARY KEY, name TEXT NOT NULL)`); err != nil {
		t.Fatalf("create legacy _collections: %v", err)
	}
	if _, err := legacyDB.Exec(`CREATE TABLE _migrations (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("create legacy _migrations: %v", err)
	}
	if _, err := legacyDB.Exec(`CREATE TABLE _params (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatalf("create legacy _params: %v", err)
	}
	if _, err := legacyDB.Exec(`INSERT INTO _collections(id, name) VALUES('legacy-meta-media', 'media')`); err != nil {
		t.Fatalf("insert legacy collection: %v", err)
	}
	if _, err := legacyDB.Exec(`INSERT INTO _migrations(id) VALUES('m1')`); err != nil {
		t.Fatalf("insert legacy migration: %v", err)
	}
	if _, err := legacyDB.Exec(`INSERT INTO _params(key, value) VALUES('settings', '{"s3":{"enabled":true,"bucket":"b","region":"r","accessKey":"a","secret":"s"}}')`); err != nil {
		t.Fatalf("insert legacy settings: %v", err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("close legacy meta db: %v", err)
	}

	db2, err := openSQLite(filepath.Join(dataDir, "runtime2.db"))
	if err != nil {
		t.Fatalf("openSQLite runtime2: %v", err)
	}
	defer db2.Close()
	if err := createSchema(db2); err != nil {
		t.Fatalf("createSchema runtime2: %v", err)
	}
	if err := ensureRuntimeMetadata(db2, dataDir, legacyPath); err != nil {
		t.Fatalf("ensureRuntimeMetadata with legacy path: %v", err)
	}
	if mediaCollectionID, err := getMeta(db2, "legacy.media_collection_id"); err != nil || mediaCollectionID != "legacy-meta-media" {
		t.Fatalf("legacy.media_collection_id = %q, %v", mediaCollectionID, err)
	}
	if cfg, err := getLegacyS3Config(db2); err != nil || cfg == nil || cfg.Bucket != "b" {
		t.Fatalf("getLegacyS3Config runtime2 = %#v, %v", cfg, err)
	}
}

func ioReadAllAndClose(rc interface {
	Read([]byte) (int, error)
	Close() error
}) ([]byte, error) {
	defer rc.Close()
	return io.ReadAll(rc)
}
