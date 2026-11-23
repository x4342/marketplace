package media

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
)

const maxProfile = 512 << 10

type metadata struct {
	orientation  int
	profile      []byte
	cicp         []byte
	chromaticity []byte
	gamma        uint32
	srgb         bool
}

func readMetadata(raw []byte, format string) (metadata, error) {
	m := metadata{orientation: 1}
	var err error
	switch format {
	case "jpeg":
		err = m.readJPEG(raw)
	case "png":
		err = m.readChunks(raw[8:], binary.BigEndian, false)
	case "webp":
		err = m.readChunks(raw[12:], binary.LittleEndian, true)
	default:
		return m, errCorrupt
	}
	return m, err
}

func (m *metadata) readJPEG(raw []byte) error {
	var segments [256][]byte
	total, count := 0, 0
	for i := 2; i < len(raw); {
		if raw[i] != 0xff {
			return errCorrupt
		}
		for i < len(raw) && raw[i] == 0xff {
			i++
		}
		if i == len(raw) {
			return errCorrupt
		}
		marker := raw[i]
		i++
		if marker == 0xda || marker == 0xd9 {
			break
		}
		if len(raw)-i < 2 {
			return errCorrupt
		}
		n := int(binary.BigEndian.Uint16(raw[i:]))
		if n < 2 || n > len(raw)-i {
			return errCorrupt
		}
		part := raw[i+2 : i+n]
		i += n
		if marker == 0xe1 && bytes.HasPrefix(part, []byte("Exif\x00\x00")) {
			m.orientation = orientation(part)
		}
		if marker != 0xe2 || !bytes.HasPrefix(part, []byte("ICC_PROFILE\x00")) {
			continue
		}
		if len(part) < 14 {
			return errColor
		}
		index, size := int(part[12]), int(part[13])
		if index == 0 || index > size || (count != 0 && count != size) || segments[index] != nil {
			return errColor
		}
		count = size
		segments[index] = part[14:]
		total += len(part) - 14
		if total > maxProfile {
			return errColor
		}
	}
	for i := 1; i <= count; i++ {
		if len(segments[i]) == 0 {
			return errColor
		}
		m.profile = append(m.profile, segments[i]...)
	}
	return nil
}

func (m *metadata) readChunks(raw []byte, order binary.ByteOrder, webp bool) error {
	for len(raw) >= 8 {
		length, name, trailer := order.Uint32(raw), string(raw[4:8]), 4
		if webp {
			name, length = string(raw[:4]), order.Uint32(raw[4:])
			trailer = int(length % 2)
		}
		if uint64(length)+8+uint64(trailer) > uint64(len(raw)) {
			return errCorrupt
		}
		part := raw[8 : 8+int(length)]
		raw = raw[8+int(length)+trailer:]
		switch name {
		case "EXIF", "eXIf":
			m.orientation = orientation(part)
		case "iCCP", "ICCP":
			if m.profile != nil {
				return errColor
			}
			if webp {
				if len(part) == 0 || len(part) > maxProfile {
					return errColor
				}
				m.profile = part
				continue
			}
			sep := bytes.IndexByte(part, 0)
			if sep < 1 || sep+2 >= len(part) || part[sep+1] != 0 {
				return errColor
			}
			reader, err := zlib.NewReader(bytes.NewReader(part[sep+2:]))
			if err != nil {
				return errColor
			}
			m.profile, err = io.ReadAll(io.LimitReader(reader, maxProfile+1))
			reader.Close()
			if err != nil || len(m.profile) == 0 || len(m.profile) > maxProfile {
				return errColor
			}
		case "cICP":
			if len(part) != 4 || m.cicp != nil {
				return errColor
			}
			m.cicp = part
		case "sRGB":
			m.srgb = true
		case "gAMA":
			if len(part) != 4 || binary.BigEndian.Uint32(part) == 0 {
				return errColor
			}
			m.gamma = binary.BigEndian.Uint32(part)
		case "cHRM":
			m.chromaticity = part
		case "acTL", "ANIM", "ANMF":
			return errAnimated
		case "IEND":
			return nil
		}
	}
	if webp && len(raw) == 0 {
		return nil
	}
	return errCorrupt
}

func orientation(tiff []byte) int {
	tiff = bytes.TrimPrefix(tiff, []byte("Exif\x00\x00"))
	if len(tiff) < 8 {
		return 1
	}
	var order binary.ByteOrder = binary.BigEndian
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
	default:
		return 1
	}
	offset := int(order.Uint32(tiff[4:]))
	if order.Uint16(tiff[2:]) != 42 || offset < 8 || offset > len(tiff)-2 {
		return 1
	}
	directory := tiff[offset:]
	count := int(order.Uint16(directory))
	if count > (len(directory)-2)/12 {
		return 1
	}
	for i := 0; i < count; i++ {
		tag := directory[2+i*12:]
		if order.Uint16(tag) == 0x112 && order.Uint16(tag[2:]) == 3 && order.Uint32(tag[4:]) == 1 {
			v := int(order.Uint16(tag[8:]))
			if v >= 1 && v <= 8 {
				return v
			}
		}
	}
	return 1
}
