package photo

import (
	"os"
	"path/filepath"
	"testing"
	"uuid"
)

func TestPath_IsGardenThenPlantThenIDWithTheKindsExtension(t *testing.T) {
	garden := uuid.MustParse("00000000-0000-7000-8000-000000000001")
	plant := uuid.MustParse("00000000-0000-7000-8000-000000000002")
	id := uuid.MustParse("00000000-0000-7000-8000-000000000003")

	got := Path(garden, plant, id, WebP)

	want := "00000000-0000-7000-8000-000000000001/00000000-0000-7000-8000-000000000002/00000000-0000-7000-8000-000000000003.webp"
	if got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
	if square := SquarePath(got); square != "00000000-0000-7000-8000-000000000001/00000000-0000-7000-8000-000000000002/00000000-0000-7000-8000-000000000003-square.webp" {
		t.Errorf("SquarePath = %q, want -square before the extension", square)
	}
}

func TestNewStore_CreatesAMissingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "photos", "nested")

	if _, err := NewStore(dir, 1<<30); err != nil {
		t.Fatal(err)
	}

	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("after NewStore, %s is not a directory: %v", dir, err)
	}
}

func TestNewStore_ReportsADirectoryItCannotCreate(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := NewStore(filepath.Join(file, "photos"), 1<<30); err == nil {
		t.Error("NewStore under a file returned no error")
	}
}
