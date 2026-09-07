package photo

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"path/filepath"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/db"
	"github.com/ismailshak/sprig/internal/pgtest"
	"github.com/ismailshak/sprig/internal/store"
)

func TestMain(m *testing.M) {
	pgtest.Main(m)
}

func migrateSchema(ctx context.Context, databaseURL string) error {
	pool, err := store.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	// CREATE DATABASE refuses a template another session is connected to.
	defer pool.Close()

	return store.Migrate(ctx, pool, db.Migrations, slog.New(slog.DiscardHandler))
}

var (
	gardenID      = uuid.MustParse("00000000-0000-7000-8000-000000000901")
	otherGardenID = uuid.MustParse("00000000-0000-7000-8000-000000000902")
	userID        = uuid.MustParse("00000000-0000-7000-8000-000000000903")
	plantID       = uuid.MustParse("00000000-0000-7000-8000-000000000904")
	otherPlantID  = uuid.MustParse("00000000-0000-7000-8000-000000000905")
)

// quotaFixture is a photo store over an empty directory, with a seeded garden
// and plant to upload to.
type quotaFixture struct {
	store   *Store
	dir     string
	tx      pgx.Tx
	queries *store.Queries
}

func gardenWithQuota(t *testing.T, quota int64) *quotaFixture {
	t.Helper()

	tx := pgtest.Tx(t, migrateSchema)
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO garden (id, name) VALUES ($1, 'Rosewood'), ($2, 'Fairview')", []any{gardenID, otherGardenID}},
		{"INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Ellie', 'ellie', 'Europe/London')", []any{userID}},
		{"INSERT INTO plant (id, garden_id, nickname) VALUES ($1, $2, 'Fern'), ($3, $4, 'Ivy')", []any{plantID, gardenID, otherPlantID, otherGardenID}},
	} {
		if _, err := tx.Exec(t.Context(), q.sql, q.args...); err != nil {
			t.Fatalf("seeding: %v\n%s", err, q.sql)
		}
	}
	dir := t.TempDir()
	s, err := NewStore(dir, quota)
	if err != nil {
		t.Fatal(err)
	}
	return &quotaFixture{store: s, dir: dir, tx: tx, queries: store.New(tx)}
}

// insertPhoto records a photo row of the given sizes. No file is written,
// because the sum reads the rows alone.
func (f *quotaFixture) insertPhoto(t *testing.T, garden, plant uuid.UUID, bytes int64, square *int64) uuid.UUID {
	t.Helper()

	row, err := f.queries.CreatePhoto(t.Context(), store.CreatePhotoParams{
		ID: uuid.NewV7(), GardenID: garden, PlantID: plant, UploadedBy: userID,
		Kind: string(JPEG), Path: "x", Width: 1, Height: 1, Bytes: bytes, SquareBytes: square,
	})
	if err != nil {
		t.Fatal(err)
	}
	return row.ID
}

func (f *quotaFixture) save(t *testing.T, u Upload) (store.Photo, error) {
	t.Helper()

	u.GardenID, u.PlantID, u.UploadedBy = gardenID, plantID, userID
	return f.store.Save(t.Context(), f.queries, u)
}

func (f *quotaFixture) used(t *testing.T) int64 {
	t.Helper()

	usage, err := f.store.Usage(t.Context(), f.queries, gardenID)
	if err != nil {
		t.Fatal(err)
	}
	return usage.Used
}

// files lists every file under the directory, relative to it.
func (f *quotaFixture) files(t *testing.T) []string {
	t.Helper()

	var files []string
	err := filepath.WalkDir(f.dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(f.dir, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// padded is a valid JPEG made n bytes long, so its size is the only thing a
// test varies.
func padded(t *testing.T, n int) []byte {
	t.Helper()

	image := testJPEG(t, 8, 8)
	if len(image) > n {
		t.Fatalf("the smallest JPEG is %d bytes, over the %d asked for", len(image), n)
	}
	return append(image, make([]byte, n-len(image))...)
}

func TestSave_AFileLongerThanItsDeclaredSizeIsCutOffAtTheRoomLeftAndRemoved(t *testing.T) {
	f := gardenWithQuota(t, 4000)
	f.insertPhoto(t, gardenID, plantID, 2000, nil)

	_, err := f.save(t, Upload{File: bytes.NewReader(padded(t, 3200)), Size: 1600})

	if !errors.Is(err, ErrQuotaFull) {
		t.Errorf("err = %v, want ErrQuotaFull", err)
	}
	if got := f.files(t); len(got) != 0 {
		t.Errorf("the directory holds %v, want nothing", got)
	}
	if used := f.used(t); used != 2000 {
		t.Errorf("the garden's usage is %d, want the 2000 it started with", used)
	}
}

func TestSave_AnUploadWithNoDeclaredSizeIsStillBoundedByTheRoomLeft(t *testing.T) {
	f := gardenWithQuota(t, 2800)

	_, err := f.save(t, Upload{File: bytes.NewReader(padded(t, 3200))})

	if !errors.Is(err, ErrQuotaFull) {
		t.Errorf("err = %v, want ErrQuotaFull", err)
	}
	if got := f.files(t); len(got) != 0 {
		t.Errorf("the directory holds %v, want nothing", got)
	}
}

func TestSave_AnUploadWithNoDeclaredSizeIsStillBoundedByTheFileLimit(t *testing.T) {
	f := gardenWithQuota(t, 1<<30)

	_, err := f.save(t, Upload{File: bytes.NewReader(padded(t, MaxBytes+1))})

	if !errors.Is(err, ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
	if got := f.files(t); len(got) != 0 {
		t.Errorf("the directory holds %v, want nothing", got)
	}
}

func TestSave_ADeclaredSizeOverTheRoomLeftIsRefusedBeforeAnythingIsRead(t *testing.T) {
	f := gardenWithQuota(t, 4000)
	file := bytes.NewReader(padded(t, 3200))

	_, err := f.save(t, Upload{File: file, Size: 4004})

	if !errors.Is(err, ErrQuotaFull) {
		t.Errorf("err = %v, want ErrQuotaFull", err)
	}
	if file.Len() != 3200 {
		t.Errorf("%d bytes of the file were read, want none", 3200-file.Len())
	}
}

func TestSave_TheRowRecordsTheBytesWrittenAndNotTheBytesDeclared(t *testing.T) {
	f := gardenWithQuota(t, 4000)

	row, err := f.save(t, Upload{File: bytes.NewReader(padded(t, 3200)), Size: 10})

	if err != nil {
		t.Fatal(err)
	}
	if row.Bytes != 3200 {
		t.Errorf("the row counts %d bytes, want the 3200 written", row.Bytes)
	}
	if used := f.used(t); used != 3200 {
		t.Errorf("the garden's usage is %d, want 3200", used)
	}
}

func TestSave_AnUploadOfExactlyTheRoomLeftIsStored(t *testing.T) {
	f := gardenWithQuota(t, 4000)
	f.insertPhoto(t, gardenID, plantID, 1600, nil)

	row, err := f.save(t, Upload{File: bytes.NewReader(padded(t, 2400)), Size: 2400})

	if err != nil {
		t.Fatal(err)
	}
	if got := f.files(t); len(got) != 1 || got[0] != row.Path {
		t.Errorf("the directory holds %v, want the one file at %s", got, row.Path)
	}
	if used := f.used(t); used != 4000 {
		t.Errorf("the garden's usage is %d, want the whole quota of 4000", used)
	}
}

func TestSave_ASquareVariantThatCrossesTheRoomLeftRemovesThePhotoToo(t *testing.T) {
	f := gardenWithQuota(t, 4000)
	image, square := padded(t, 2400), padded(t, 2000)

	_, err := f.save(t, Upload{File: bytes.NewReader(image), Square: bytes.NewReader(square)})

	if !errors.Is(err, ErrQuotaFull) {
		t.Errorf("err = %v, want ErrQuotaFull", err)
	}
	if got := f.files(t); len(got) != 0 {
		t.Errorf("the directory holds %v, want nothing", got)
	}
	if used := f.used(t); used != 0 {
		t.Errorf("the garden's usage is %d, want 0", used)
	}
}

func TestUsage_CountsTheGardensPhotosAndSquaresAndNotAnotherGardens(t *testing.T) {
	f := gardenWithQuota(t, 1000)
	square := int64(50)
	f.insertPhoto(t, gardenID, plantID, 300, &square)
	f.insertPhoto(t, gardenID, plantID, 200, nil)
	f.insertPhoto(t, otherGardenID, otherPlantID, 400, &square)

	if used := f.used(t); used != 550 {
		t.Errorf("the garden's usage is %d, want 550", used)
	}
}

func TestUsage_ADeletedPhotosBytesAreNoLongerCounted(t *testing.T) {
	f := gardenWithQuota(t, 1000)
	square := int64(50)
	kept := f.insertPhoto(t, gardenID, plantID, 300, nil)
	f.insertPhoto(t, gardenID, plantID, 200, &square)

	if _, err := f.tx.Exec(t.Context(), "DELETE FROM photo WHERE id <> $1", kept); err != nil {
		t.Fatal(err)
	}

	if used := f.used(t); used != 300 {
		t.Errorf("the garden's usage is %d, want the 300 of the photo that is left", used)
	}
}

func TestUsage_RemainingIsTheQuotaLessWhatIsUsed(t *testing.T) {
	if got := (Usage{Used: 400, Quota: 1000}).Remaining(); got != 600 {
		t.Errorf("Remaining = %d, want 600", got)
	}
}

func TestUsage_RemainingIsZeroWhenTheGardenIsOverItsQuota(t *testing.T) {
	if got := (Usage{Used: 1200, Quota: 1000}).Remaining(); got != 0 {
		t.Errorf("Remaining = %d, want 0", got)
	}
}
