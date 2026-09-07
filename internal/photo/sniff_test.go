package photo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

// testJPEG is a real JPEG of the given size, from the standard encoder.
func testJPEG(t *testing.T, width, height int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := range width {
		img.Set(x, 0, color.RGBA{R: uint8(x), G: 90, B: 40, A: 255})
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// webpFile is a WebP RIFF header followed by the given first chunk.
func webpFile(chunk string, payload []byte) []byte {
	var out []byte
	out = append(out, "RIFF"...)
	out = binary.LittleEndian.AppendUint32(out, uint32(4+8+len(payload))) //nolint:gosec // a fixture is a few bytes long
	out = append(out, "WEBP"...)
	out = append(out, chunk...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(payload))) //nolint:gosec // a fixture is a few bytes long
	return append(out, payload...)
}

// littleEndian3 is n as the three little-endian bytes a VP8X chunk holds a
// canvas dimension in.
func littleEndian3(n uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, n)[:3]
}

func TestSniff_ReadsTheKindAndSizeOfAJPEG(t *testing.T) {
	kind, size, err := Sniff(bytes.NewReader(testJPEG(t, 30, 20)))

	if err != nil {
		t.Fatal(err)
	}
	if kind != JPEG || size != (Size{Width: 30, Height: 20}) {
		t.Errorf("Sniff = %s %+v, want %s 30 by 20", kind, size, JPEG)
	}
}

func TestSniff_ReadsTheSizeOfAJPEGWithA40KiBSegmentBeforeTheFrame(t *testing.T) {
	// The added segment is an APP2, where a large colour profile is stored. A
	// real photo has one, so the start-of-frame can be tens of kilobytes in.
	encoded := testJPEG(t, 30, 20)
	segment := []byte{0xFF, 0xE2}
	segment = binary.BigEndian.AppendUint16(segment, 40<<10+2)
	segment = append(segment, make([]byte, 40<<10)...)
	file := append(append([]byte{}, encoded[:2]...), segment...)
	file = append(file, encoded[2:]...)

	kind, size, err := Sniff(bytes.NewReader(file))

	if err != nil {
		t.Fatal(err)
	}
	if kind != JPEG || size != (Size{Width: 30, Height: 20}) {
		t.Errorf("Sniff = %s %+v, want %s 30 by 20", kind, size, JPEG)
	}
}

func TestSniff_ReadsTheSizeOfEachWebPBitstream(t *testing.T) {
	lossy := []byte{0, 0, 0, 0x9D, 0x01, 0x2A}
	lossy = binary.LittleEndian.AppendUint16(lossy, 2048)
	lossy = binary.LittleEndian.AppendUint16(lossy, 1536)

	// The five header bytes, then padding, because Sniff reads ten payload
	// bytes whatever the chunk holds.
	lossless := []byte{0x2F}
	lossless = binary.LittleEndian.AppendUint32(lossless, uint32(2048-1)|uint32(1536-1)<<14)
	lossless = append(lossless, make([]byte, 8)...)

	extended := []byte{0, 0, 0, 0}
	extended = append(extended, littleEndian3(2048-1)...)
	extended = append(extended, littleEndian3(1536-1)...)

	for name, file := range map[string][]byte{
		"lossy":    webpFile("VP8 ", lossy),
		"lossless": webpFile("VP8L", lossless),
		"extended": webpFile("VP8X", extended),
	} {
		kind, size, err := Sniff(bytes.NewReader(file))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if kind != WebP || size != (Size{Width: 2048, Height: 1536}) {
			t.Errorf("%s: Sniff = %s %+v, want %s 2048 by 1536", name, kind, size, WebP)
		}
	}
}

func TestSniff_RefusesWhatIsNotAJPEGOrAWebP(t *testing.T) {
	for name, file := range map[string][]byte{
		"png":                    {0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0x0D, 'I', 'H', 'D', 'R'},
		"empty":                  {},
		"text":                   []byte("the resized bytes"),
		"truncated jpeg":         {0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0, 0},
		"jpeg with scan first":   {0xFF, 0xD8, 0xFF, 0xDA, 0x00, 0x08, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		"webp with no bitstream": webpFile("EXIF", make([]byte, 10)),
	} {
		_, _, err := Sniff(bytes.NewReader(file))
		if !errors.Is(err, ErrNotImage) {
			t.Errorf("%s: err = %v, want ErrNotImage", name, err)
		}
	}
}

func TestSniff_LeavesTheReaderAtItsStart(t *testing.T) {
	file := testJPEG(t, 30, 20)
	r := bytes.NewReader(file)

	if _, _, err := Sniff(r); err != nil {
		t.Fatal(err)
	}

	rest := make([]byte, len(file))
	n, _ := r.Read(rest)
	if n != len(file) || !bytes.Equal(rest, file) {
		t.Errorf("after Sniff the reader gave %d bytes, want the whole file of %d", n, len(file))
	}
}
