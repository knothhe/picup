package imageproc

import "encoding/binary"

// The AVIF sequence brand is declared in the ISO BMFF file-type box. Read
// brands instead of decoding every frame, which could allocate unbounded memory.
func isAnimatedAVIF(b []byte) bool {
	if len(b) < 16 || string(b[4:8]) != "ftyp" {
		return false
	}
	size, header := uint64(binary.BigEndian.Uint32(b[:4])), 8
	if size == 1 {
		if len(b) < 24 {
			return false
		}
		size, header = binary.BigEndian.Uint64(b[8:16]), 16
	} else if size == 0 {
		size = uint64(len(b))
	}
	if size < uint64(header+8) || size > uint64(len(b)) {
		return false
	}
	if string(b[header:header+4]) == "avis" {
		return true
	}
	for i := header + 8; i+4 <= int(size); i += 4 {
		if string(b[i:i+4]) == "avis" {
			return true
		}
	}
	return false
}
