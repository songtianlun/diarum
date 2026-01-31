package logger

import (
	"log"
	"os"
	"strings"
)

// Level represents the logging level
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

var currentLevel = LevelInfo

func init() {
	// Set log level from environment variable
	level := strings.ToUpper(os.Getenv("LOG_LEVEL"))
	switch level {
	case "DEBUG":
		currentLevel = LevelDebug
	case "INFO":
		currentLevel = LevelInfo
	case "WARN":
		currentLevel = LevelWarn
	case "ERROR":
		currentLevel = LevelError
	default:
		// Also check DEBUG env var for backwards compatibility
		if os.Getenv("DEBUG") != "" {
			currentLevel = LevelDebug
		}
	}
}

// Debug logs debug level messages
func Debug(format string, v ...any) {
	if currentLevel <= LevelDebug {
		log.Printf("[DEBUG] "+format, v...)
	}
}

// Info logs info level messages
func Info(format string, v ...any) {
	if currentLevel <= LevelInfo {
		log.Printf("[INFO] "+format, v...)
	}
}

// Warn logs warning level messages
func Warn(format string, v ...any) {
	if currentLevel <= LevelWarn {
		log.Printf("[WARN] "+format, v...)
	}
}

// Error logs error level messages
func Error(format string, v ...any) {
	if currentLevel <= LevelError {
		log.Printf("[ERROR] "+format, v...)
	}
}

// SetLevel sets the current logging level
func SetLevel(level Level) {
	currentLevel = level
}

// GetLevel returns the current logging level
func GetLevel() Level {
	return currentLevel
}
