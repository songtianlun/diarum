package imaging

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func testImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// Left half red, right half blue: lets tests see rotations.
			if x < w/2 {
				img.Set(x, y, color.RGBA{255, 0, 0, 255})
			} else {
				img.Set(x, y, color.RGBA{0, 0, 255, 255})
			}
		}
	}
	return img
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

// withOrientation inserts an EXIF APP1 segment carrying the orientation tag.
func withOrientation(data []byte, orientation uint16) []byte {
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08")
	tiff = append(tiff, 0x00, 0x01) // one IFD entry
	entry := make([]byte, 12)
	binary.BigEndian.PutUint16(entry[0:], 0x0112)
	binary.BigEndian.PutUint16(entry[2:], 3) // SHORT
	binary.BigEndian.PutUint32(entry[4:], 1)
	binary.BigEndian.PutUint16(entry[8:], orientation)
	tiff = append(tiff, entry...)
	tiff = append(tiff, 0, 0, 0, 0)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	segment := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(segment[2:], uint16(len(payload)+2))
	segment = append(segment, payload...)
	out := append([]byte{}, data[:2]...)
	out = append(out, segment...)
	return append(out, data[2:]...)
}

func decodeSize(t *testing.T, data []byte) (int, int) {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode variant: %v", err)
	}
	return cfg.Width, cfg.Height
}

func TestVariantFilenames(t *testing.T) {
	if got := VariantFilename("photo.jpg", Medium); got != "photo.md.jpg" {
		t.Fatalf("VariantFilename = %q", got)
	}
	if got := VariantFilename("a.b.png", Thumb); got != "a.b.th.png" {
		t.Fatalf("VariantFilename = %q", got)
	}
	if v, ok := ParseVariant("photo.jpg", "photo.th.jpg"); !ok || v != Thumb {
		t.Fatalf("ParseVariant th = %v %v", v, ok)
	}
	for _, name := range []string{"photo.jpg", "photo.xx.jpg", "other.md.jpg"} {
		if _, ok := ParseVariant("photo.jpg", name); ok {
			t.Fatalf("ParseVariant(%q) should not match", name)
		}
	}
	if Supported("a.gif") || Supported("a.webp") || Supported("a.svg") || !Supported("A.JPEG") {
		t.Fatal("Supported mismatch")
	}
}

func TestGenerateSizes(t *testing.T) {
	variants, err := Generate(encodeJPEG(t, testImage(3000, 2000)), "big.jpg")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if w, h := decodeSize(t, variants[Medium]); w != 1440 || h != 960 {
		t.Fatalf("medium = %dx%d", w, h)
	}
	if w, h := decodeSize(t, variants[Thumb]); w != 480 || h != 320 {
		t.Fatalf("thumb = %dx%d", w, h)
	}

	// Between the boxes: only a thumbnail, never an upscaled medium.
	variants, err = Generate(encodeJPEG(t, testImage(1000, 600)), "mid.jpg")
	if err != nil {
		t.Fatalf("Generate mid: %v", err)
	}
	if _, ok := variants[Medium]; ok {
		t.Fatal("medium should be skipped for a 1000px image")
	}
	if w, _ := decodeSize(t, variants[Thumb]); w != 480 {
		t.Fatalf("thumb width = %d", w)
	}

	variants, err = Generate(encodeJPEG(t, testImage(300, 200)), "small.jpg")
	if err != nil || len(variants) != 0 {
		t.Fatalf("small image variants = %v, err %v", len(variants), err)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, testImage(1200, 1800)); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	variants, err = Generate(buf.Bytes(), "tall.png")
	if err != nil {
		t.Fatalf("Generate png: %v", err)
	}
	if _, format, _ := image.DecodeConfig(bytes.NewReader(variants[Thumb])); format != "png" {
		t.Fatalf("png thumb format = %s", format)
	}
	if w, h := decodeSize(t, variants[Medium]); w != 960 || h != 1440 {
		t.Fatalf("png medium = %dx%d", w, h)
	}

	if _, err := Generate([]byte("GIF89a"), "anim.gif"); err != ErrUnsupported {
		t.Fatalf("gif err = %v", err)
	}
	if _, err := Generate([]byte("not an image"), "broken.jpg"); err == nil {
		t.Fatal("broken image should fail")
	}
}

func TestGenerateAppliesOrientation(t *testing.T) {
	data := withOrientation(encodeJPEG(t, testImage(2000, 1000)), 6)
	if got := jpegOrientation(data); got != 6 {
		t.Fatalf("jpegOrientation = %d", got)
	}
	variants, err := Generate(data, "phone.jpg")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	thumb, _, err := image.Decode(bytes.NewReader(variants[Thumb]))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	b := thumb.Bounds()
	if b.Dx() != 240 || b.Dy() != 480 {
		t.Fatalf("rotated thumb = %dx%d, want 240x480", b.Dx(), b.Dy())
	}
	// Rotating 90° clockwise puts the red (left) half on top.
	top, _, _, _ := thumb.At(b.Dx()/2, b.Dy()/4).RGBA()
	bottom, _, _, _ := thumb.At(b.Dx()/2, b.Dy()*3/4).RGBA()
	if top < 0x8000 || bottom > 0x8000 {
		t.Fatalf("orientation not applied: top red=%x bottom red=%x", top, bottom)
	}

	if got := jpegOrientation([]byte("nope")); got != 1 {
		t.Fatalf("invalid data orientation = %d", got)
	}
}
