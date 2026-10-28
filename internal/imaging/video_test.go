package imaging

import "testing"

func TestDetectVideo(t *testing.T) {
	cases := map[string]string{
		"\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isomiso2": ".mp4",
		"\x00\x00\x00\x14ftypqt  \x00\x00\x00\x00":         ".mp4",
		"\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic": "",
		"\x1a\x45\xdf\xa3\x9f\x42\x86\x81\x01\x42\xf7\x81": ".webm",
		"OggS\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00":     ".ogv",
		"\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00":     "",
	}
	for head, want := range cases {
		if got := DetectVideo([]byte(head)); got != want {
			t.Errorf("DetectVideo(%q) = %q, want %q", head, got, want)
		}
	}
}

func TestVideoNames(t *testing.T) {
	names := map[[2]string]string{
		{"a.m4v", ".mp4"}:  "a.m4v",
		{"a.MOV", ".mp4"}:  "a.MOV",
		{"a.avi", ".mp4"}:  "a.mp4",
		{"a.mkv", ".webm"}: "a.webm",
		{"a.ogg", ".ogv"}:  "a.ogv",
		{".mp4", ".webm"}:  "video.webm",
	}
	for in, want := range names {
		if got := VideoFilename(in[0], in[1]); got != want {
			t.Errorf("VideoFilename(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
	if !IsVideo("a.M4V") || IsVideo("a.jpg") || IsVideo("a.live.jpg") {
		t.Error("IsVideo")
	}
	if VideoContentType("a.mov") != "video/mp4" || VideoContentType("a.webm") != "video/webm" {
		t.Error("VideoContentType")
	}
	if PosterFilename("trip.m4v") != "trip.poster.jpg" {
		t.Error("PosterFilename")
	}
}
