// Package imaging builds the downscaled variants of uploaded images.
//
// Variants follow Chevereto's naming, next to the original file:
// photo.jpg -> photo.th.jpg (thumbnail) and photo.md.jpg (medium). The
// frontend can then derive a smaller URL the same way for both backends.
package imaging

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
)

// Variant identifies a downscaled copy of an image.
type Variant string

const (
	Thumb  Variant = "th"
	Medium Variant = "md"
)

// Variants lists the variants in the order they are generated: each one is
// scaled from the previous, larger result.
var Variants = []Variant{Medium, Thumb}

// maxEdge is the bounding box (both width and height) of each variant.
var maxEdge = map[Variant]int{
	Thumb:  480,
	Medium: 1440,
}

// ErrUnsupported is returned for formats that are served as-is: GIF (would
// lose animation), SVG (already scalable) and WebP (no pure-Go encoder).
var ErrUnsupported = errors.New("image format has no variants")

// VariantFilename returns the variant's file name: "a.jpg" -> "a.md.jpg".
func VariantFilename(filename string, v Variant) string {
	ext := filepath.Ext(filename)
	return strings.TrimSuffix(filename, ext) + "." + string(v) + ext
}

// ParseVariant reports which variant of original the requested file name is.
func ParseVariant(original, requested string) (Variant, bool) {
	for _, v := range Variants {
		if requested == VariantFilename(original, v) {
			return v, true
		}
	}
	return "", false
}

// Supported reports whether variants can be generated for this file name.
func Supported(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".jpg", ".jpeg", ".png":
		return true
	}
	return false
}

// Generate decodes an image and returns its encoded variants. A variant is
// omitted when the original already fits its box: serving the original is
// then just as fast, and never upscaling keeps quality.
func Generate(data []byte, filename string) (map[Variant][]byte, error) {
	if !Supported(filename) {
		return nil, ErrUnsupported
	}
	// Reading the header is cheap; skip decoding images that need no variant.
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if smallest := maxEdge[Thumb]; config.Width <= smallest && config.Height <= smallest {
		return map[Variant][]byte{}, nil
	}
	src, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	orientation := 1
	if format == "jpeg" {
		orientation = jpegOrientation(data)
	}

	result := make(map[Variant][]byte, len(Variants))
	current := src
	for _, v := range Variants {
		scaled, ok := fit(current, maxEdge[v])
		if !ok {
			continue
		}
		current = scaled
		encoded, err := encode(orient(scaled, orientation), format)
		if err != nil {
			return nil, err
		}
		result[v] = encoded
	}
	return result, nil
}

// fit scales img down to fit a size x size box, keeping its aspect ratio.
func fit(img image.Image, size int) (image.Image, bool) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= size && h <= size {
		return nil, false
	}
	if w >= h {
		h = max(1, h*size/w)
		w = size
	} else {
		w = max(1, w*size/h)
		h = size
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst, true
}

func encode(img image.Image, format string) ([]byte, error) {
	var buf bytes.Buffer
	var err error
	if format == "png" {
		err = (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 82})
	}
	return buf.Bytes(), err
}

// orient applies an EXIF orientation. Browsers honour the tag on the
// original, and the re-encoded variant carries no EXIF, so the rotation must
// be baked in or thumbnails of phone photos come out sideways.
func orient(img image.Image, orientation int) image.Image {
	if orientation < 2 || orientation > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	swap := orientation >= 5
	dw, dh := w, h
	if swap {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch orientation {
			case 2: // mirror horizontal
				dx, dy = w-1-x, y
			case 3: // rotate 180
				dx, dy = w-1-x, h-1-y
			case 4: // mirror vertical
				dx, dy = x, h-1-y
			case 5: // transpose
				dx, dy = y, x
			case 6: // rotate 90 clockwise
				dx, dy = h-1-y, x
			case 7: // transverse
				dx, dy = h-1-y, w-1-x
			case 8: // rotate 90 counter-clockwise
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// jpegOrientation reads the EXIF orientation tag (1 when absent or invalid).
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		if marker == 0xDA || marker == 0xD9 { // image data starts; no EXIF before it
			return 1
		}
		size := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		if size < 2 || i+2+size > len(data) {
			return 1
		}
		segment := data[i+4 : i+2+size]
		if marker == 0xE1 && len(segment) > 6 && string(segment[:6]) == "Exif\x00\x00" {
			return exifOrientation(segment[6:])
		}
		i += 2 + size
	}
	return 1
}

func exifOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	offset := int(order.Uint32(tiff[4:8]))
	if offset+2 > len(tiff) {
		return 1
	}
	count := int(order.Uint16(tiff[offset : offset+2]))
	for i := 0; i < count; i++ {
		entry := offset + 2 + i*12
		if entry+12 > len(tiff) {
			return 1
		}
		if order.Uint16(tiff[entry:entry+2]) == 0x0112 {
			value := int(order.Uint16(tiff[entry+8 : entry+10]))
			if value >= 1 && value <= 8 {
				return value
			}
			return 1
		}
	}
	return 1
}
