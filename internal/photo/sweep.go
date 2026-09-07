package photo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/store"
)

// sweepMinAge is how long a file with no row is left alone before the sweep
// removes it. A photo's file is written before its row is inserted, so a file
// younger than this may be an upload still in flight.
const sweepMinAge = time.Hour

// SweepReport counts what one sweep found.
type SweepReport struct {
	// Removed and Kept are files no row points at. Kept were written less
	// than sweepMinAge ago and left on disk.
	Removed int
	Kept    int
	// Missing is rows whose file or square variant is not on disk, counted
	// once per row. The rows are left in the database.
	Missing int
}

// Sweep compares the photo rows with the files under the store's directory
// and writes one line per finding to out, then a summary. A file no row
// points at is deleted once it is older than sweepMinAge. A row whose file is
// not on disk is reported and left alone, because the row is the only record
// left that the photo existed.
//
// Reads and deletes go through an os.Root, so a symlink under the directory
// cannot reach a file outside it. Only a file at <uuid>/<uuid>/<name> is
// deleted, so a sweep pointed at the wrong directory removes nothing.
func (s *Store) Sweep(ctx context.Context, q *store.Queries, out io.Writer) (SweepReport, error) {
	rows, err := q.ListPhotoFiles(ctx)
	if err != nil {
		return SweepReport{}, fmt.Errorf("list the photo rows: %w", err)
	}
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return SweepReport{}, fmt.Errorf("open the photo directory: %w", err)
	}
	defer func() { _ = root.Close() }()

	// A write error stops the sweep, so no file is removed without a line
	// saying it was.
	var writeErr error
	say := func(format string, args ...any) {
		if writeErr == nil {
			_, writeErr = fmt.Fprintf(out, format, args...)
		}
	}

	var report SweepReport
	// expected holds every file path the rows point at, relative to the
	// photo directory.
	expected := make(map[string]bool, 2*len(rows))
	for _, row := range rows {
		expected[row.Path] = true
		present, err := onDisk(root, row.Path)
		if err != nil {
			return report, err
		}
		missing := !present
		if !present {
			say("MISSING %s: photo %s of plant %s has no file\n", row.Path, row.ID, row.PlantID)
		}
		if row.SquareBytes != nil {
			square := SquarePath(row.Path)
			expected[square] = true
			present, err := onDisk(root, square)
			if err != nil {
				return report, err
			}
			if !present {
				missing = true
				say("MISSING %s: photo %s of plant %s has no square variant\n", square, row.ID, row.PlantID)
			}
		}
		// A row missing both its file and its square counts once, because
		// the summary and the command's exit status count rows, not files.
		if missing {
			report.Missing++
		}
	}

	now := time.Now()
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case writeErr != nil:
			return writeErr
		case ctx.Err() != nil:
			return ctx.Err()
		case !d.Type().IsRegular() || expected[p]:
			return nil
		case !isPhotoPath(p):
			say("left %s: not under a garden and plant directory\n", p)
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		age := now.Sub(info.ModTime())
		if age < sweepMinAge {
			report.Kept++
			say("kept %s: no row, but written %s ago\n", p, age.Truncate(time.Second))
			return nil
		}
		say("removing %s: no row, %d bytes, written %s ago\n", p, info.Size(), age.Truncate(time.Minute))
		if err := root.Remove(p); err != nil {
			return err
		}
		report.Removed++
		return nil
	})
	if err != nil {
		return report, fmt.Errorf("sweep %s: %w", s.dir, err)
	}

	summary := "nothing to remove"
	if report.Removed > 0 {
		summary = fmt.Sprintf("removed %d files", report.Removed)
	}
	say("%s. %d rows checked, %d files kept, %d rows with no file\n", summary, len(rows), report.Kept, report.Missing)
	return report, writeErr
}

// onDisk reports whether a file exists at p under root. Any stat error other
// than the file not existing is returned, because a permission or I/O error
// is not a missing photo.
func onDisk(root *os.Root, p string) (bool, error) {
	switch _, err := root.Stat(p); {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, err
	}
}

// isPhotoPath reports whether p is a photo file's path: a file name inside a
// plant directory inside a garden directory, both named by a UUID.
func isPhotoPath(p string) bool {
	parts := strings.Split(p, "/")
	if len(parts) != 3 {
		return false
	}
	for _, id := range parts[:2] {
		if _, err := uuid.Parse(id); err != nil {
			return false
		}
	}
	return true
}
