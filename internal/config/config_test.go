package config

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/songtianlun/diarum/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()

	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

func newTestUser(t *testing.T, s *store.Store) *store.User {
	t.Helper()

	id, err := store.GenerateID()
	if err != nil {
		t.Fatalf("GenerateID: %v", err)
	}
	user, err := s.CreateUser("user_"+id, id+"@example.com", "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return user
}

func TestRegistryHelpers(t *testing.T) {
	meta, ok := GetConfigMeta("ai.api_key")
	if !ok || meta.Type != "string" || !meta.Encrypted {
		t.Fatalf("GetConfigMeta(ai.api_key) = %#v, %v", meta, ok)
	}
	if !IsEncrypted("chevereto.api_key") {
		t.Fatal("IsEncrypted should be true for chevereto.api_key")
	}
	if IsEncrypted("does.not.exist") {
		t.Fatal("IsEncrypted should be false for unknown keys")
	}
	if got := GetDefault("image_upload.provider"); got != "local" {
		t.Fatalf("GetDefault(image_upload.provider) = %#v, want %q", got, "local")
	}
	if GetDefault("missing") != nil {
		t.Fatal("GetDefault(missing) should be nil")
	}
	if !isSensitiveKey("ai.api_key") || !isSensitiveKey("api.token") || isSensitiveKey("sync.cacheDays") {
		t.Fatal("isSensitiveKey results unexpected")
	}
	if got := maskSensitiveValue("12345678"); got != "***" {
		t.Fatalf("maskSensitiveValue short = %q, want ***", got)
	}
	if got := maskSensitiveValue("1234567890"); got != "1234***7890" {
		t.Fatalf("maskSensitiveValue = %q, want 1234***7890", got)
	}
}

func TestConfigServiceCRUDAndDefaults(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	service := NewConfigService(s)

	value, err := service.Get(user.ID, "image_upload.provider")
	if err != nil {
		t.Fatalf("Get default: %v", err)
	}
	if value != "local" {
		t.Fatalf("Get default value = %#v, want local", value)
	}

	if err := service.Set(user.ID, "api.enabled", true); err != nil {
		t.Fatalf("Set api.enabled: %v", err)
	}
	if err := service.Set(user.ID, "sync.cacheDays", 7); err != nil {
		t.Fatalf("Set sync.cacheDays: %v", err)
	}
	if err := service.Set(user.ID, "api.token", "token-1234"); err != nil {
		t.Fatalf("Set api.token: %v", err)
	}

	boolValue, err := service.GetBool(user.ID, "api.enabled")
	if err != nil {
		t.Fatalf("GetBool(api.enabled): %v", err)
	}
	if !boolValue {
		t.Fatal("GetBool(api.enabled) should be true")
	}

	stringValue, err := service.GetString(user.ID, "api.token")
	if err != nil {
		t.Fatalf("GetString(api.token): %v", err)
	}
	if stringValue != "token-1234" {
		t.Fatalf("GetString(api.token) = %q, want token-1234", stringValue)
	}

	if err := s.SetSetting(user.ID, "bool.as.float", 1, false); err != nil {
		t.Fatalf("SetSetting bool.as.float: %v", err)
	}
	if err := s.SetSetting(user.ID, "bool.as.string", "true", false); err != nil {
		t.Fatalf("SetSetting bool.as.string: %v", err)
	}
	if got, err := service.GetBool(user.ID, "bool.as.float"); err != nil || !got {
		t.Fatalf("GetBool(bool.as.float) = %v, %v", got, err)
	}
	if got, err := service.GetBool(user.ID, "bool.as.string"); err != nil || !got {
		t.Fatalf("GetBool(bool.as.string) = %v, %v", got, err)
	}
	if err := s.SetSetting(user.ID, "bool.zero", 0, false); err != nil {
		t.Fatalf("SetSetting bool.zero: %v", err)
	}
	if err := s.SetSetting(user.ID, "bool.false.string", "false", false); err != nil {
		t.Fatalf("SetSetting bool.false.string: %v", err)
	}
	if err := s.SetSetting(user.ID, "bool.object", map[string]any{"enabled": true}, false); err != nil {
		t.Fatalf("SetSetting bool.object: %v", err)
	}
	if got, err := service.GetBool(user.ID, "bool.zero"); err != nil || got {
		t.Fatalf("GetBool(bool.zero) = %v, %v, want false", got, err)
	}
	if got, err := service.GetBool(user.ID, "bool.false.string"); err != nil || got {
		t.Fatalf("GetBool(bool.false.string) = %v, %v, want false", got, err)
	}
	if got, err := service.GetBool(user.ID, "bool.object"); err != nil || got {
		t.Fatalf("GetBool(bool.object) = %v, %v, want false", got, err)
	}
	if err := s.SetSetting(user.ID, "string.number", 12, false); err != nil {
		t.Fatalf("SetSetting string.number: %v", err)
	}
	if got, err := service.GetString(user.ID, "string.number"); err != nil || got != "" {
		t.Fatalf("GetString(string.number) = %q, %v, want empty", got, err)
	}
	if got, err := service.GetString(user.ID, "missing.string"); err != nil || got != "" {
		t.Fatalf("GetString(missing.string) = %q, %v, want empty", got, err)
	}
	if got, err := service.GetBool(user.ID, "missing.bool"); err != nil || got {
		t.Fatalf("GetBool(missing.bool) = %v, %v, want false", got, err)
	}

	if err := service.SetBatch(user.ID, map[string]any{
		"api.enabled":    false,
		"sync.cacheDays": 14,
		"unknown.key":    "ignored",
	}); err != nil {
		t.Fatalf("SetBatch: %v", err)
	}
	settings, err := service.GetBatch(user.ID)
	if err != nil {
		t.Fatalf("GetBatch: %v", err)
	}
	if settings["sync.cacheDays"] != float64(14) {
		t.Fatalf("GetBatch sync.cacheDays = %#v, want 14", settings["sync.cacheDays"])
	}
	if _, ok := settings["unknown.key"]; ok {
		t.Fatal("GetBatch should not contain unknown.key")
	}

	if err := service.Delete(user.ID, "api.token"); err != nil {
		t.Fatalf("Delete(api.token): %v", err)
	}
	if _, err := s.GetSetting(user.ID, "api.token"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted api.token error = %v, want sql.ErrNoRows", err)
	}

	if err := service.Set(user.ID, "unknown.key", "value"); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("Set unknown key error = %v, want ErrUnknownKey", err)
	}
	if err := service.SetBatch(user.ID, map[string]any{}); err != nil {
		t.Fatalf("SetBatch empty: %v", err)
	}

	if got := service.parseStringValue("hello"); got != "hello" {
		t.Fatalf("parseStringValue string = %q, want hello", got)
	}
	if got := service.parseStringValue(nil); got != "" {
		t.Fatalf("parseStringValue nil = %q, want empty", got)
	}
	if got := service.parseStringValue([]byte("hello")); got != "aGVsbG8=" {
		t.Fatalf("parseStringValue bytes = %q, want aGVsbG8=", got)
	}
}

func TestConfigServiceStoreErrorEdges(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	service := NewConfigService(s)

	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	if got, err := service.GetString(user.ID, "image_upload.provider"); err != nil || got != "local" {
		t.Fatalf("GetString default after store error = %q, %v, want local", got, err)
	}
	if got, err := service.GetString(user.ID, "missing.string"); err != nil || got != "" {
		t.Fatalf("GetString nil default after store error = %q, %v, want empty", got, err)
	}
	if got, err := service.GetBool(user.ID, "api.enabled"); err != nil || got {
		t.Fatalf("GetBool default after store error = %v, %v, want false", got, err)
	}
	settings, err := service.GetBatch(user.ID)
	if err != nil {
		t.Fatalf("GetBatch after store error: %v", err)
	}
	if len(settings) != 0 {
		t.Fatalf("GetBatch after store error = %#v, want empty", settings)
	}
	if err := service.SetBatch(user.ID, map[string]any{"api.enabled": true}); err == nil {
		t.Fatal("SetBatch should return store error after close")
	}
}

func TestValidateTokenAndGetUser(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	service := NewConfigService(s)

	if err := service.Set(user.ID, "api.token", "secret-token"); err != nil {
		t.Fatalf("Set api.token: %v", err)
	}
	if err := service.Set(user.ID, "api.enabled", true); err != nil {
		t.Fatalf("Set api.enabled: %v", err)
	}

	userID, err := service.ValidateTokenAndGetUser("secret-token")
	if err != nil {
		t.Fatalf("ValidateTokenAndGetUser success: %v", err)
	}
	if userID != user.ID {
		t.Fatalf("ValidateTokenAndGetUser user = %q, want %q", userID, user.ID)
	}

	userID, err = service.ValidateTokenAndGetUser("wrong-token")
	if err != nil {
		t.Fatalf("ValidateTokenAndGetUser wrong token error: %v", err)
	}
	if userID != "" {
		t.Fatalf("ValidateTokenAndGetUser wrong token user = %q, want empty", userID)
	}

	if err := service.Set(user.ID, "api.enabled", false); err != nil {
		t.Fatalf("disable api.enabled: %v", err)
	}
	if _, err := service.ValidateTokenAndGetUser("secret-token"); !errors.Is(err, ErrAPIDisabled) {
		t.Fatalf("ValidateTokenAndGetUser disabled error = %v, want ErrAPIDisabled", err)
	}

	noAPIUser := newTestUser(t, s)
	if err := service.Set(noAPIUser.ID, "api.token", "disabled-by-default"); err != nil {
		t.Fatalf("Set disabled-by-default token: %v", err)
	}
	if _, err := service.ValidateTokenAndGetUser("disabled-by-default"); !errors.Is(err, ErrAPIDisabled) {
		t.Fatalf("ValidateTokenAndGetUser default disabled error = %v, want ErrAPIDisabled", err)
	}

	emptyTokenUser := newTestUser(t, s)
	if err := service.Set(emptyTokenUser.ID, "api.token", ""); err != nil {
		t.Fatalf("Set empty api.token: %v", err)
	}
	if err := service.Set(emptyTokenUser.ID, "api.enabled", true); err != nil {
		t.Fatalf("enable empty token user: %v", err)
	}
	userID, err = service.ValidateTokenAndGetUser("")
	if err != nil {
		t.Fatalf("ValidateTokenAndGetUser empty token: %v", err)
	}
	if userID != emptyTokenUser.ID {
		t.Fatalf("ValidateTokenAndGetUser empty token user = %q, want %q", userID, emptyTokenUser.ID)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	if userID, err := service.ValidateTokenAndGetUser("secret-token"); err != nil || userID != "" {
		t.Fatalf("ValidateTokenAndGetUser store error = %q, %v, want empty nil", userID, err)
	}
}

func TestIsAllowedMediaType(t *testing.T) {
	svg := []byte(`<?xml version="1.0" encoding="UTF-8"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"></svg>`)
	if got, allowed := IsAllowedMediaType(svg); !allowed || got != "image/svg+xml" {
		t.Fatalf("IsAllowedMediaType(svg) = %q, %v", got, allowed)
	}

	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x03, 0x01, 0x01, 0x00, 0xc9, 0xfe, 0x92,
		0xef, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
		0x44, 0xae, 0x42, 0x60, 0x82,
	}
	if got, allowed := IsAllowedMediaType(png); !allowed || got != "image/png" {
		t.Fatalf("IsAllowedMediaType(png) = %q, %v", got, allowed)
	}

	if got, allowed := IsAllowedMediaType([]byte("plain text")); allowed || got != "text/plain" {
		t.Fatalf("IsAllowedMediaType(text) = %q, %v", got, allowed)
	}
}
