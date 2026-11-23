// Package media validates photographs and creates metadata-free JPEG derivatives.
package media

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"strconv"
	"strings"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	MaxUpload   = 10_000_000
	MinSide     = 256
	AvatarBytes = 512*1024 - 1
	MaxSide     = 2560
	maxPixels   = 36_000_000
	maxEdge     = 8192
	frameEdge   = 1024
)

var (
	active     = make(chan struct{}, 1)
	errCorrupt = errors.New("This image is corrupt or incomplete.")
	errColor   = errors.New("This image uses an unsupported color profile. Export it as sRGB and try again.")
)

// TryAcquire bounds simultaneous decoding and upload buffering within this process.
func TryAcquire() (release func(), ok bool) {
	select {
	case active <- struct{}{}:
		return func() { <-active }, true
	default:
		return nil, false
	}
}

type Crop struct{ X, Y, Side int }

func ParseCrop(value string) (Crop, error) {
	if value == "" {
		return Crop{}, nil
	}
	parts := strings.Split(value, ",")
	if len(parts) != 3 || len(value) > 20 {
		return Crop{}, errors.New("The crop could not be read. Adjust it and confirm again.")
	}
	var fields [3]int
	for i, part := range parts {
		n, err := strconv.ParseUint(part, 10, 14)
		if err != nil {
			return Crop{}, errors.New("The crop could not be read. Adjust it and confirm again.")
		}
		fields[i] = int(n)
	}
	if fields[2] < MinSide {
		return Crop{}, errors.New("The crop must include at least 256 × 256 source pixels.")
	}
	return Crop{fields[0], fields[1], fields[2]}, nil
}

type Result struct {
	JPEG []byte
	// Source is the full size after applying EXIF orientation.
	Source image.Point
}

func Square(raw []byte, crop Crop) (Result, error) {
	return square(raw, crop, MaxSide, MaxUpload)
}

func Avatar(raw []byte, crop Crop) (Result, error) {
	return square(raw, crop, MinSide, AvatarBytes)
}

func square(raw []byte, crop Crop, maxSide, limit int) (Result, error) {
	src, err := decode(raw)
	if err != nil {
		return Result{}, err
	}
	size := src.Bounds().Size()
	short := min(size.X, size.Y)
	if crop == (Crop{}) {
		crop = Crop{(size.X - short) / 2, (size.Y - short) / 2, short}
	}
	if crop.Side < MinSide || crop.Side > short || crop.X < 0 || crop.Y < 0 ||
		crop.X > size.X-crop.Side || crop.Y > size.Y-crop.Side {
		return Result{}, errors.New("Choose a square crop of at least 256 × 256 pixels inside the image.")
	}
	side := min(crop.Side, maxSide)
	return derivative(src, image.Rect(crop.X, crop.Y, crop.X+crop.Side, crop.Y+crop.Side), side, side, limit)
}

// Frame generates a bounded, oriented preview. It never returns the uploaded bytes.
func Frame(raw []byte) (Result, error) {
	src, err := decode(raw)
	if err != nil {
		return Result{}, err
	}
	size := src.Bounds().Size()
	edge := max(frameEdge, size.X, size.Y)
	return derivative(src, src.Bounds(), max(1, size.X*frameEdge/edge), max(1, size.Y*frameEdge/edge), MaxUpload)
}

func decode(raw []byte) (image.Image, error) {
	if len(raw) == 0 || len(raw) > MaxUpload {
		return nil, errors.New("Choose a nonempty image of 10 MB or less.")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		if format != "" {
			return nil, errCorrupt
		}
		return nil, errors.New("Choose a JPEG, PNG, or WebP photo. Export AVIF, HEIC, or other formats as sRGB JPEG or PNG first.")
	}
	if cfg.Width < MinSide || cfg.Height < MinSide {
		return nil, errors.New("The original image must be at least 256 × 256 pixels.")
	}
	if cfg.Width > maxEdge || cfg.Height > maxEdge || cfg.Width > maxPixels/cfg.Height {
		return nil, errors.New("The image exceeds the safe decoding limit of 36 megapixels or 8192 pixels on one side.")
	}
	info, err := readMetadata(raw, format)
	if err != nil {
		return nil, err
	}
	transform, err := info.transform()
	if err != nil {
		return nil, err
	}
	img, actual, err := image.Decode(bytes.NewReader(raw))
	if err != nil || actual != format || img.Bounds().Size() != image.Pt(cfg.Width, cfg.Height) {
		return nil, errCorrupt
	}
	if img.ColorModel() == color.CMYKModel {
		return nil, errColor
	}
	return photo{img, info.orientation, transform}, nil
}

type photo struct {
	source      image.Image
	orientation int
	convert     func(float64, float64, float64) (float64, float64, float64)
}

func (p photo) ColorModel() color.Model { return color.RGBAModel }

func (p photo) Bounds() image.Rectangle {
	size := p.source.Bounds().Size()
	if p.orientation >= 5 {
		size.X, size.Y = size.Y, size.X
	}
	return image.Rectangle{Max: size}
}

func (p photo) At(x, y int) color.Color {
	w, h := p.source.Bounds().Dx(), p.source.Bounds().Dy()
	switch p.orientation {
	case 2:
		x = w - 1 - x
	case 3:
		x, y = w-1-x, h-1-y
	case 4:
		y = h - 1 - y
	case 5:
		x, y = y, x
	case 6:
		x, y = y, h-1-x
	case 7:
		x, y = w-1-y, h-1-x
	case 8:
		x, y = w-1-y, x
	}
	pixel := color.NRGBA64Model.Convert(p.source.At(x, y)).(color.NRGBA64)
	r, g, b := float64(pixel.R)/65535, float64(pixel.G)/65535, float64(pixel.B)/65535
	if p.convert != nil {
		r, g, b = p.convert(r, g, b)
	}
	a := float64(pixel.A) / 65535
	// Use a fixed neutral matte so transparent input also has a deterministic JPEG output.
	channel := func(v float64) uint8 { return uint8((clamp(v)*a+(1-a)*0.12)*255 + 0.5) }
	return color.RGBA{R: channel(r), G: channel(g), B: channel(b), A: 255}
}

func derivative(src image.Image, crop image.Rectangle, w, h, limit int) (Result, error) {
	dst := resize(src, crop, w, h)
	out := limitedBuffer{limit: limit}
	for _, quality := range []int{90, 84, 76, 68} {
		out.Reset()
		err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: quality})
		if err == nil {
			return Result{JPEG: out.Bytes(), Source: src.Bounds().Size()}, nil
		}
		if !errors.Is(err, errOutputLimit) {
			return Result{}, errCorrupt
		}
	}
	return Result{}, errors.New("The processed JPEG could not fit within its size limit. Choose a simpler photo.")
}

// Stage large reductions to avoid the cubic scaler's 32-byte-per-sample
// working buffer for tall source images. Each approximate pass reduces either
// axis by no more than a factor of two, preserving detail in successive samples.
func resize(src image.Image, crop image.Rectangle, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	if crop.Size() == dst.Bounds().Size() {
		xdraw.Draw(dst, dst.Bounds(), src, crop.Min, xdraw.Src)
		return dst
	}
	const maxCubicSamples = 2_000_000
	for crop.Dx() > 2*w || crop.Dy() > 2*h {
		next := image.NewRGBA(image.Rect(0, 0, max(w, (crop.Dx()+1)/2), max(h, (crop.Dy()+1)/2)))
		xdraw.ApproxBiLinear.Scale(next, next.Bounds(), src, crop, xdraw.Src, nil)
		src, crop = next, next.Bounds()
	}
	scaler := xdraw.ApproxBiLinear
	if w*crop.Dy() <= maxCubicSamples {
		scaler = xdraw.CatmullRom
	}
	scaler.Scale(dst, dst.Bounds(), src, crop, xdraw.Src, nil)
	return dst
}

var errOutputLimit = errors.New("The processed JPEG exceeds 10 MB. Choose a smaller crop.")

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errOutputLimit
	}
	return b.Buffer.Write(p)
}
