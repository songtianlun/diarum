package imaging

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"strings"
)

// Live photos (Apple Live Photos, Android/HarmonyOS motion photos) are kept
// as their still image plus the short clip that goes with it. Whatever the
// source format, the clip is stored beside the original under one name,
// photo.jpg -> photo.live.mp4, and served as MP4: QuickTime clips from
// iPhones are the same ISO base media format and play as such.

// LiveSuffix ends the file name of a live photo's clip.
const LiveSuffix = ".live.mp4"

// MaxLiveVideoBytes caps the size of a live photo's clip. Real ones are a
// few megabytes (about three seconds); this leaves room for 4K/HDR clips.
const MaxLiveVideoBytes = 50 << 20

// LiveFilename returns the clip's file name for an original: "a.jpg" ->
// "a.live.mp4".
func LiveFilename(original string) string {
	return strings.TrimSuffix(original, filepath.Ext(original)) + LiveSuffix
}

// imageBrands are ftyp brands of still images in an ISO base media file
// (HEIF/HEIC/AVIF), which must not pass for a clip.
var imageBrands = map[string]bool{
	"heic": true, "heix": true, "heim": true, "heis": true,
	"hevc": true, "hevx": true, "hevm": true, "hevs": true,
	"mif1": true, "msf1": true, "avif": true, "avis": true, "avio": true,
}

// leadingBoxes are the top-level box types a QuickTime or MP4 file may start
// with: ftyp normally, the others in old QuickTime files without one.
var leadingBoxes = map[string]bool{
	"ftyp": true, "moov": true, "mdat": true, "wide": true, "free": true, "skip": true,
}

// IsLiveVideo reports whether head, the first bytes of a file, looks like an
// MP4 or QuickTime video. It checks the first box, and its major brand when
// that is a file type box.
func IsLiveVideo(head []byte) bool {
	if len(head) < 12 {
		return false
	}
	size := binary.BigEndian.Uint32(head[:4])
	kind := string(head[4:8])
	if !leadingBoxes[kind] || (size != 0 && size != 1 && size < 8) {
		return false
	}
	if kind != "ftyp" {
		return true
	}
	if size < 16 || len(head) < 16 {
		return false
	}
	brand := strings.TrimSpace(string(bytes.ToLower(head[8:12])))
	return brand != "" && !imageBrands[brand]
}
