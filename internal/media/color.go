package media

import (
	"encoding/binary"
	"errors"
	"math"
)

var errAnimated = errors.New("Choose a still photo. Animated images are not supported.")

type transfer func(float64) float64
type transform func(float64, float64, float64) (float64, float64, float64)
type matrix [3][3]float64

func (m matrix) apply(r, g, b float64) (float64, float64, float64) {
	return m[0][0]*r + m[0][1]*g + m[0][2]*b, m[1][0]*r + m[1][1]*g + m[1][2]*b, m[2][0]*r + m[2][1]*g + m[2][2]*b
}

func (m metadata) transform() (transform, error) {
	if m.cicp != nil {
		return cicpTransform(m.cicp)
	}
	if m.profile != nil {
		return iccTransform(m.profile)
	}
	if m.chromaticity != nil && !m.srgb {
		srgb := [...]uint32{31270, 32900, 64000, 33000, 30000, 60000, 15000, 6000}
		if len(m.chromaticity) != len(srgb)*4 {
			return nil, errColor
		}
		for i, reference := range srgb {
			if math.Abs(float64(binary.BigEndian.Uint32(m.chromaticity[4*i:]))-float64(reference)) > 150 {
				return nil, errColor
			}
		}
	}
	if m.srgb || m.gamma == 0 || m.gamma == 45455 {
		return nil, nil
	}
	if m.gamma < 10_000 || m.gamma > 200_000 {
		return nil, errColor
	}
	curve := func(x float64) float64 { return math.Pow(x, 100_000/float64(m.gamma)) }
	return func(r, g, b float64) (float64, float64, float64) {
		return encodeSRGB(curve(r)), encodeSRGB(curve(g)), encodeSRGB(curve(b))
	}, nil
}

func clamp(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	return min(1, v)
}

func decodeSRGB(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func encodeSRGB(v float64) float64 {
	v = clamp(v)
	if v <= 0.0031308 {
		return 12.92 * v
	}
	return 1.055*math.Pow(v, 1/2.4) - 0.055
}

func cicpTransform(flags []byte) (transform, error) {
	if flags[2] != 0 || flags[3] != 1 { // Full-range RGB.
		return nil, errColor
	}
	var primaries matrix
	switch flags[0] {
	case 1: // BT.709 / sRGB
		primaries = matrix{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}
	case 9: // BT.2020 to linear sRGB
		primaries = matrix{{1.66049, -.58764, -.07285}, {-.12455, 1.13290, -.00835}, {-.01815, -.10058, 1.11873}}
	case 12: // Display P3 to linear sRGB
		primaries = matrix{{1.22494, -.22494, 0}, {-.04206, 1.04206, 0}, {-.01964, -.07864, 1.09828}}
	default:
		return nil, errColor
	}
	var curve transfer
	switch flags[1] {
	case 13:
		curve = decodeSRGB
	case 8:
		curve = func(v float64) float64 { return v }
	case 1, 14, 15:
		curve = func(v float64) float64 {
			if v < .081 {
				return v / 4.5
			}
			return math.Pow((v+.099)/1.099, 1/.45)
		}
	case 16: // ST 2084 PQ, expressed in units of 203-nit reference white.
		curve = func(v float64) float64 {
			const m1, m2, c1, c2, c3 = 2610.0 / 16384, 2523.0 / 32, 3424.0 / 4096, 2413.0 / 128, 2392.0 / 128
			p := math.Pow(v, 1/m2)
			return (10_000.0 / 203) * math.Pow(max(0, p-c1)/(c2-c3*p), 1/m1)
		}
	case 18: // HLG inverse OETF, before display OOTF.
		curve = func(v float64) float64 {
			if v <= .5 {
				return v * v / 3
			}
			return (math.Exp((v-.55991073)/.17883277) + .28466892) / 12
		}
	default:
		return nil, errColor
	}
	return func(r, g, b float64) (float64, float64, float64) {
		r, g, b = primaries.apply(curve(r), curve(g), curve(b))
		if flags[1] == 16 || flags[1] == 18 {
			luma := max(0, .2126*r+.7152*g+.0722*b)
			if flags[1] == 18 {
				gain := (1000.0 / 203) * math.Pow(luma, .2)
				r, g, b, luma = r*gain, g*gain, b*gain, luma*gain
			}
			// Preserve channel ratios while compressing HDR highlights into SDR.
			gain := 1 / (1 + luma)
			r, g, b = r*gain, g*gain, b*gain
		}
		return encodeSRGB(r), encodeSRGB(g), encodeSRGB(b)
	}, nil
}
