package imaging

import (
	"bytes"
	"mime"
	"path/filepath"
	"strings"
)

// Videos are stored and served as uploaded, without transcoding: browsers
// play them straight from the file, streaming with range requests. Only
// containers browsers can play are accepted: MP4 (also .m4v), QuickTime
// (.mov, the same ISO base media format), WebM and Ogg.

// MaxVideoBytes caps the size of an uploaded video.
const MaxVideoBytes = 200 << 20

// PosterSuffix ends the file name of a video's poster frame, which the
// browser captures on upload: clip.mp4 -> clip.poster.jpg.
const PosterSuffix = ".poster.jpg"

// MaxPosterBytes caps the size of a video's poster frame.
const MaxPosterBytes = 5 << 20

// videoTypes maps the video extensions Diarum keeps to the type they are
// served as. QuickTime and M4V are served as MP4, which every browser that
// can decode their codecs accepts (video/quicktime is refused by some).
var videoTypes = map[string]string{
	".mp4":  "video/mp4",
	".m4v":  "video/mp4",
	".mov":  "video/mp4",
	".webm": "video/webm",
	".ogv":  "video/ogg",
}

func init() {
	// Go's built-in table has no video types, and the container may have no
	// /etc/mime.types: register the ones videos are served as.
	for ext, contentType := range videoTypes {
		_ = mime.AddExtensionType(ext, contentType)
	}
}

// IsVideo reports whether filename is a stored video.
func IsVideo(filename string) bool {
	_, ok := videoTypes[strings.ToLower(filepath.Ext(filename))]
	return ok
}

// VideoContentType is the type a stored video is served as; "" for a file
// that is not a video.
func VideoContentType(filename string) string {
	return videoTypes[strings.ToLower(filepath.Ext(filename))]
}

// PosterFilename returns the poster's file name for a video: "a.mp4" ->
// "a.poster.jpg".
func PosterFilename(video string) string {
	return strings.TrimSuffix(video, filepath.Ext(video)) + PosterSuffix
}

// DetectVideo reports the container of a video from its first bytes, as the
// extension it is stored under (".mp4", ".webm" or ".ogv"); "" when head is
// not a video Diarum accepts.
func DetectVideo(head []byte) string {
	switch {
	case IsLiveVideo(head):
		return ".mp4"
	case bytes.HasPrefix(head, []byte{0x1A, 0x45, 0xDF, 0xA3}):
		// EBML: WebM, or Matroska, which browsers that play WebM also read.
		return ".webm"
	case bytes.HasPrefix(head, []byte("OggS")):
		return ".ogv"
	}
	return ""
}

// VideoFilename gives an uploaded video's file name the extension of its
// actual container, keeping the name. MP4-family names (.mp4, .m4v, .mov)
// are left alone, as they share one container.
func VideoFilename(name, container string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if container == ".mp4" && (ext == ".mp4" || ext == ".m4v" || ext == ".mov") {
		return name
	}
	if ext == container {
		return name
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if base == "" {
		base = "video"
	}
	return base + container
}
