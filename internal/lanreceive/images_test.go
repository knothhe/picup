package lanreceive

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"strings"
	"testing"
)

func containerBox(kind string, payload []byte) []byte {
	b := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(b, uint32(len(b)))
	copy(b[4:8], kind)
	copy(b[8:], payload)
	return b
}

func heicHeader(width, height uint32) []byte {
	ftyp := containerBox("ftyp", append([]byte("heic\x00\x00\x00\x00"), []byte("mif1heic")...))
	spatial := make([]byte, 12)
	binary.BigEndian.PutUint32(spatial[4:8], width)
	binary.BigEndian.PutUint32(spatial[8:12], height)
	ipco := containerBox("ipco", containerBox("ispe", spatial))
	meta := containerBox("meta", append(make([]byte, 4), containerBox("iprp", ipco)...))
	return append(append(ftyp, meta...), containerBox("mdat", []byte("fixture"))...)
}

func TestHEICValidationUsesBrandsAndSpatialProperties(t *testing.T) {
	valid := heicHeader(4032, 3024)
	invalidSize := append([]byte(nil), valid...)
	binary.BigEndian.PutUint32(invalidSize[:4], 0xffffffff)
	avif := append([]byte(nil), valid...)
	copy(avif[8:12], "avif")
	missingMeta := containerBox("ftyp", []byte("heic\x00\x00\x00\x00mif1"))
	standaloneSpatial := append(append([]byte(nil), missingMeta...), containerBox("ispe", make([]byte, 12))...)
	for _, tc := range []struct {
		name       string
		data       []byte
		wantValid  bool
		wantPixels bool
	}{
		{name: "heic container", data: valid, wantValid: true},
		{name: "48MP", data: heicHeader(8064, 6048), wantPixels: true},
		{name: "zero dimensions", data: heicHeader(0, 3024), wantPixels: true},
		{name: "overflow dimensions", data: heicHeader(0xffffffff, 0xffffffff), wantPixels: true},
		{name: "invalid box length", data: invalidSize},
		{name: "truncated", data: valid[:len(valid)-1]},
		{name: "ftyp only", data: missingMeta},
		{name: "ispe outside properties", data: standaloneSpatial},
		{name: "damaged avif", data: avif},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateHEIF(bytes.NewReader(tc.data), int64(len(tc.data)))
			if (err == nil) != tc.wantValid {
				t.Fatal("unexpected validation", err)
			}
			if tc.wantPixels && !strings.Contains(err.Error(), "40 megapixels") {
				t.Fatal("lost dimension guidance", err)
			}
		})
	}
}

func TestImageValidationNormalizesHEICContentRegardlessOfExtension(t *testing.T) {
	var got File
	s := testServer(t, func(_ context.Context, f File) error { got = f; return nil })
	w := serve(s, imageRequest(t, s, "heic_00001", "photo.txt", heicHeader(4032, 3024)))
	if w.Code != 200 || got.Name != "photo.heic" || !strings.HasSuffix(got.Path, ".heic") {
		t.Fatal(w.Code, w.Body, got)
	}
}

func TestPrivateDirectoryRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	path := root + "/inbox"
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := prepareDirectory(path); err == nil {
		t.Fatal("symlink inbox accepted")
	}
	if err := prepareDirectory("relative/inbox"); err == nil {
		t.Fatal("relative inbox accepted")
	}
}
