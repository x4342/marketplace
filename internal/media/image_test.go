package media

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"testing"
)

func encodePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestSquareLimits(t *testing.T) {
	wide := encodePNG(t, 2912, 2600)
	result, err := Square(wide, Crop{})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(result.JPEG))
	if err != nil || cfg.Width != MaxSide || cfg.Height != MaxSide || len(result.JPEG) > MaxUpload {
		t.Fatalf("unexpected result: %v, %d bytes, %v", cfg, len(result.JPEG), err)
	}
	bomb := encodePNG(t, 256, 256)
	binary.BigEndian.PutUint32(bomb[16:20], 8193)
	binary.BigEndian.PutUint32(bomb[29:33], crc32.ChecksumIEEE(bomb[12:29]))
	for _, input := range []struct {
		name string
		raw  []byte
		crop Crop
	}{
		{"unsupported", []byte("<svg/>"), Crop{}},
		{"truncated", wide[:len(wide)/2], Crop{}},
		{"minimum side", encodePNG(t, 255, 256), Crop{}},
		{"byte limit", make([]byte, MaxUpload+1), Crop{}},
		{"header edge limit", bomb, Crop{}},
		{"crop out of bounds", wide, Crop{2700, 0, 256}},
		{"crop below minimum", wide, Crop{0, 0, 255}},
	} {
		t.Run(input.name, func(t *testing.T) {
			if result, err := Square(input.raw, input.crop); err == nil || len(result.JPEG) != 0 {
				t.Fatalf("unexpected acceptance: %v", err)
			}
		})
	}
	writer := limitedBuffer{limit: MaxUpload}
	if _, err := writer.Write(make([]byte, MaxUpload)); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte{0}); err != errOutputLimit {
		t.Fatalf("expected bounded output, got %v", err)
	}
}

func TestOrientationCropAndMetadataRemoval(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 512, 256))
	draw.Draw(img, image.Rect(0, 0, 256, 256), image.NewUniform(color.RGBA{R: 240, A: 255}), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(256, 0, 512, 256), image.NewUniform(color.RGBA{B: 240, A: 255}), image.Point{}, draw.Src)
	var data bytes.Buffer
	if err := jpeg.Encode(&data, img, nil); err != nil {
		t.Fatal(err)
	}
	exif := append([]byte("Exif\x00\x00II\x2a\x00\x08\x00\x00\x00\x01\x00\x12\x01\x03\x00\x01\x00\x00\x00\x06\x00\x00\x00\x00\x00\x00\x00"), []byte("private GPS fixture")...)
	input := append([]byte{255, 216, 255, 225, 0, byte(2 + len(exif))}, exif...)
	input = append(input, data.Bytes()[2:]...)
	for _, test := range []struct {
		crop Crop
		blue bool
	}{{Crop{0, 0, 256}, false}, {Crop{0, 256, 256}, true}} {
		result, err := Square(input, test.crop)
		if err != nil {
			t.Fatal(err)
		}
		if result.Source != image.Pt(256, 512) || bytes.Contains(result.JPEG, []byte("Exif")) || bytes.Contains(result.JPEG, []byte("private GPS fixture")) {
			t.Fatal("incorrect orientation or leaked EXIF")
		}
		decoded, err := jpeg.Decode(bytes.NewReader(result.JPEG))
		if err != nil || decoded.Bounds().Size() != image.Pt(256, 256) {
			t.Fatalf("unexpected output: %v", err)
		}
		r, _, b, _ := decoded.At(128, 128).RGBA()
		if (b > r) != test.blue {
			t.Fatal("crop selected the wrong oriented region")
		}
	}
}

func TestHDRAndICCValidation(t *testing.T) {
	for _, transfer := range []byte{16, 18} {
		fn, err := cicpTransform([]byte{9, transfer, 0, 1})
		if err != nil {
			t.Fatal(err)
		}
		r, g, b := fn(.8, .8, .8)
		if r <= .2 || r >= 1 || g <= .2 || g >= 1 || b <= .2 || b >= 1 {
			t.Fatalf("unexpected tone mapping: %f,%f,%f", r, g, b)
		}
	}
	if _, err := iccTransform(make([]byte, 133)); err == nil {
		t.Fatal("malformed profile was accepted")
	}
}

func TestWebPDecode(t *testing.T) {
	input, err := base64.StdEncoding.DecodeString("UklGRigAAABXRUJQVlA4TBsAAAAvP8E/AAfQoO4VpP8BAUnS//9kRP8z/vN/AAAA")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Square(input, Crop{})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(result.JPEG))
	if err != nil || cfg.Width != 256 || cfg.Height != 256 {
		t.Fatalf("unexpected WebP conversion: %v (%v)", cfg, err)
	}
}

func TestAvatarDerivative(t *testing.T) {
	image, err := Avatar(encodePNG(t, 500, 320), Crop{X: 175, Y: 25, Side: 280})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(image.JPEG))
	if err != nil || cfg.Width != 256 || cfg.Height != 256 || len(image.JPEG) >= 512*1024 {
		t.Fatalf("invalid avatar dimensions or size: %v, %d, %v", cfg, len(image.JPEG), err)
	}
}
