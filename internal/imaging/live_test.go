package imaging

import "testing"

func TestLiveFilename(t *testing.T) {
	for in, want := range map[string]string{
		"IMG_0001.jpg":  "IMG_0001.live.mp4",
		"photo.MP.jpeg": "photo.MP.live.mp4",
		"noext":         "noext.live.mp4",
	} {
		if got := LiveFilename(in); got != want {
			t.Fatalf("LiveFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func box(size uint32, kind string, rest ...byte) []byte {
	out := []byte{byte(size >> 24), byte(size >> 16), byte(size >> 8), byte(size)}
	out = append(out, kind...)
	return append(out, rest...)
}

func TestIsLiveVideo(t *testing.T) {
	brand := func(b string) []byte { return append([]byte(b), 0, 0, 0, 0) }
	cases := map[string]struct {
		head []byte
		want bool
	}{
		"mp4":            {box(24, "ftyp", brand("isom")...), true},
		"quicktime":      {box(20, "ftyp", brand("qt  ")...), true},
		"android mp42":   {box(24, "ftyp", brand("mp42")...), true},
		"old quicktime":  {box(8, "wide", 0, 0, 0, 0), true},
		"size to eof":    {box(0, "mdat", 0, 0, 0, 0), true},
		"heic image":     {box(24, "ftyp", brand("heic")...), false},
		"avif image":     {box(24, "ftyp", brand("avif")...), false},
		"mif1 image":     {box(24, "ftyp", brand("MIF1")...), false},
		"empty brand":    {box(16, "ftyp", brand("    ")...), false},
		"short ftyp box": {box(12, "ftyp", brand("isom")...), false},
		"truncated ftyp": {box(24, "ftyp", 'i', 's', 'o', 'm')[:12], false},
		"bad box size":   {box(4, "moov", 0, 0, 0, 0), false},
		"jpeg":           {[]byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 16, 'J', 'F', 'I', 'F', 0, 1, 1, 0}, false},
		"too short":      {[]byte("ftyp"), false},
	}
	for name, tc := range cases {
		if got := IsLiveVideo(tc.head); got != tc.want {
			t.Fatalf("%s: IsLiveVideo = %v, want %v", name, got, tc.want)
		}
	}
}
