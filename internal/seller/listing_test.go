package seller

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestReadImageSanitizesAndBoundsListingImages(t *testing.T) {
	encodePNG := func(width, height int) []byte {
		t.Helper()
		var file bytes.Buffer
		if err := png.Encode(&file, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
			t.Fatal(err)
		}
		return file.Bytes()
	}
	const extra = "fixture metadata should not be retained"
	valid := encodePNG(4, 2)
	output, err := ReadImage(bytes.NewReader(append(append([]byte(nil), valid...), extra...)))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(output))
	if err != nil || decoded.Bounds().Dx() != 4 || decoded.Bounds().Dy() != 2 {
		t.Fatalf("expected a normalized JPEG with the original dimensions, decode error: %v", err)
	}
	if bytes.Contains(output, []byte(extra)) {
		t.Fatal("normalized JPEG retained input trailing data")
	}

	for _, sample := range []struct {
		name string
		data []byte
	}{
		{"unsupported", []byte("plain text fixture")},
		{"truncated", valid[:35]},
		{"too wide", encodePNG(2049, 1)},
		{"too tall", encodePNG(1, 2049)},
		{"too many bytes", make([]byte, MaxUpload+1)},
	} {
		t.Run(sample.name, func(t *testing.T) {
			if clean, err := ReadImage(bytes.NewReader(sample.data)); err == nil || len(clean) != 0 {
				t.Fatalf("expected rejected image, got %d output bytes and error %v", len(clean), err)
			}
		})
	}
}
