//go:build ignore

// This program writes the two photos the plant form tests choose. Run it from
// the repository root:
//
//	go run e2e/fixtures/generate.go
//
// gps-tagged.jpg is 3000 by 2000 pixels with an EXIF block holding a GPS
// position. It is wider than the 2048 pixel long edge the browser resizes a
// photo to, and the resize has to strip the block.
//
// sideways.jpg is 1200 by 800 pixels with an EXIF orientation of 6: landscape
// pixels and a tag saying to turn them a quarter turn clockwise, how a phone
// stores a portrait photo. Applying the tag makes the resized photo 800 wide
// and 1200 high.
//
// Both are gradients rather than photographs so the files stay small. Go's
// JPEG encoder writes no EXIF block, so the block is built here and spliced in
// after the start-of-image marker.
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
)

func main() {
	dir := filepath.Join("e2e", "fixtures")
	write(filepath.Join(dir, "gps-tagged.jpg"), gradient(3000, 2000), exif(1, true))
	write(filepath.Join(dir, "sideways.jpg"), gradient(1200, 800), exif(6, false))
}

// gradient runs from green at the left to brown at the right, with a dark band
// along the top edge so a turned picture can be told from one that was not.
func gradient(width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			t := float64(x) / float64(width)
			c := color.RGBA{uint8(60 + 100*t), uint8(140 - 80*t), uint8(50), 255}
			if y < height/10 {
				c = color.RGBA{30, 30, 30, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// write encodes img as a JPEG and inserts the EXIF segment after the
// start-of-image marker, where a camera puts it.
func write(path string, img image.Image, exif []byte) {
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 60}); err != nil {
		panic(err)
	}
	data := encoded.Bytes()
	var out bytes.Buffer
	out.Write(data[:2])
	out.Write(exif)
	out.Write(data[2:])
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		panic(err)
	}
}

// exif builds an APP1 segment holding a TIFF structure with an Orientation tag
// and, when located is true, a GPS IFD with a latitude and a longitude. The
// format counts every offset from the start of the TIFF header, and stores a
// value of four bytes or fewer in the entry itself rather than at an offset.
func exif(orientation uint16, located bool) []byte {
	le := binary.LittleEndian
	var tiff bytes.Buffer
	tiff.Write([]byte{'I', 'I', 0x2A, 0x00})
	_ = binary.Write(&tiff, le, uint32(8)) // IFD0 follows the header

	entry := func(tag, typ uint16, count uint32, value []byte) {
		_ = binary.Write(&tiff, le, tag)
		_ = binary.Write(&tiff, le, typ)
		_ = binary.Write(&tiff, le, count)
		tiff.Write(value)
		tiff.Write(make([]byte, 4-len(value)))
	}
	short := func(v uint16) []byte { return le.AppendUint16(nil, v) }
	long := func(v uint32) []byte { return le.AppendUint32(nil, v) }

	const (
		byteType     = 1
		asciiType    = 2
		shortType    = 3
		longType     = 4
		rationalType = 5
		entrySize    = 12
	)
	ifd0Entries := uint32(1)
	if located {
		ifd0Entries = 2
	}
	// Each IFD is a two-byte count, its entries and a four-byte offset of the
	// next IFD, zero here.
	ifd0Size := 2 + ifd0Entries*entrySize + 4
	gpsOffset := 8 + ifd0Size
	gpsSize := uint32(2 + 5*entrySize + 4)
	latitudeOffset := gpsOffset + gpsSize
	longitudeOffset := latitudeOffset + 24

	_ = binary.Write(&tiff, le, uint16(ifd0Entries))
	entry(0x0112, shortType, 1, short(orientation))
	if located {
		entry(0x8825, longType, 1, long(gpsOffset))
	}
	_ = binary.Write(&tiff, le, uint32(0))

	if located {
		_ = binary.Write(&tiff, le, uint16(5))
		entry(0x0000, byteType, 4, []byte{2, 3, 0, 0})
		entry(0x0001, asciiType, 2, []byte("N\x00"))
		entry(0x0002, rationalType, 3, long(latitudeOffset))
		entry(0x0003, asciiType, 2, []byte("W\x00"))
		entry(0x0004, rationalType, 3, long(longitudeOffset))
		_ = binary.Write(&tiff, le, uint32(0))
		// 51° 30' 26" N, 0° 7' 39" W: a position in London.
		for _, v := range []uint32{51, 1, 30, 1, 26, 1, 0, 1, 7, 1, 39, 1} {
			_ = binary.Write(&tiff, le, v)
		}
	}

	body := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	segment := []byte{0xFF, 0xE1}
	segment = binary.BigEndian.AppendUint16(segment, uint16(len(body)+2))
	return append(segment, body...)
}
