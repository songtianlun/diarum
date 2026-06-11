package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestStoreClosedDatabaseErrorBranches(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	diary, err := s.InsertImportedDiary(user.ID, "closed-diary", "2024-01-01", "content", "", "")
	if err != nil {
		t.Fatalf("InsertImportedDiary before close: %v", err)
	}
	media, err := s.InsertImportedMedia(user.ID, "closed-media", "photo.png", "Photo", "Alt", []string{diary.ID})
	if err != nil {
		t.Fatalf("InsertImportedMedia before close: %v", err)
	}
	conv, err := s.InsertImportedConversation(user.ID, "closed-conv", "Conversation")
	if err != nil {
		t.Fatalf("InsertImportedConversation before close: %v", err)
	}
	msg, err := s.InsertImportedMessage(user.ID, "closed-msg", conv.ID, "user", "hello", []string{diary.ID})
	if err != nil {
		t.Fatalf("InsertImportedMessage before close: %v", err)
	}
	if err := s.DB.Close(); err != nil {
		t.Fatalf("Close DB: %v", err)
	}

	checks := []struct {
		name string
		fn   func() error
	}{
		{"createSchema", func() error { return createSchema(s.DB) }},
		{"ensureRuntimeMetadata", func() error {
			return ensureRuntimeMetadata(s.DB, s.DataDir, filepath.Join(s.DataDir, LegacyDatabaseName))
		}},
		{"ensureImageUploadSettings", func() error { return ensureImageUploadSettings(s.DB, s.DataDir, nil) }},
		{"insertUserSettingIfMissing", func() error { return insertUserSettingIfMissing(s.DB, user.ID, "x", "y", false) }},
		{"getUserSettingRaw", func() error { _, err := getUserSettingRaw(s.DB, user.ID, "x"); return err }},
		{"getMeta", func() error { _, err := getMeta(s.DB, "x"); return err }},
		{"setMeta", func() error { return setMeta(s.DB, "x", "y") }},
		{"getLegacyS3Config", func() error { _, err := getLegacyS3Config(s.DB); return err }},
		{"Transaction", func() error { return s.Transaction(context.Background(), func(tx *sql.Tx) error { return nil }) }},
		{"GetUserByID", func() error { _, err := s.GetUserByID(user.ID); return err }},
		{"GetUserByIdentity", func() error { _, err := s.GetUserByIdentity(user.Email); return err }},
		{"CreateUser", func() error { _, err := s.CreateUser("closed", "closed@example.com", "hash"); return err }},
		{"UpsertDiary", func() error { _, _, err := s.UpsertDiary(user.ID, "2024-01-02", "x", "", ""); return err }},
		{"GetDiaryByDate", func() error {
			_, err := s.GetDiaryByDate(user.ID, "2024-01-01 00:00:00.000Z", "2024-01-01 23:59:59.999Z")
			return err
		}},
		{"GetDiaryByID", func() error { _, err := s.GetDiaryByID(diary.ID); return err }},
		{"DeleteDiary", func() error { return s.DeleteDiary(diary.ID, user.ID) }},
		{"ListDiaries", func() error { _, err := s.ListDiaries(user.ID, "", "", "-date", 1); return err }},
		{"SearchDiaries", func() error { _, err := s.SearchDiaries(user.ID, "x", 1); return err }},
		{"GetSetting", func() error { _, err := s.GetSetting(user.ID, "x"); return err }},
		{"SetSetting", func() error { return s.SetSetting(user.ID, "x", "y", false) }},
		{"DeleteSetting", func() error { return s.DeleteSetting(user.ID, "x") }},
		{"GetSettings", func() error { _, err := s.GetSettings(user.ID); return err }},
		{"ValidateAPIToken", func() error { _, err := s.ValidateAPIToken("token"); return err }},
		{"ListMedia", func() error { _, _, err := s.ListMedia(user.ID, 1, 10); return err }},
		{"GetMedia", func() error { _, err := s.GetMedia(media.ID, user.ID); return err }},
		{"CreateMedia", func() error { _, err := s.CreateMedia(user.ID, "x.png", "x", "", nil); return err }},
		{"InsertImportedMedia", func() error { _, err := s.InsertImportedMedia(user.ID, "x", "x.png", "x", "", nil); return err }},
		{"UpdateMediaDiary", func() error { _, err := s.UpdateMediaDiary(media.ID, user.ID, nil); return err }},
		{"DeleteMedia", func() error { return s.DeleteMedia(media.ID, user.ID) }},
		{"ListConversations", func() error { _, err := s.ListConversations(user.ID, 10); return err }},
		{"GetConversation", func() error { _, err := s.GetConversation(conv.ID, user.ID); return err }},
		{"CreateConversation", func() error { _, err := s.CreateConversation(user.ID, "x"); return err }},
		{"InsertImportedConversation", func() error { _, err := s.InsertImportedConversation(user.ID, "x", "x"); return err }},
		{"UpdateConversationTitle", func() error { _, err := s.UpdateConversationTitle(conv.ID, user.ID, "x"); return err }},
		{"DeleteConversation", func() error { return s.DeleteConversation(conv.ID, user.ID) }},
		{"ListMessages", func() error { _, err := s.ListMessages(conv.ID, 10); return err }},
		{"CountMessages", func() error { _, err := s.CountMessages(conv.ID); return err }},
		{"CreateMessage", func() error { _, err := s.CreateMessage(user.ID, conv.ID, "user", "x", nil); return err }},
		{"InsertImportedMessage", func() error { _, err := s.InsertImportedMessage(user.ID, "x", conv.ID, "user", "x", nil); return err }},
		{"GetMessage", func() error { _, err := s.GetMessage(msg.ID); return err }},
		{"InsertImportedDiary", func() error { _, err := s.InsertImportedDiary(user.ID, "x", "2024-01-03", "x", "", ""); return err }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.fn(); err == nil {
				t.Fatalf("%s should fail after DB close", check.name)
			}
		})
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

func TestUserS3MediaFileBranches(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	s.MediaCollectionID = "legacy-media"
	media := &Media{ID: "mid", File: "photo.png", Owner: user.ID}

	var sawFallbackGet bool
	var sawDelete bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if strings.Contains(r.URL.Path, "/legacy-media/") {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>NoSuchKey</Code></Error>`))
				return
			}
			sawFallbackGet = true
			_, _ = w.Write([]byte("user-s3-body"))
		case http.MethodDelete:
			sawDelete = true
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	for key, value := range map[string]any{
		"image_upload.provider":            "s3",
		"image_upload.s3.bucket":           "bucket",
		"image_upload.s3.region":           "us-east-1",
		"image_upload.s3.endpoint":         server.URL,
		"image_upload.s3.access_key":       "key",
		"image_upload.s3.secret":           "secret",
		"image_upload.s3.force_path_style": true,
	} {
		if err := s.SetSetting(user.ID, key, value, false); err != nil {
			t.Fatalf("SetSetting %s: %v", key, err)
		}
	}

	reader, err := s.OpenMediaFile(media)
	if err != nil {
		t.Fatalf("OpenMediaFile user s3: %v", err)
	}
	data, err := ioReadAllAndClose(reader)
	if err != nil {
		t.Fatalf("read user s3 body: %v", err)
	}
	if string(data) != "user-s3-body" || !sawFallbackGet {
		t.Fatalf("OpenMediaFile user s3 data/fallback = %q/%v", string(data), sawFallbackGet)
	}
	if err := s.DeleteMediaFile(media); err != nil {
		t.Fatalf("DeleteMediaFile user s3: %v", err)
	}
	if !sawDelete {
		t.Fatal("DeleteMediaFile should call user S3 delete")
	}

	failingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>InternalError</Code></Error>`))
	}))
	defer failingServer.Close()
	if err := s.SetSetting(user.ID, "image_upload.s3.endpoint", failingServer.URL, false); err != nil {
		t.Fatalf("SetSetting failing endpoint: %v", err)
	}
	if _, err := s.OpenMediaFile(media); err == nil {
		t.Fatal("OpenMediaFile user s3 should fail on non-NoSuchKey response")
	}
	if err := s.DeleteMediaFile(media); err == nil {
		t.Fatal("DeleteMediaFile user s3 should fail on non-NoSuchKey response")
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

func TestOpenAndSQLiteErrorBranches(t *testing.T) {
	base := t.TempDir()
	notDir := filepath.Join(base, "not-a-dir")
	if err := os.WriteFile(notDir, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile notDir: %v", err)
	}
	if _, err := Open(filepath.Join(notDir, "child")); err == nil {
		t.Fatal("Open should fail when dataDir parent is a file")
	}

	if _, err := openSQLite(filepath.Join(base, "missing", "db.sqlite")); err == nil {
		t.Fatal("openSQLite should fail for missing parent directory")
	}

	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, DatabaseName), []byte("not sqlite"), 0o600); err != nil {
		t.Fatalf("WriteFile corrupt db: %v", err)
	}
	if s, err := Open(dataDir); err == nil {
		_ = s.Close()
		t.Fatal("Open should fail for corrupt existing database")
	}
}

func TestLegacyHelpersEdgeBranches(t *testing.T) {
	dataDir := t.TempDir()
	nonLegacyPath := filepath.Join(dataDir, "plain.db")
	db, err := openSQLite(nonLegacyPath)
	if err != nil {
		t.Fatalf("open plain sqlite: %v", err)
	}
	if err := createSchema(db); err != nil {
		t.Fatalf("create plain schema: %v", err)
	}
	if err := migrateLegacyData(db, nonLegacyPath); err != nil {
		t.Fatalf("migrateLegacyData non legacy: %v", err)
	}
	if isLegacyDataDB(nonLegacyPath) {
		t.Fatal("plain schema should not be detected as legacy")
	}
	_ = db.Close()

	if isLegacyDataDB(filepath.Join(dataDir, "missing.db")) {
		t.Fatal("missing database should not be legacy")
	}
	if _, err := loadLegacyS3ConfigFromPath(filepath.Join(dataDir, "missing", "data.db")); err == nil {
		t.Fatal("loadLegacyS3ConfigFromPath should fail for missing parent")
	}

	legacyPath := filepath.Join(dataDir, "legacy-no-settings.db")
	legacyDB, err := openSQLite(legacyPath)
	if err != nil {
		t.Fatalf("open legacy no settings: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE _collections (id TEXT PRIMARY KEY, name TEXT NOT NULL)`,
		`CREATE TABLE _migrations (id TEXT PRIMARY KEY)`,
		`CREATE TABLE _params (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
	} {
		if _, err := legacyDB.Exec(stmt); err != nil {
			t.Fatalf("create legacy table: %v", err)
		}
	}
	if _, err := legacyDB.Exec(`INSERT INTO _migrations(id) VALUES('m1')`); err != nil {
		t.Fatalf("insert migration: %v", err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("close legacy no settings: %v", err)
	}
	if cfg, err := loadLegacyS3ConfigFromPath(legacyPath); err != nil || cfg != nil {
		t.Fatalf("loadLegacyS3ConfigFromPath no settings = %#v, %v", cfg, err)
	}

	attachedDB, err := openSQLite(filepath.Join(dataDir, "attached.db"))
	if err != nil {
		t.Fatalf("open attached target: %v", err)
	}
	defer attachedDB.Close()
	if _, err := attachedDB.Exec(`ATTACH DATABASE ? AS legacy`, legacyPath); err != nil {
		t.Fatalf("attach legacy: %v", err)
	}
	tx, err := attachedDB.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if cfg, err := loadLegacyS3ConfigFromAttachedDB(tx); err != nil || cfg != nil {
		t.Fatalf("loadLegacyS3ConfigFromAttachedDB no rows = %#v, %v", cfg, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback tx: %v", err)
	}
	if _, err := attachedDB.Exec(`DETACH DATABASE legacy`); err != nil {
		t.Fatalf("detach legacy: %v", err)
	}

	for _, raw := range []string{"", `{"s3":{"enabled":true,"bucket":"b","region":"r","accessKey":"a"}}`} {
		if cfg, err := parseLegacyS3Config(raw); err != nil || cfg != nil {
			t.Fatalf("parseLegacyS3Config(%q) = %#v, %v", raw, cfg, err)
		}
	}
	if _, err := parseLegacyS3Config(`{`); err == nil {
		t.Fatal("parseLegacyS3Config should fail on invalid JSON")
	}

	if _, err := backupLegacyData(dataDir, filepath.Join(dataDir, "missing-data.db")); err == nil {
		t.Fatal("backupLegacyData should fail when source is missing")
	}
	logsPath := filepath.Join(dataDir, "logs.db")
	if err := os.WriteFile(logsPath, []byte("logs"), 0o600); err != nil {
		t.Fatalf("WriteFile logs: %v", err)
	}
	backupDir, err := backupLegacyData(dataDir, legacyPath)
	if err != nil {
		t.Fatalf("backupLegacyData with logs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(backupDir, "logs.db")); err != nil {
		t.Fatalf("logs backup missing: %v", err)
	}

	if err := copyFile(filepath.Join(dataDir, "does-not-exist"), filepath.Join(dataDir, "dst")); err == nil {
		t.Fatal("copyFile should fail for missing source")
	}
	if err := copyFile(legacyPath, filepath.Join(dataDir, "missing-dir", "dst")); err == nil {
		t.Fatal("copyFile should fail for missing destination parent")
	}
}

func TestStoreAdditionalLowCoverageBranches(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)

	id, err := GenerateID()
	if err != nil {
		t.Fatalf("GenerateID: %v", err)
	}
	if !strings.HasPrefix(id, "r") || len(id) != 15 {
		t.Fatalf("GenerateID = %q", id)
	}
	tokenKey, err := GenerateTokenKey()
	if err != nil {
		t.Fatalf("GenerateTokenKey: %v", err)
	}
	if len(tokenKey) != 48 {
		t.Fatalf("GenerateTokenKey length = %d, want 48", len(tokenKey))
	}
	if got := DateOnly("short"); got != "short" {
		t.Fatalf("DateOnly short = %q", got)
	}

	if _, err := s.CreateUser(user.Username, "other@example.com", "hash"); err == nil {
		t.Fatal("CreateUser should fail for duplicate username")
	}
	if _, inserted, err := s.UpsertDiary("missing-owner", "2024-07-01", "x", "", ""); err == nil || !inserted {
		t.Fatalf("UpsertDiary missing owner inserted/error = %v/%v, want insert attempt FK error", inserted, err)
	}

	first, _, err := s.UpsertDiary(user.ID, "2024-07-01", "first", "", "")
	if err != nil {
		t.Fatalf("UpsertDiary first: %v", err)
	}
	_, _, err = s.UpsertDiary(user.ID, "2024-07-02", "second", "", "")
	if err != nil {
		t.Fatalf("UpsertDiary second: %v", err)
	}
	byCreated, err := s.ListDiaries(user.ID, "", "", "created", 0)
	if err != nil {
		t.Fatalf("ListDiaries created: %v", err)
	}
	if len(byCreated) != 2 || byCreated[0].ID != first.ID {
		t.Fatalf("ListDiaries created order = %#v", byCreated)
	}
	byUpdated, err := s.ListDiaries(user.ID, "", "", "updated", 0)
	if err != nil {
		t.Fatalf("ListDiaries updated: %v", err)
	}
	if len(byUpdated) != 2 {
		t.Fatalf("ListDiaries updated order = %#v", byUpdated)
	}

	if err := s.SetSetting(user.ID, "invalid.raw", "placeholder", false); err != nil {
		t.Fatalf("SetSetting invalid.raw: %v", err)
	}
	if _, err := s.DB.Exec(`UPDATE user_settings SET value = 'not-json' WHERE user = ? AND key = 'invalid.raw'`, user.ID); err != nil {
		t.Fatalf("update invalid raw: %v", err)
	}
	if value, err := s.GetSetting(user.ID, "invalid.raw"); err != nil || value != "not-json" {
		t.Fatalf("GetSetting invalid raw = %#v, %v", value, err)
	}
	if err := s.SetSetting(user.ID, "null.raw", nil, false); err != nil {
		t.Fatalf("SetSetting null.raw: %v", err)
	}
	if value, err := s.GetSetting(user.ID, "null.raw"); err != nil || value != nil {
		t.Fatalf("GetSetting null raw = %#v, %v", value, err)
	}
	settings, err := s.GetSettings(user.ID)
	if err != nil {
		t.Fatalf("GetSettings with invalid/null: %v", err)
	}
	if settings["invalid.raw"] != "not-json" || settings["null.raw"] != nil {
		t.Fatalf("GetSettings invalid/null = %#v / %#v", settings["invalid.raw"], settings["null.raw"])
	}

	if _, err := s.CreateMedia("missing-owner", "x.png", "x", "", nil); err == nil {
		t.Fatal("CreateMedia should fail for missing owner")
	}
	if _, err := s.CreateConversation("missing-owner", "x"); err == nil {
		t.Fatal("CreateConversation should fail for missing owner")
	}
	if _, err := s.CreateMessage(user.ID, "missing-conversation", "user", "x", nil); err == nil {
		t.Fatal("CreateMessage should fail for missing conversation")
	}

	media := &Media{ID: "missing-media", File: "missing.png", Owner: user.ID}
	if got := s.MediaFilePath(media); !strings.HasSuffix(got, filepath.Join(DefaultMediaCollectionID, media.ID, media.File)) {
		t.Fatalf("MediaFilePath missing fallback = %q", got)
	}
	if err := s.SetSetting(user.ID, "image_upload.provider", "s3", false); err != nil {
		t.Fatalf("SetSetting provider s3: %v", err)
	}
	if _, err := s.OpenMediaFile(media); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OpenMediaFile incomplete user s3 = %v, want os.ErrNotExist", err)
	}
	if err := s.DeleteMediaFile(media); err != nil {
		t.Fatalf("DeleteMediaFile incomplete user s3: %v", err)
	}

	if got := anyToString(nil); got != "" {
		t.Fatalf("anyToString nil = %q", got)
	}
	if got := anyToString("literal"); got != "literal" {
		t.Fatalf("anyToString string = %q", got)
	}
	if !anyToBool(float64(1)) || anyToBool(float64(0)) || anyToBool(nil) || anyToBool(" true ") != true {
		t.Fatal("anyToBool edge results unexpected")
	}
	if got := TotalPages(20, 10); got != 2 {
		t.Fatalf("TotalPages exact = %d, want 2", got)
	}
	if got := TotalPages(20, 0); got != 0 {
		t.Fatalf("TotalPages bad perPage = %d, want 0", got)
	}
}

func TestOpenExistingDBAndLegacySkipBranches(t *testing.T) {
	existingDir := t.TempDir()
	existingDB, err := openSQLite(filepath.Join(existingDir, DatabaseName))
	if err != nil {
		t.Fatalf("open existing sqlite: %v", err)
	}
	if err := createSchema(existingDB); err != nil {
		t.Fatalf("create existing schema: %v", err)
	}
	if err := setMeta(existingDB, "auth.secret", "plain-secret"); err != nil {
		t.Fatalf("set plain auth secret: %v", err)
	}
	if err := existingDB.Close(); err != nil {
		t.Fatalf("close existing db: %v", err)
	}

	existingStore, err := Open(existingDir)
	if err != nil {
		t.Fatalf("Open existing store: %v", err)
	}
	if string(existingStore.AuthSecret) != "plain-secret" {
		t.Fatalf("AuthSecret = %q, want plain-secret", string(existingStore.AuthSecret))
	}
	if existingStore.MediaCollectionID != DefaultMediaCollectionID {
		t.Fatalf("MediaCollectionID = %q, want %q", existingStore.MediaCollectionID, DefaultMediaCollectionID)
	}
	if err := existingStore.Close(); err != nil {
		t.Fatalf("close existing store: %v", err)
	}

	skipDir := t.TempDir()
	nonLegacyDB, err := openSQLite(filepath.Join(skipDir, LegacyDatabaseName))
	if err != nil {
		t.Fatalf("open non legacy data.db: %v", err)
	}
	if err := createSchema(nonLegacyDB); err != nil {
		t.Fatalf("create non legacy schema: %v", err)
	}
	if err := nonLegacyDB.Close(); err != nil {
		t.Fatalf("close non legacy db: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skipDir, "logs.db"), []byte("logs"), 0o600); err != nil {
		t.Fatalf("write logs.db: %v", err)
	}

	skipStore, err := Open(skipDir)
	if err != nil {
		t.Fatalf("Open with non legacy data.db: %v", err)
	}
	if skipStore.MediaCollectionID != DefaultMediaCollectionID {
		t.Fatalf("skip MediaCollectionID = %q, want %q", skipStore.MediaCollectionID, DefaultMediaCollectionID)
	}
	if _, err := getMeta(skipStore.DB, "migration.source"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("migration.source error = %v, want sql.ErrNoRows", err)
	}
	if err := skipStore.Close(); err != nil {
		t.Fatalf("close skip store: %v", err)
	}
	backupEntries, err := os.ReadDir(filepath.Join(skipDir, "backups"))
	if err != nil || len(backupEntries) != 1 {
		t.Fatalf("backup entries = %v, err=%v", backupEntries, err)
	}
	if _, err := os.Stat(filepath.Join(skipDir, "backups", backupEntries[0].Name(), "logs.db")); err != nil {
		t.Fatalf("logs.db should be backed up for skipped migration: %v", err)
	}
}

func TestOpenLegacyDBWithBackupLogsAndDefaults(t *testing.T) {
	dataDir := t.TempDir()
	legacyPath := filepath.Join(dataDir, LegacyDatabaseName)
	legacyDB, err := openSQLite(legacyPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE _collections (id TEXT PRIMARY KEY, name TEXT NOT NULL)`,
		`CREATE TABLE _migrations (id TEXT PRIMARY KEY)`,
		`CREATE TABLE _params (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE users (avatar TEXT, created TEXT, email TEXT, emailVisibility BOOLEAN, id TEXT PRIMARY KEY, lastLoginAlertSentAt TEXT, lastResetSentAt TEXT, lastVerificationSentAt TEXT, name TEXT, passwordHash TEXT, tokenKey TEXT, updated TEXT, username TEXT, verified BOOLEAN)`,
		`CREATE TABLE media (alt TEXT, created TEXT, file TEXT, id TEXT PRIMARY KEY, name TEXT, owner TEXT, updated TEXT, diary JSON)`,
	} {
		if _, err := legacyDB.Exec(stmt); err != nil {
			t.Fatalf("create legacy schema: %v", err)
		}
	}
	legacySettings := `{"s3":{"enabled":true,"bucket":"legacy-bucket","region":"us-east-1","endpoint":"s3.example.com","accessKey":"key","secret":"secret","forcePathStyle":true}}`
	if _, err := legacyDB.Exec(`INSERT INTO _params(key, value) VALUES('settings', ?)`, legacySettings); err != nil {
		t.Fatalf("insert legacy settings: %v", err)
	}
	now := nowString()
	if _, err := legacyDB.Exec(`INSERT INTO users(avatar, created, email, emailVisibility, id, lastLoginAlertSentAt, lastResetSentAt, lastVerificationSentAt, name, passwordHash, tokenKey, updated, username, verified) VALUES('', ?, '', false, 'legacy-user', '', '', '', '', 'hash', 'token', ?, 'legacy-user', false)`, now, now); err != nil {
		t.Fatalf("insert legacy user: %v", err)
	}
	if _, err := legacyDB.Exec(`INSERT INTO media(alt, created, file, id, name, owner, updated, diary) VALUES('', ?, 'photo.png', 'legacy-media-row', 'Photo', 'legacy-user', ?, NULL)`, now, now); err != nil {
		t.Fatalf("insert legacy media: %v", err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "logs.db"), []byte("logs"), 0o600); err != nil {
		t.Fatalf("write logs.db: %v", err)
	}

	s, err := Open(dataDir)
	if err != nil {
		t.Fatalf("Open legacy store: %v", err)
	}
	defer s.Close()
	if s.MediaCollectionID != DefaultMediaCollectionID {
		t.Fatalf("MediaCollectionID = %q, want default", s.MediaCollectionID)
	}
	media, err := s.GetMedia("legacy-media-row", "legacy-user")
	if err != nil {
		t.Fatalf("GetMedia migrated: %v", err)
	}
	if len(media.Diary) != 0 {
		t.Fatalf("migrated null diary = %#v, want empty", media.Diary)
	}
	if got := s.imageUploadProvider("legacy-user"); got != "s3" {
		t.Fatalf("imageUploadProvider legacy user = %q, want s3", got)
	}
	backupEntries, err := os.ReadDir(filepath.Join(dataDir, "backups"))
	if err != nil || len(backupEntries) != 1 {
		t.Fatalf("backup entries = %v, err=%v", backupEntries, err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "backups", backupEntries[0].Name(), "logs.db")); err != nil {
		t.Fatalf("logs.db backup missing: %v", err)
	}
}

func TestEnsureImageUploadSettingsCheveretoEnabledAndLegacyS3(t *testing.T) {
	s := newTestStore(t)
	cheveretoUser := newTestUser(t, s)
	legacyS3User := newTestUser(t, s)
	if err := s.SetSetting(cheveretoUser.ID, "chevereto.enabled", true, false); err != nil {
		t.Fatalf("Set chevereto.enabled: %v", err)
	}
	legacyS3 := &LegacyS3Config{Enabled: true, Bucket: "bucket", Region: "region", Endpoint: "endpoint", AccessKey: "access", Secret: "secret", ForcePathStyle: true}
	if err := ensureImageUploadSettings(s.DB, s.DataDir, legacyS3); err != nil {
		t.Fatalf("ensureImageUploadSettings: %v", err)
	}
	if got := s.imageUploadProvider(cheveretoUser.ID); got != "chevereto" {
		t.Fatalf("chevereto enabled provider = %q, want chevereto", got)
	}
	if got := s.imageUploadProvider(legacyS3User.ID); got != "s3" {
		t.Fatalf("legacy S3 provider = %q, want s3", got)
	}
	if got := userSettingStringValue(s.DB, legacyS3User.ID, "image_upload.s3.secret"); got != "secret" {
		t.Fatalf("legacy S3 secret = %q, want secret", got)
	}
}

func TestCreateEmptyDefaultsSetSettingAndListMediaEdges(t *testing.T) {
	s := newTestStore(t)
	user, err := s.CreateUser("", "", "")
	if err != nil {
		t.Fatalf("CreateUser empty defaults: %v", err)
	}
	if user.Username != "" || user.Email != "" || user.PasswordHash != "" || user.Verified || user.EmailVisibility {
		t.Fatalf("empty user defaults = %#v", user)
	}
	if err := s.SetSetting(user.ID, "plain", "first", false); err != nil {
		t.Fatalf("SetSetting insert: %v", err)
	}
	if err := s.SetSetting(user.ID, "plain", "second", true); err != nil {
		t.Fatalf("SetSetting update: %v", err)
	}
	if value, err := s.GetSetting(user.ID, "plain"); err != nil || value != "second" {
		t.Fatalf("updated setting = %#v, %v", value, err)
	}
	if err := s.SetSetting("missing-user", "bad", "value", false); err == nil {
		t.Fatal("SetSetting should fail for missing user")
	}

	conversation, err := s.CreateConversation(user.ID, "")
	if err != nil {
		t.Fatalf("CreateConversation empty title: %v", err)
	}
	if conversation.Title != "" {
		t.Fatalf("empty conversation title = %q", conversation.Title)
	}
	message, err := s.CreateMessage(user.ID, conversation.ID, "", "", nil)
	if err != nil {
		t.Fatalf("CreateMessage empty fields: %v", err)
	}
	if message.Role != "" || message.Content != "" || len(message.ReferencedDiaries) != 0 {
		t.Fatalf("empty message defaults = %#v", message)
	}

	for i := 0; i < 3; i++ {
		if _, err := s.InsertImportedMedia(user.ID, fmt.Sprintf("media-%d", i), fmt.Sprintf("%d.png", i), "", "", nil); err != nil {
			t.Fatalf("InsertImportedMedia %d: %v", i, err)
		}
	}
	items, total, err := s.ListMedia(user.ID, 2, 2)
	if err != nil {
		t.Fatalf("ListMedia page 2: %v", err)
	}
	if total != 3 || len(items) != 1 || items[0].Expand != nil {
		t.Fatalf("ListMedia page 2 total/items/expand = %d/%d/%#v", total, len(items), items)
	}
	items, total, err = s.ListMedia(user.ID, 99, 2)
	if err != nil {
		t.Fatalf("ListMedia high page: %v", err)
	}
	if total != 3 || len(items) != 0 {
		t.Fatalf("ListMedia high page total/items = %d/%d", total, len(items))
	}
}

func TestOpenAndDeleteMediaFileS3FallbackBranchesWithMockTransport(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	media := &Media{ID: "mid", File: "photo.png", Owner: user.ID}
	s.MediaCollectionID = "legacy-media"
	userS3Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>NoSuchKey</Code></Error>`))
	}))
	defer userS3Server.Close()
	for key, value := range map[string]any{
		"image_upload.provider":            "s3",
		"image_upload.s3.bucket":           "user-bucket",
		"image_upload.s3.region":           "us-east-1",
		"image_upload.s3.endpoint":         userS3Server.URL,
		"image_upload.s3.access_key":       "key",
		"image_upload.s3.secret":           "secret",
		"image_upload.s3.force_path_style": true,
	} {
		if err := s.SetSetting(user.ID, key, value, false); err != nil {
			t.Fatalf("SetSetting %s: %v", key, err)
		}
	}

	var sawLegacyGet bool
	var sawLegacyDelete bool
	legacyClient := newMockS3Client(t, func(req *http.Request) (*http.Response, error) {
		switch req.Method {
		case http.MethodGet:
			sawLegacyGet = true
			return httpResponse(http.StatusOK, "legacy-body"), nil
		case http.MethodDelete:
			sawLegacyDelete = true
			return httpResponse(http.StatusNoContent, ""), nil
		default:
			return httpResponse(http.StatusMethodNotAllowed, ""), nil
		}
	})
	s.LegacyS3 = &LegacyS3Config{Enabled: true, Bucket: "legacy-bucket", Region: "us-east-1"}
	s.legacyS3Client = legacyClient

	reader, err := s.OpenMediaFile(media)
	if err != nil {
		t.Fatalf("OpenMediaFile fallback legacy S3: %v", err)
	}
	data, err := ioReadAllAndClose(reader)
	if err != nil {
		t.Fatalf("read fallback legacy S3: %v", err)
	}
	if string(data) != "legacy-body" || !sawLegacyGet {
		t.Fatalf("fallback legacy S3 data/saw = %q/%v", string(data), sawLegacyGet)
	}
	if err := s.DeleteMediaFile(media); err != nil {
		t.Fatalf("DeleteMediaFile fallback legacy S3: %v", err)
	}
	if !sawLegacyDelete {
		t.Fatal("DeleteMediaFile should call legacy S3 delete")
	}

	s.legacyS3Client = newMockS3Client(t, func(req *http.Request) (*http.Response, error) {
		return httpResponse(http.StatusInternalServerError, `<?xml version="1.0"?><Error><Code>InternalError</Code></Error>`), nil
	})
	if err := s.SetSetting(user.ID, "image_upload.provider", "local", false); err != nil {
		t.Fatalf("SetSetting local provider: %v", err)
	}
	if err := s.DeleteMediaFile(media); err == nil {
		t.Fatal("DeleteMediaFile should return legacy S3 error when no earlier error exists")
	}
}

func ioReadAllAndClose(rc interface {
	Read([]byte) (int, error)
	Close() error
}) ([]byte, error) {
	defer rc.Close()
	return io.ReadAll(rc)
}

func TestOpenRenameAndRuntimeErrorBranches(t *testing.T) {
	dataDir := t.TempDir()

	t.Run("rename failure when newPath already locked", func(t *testing.T) {
		lockedPath := filepath.Join(dataDir, "locked")
		if err := os.MkdirAll(lockedPath, 0755); err != nil {
			t.Fatalf("Mkdir locked: %v", err)
		}
		newPathInLocked := filepath.Join(lockedPath, DatabaseName)
		if err := os.WriteFile(newPathInLocked, []byte("locked"), 0o400); err != nil {
			t.Fatalf("WriteFile locked: %v", err)
		}
		if _, err := Open(lockedPath); err == nil {
			t.Fatal("Open should fail when database file is occupied")
		}
	})

	t.Run("ensureRuntimeMetadata failure on existing db", func(t *testing.T) {
		base := t.TempDir()
		dbPath := filepath.Join(base, DatabaseName)
		db, err := openSQLite(dbPath)
		if err != nil {
			t.Fatalf("openSQLite: %v", err)
		}
		if err := createSchema(db); err != nil {
			t.Fatalf("createSchema: %v", err)
		}
		if err := setMeta(db, "auth.secret", "valid-hex"); err != nil {
			t.Fatalf("setMeta: %v", err)
		}
		if err := setMeta(db, "legacy.media_collection_id", DefaultMediaCollectionID); err != nil {
			t.Fatalf("setMeta media_collection_id: %v", err)
		}
		if err := setMeta(db, "legacy.s3", `{invalid`); err != nil {
			t.Fatalf("setMeta invalid s3: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
		if _, err := Open(base); err == nil {
			t.Fatal("Open should fail when invalid legacy.s3 breaks runtime metadata")
		}
	})
}

func TestImageUploadProviderCheveretoFallback(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)

	if got := s.imageUploadProvider(user.ID); got != "local" {
		t.Fatalf("default imageUploadProvider = %q, want local", got)
	}

	if err := s.SetSetting(user.ID, "image_upload.provider", "unknown", false); err != nil {
		t.Fatalf("SetSetting provider unknown: %v", err)
	}
	if err := s.SetSetting(user.ID, "chevereto.enabled", true, false); err != nil {
		t.Fatalf("SetSetting chevereto.enabled: %v", err)
	}
	if got := s.imageUploadProvider(user.ID); got != "chevereto" {
		t.Fatalf("imageUploadProvider chevereto fallback = %q, want chevereto", got)
	}
}

func TestUserS3AndFileHelpersEdges(t *testing.T) {
	s := newTestStore(t)

	if got := s.userS3Config(""); got != nil {
		t.Fatalf("userS3Config empty user = %#v, want nil", got)
	}

	user := newTestUser(t, s)
	if got := s.userS3Config(user.ID); got != nil {
		t.Fatalf("userS3Config without s3 settings = %#v, want nil", got)
	}

	if got := s.userLocalMediaDir(""); got != s.DefaultLocalMediaDir() {
		t.Fatalf("userLocalMediaDir empty = %q, want default", got)
	}

	_ = user
}

func TestGetConversationListMessagesWithoutOwner(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)

	conv, err := s.CreateConversation(user.ID, "Test")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	got, err := s.GetConversation(conv.ID, "")
	if err != nil {
		t.Fatalf("GetConversation without owner: %v", err)
	}
	if got.ID != conv.ID {
		t.Fatalf("GetConversation without owner ID = %q, want %q", got.ID, conv.ID)
	}

	if _, err := s.GetConversation(conv.ID, "other-owner"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetConversation wrong owner error = %v, want sql.ErrNoRows", err)
	}

	msgs, err := s.ListMessages(conv.ID, 0)
	if err != nil {
		t.Fatalf("ListMessages without limit: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("ListMessages without limit = %d, want 0", len(msgs))
	}
}

func TestSaveUploadedFileErrors(t *testing.T) {
	s := newTestStore(t)

	dst := filepath.Join(t.TempDir(), "existing-file")
	if err := os.WriteFile(dst, []byte("block"), 0o400); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := s.SaveUploadedFile(filepath.Join(dst, "nested"), bytes.NewBufferString("x")); err == nil {
		t.Fatal("SaveUploadedFile should fail when parent is a file not directory")
	}
}

func TestListMediaRowsError(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)

	if err := s.DB.Close(); err != nil {
		t.Fatalf("close DB: %v", err)
	}
	if _, _, err := s.ListMedia(user.ID, 1, 10); err == nil {
		t.Fatal("ListMedia should fail after DB close")
	}
}

func TestEnsureImageUploadSettingsScanErrors(t *testing.T) {
	s := newTestStore(t)
	_ = newTestUser(t, s)

	if err := s.DB.Close(); err != nil {
		t.Fatalf("close DB: %v", err)
	}
	if err := ensureImageUploadSettings(s.DB, s.DataDir, nil); err == nil {
		t.Fatal("ensureImageUploadSettings should fail after DB close")
	}
}

func TestGetSettingsNullValue(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)

	if _, err := s.DB.Exec(`INSERT INTO user_settings(created, encrypted, id, key, updated, user, value) VALUES(?, false, ?, ?, ?, ?, NULL)`, nowString(), "null-val", "null.key", nowString(), user.ID); err != nil {
		t.Fatalf("insert null value: %v", err)
	}

	settings, err := s.GetSettings(user.ID)
	if err != nil {
		t.Fatalf("GetSettings with null: %v", err)
	}
	if settings["null.key"] != nil {
		t.Fatalf("GetSettings null.key = %#v, want nil", settings["null.key"])
	}
}

func TestGenerateIDAndTokenError(t *testing.T) {
	if id, err := GenerateID(); err != nil || id == "" {
		t.Fatalf("GenerateID = %q, %v", id, err)
	}
	if token, err := GenerateTokenKey(); err != nil || token == "" {
		t.Fatalf("GenerateTokenKey = %q, %v", token, err)
	}
}

func TestGetUserSettingRawNullValue(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)

	if _, err := s.DB.Exec(`INSERT INTO user_settings(created, encrypted, id, key, updated, user, value) VALUES(?, false, ?, ?, ?, ?, NULL)`, nowString(), "null-setting", "settings.null", nowString(), user.ID); err != nil {
		t.Fatalf("insert null setting: %v", err)
	}

	raw, err := getUserSettingRaw(s.DB, user.ID, "settings.null")
	if err != nil {
		t.Fatalf("getUserSettingRaw null: %v", err)
	}
	if raw != "" {
		t.Fatalf("getUserSettingRaw null value = %q, want empty", raw)
	}
}

func TestSettingHasValueEdgeCases(t *testing.T) {
	if settingHasValue("") {
		t.Fatal("settingHasValue empty should be false")
	}
	if settingHasValue("null") {
		t.Fatal("settingHasValue null should be false")
	}
	if !settingHasValue(`"hello"`) {
		t.Fatal("settingHasValue string should be true")
	}
	if !settingHasValue("123") {
		t.Fatal("settingHasValue number should be true")
	}
}

func TestUserSettingStringBoolValueEdges(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)

	if got := userSettingStringValue(s.DB, user.ID, "missing.key"); got != "" {
		t.Fatalf("userSettingStringValue missing = %q, want empty", got)
	}
	if got := userSettingBoolValue(s.DB, user.ID, "missing.key"); got {
		t.Fatal("userSettingBoolValue missing should be false")
	}

	if err := s.SetSetting(user.ID, "bool.true.string", "true", false); err != nil {
		t.Fatalf("SetSetting bool.true.string: %v", err)
	}
	if !userSettingBoolValue(s.DB, user.ID, "bool.true.string") {
		t.Fatal("userSettingBoolValue true string should be true")
	}

	if err := s.SetSetting(user.ID, "string.raw", "just-a-string", false); err != nil {
		t.Fatalf("SetSetting string.raw: %v", err)
	}
	if got := userSettingStringValue(s.DB, user.ID, "string.raw"); got != "just-a-string" {
		t.Fatalf("userSettingStringValue raw = %q, want just-a-string", got)
	}
}

func TestOpenSQLiteErrorOnPing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.db")
	if err := os.WriteFile(path, []byte("not a database"), 0o600); err != nil {
		t.Fatalf("WriteFile corrupt: %v", err)
	}
	if _, err := openSQLite(path); err == nil {
		t.Fatal("openSQLite should fail on corrupt database")
	}
}

func TestMigrateLegacyDataAttachError(t *testing.T) {
	base := t.TempDir()
	target, err := openSQLite(filepath.Join(base, "target.db"))
	if err != nil {
		t.Fatalf("openSQLite target: %v", err)
	}
	defer target.Close()
	if err := createSchema(target); err != nil {
		t.Fatalf("createSchema target: %v", err)
	}

	legacyPath := filepath.Join(base, "legacy-data.db")
	legacyDB, err := openSQLite(legacyPath)
	if err != nil {
		t.Fatalf("openSQLite legacy: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE _collections (id TEXT PRIMARY KEY, name TEXT NOT NULL)`,
		`CREATE TABLE _migrations (id TEXT PRIMARY KEY)`,
	} {
		if _, err := legacyDB.Exec(stmt); err != nil {
			t.Fatalf("create legacy schema: %v", err)
		}
	}
	if _, err := legacyDB.Exec(`INSERT INTO _migrations(id) VALUES('m1')`); err != nil {
		t.Fatalf("insert migration: %v", err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	if err := target.Close(); err != nil {
		t.Fatalf("close target db: %v", err)
	}
	if err := migrateLegacyData(target, legacyPath); err == nil {
		t.Fatal("migrateLegacyData should fail on closed target db")
	}
}

func TestNewS3ClientEndpointWithoutScheme(t *testing.T) {
	cfg := &LegacyS3Config{
		Enabled:   true,
		Bucket:    "bucket",
		Region:    "us-east-1",
		AccessKey: "key",
		Secret:    "secret",
		Endpoint:  "s3.example.com",
	}
	client, err := newS3Client(cfg)
	if err != nil {
		t.Fatalf("newS3Client endpoint without scheme: %v", err)
	}
	if client == nil {
		t.Fatal("newS3Client should return non-nil client")
	}
}

func TestUserLocalMediaDirErrorPath(t *testing.T) {
	s := newTestStore(t)
	dir := s.userLocalMediaDir("non-existent-user")
	if dir == "" {
		t.Fatal("userLocalMediaDir should return default for missing user")
	}
}

func TestOpenMediaCollectionIDDefault(t *testing.T) {
	base := t.TempDir()
	dbPath := filepath.Join(base, DatabaseName)
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("openSQLite: %v", err)
	}
	if err := createSchema(db); err != nil {
		t.Fatalf("createSchema: %v", err)
	}
	if err := setMeta(db, "auth.secret", "ab"); err != nil {
		t.Fatalf("setMeta auth.secret: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	s, err := Open(base)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if s.MediaCollectionID != DefaultMediaCollectionID {
		t.Fatalf("MediaCollectionID = %q, want %q", s.MediaCollectionID, DefaultMediaCollectionID)
	}
	if string(s.AuthSecret) != "\xab" {
		t.Fatalf("AuthSecret = %q, want \\xab", string(s.AuthSecret))
	}
}

func TestOpenS3ClientInitError(t *testing.T) {
	base := t.TempDir()
	dbPath := filepath.Join(base, DatabaseName)
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("openSQLite: %v", err)
	}
	if err := createSchema(db); err != nil {
		t.Fatalf("createSchema: %v", err)
	}
	if err := setMeta(db, "auth.secret", "ab"); err != nil {
		t.Fatalf("setMeta auth.secret: %v", err)
	}
	if err := setMeta(db, "legacy.media_collection_id", DefaultMediaCollectionID); err != nil {
		t.Fatalf("setMeta media_collection_id: %v", err)
	}
	if err := setMeta(db, "legacy.s3", `{"enabled":true,"bucket":"b","region":"us-east-1","endpoint":"invalid\u0000","accessKey":"a","secret":"s"}`); err != nil {
		t.Fatalf("setMeta legacy.s3: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	s, err := Open(base)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if s.LegacyS3 == nil {
		t.Fatal("LegacyS3 should be loaded")
	}
}

func TestOpenBackupLegacyDataError(t *testing.T) {
	base := t.TempDir()
	oldPath := filepath.Join(base, LegacyDatabaseName)
	legacyDB, err := openSQLite(oldPath)
	if err != nil {
		t.Fatalf("open legacy: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE _collections (id TEXT PRIMARY KEY, name TEXT NOT NULL)`,
		`CREATE TABLE _migrations (id TEXT PRIMARY KEY)`,
	} {
		if _, err := legacyDB.Exec(stmt); err != nil {
			t.Fatalf("create legacy: %v", err)
		}
	}
	if _, err := legacyDB.Exec(`INSERT INTO _migrations(id) VALUES('m1')`); err != nil {
		t.Fatalf("insert migration: %v", err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("close legacy: %v", err)
	}

	backupsPath := filepath.Join(base, "backups")
	if err := os.WriteFile(backupsPath, []byte("block"), 0o600); err != nil {
		t.Fatalf("WriteFile block backups dir: %v", err)
	}

	if _, err := Open(base); err == nil {
		t.Fatal("Open should fail when backup directory creation fails")
	}
}

func TestSettingHasValueInvalidJSON(t *testing.T) {
	if settingHasValue(`{invalid`) != true {
		t.Fatal("settingHasValue should be true for invalid non-empty JSON")
	}
}

func TestUserSettingStringBoolValueInvalidJSON(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)

	if err := s.SetSetting(user.ID, "invalid.json", "{broken", false); err != nil {
		t.Fatalf("SetSetting invalid.json: %v", err)
	}

	if got := userSettingStringValue(s.DB, user.ID, "invalid.json"); got != "{broken" {
		t.Fatalf("userSettingStringValue invalid json = %q, want {broken", got)
	}
	if got := userSettingBoolValue(s.DB, user.ID, "invalid.json"); got {
		t.Fatal("userSettingBoolValue invalid json should be false")
	}
}

func TestEnsureImageUploadSettingsContinueWhenSettingExists(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)

	if err := s.SetSetting(user.ID, "image_upload.provider", "local", false); err != nil {
		t.Fatalf("SetSetting provider: %v", err)
	}

	legacyS3 := &LegacyS3Config{Enabled: true, Bucket: "b", Region: "r", Endpoint: "e", AccessKey: "a", Secret: "s", ForcePathStyle: true}
	if err := ensureImageUploadSettings(s.DB, s.DataDir, legacyS3); err != nil {
		t.Fatalf("ensureImageUploadSettings with existing provider: %v", err)
	}
	if got := userSettingStringValue(s.DB, user.ID, "image_upload.provider"); got != "local" {
		t.Fatalf("existing provider should be preserved, got %q", got)
	}
}

func TestIsLegacyDataDBCorruptDB(t *testing.T) {
	base := t.TempDir()
	corruptPath := filepath.Join(base, "corrupt.db")
	if err := os.WriteFile(corruptPath, []byte("not a database"), 0o600); err != nil {
		t.Fatalf("WriteFile corrupt: %v", err)
	}
	if isLegacyDataDB(corruptPath) {
		t.Fatal("isLegacyDataDB should return false for corrupt database")
	}
}

func TestSaveUploadedMediaS3WithoutCfg(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)

	if err := s.SetSetting(user.ID, "image_upload.provider", "s3", false); err != nil {
		t.Fatalf("SetSetting provider s3: %v", err)
	}
	media := &Media{ID: "mid", File: "f.png", Owner: user.ID}
	if err := s.SaveUploadedMedia(media, bytes.NewBufferString("x")); err == nil || !strings.Contains(err.Error(), "s3 settings are incomplete") {
		t.Fatalf("SaveUploadedMedia s3 incomplete error = %v", err)
	}
}

func TestEnsureImageUploadSettingsDroppedTable(t *testing.T) {
	s := newTestStore(t)
	_ = newTestUser(t, s)

	if _, err := s.DB.Exec(`DROP TABLE IF EXISTS user_settings`); err != nil {
		t.Fatalf("drop user_settings: %v", err)
	}

	err := ensureImageUploadSettings(s.DB, s.DataDir, nil)
	if err == nil {
		t.Fatal("ensureImageUploadSettings should fail without user_settings table")
	}
}
