package http

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ismailshak/sprig/internal/photo"
	"github.com/ismailshak/sprig/internal/store"
)

// testJPEG is a real JPEG of the given size from the standard encoder.
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

// testWebP is a lossless WebP of the given size: the RIFF header and a VP8L
// chunk with the dimensions in it. The standard library has no WebP encoder,
// and nothing here reads past these bytes.
func testWebP(width, height int) []byte {
	chunk := []byte{0x2F}
	chunk = binary.LittleEndian.AppendUint32(chunk, uint32(width-1)|uint32(height-1)<<14) //nolint:gosec // a fixture is a few hundred pixels
	chunk = append(chunk, make([]byte, 8)...)

	file := append([]byte{}, "RIFF"...)
	file = binary.LittleEndian.AppendUint32(file, uint32(4+8+len(chunk))) //nolint:gosec // a fixture is a few bytes long
	file = append(file, "WEBP"...)
	file = append(file, "VP8L"...)
	file = binary.LittleEndian.AppendUint32(file, uint32(len(chunk))) //nolint:gosec // a fixture is a few bytes long
	return append(file, chunk...)
}

// servedPhotos is a handler with every route, over a temporary photo
// directory holding the files for the seeded photo row: image at its path and
// square at the -square path beside it.
type servedPhotos struct {
	handler http.Handler
	image   []byte
	square  []byte
}

func photosServedTo(t *testing.T, queries *store.Queries) servedPhotos {
	t.Helper()

	dir := t.TempDir()
	photos, err := photo.NewStore(dir, testPhotoQuota)
	if err != nil {
		t.Fatal(err)
	}
	row, err := queries.GetPhoto(t.Context(), sitterPrincipal().Garden.ID, rosewoodPhotoID)
	if err != nil {
		t.Fatal(err)
	}
	f := servedPhotos{image: testJPEG(t, 30, 20), square: testJPEG(t, 8, 8)}
	for p, file := range map[string][]byte{row.Path: f.image, photo.SquarePath(row.Path): f.square} {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, file, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f.handler = New(testLogger, testSessions(), testPasskeys(), acceptEveryToken(memberWith(everyCapability())), queries, photos, testTemplates(), testAssets(), "", false, testPushKey, nil, nil)
	return f
}

func (f servedPhotos) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, signedIn(httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)))
	return rec
}

func TestPhoto_AStoredPhotoServesWithItsStoredKindAndNosniff(t *testing.T) {
	f := photosServedTo(t, routeQueries(t))

	rec := f.get(t, photoFullPath(rosewoodPlantID, rosewoodPhotoID))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), f.image) {
		t.Errorf("the body is %d bytes, want the %d stored", rec.Body.Len(), len(f.image))
	}
}

func TestPhoto_APhotoIsCachedPrivatelyForAYear(t *testing.T) {
	f := photosServedTo(t, routeQueries(t))

	rec := f.get(t, photoFullPath(rosewoodPlantID, rosewoodPhotoID))

	if got := rec.Header().Get("Cache-Control"); got != "private, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q, want private, a year and immutable", got)
	}
}

func TestPhoto_TheSquareRouteServesTheSquareVariant(t *testing.T) {
	f := photosServedTo(t, routeQueries(t))

	rec := f.get(t, photoSquarePath(rosewoodPlantID, rosewoodPhotoID))

	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), f.square) {
		t.Errorf("status = %d with %d bytes, want %d with the %d byte square", rec.Code, rec.Body.Len(), http.StatusOK, len(f.square))
	}
}

func TestPhoto_APhotoUnderAnotherPlantsURLIs404(t *testing.T) {
	f := photosServedTo(t, routeQueries(t))

	rec := f.get(t, photoFullPath(rosewoodArchivedID, rosewoodPhotoID))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestPhoto_TheSquareRouteIs404ForAPhotoUploadedWithoutOne(t *testing.T) {
	f := photosServedTo(t, routeQueries(t))

	rec := f.get(t, photoSquarePath(rosewoodPlantID, rosewoodPlainPhotoID))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestPhoto_ARowWhoseFileIsMissingIsAServerError(t *testing.T) {
	f := photosServedTo(t, routeQueries(t))

	rec := f.get(t, photoFullPath(rosewoodPlantID, rosewoodPlainPhotoID))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestPhoto_AnIDThatIsNotAUUIDIs404(t *testing.T) {
	f := photosServedTo(t, routeQueries(t))

	rec := f.get(t, plantPath(rosewoodPlantID)+"/photos/not-an-id/full")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
