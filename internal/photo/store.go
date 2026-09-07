package photo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/store"
)

// MaxBytes is the largest photo file accepted, 8 MiB. The plant form's resize
// produces a JPEG of about a megabyte, so the cap is for a client that did not
// resize.
const MaxBytes = 8 << 20

// ErrTooLarge is returned for a file over MaxBytes.
var ErrTooLarge = errors.New("the photo is over the size limit")

// cacheControl is the Cache-Control header on every photo response. The file
// under a photo's id never changes, so a browser may keep it for a year. It is
// private because a photo is served behind a session and a shared cache must
// not keep it.
const cacheControl = "private, max-age=31536000, immutable"

// Store writes and reads photo files under one directory, SPRIG_PHOTO_DIR.
type Store struct {
	dir string
}

// NewStore creates dir when it is missing and returns a store over it. A
// directory that cannot be created is reported here, at startup, rather than
// on the first upload.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create the photo directory: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Upload is one photo as posted, with the square variant beside it when the
// client sent one.
type Upload struct {
	GardenID   uuid.UUID
	PlantID    uuid.UUID
	UploadedBy uuid.UUID
	// TakenAt is when the photo was taken. nil when the uploader did not say.
	TakenAt *time.Time
	File    io.ReadSeeker
	// Size is the file's length in bytes.
	Size int64
	// Square is the square variant, nil when none was posted.
	Square     io.ReadSeeker
	SquareSize int64
}

// Save writes the upload's files and inserts its row through q, so the insert
// joins the caller's transaction. The files are written first, so a failure
// leaves at most a file no row points at and never a row without its file. A
// file over MaxBytes is ErrTooLarge and one that is not a JPEG or a WebP is
// ErrNotImage, and neither is written.
func (s *Store) Save(ctx context.Context, q *store.Queries, u Upload) (store.Photo, error) {
	if u.Size > MaxBytes || u.SquareSize > MaxBytes {
		return store.Photo{}, ErrTooLarge
	}
	kind, size, err := Sniff(u.File)
	if err != nil {
		return store.Photo{}, err
	}
	if u.Square != nil {
		squareKind, _, err := Sniff(u.Square)
		if err != nil {
			return store.Photo{}, err
		}
		// The row records one kind and the square is served under it, so the
		// two files have to match.
		if squareKind != kind {
			return store.Photo{}, fmt.Errorf("%w: the square variant is a %s and the photo a %s", ErrNotImage, squareKind, kind)
		}
	}

	id := uuid.NewV7()
	p := Path(u.GardenID, u.PlantID, id, kind)
	written, err := s.write(p, u.File)
	if err != nil {
		return store.Photo{}, err
	}
	var squareBytes *int64
	if u.Square != nil {
		n, err := s.write(SquarePath(p), u.Square)
		if err != nil {
			s.remove(p)
			return store.Photo{}, err
		}
		squareBytes = &n
	}

	row, err := q.CreatePhoto(ctx, store.CreatePhotoParams{
		ID:          id,
		GardenID:    u.GardenID,
		PlantID:     u.PlantID,
		UploadedBy:  u.UploadedBy,
		TakenAt:     u.TakenAt,
		Kind:        string(kind),
		Path:        p,
		Width:       int32(size.Width),  //nolint:gosec // a JPEG or WebP dimension is at most 24 bits
		Height:      int32(size.Height), //nolint:gosec // a JPEG or WebP dimension is at most 24 bits
		Bytes:       written,
		SquareBytes: squareBytes,
	})
	if err != nil {
		s.remove(p)
		if u.Square != nil {
			s.remove(SquarePath(p))
		}
		return store.Photo{}, fmt.Errorf("insert the photo row: %w", err)
	}
	return row, nil
}

// Path is the file a photo is stored at under the directory:
// <garden>/<plant>/<id>.<ext>. One garden is one subtree, so its photos can be
// copied, measured or removed as a directory.
func Path(gardenID, plantID, photoID uuid.UUID, kind Kind) string {
	return path.Join(gardenID.String(), plantID.String(), photoID.String()+kind.Ext())
}

// SquarePath is the file the square variant of the photo at p is stored at:
// the same name with -square before the extension.
func SquarePath(p string) string {
	ext := path.Ext(p)
	return strings.TrimSuffix(p, ext) + "-square" + ext
}

// Serve writes the photo's file, or its square variant, as the response. The
// Content-Type is the kind recorded when the file was uploaded, never read
// from the file again. A row whose file is missing returns an error rather
// than a 404, because it means a restore or a mount went wrong and not that
// the photo never existed.
func (s *Store) Serve(w http.ResponseWriter, r *http.Request, row store.Photo, square bool) error {
	p := row.Path
	if square {
		p = SquarePath(p)
	}
	f, err := os.Open(s.full(p))
	if err != nil {
		return fmt.Errorf("open the photo's file: %w", err)
	}
	defer func() { _ = f.Close() }()
	w.Header().Set("Content-Type", row.Kind)
	w.Header().Set("Cache-Control", cacheControl)
	// The empty name stops ServeContent from setting Content-Type from a file
	// extension.
	http.ServeContent(w, r, "", row.UploadedAt, f)
	return nil
}

// write copies r to the file at p and returns the bytes written. It creates
// the plant's directory first and syncs the file, so the bytes are on disk
// before the row that points at them is inserted. A part-written file is
// removed when the copy fails.
func (s *Store) write(p string, r io.Reader) (int64, error) {
	full := s.full(p)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return 0, fmt.Errorf("create the plant's photo directory: %w", err)
	}
	// Only the app's own user reads or writes under the directory.
	f, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // the path is built from ids, never from the request
	if err != nil {
		return 0, fmt.Errorf("create the photo's file: %w", err)
	}
	n, err := io.Copy(f, r)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(full)
		return 0, fmt.Errorf("write the photo's file: %w", err)
	}
	return n, nil
}

func (s *Store) remove(p string) {
	_ = os.Remove(s.full(p))
}

func (s *Store) full(p string) string {
	return filepath.Join(s.dir, filepath.FromSlash(p))
}
