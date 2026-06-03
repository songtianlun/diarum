package logger

import "testing"

func TestSetAndGetLevel(t *testing.T) {
	original := GetLevel()
	t.Cleanup(func() {
		SetLevel(original)
	})

	SetLevel(LevelDebug)
	if GetLevel() != LevelDebug {
		t.Fatalf("GetLevel() = %v, want %v", GetLevel(), LevelDebug)
	}

	Debug("debug")
	Info("info")
	Warn("warn")
	Error("error")
}
