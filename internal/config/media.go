package config

import (
	"bytes"
	"net/http"
	"strings"
)

// AllowedMediaMimeTypes defines the allowed MIME types for media files
// This is the single source of truth for media type validation
var AllowedMediaMimeTypes = []string{
	"image/jpeg",
	"image/png",
	"image/gif",
	"image/webp",
	"image/svg+xml",
}

// allowedMediaMimeSet is a map for fast lookup
var allowedMediaMimeSet = func() map[string]bool {
	m := make(map[string]bool, len(AllowedMediaMimeTypes))
	for _, t := range AllowedMediaMimeTypes {
		m[t] = true
	}
	return m
}()

// IsAllowedMediaType checks if the file content has an allowed MIME type
// Returns the detected MIME type and whether it's allowed
func IsAllowedMediaType(data []byte) (string, bool) {
	// http.DetectContentType reads at most 512 bytes
	mimeType := http.DetectContentType(data)
	// DetectContentType may return "text/xml; charset=utf-8" for SVG
	// so we need to handle the base type
	baseMime := strings.Split(mimeType, ";")[0]
	baseMime = strings.TrimSpace(baseMime)

	// Special handling for SVG (may be detected as text/xml or application/xml)
	if baseMime == "text/xml" || baseMime == "application/xml" {
		// Check if it looks like SVG by examining content
		checkLen := len(data)
		if checkLen > 1024 {
			checkLen = 1024
		}
		if checkLen > 0 && (bytes.Contains(data[:checkLen], []byte("<svg")) ||
			bytes.Contains(data[:checkLen], []byte("<!DOCTYPE svg"))) {
			return "image/svg+xml", true
		}
	}

	return baseMime, allowedMediaMimeSet[baseMime]
}
