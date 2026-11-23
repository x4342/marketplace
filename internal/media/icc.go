package media

import (
	"encoding/binary"
	"math"
)

var bigEndian = binary.BigEndian

// iccTransform supports RGB matrix/TRC profiles whose connection space is XYZ D50.
func iccTransform(profile []byte) (transform, error) {
	if len(profile) < 132 || len(profile) > maxProfile || bigEndian.Uint32(profile) != uint32(len(profile)) ||
		string(profile[16:24]) != "RGB XYZ " || string(profile[36:40]) != "acsp" {
		return nil, errColor
	}
	n := uint64(bigEndian.Uint32(profile[128:132]))
	if n > 128 || 132+12*n > uint64(len(profile)) {
		return nil, errColor
	}
	tags := make(map[string][]byte, n)
	for i := uint64(0); i < n; i++ {
		entry := profile[132+i*12:]
		tag := string(entry[:4])
		offset, size := uint64(bigEndian.Uint32(entry[4:])), uint64(bigEndian.Uint32(entry[8:]))
		if offset < 132+12*n || size < 8 || offset+size > uint64(len(profile)) || tags[tag] != nil ||
			tag == "A2B0" || tag == "D2B0" {
			return nil, errColor
		}
		tags[tag] = profile[offset : offset+size]
	}
	var toXYZ matrix
	var curves [3]transfer
	for channel, code := range []string{"r", "g", "b"} {
		chroma := tags[code+"XYZ"]
		if len(chroma) < 20 || string(chroma[:4]) != "XYZ " {
			return nil, errColor
		}
		for i := range 3 {
			value := fixed(chroma[8+4*i:])
			if math.Abs(value) > 2 {
				return nil, errColor
			}
			toXYZ[i][channel] = value
		}
		curve, err := readCurve(tags[code+"TRC"])
		if err != nil {
			return nil, err
		}
		curves[channel] = sampled(curve)
	}
	d50ToSRGB := matrix{{3.1338561, -1.6168667, -.4906146}, {-.9787684, 1.9161415, .0334540}, {.0719453, -.2289914, 1.4052427}}
	return func(r, g, b float64) (float64, float64, float64) {
		x, y, z := toXYZ.apply(curves[0](r), curves[1](g), curves[2](b))
		r, g, b = d50ToSRGB.apply(x, y, z)
		return encodeSRGB(r), encodeSRGB(g), encodeSRGB(b)
	}, nil
}

func fixed(p []byte) float64 { return float64(int32(bigEndian.Uint32(p))) / 65536 }

func sampled(curve transfer) transfer {
	const samples = 4096
	values := make([]float64, samples+1)
	for i := range values {
		values[i] = clamp(curve(float64(i) / samples))
	}
	return func(v float64) float64 {
		x := clamp(v) * samples
		i := min(int(x), samples-1)
		return values[i] + (values[i+1]-values[i])*(x-float64(i))
	}
}

func readCurve(tag []byte) (transfer, error) {
	if len(tag) < 12 {
		return nil, errColor
	}
	switch string(tag[:4]) {
	case "curv":
		n := uint64(bigEndian.Uint32(tag[8:12]))
		if n > 65536 || 12+2*n > uint64(len(tag)) {
			return nil, errColor
		}
		switch n {
		case 0:
			return func(v float64) float64 { return v }, nil
		case 1:
			g := float64(bigEndian.Uint16(tag[12:14])) / 256
			if g <= 0 || g > 10 {
				return nil, errColor
			}
			return func(v float64) float64 { return math.Pow(v, g) }, nil
		default:
			return func(v float64) float64 {
				position := v * float64(n-1)
				i := min(int(position), int(n)-2)
				a := float64(bigEndian.Uint16(tag[12+2*i:]))
				b := float64(bigEndian.Uint16(tag[14+2*i:]))
				return (a + (b-a)*(position-float64(i))) / 65535
			}, nil
		}
	case "para":
		return parametricCurve(tag)
	default:
		return nil, errColor
	}
}

func parametricCurve(tag []byte) (transfer, error) {
	kind := int(bigEndian.Uint16(tag[8:10]))
	arity := [...]int{1, 3, 4, 5, 7}
	if kind >= len(arity) || len(tag) < 12+4*arity[kind] {
		return nil, errColor
	}
	var p [7]float64
	for i := 0; i < arity[kind]; i++ {
		p[i] = fixed(tag[12+4*i:])
		if math.Abs(p[i]) > 10 {
			return nil, errColor
		}
	}
	g, a, b, c, d, e, f := p[0], p[1], p[2], p[3], p[4], p[5], p[6]
	if g <= 0 || (kind != 0 && a <= 0) {
		return nil, errColor
	}
	return func(x float64) float64 {
		switch kind {
		case 0:
			return math.Pow(x, g)
		case 1, 2:
			if x < -b/a {
				return c
			}
			return math.Pow(max(0, a*x+b), g) + c
		default:
			if x < d {
				return c*x + f
			}
			return math.Pow(max(0, a*x+b), g) + e
		}
	}, nil
}
