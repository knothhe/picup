package lanreceive

import (
	"encoding/binary"
	"errors"
	"io"
	"os"

	"github.com/knothhe/picup/internal/imageproc"
)

func validateImage(f *os.File, size int64) (string, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	cfg, format, err := imageproc.DecodeConfig(f)
	if err == nil {
		if !imageproc.ValidDimensions(cfg.Width, cfg.Height) {
			return "", errors.New("Images must be at most 40 megapixels. Choose a smaller version of this photo.")
		}
		switch format {
		case "png", "gif", "webp", "avif":
			return "." + format, nil
		case "jpeg":
			return ".jpg", nil
		case "tiff":
			return ".tiff", nil
		default:
			return "", errors.New("This image format is not supported.")
		}
	}
	// HEIC is decoded by the existing native conversion path. Inspect its box
	// structure here so an extension or a caller-provided MIME type is never
	// enough to turn an arbitrary file into an admitted image.
	if err := validateHEIF(f, size); err != nil {
		return "", err
	}
	return ".heic", nil
}

type imageBox struct {
	start, size, header int64
	kind                string
}

func readBox(r io.ReaderAt, offset, end int64) (imageBox, error) {
	var h [16]byte
	if end-offset < 8 {
		return imageBox{}, io.ErrUnexpectedEOF
	}
	if _, err := r.ReadAt(h[:8], offset); err != nil {
		return imageBox{}, err
	}
	size, header := uint64(binary.BigEndian.Uint32(h[:4])), int64(8)
	if size == 1 {
		if end-offset < 16 {
			return imageBox{}, io.ErrUnexpectedEOF
		}
		if _, err := r.ReadAt(h[8:], offset+8); err != nil {
			return imageBox{}, err
		}
		size, header = binary.BigEndian.Uint64(h[8:]), 16
	} else if size == 0 {
		size = uint64(end - offset)
	}
	if size < uint64(header) || size > uint64(end-offset) {
		return imageBox{}, errors.New("Invalid image container.")
	}
	return imageBox{start: offset, size: int64(size), header: header, kind: string(h[4:8])}, nil
}

func validateHEIF(r io.ReaderAt, size int64) error {
	invalid := errors.New("Choose a valid PNG, JPEG, WebP, GIF, HEIC, AVIF or TIFF image.")
	first, err := readBox(r, 0, size)
	if err != nil || first.kind != "ftyp" || first.size < first.header+8 || first.size > 4096 {
		return invalid
	}
	brands := make([]byte, first.size-first.header)
	if len(brands)%4 != 0 {
		return invalid
	}
	if _, err := r.ReadAt(brands, first.header); err != nil {
		return invalid
	}
	compatible, hevc := false, false
	for i := 0; i+4 <= len(brands); i += 4 {
		if i == 4 {
			continue
		} // minor_version is not a brand.
		switch string(brands[i : i+4]) {
		case "heic", "heix", "hevc", "hevx", "heim", "heis", "hevm", "hevs":
			compatible, hevc = true, true
		case "mif1", "msf1":
			compatible = true
		case "avif", "avis":
			return invalid // Damaged AVIF must not masquerade as HEIC.
		}
	}
	if !compatible {
		return invalid
	}
	boxes, dimensions := 0, 0
	var scan func(int64, int64, int, string) error
	scan = func(start, end int64, depth int, parent string) error {
		if depth > 4 {
			return invalid
		}
		for start < end {
			boxes++
			if boxes > 10000 {
				return invalid
			}
			b, err := readBox(r, start, end)
			if err != nil {
				return invalid
			}
			payload := b.start + b.header
			switch b.kind {
			case "meta":
				if parent != "" {
					return invalid
				}
				if b.size < b.header+4 {
					return invalid
				}
				if err := scan(payload+4, b.start+b.size, depth+1, b.kind); err != nil {
					return err
				}
			case "iprp", "ipco":
				if b.kind == "iprp" && parent != "meta" || b.kind == "ipco" && parent != "iprp" {
					return invalid
				}
				if err := scan(payload, b.start+b.size, depth+1, b.kind); err != nil {
					return err
				}
			case "ispe":
				if parent != "ipco" {
					return invalid
				}
				if b.size < b.header+12 {
					return invalid
				}
				var data [12]byte
				if _, err := r.ReadAt(data[:], payload); err != nil {
					return invalid
				}
				width, height := binary.BigEndian.Uint32(data[4:8]), binary.BigEndian.Uint32(data[8:12])
				if !imageproc.ValidDimensions(int(width), int(height)) {
					return errors.New("Images must be at most 40 megapixels. Choose a smaller version of this photo.")
				}
				dimensions++
			case "hvcC":
				if parent != "ipco" {
					return invalid
				}
				hevc = true
			}
			start += b.size
		}
		return nil
	}
	if err := scan(first.size, size, 0, ""); err != nil {
		return err
	}
	if !hevc || dimensions == 0 {
		return invalid
	}
	return nil
}
