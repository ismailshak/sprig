package photo

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Kind is the media type of a photo, the value served as Content-Type.
type Kind string

// The two kinds accepted. The plant form's script always encodes a JPEG. A
// post with no script running sends the file as it was chosen, and that may be
// a WebP.
const (
	JPEG Kind = "image/jpeg"
	WebP Kind = "image/webp"
)

// Ext is the file extension a photo of this kind is stored under.
func (k Kind) Ext() string {
	if k == WebP {
		return ".webp"
	}
	return ".jpg"
}

// ErrNotImage is returned for bytes that are not a JPEG or a WebP, or that
// start like one and have no readable frame header.
var ErrNotImage = errors.New("not a JPEG or a WebP")

// Size is the pixel dimensions of a photo.
type Size struct {
	Width, Height int
}

// Sniff reads the kind and pixel size from the head of an image and seeks r
// back to the start. It reads segment headers only, never pixel data, so the
// most a crafted file can cost is a walk over its segment lengths.
func Sniff(r io.ReadSeeker) (Kind, Size, error) {
	defer func() { _, _ = r.Seek(0, io.SeekStart) }()

	var head [12]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return "", Size{}, ErrNotImage
	}
	var kind Kind
	var size Size
	var err error
	switch {
	case head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF:
		kind = JPEG
		size, err = jpegSize(r)
	case string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP":
		kind = WebP
		size, err = webpSize(r)
	default:
		return "", Size{}, ErrNotImage
	}
	if err != nil {
		return "", Size{}, fmt.Errorf("%w: %w", ErrNotImage, err)
	}
	return kind, size, nil
}

// jpegSize walks the segments after the two-byte start-of-image marker to the
// start-of-frame segment and reads the dimensions from it.
func jpegSize(r io.ReadSeeker) (Size, error) {
	if _, err := r.Seek(2, io.SeekStart); err != nil {
		return Size{}, fmt.Errorf("jpeg: %w", err)
	}
	for {
		// Each segment is 0xFF, a marker byte and, for all but a few markers,
		// a two-byte length that counts itself.
		var head [2]byte
		if _, err := io.ReadFull(r, head[:]); err != nil {
			return Size{}, fmt.Errorf("jpeg: %w", err)
		}
		if head[0] != 0xFF {
			return Size{}, errors.New("jpeg: no marker where a segment should start")
		}
		m := head[1]
		switch {
		case m == 0xFF:
			// A fill byte before the marker. Step back one so the next read
			// pairs this 0xFF with the byte after it.
			if _, err := r.Seek(-1, io.SeekCurrent); err != nil {
				return Size{}, fmt.Errorf("jpeg: %w", err)
			}
			continue
		case m >= 0xD0 && m <= 0xD7, m == 0x01, m == 0xD8:
			// A restart, TEM or repeated start-of-image marker has no length.
			continue
		case m == 0xD9, m == 0xDA:
			return Size{}, errors.New("jpeg: no start-of-frame segment before the scan")
		}
		var length [2]byte
		if _, err := io.ReadFull(r, length[:]); err != nil {
			return Size{}, fmt.Errorf("jpeg: %w", err)
		}
		n := int64(binary.BigEndian.Uint16(length[:]))
		if n < 2 {
			return Size{}, errors.New("jpeg: segment shorter than its own length")
		}
		// SOF0 to SOF15, less the three markers in that range that are not
		// frames: DHT, JPG and DAC.
		if m >= 0xC0 && m <= 0xCF && m != 0xC4 && m != 0xC8 && m != 0xCC {
			// The frame is a precision byte, then the height and width as
			// two big-endian bytes each.
			var frame [5]byte
			if _, err := io.ReadFull(r, frame[:]); err != nil {
				return Size{}, fmt.Errorf("jpeg: %w", err)
			}
			size := Size{
				Height: int(binary.BigEndian.Uint16(frame[1:3])),
				Width:  int(binary.BigEndian.Uint16(frame[3:5])),
			}
			if size.Width == 0 || size.Height == 0 {
				return Size{}, errors.New("jpeg: frame has no size")
			}
			return size, nil
		}
		if _, err := r.Seek(n-2, io.SeekCurrent); err != nil {
			return Size{}, fmt.Errorf("jpeg: %w", err)
		}
	}
}

// webpSize reads the dimensions from the first chunk after the twelve-byte
// RIFF header. That chunk is one of the three WebP bitstream chunks.
func webpSize(r io.ReadSeeker) (Size, error) {
	if _, err := r.Seek(12, io.SeekStart); err != nil {
		return Size{}, fmt.Errorf("webp: %w", err)
	}
	// A chunk is a four-byte name, a four-byte little-endian length, then its
	// payload. The dimensions are within the first ten payload bytes for all
	// three chunk types.
	var chunk [18]byte
	if _, err := io.ReadFull(r, chunk[:]); err != nil {
		return Size{}, fmt.Errorf("webp: %w", err)
	}
	payload := chunk[8:]
	var size Size
	switch string(chunk[:4]) {
	case "VP8 ":
		// A lossy frame: a three-byte frame tag, the start code 9D 01 2A, then
		// the width and height as fourteen bits each in two little-endian
		// bytes. The top two bits of each are a scale factor.
		if payload[3] != 0x9D || payload[4] != 0x01 || payload[5] != 0x2A {
			return Size{}, errors.New("webp: lossy frame has no start code")
		}
		size.Width = int(binary.LittleEndian.Uint16(payload[6:8]) & 0x3FFF)
		size.Height = int(binary.LittleEndian.Uint16(payload[8:10]) & 0x3FFF)
	case "VP8L":
		// A lossless frame: a signature byte, then width minus one and height
		// minus one as fourteen bits each, packed little-endian.
		if payload[0] != 0x2F {
			return Size{}, errors.New("webp: lossless frame has no signature")
		}
		bits := binary.LittleEndian.Uint32(payload[1:5])
		size.Width = int(bits&0x3FFF) + 1
		size.Height = int((bits>>14)&0x3FFF) + 1
	case "VP8X":
		// An extended file: a flags byte, three reserved bytes, then the
		// canvas width minus one and height minus one as three little-endian
		// bytes each.
		size.Width = (int(payload[4]) | int(payload[5])<<8 | int(payload[6])<<16) + 1
		size.Height = (int(payload[7]) | int(payload[8])<<8 | int(payload[9])<<16) + 1
	default:
		return Size{}, fmt.Errorf("webp: first chunk is %q, not a bitstream", chunk[:4])
	}
	if size.Width == 0 || size.Height == 0 {
		return Size{}, errors.New("webp: frame has no size")
	}
	return size, nil
}
