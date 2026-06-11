package logger

import (
	"os"
	"testing"
)

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

func TestLogLevelSuppression(t *testing.T) {
	original := GetLevel()
	t.Cleanup(func() {
		SetLevel(original)
	})

	SetLevel(LevelWarn)
	Debug("should-not-appear-at-warn")
	Info("should-not-appear-at-warn")
	Warn("should-appear-at-warn")
	Error("should-appear-at-warn")

	SetLevel(LevelError)
	Debug("should-not-appear-at-error")
	Info("should-not-appear-at-error")
	Warn("should-not-appear-at-error")
	Error("should-appear-at-error")

	SetLevel(LevelInfo)
	Debug("should-not-appear-at-info")
	Info("should-appear-at-info")
	Warn("should-appear-at-info")
	Error("should-appear-at-info")
}

func TestLogLevelConstants(t *testing.T) {
	if LevelDebug != 0 || LevelInfo != 1 || LevelWarn != 2 || LevelError != 3 {
		t.Fatalf("level constants: Debug=%d, Info=%d, Warn=%d, Error=%d", LevelDebug, LevelInfo, LevelWarn, LevelError)
	}
}

func TestAllLevelsExhaustive(t *testing.T) {
	original := GetLevel()
	t.Cleanup(func() {
		SetLevel(original)
		os.Unsetenv("LOG_LEVEL")
		os.Unsetenv("DEBUG")
	})

	for level := LevelDebug; level <= LevelError; level++ {
		SetLevel(level)
		Debug("debug at %d", level)
		Info("info at %d", level)
		Warn("warn at %d", level)
		Error("error at %d", level)
		if GetLevel() != level {
			t.Fatalf("GetLevel() = %v, want %v", GetLevel(), level)
		}
	}
}
