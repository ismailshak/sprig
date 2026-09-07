package photo

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/store"
)

func (f *quotaFixture) writeFile(t *testing.T, p string, age time.Duration) {
	t.Helper()

	full := filepath.Join(f.dir, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.setAge(t, p, age)
}

// setAge sets the modification time of the file at p to the given duration
// ago. Sweep takes a file's age from its modification time.
func (f *quotaFixture) setAge(t *testing.T, p string, age time.Duration) {
	t.Helper()

	written := time.Now().Add(-age)
	if err := os.Chtimes(filepath.Join(f.dir, filepath.FromSlash(p)), written, written); err != nil {
		t.Fatal(err)
	}
}

func (f *quotaFixture) sweep(t *testing.T) (SweepReport, string) {
	t.Helper()

	var out bytes.Buffer
	report, err := f.store.Sweep(t.Context(), f.queries, &out)
	if err != nil {
		t.Fatal(err)
	}
	return report, out.String()
}

func (f *quotaFixture) savedPhoto(t *testing.T) store.Photo {
	t.Helper()

	row, err := f.save(t, Upload{File: bytes.NewReader(testJPEG(t, 8, 8)), Square: bytes.NewReader(testJPEG(t, 4, 4))})
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func TestSweep_AFileWithNoRowOlderThanTheThresholdIsRemoved(t *testing.T) {
	f := gardenWithQuota(t, 1<<20)
	row := f.savedPhoto(t)
	orphan := Path(gardenID, plantID, uuid.NewV7(), JPEG)
	f.writeFile(t, orphan, sweepMinAge+time.Minute)
	before := f.used(t)

	report, out := f.sweep(t)

	if report != (SweepReport{Removed: 1}) {
		t.Errorf("report = %+v, want one file removed", report)
	}
	if got := f.files(t); len(got) != 2 || got[0] != SquarePath(row.Path) || got[1] != row.Path {
		t.Errorf("the directory holds %v, want the photo and its square only", got)
	}
	if !strings.Contains(out, orphan) {
		t.Errorf("the output does not name the removed file:\n%s", out)
	}
	if used := f.used(t); used != before {
		t.Errorf("the garden's usage went from %d to %d", before, used)
	}
}

func TestSweep_AFileWithNoRowYoungerThanTheThresholdIsKept(t *testing.T) {
	f := gardenWithQuota(t, 1<<20)
	orphan := Path(gardenID, plantID, uuid.NewV7(), JPEG)
	f.writeFile(t, orphan, sweepMinAge-time.Minute)

	report, _ := f.sweep(t)

	if report != (SweepReport{Kept: 1}) {
		t.Errorf("report = %+v, want one file kept", report)
	}
	if got := f.files(t); len(got) != 1 || got[0] != orphan {
		t.Errorf("the directory holds %v, want the file that is under the threshold", got)
	}
}

func TestSweep_ARowWithNoFileIsReportedAndLeftInPlace(t *testing.T) {
	f := gardenWithQuota(t, 1<<20)
	id := uuid.NewV7()
	p := Path(gardenID, plantID, id, JPEG)
	_, err := f.queries.CreatePhoto(t.Context(), store.CreatePhotoParams{
		ID: id, GardenID: gardenID, PlantID: plantID, UploadedBy: userID,
		Kind: string(JPEG), Path: p, Width: 1, Height: 1, Bytes: 10,
	})
	if err != nil {
		t.Fatal(err)
	}

	report, out := f.sweep(t)

	if report != (SweepReport{Missing: 1}) {
		t.Errorf("report = %+v, want one row with no file", report)
	}
	if !strings.Contains(out, "MISSING "+p) {
		t.Errorf("the output does not report the missing file:\n%s", out)
	}
	if _, err := f.queries.GetPhoto(t.Context(), gardenID, id); err != nil {
		t.Errorf("the row is gone: %v", err)
	}
}

func TestSweep_ARowWithASquareVariantThatIsNotOnDiskIsReported(t *testing.T) {
	f := gardenWithQuota(t, 1<<20)
	row := f.savedPhoto(t)
	if err := os.Remove(filepath.Join(f.dir, filepath.FromSlash(SquarePath(row.Path)))); err != nil {
		t.Fatal(err)
	}

	report, out := f.sweep(t)

	if report != (SweepReport{Missing: 1}) {
		t.Errorf("report = %+v, want one row with no file", report)
	}
	if !strings.Contains(out, "MISSING "+SquarePath(row.Path)) {
		t.Errorf("the output does not name the missing square:\n%s", out)
	}
}

func TestSweep_ARowMissingBothItsFileAndItsSquareCountsAsOneRow(t *testing.T) {
	f := gardenWithQuota(t, 1<<20)
	row := f.savedPhoto(t)
	for _, p := range []string{row.Path, SquarePath(row.Path)} {
		if err := os.Remove(filepath.Join(f.dir, filepath.FromSlash(p))); err != nil {
			t.Fatal(err)
		}
	}

	report, out := f.sweep(t)

	if report != (SweepReport{Missing: 1}) {
		t.Errorf("report = %+v, want the row counted once", report)
	}
	for _, p := range []string{row.Path, SquarePath(row.Path)} {
		if !strings.Contains(out, "MISSING "+p) {
			t.Errorf("the output does not name %s:\n%s", p, out)
		}
	}
}

func TestSweep_AnotherGardensPhotoFileIsNotRemoved(t *testing.T) {
	f := gardenWithQuota(t, 1<<20)
	row, err := f.store.Save(t.Context(), f.queries, Upload{
		GardenID: otherGardenID, PlantID: otherPlantID, UploadedBy: userID,
		File: bytes.NewReader(testJPEG(t, 8, 8)),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Past the threshold, so the row is the only thing keeping the file.
	f.setAge(t, row.Path, sweepMinAge+time.Minute)

	report, _ := f.sweep(t)

	if report != (SweepReport{}) {
		t.Errorf("report = %+v, want nothing found", report)
	}
	if got := f.files(t); len(got) != 1 || got[0] != row.Path {
		t.Errorf("the directory holds %v, want the other garden's photo", got)
	}
}

func TestSweep_ASquareFileForARowWithNoSquareVariantIsRemoved(t *testing.T) {
	f := gardenWithQuota(t, 1<<20)
	row, err := f.save(t, Upload{File: bytes.NewReader(testJPEG(t, 8, 8))})
	if err != nil {
		t.Fatal(err)
	}
	f.writeFile(t, SquarePath(row.Path), sweepMinAge+time.Minute)

	report, _ := f.sweep(t)

	if report != (SweepReport{Removed: 1}) {
		t.Errorf("report = %+v, want the square removed", report)
	}
	if got := f.files(t); len(got) != 1 || got[0] != row.Path {
		t.Errorf("the directory holds %v, want the photo alone", got)
	}
}

func TestSweep_AFileOutsideAPlantDirectoryIsLeft(t *testing.T) {
	f := gardenWithQuota(t, 1<<20)
	f.writeFile(t, "notes.txt", sweepMinAge+time.Minute)
	f.writeFile(t, gardenID.String()+"/stray.jpg", sweepMinAge+time.Minute)
	f.writeFile(t, "backup/"+plantID.String()+"/old.jpg", sweepMinAge+time.Minute)

	report, out := f.sweep(t)

	if report != (SweepReport{}) {
		t.Errorf("report = %+v, want nothing removed or kept", report)
	}
	if got := f.files(t); len(got) != 3 {
		t.Errorf("the directory holds %v, want all three files", got)
	}
	if !strings.Contains(out, "left notes.txt") {
		t.Errorf("the output does not say the file was left:\n%s", out)
	}
}

func TestSweep_ADirectoryMatchingTheRowsPrintsNothingToRemove(t *testing.T) {
	f := gardenWithQuota(t, 1<<20)
	f.savedPhoto(t)
	f.savedPhoto(t)
	before := f.files(t)

	report, out := f.sweep(t)

	if report != (SweepReport{}) {
		t.Errorf("report = %+v, want nothing found", report)
	}
	if after := f.files(t); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Errorf("the directory holds %v, want the %v it started with", after, before)
	}
	if !strings.Contains(out, "nothing to remove") {
		t.Errorf("the output does not say nothing was removed:\n%s", out)
	}
}
