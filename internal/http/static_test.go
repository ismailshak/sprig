package http

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ismailshak/sprig/web"
)

func fixtureAssets(t *testing.T, files map[string]string) *Assets {
	t.Helper()
	fsys := fstest.MapFS{}
	for name, content := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(content)}
	}
	assets, err := NewAssets(fsys)
	if err != nil {
		t.Fatal(err)
	}
	return assets
}

// compressible is a stylesheet gzip makes smaller.
var compressible = strings.Repeat(".app { display: flex }\n", 40)

func getAsset(t *testing.T, assets *Assets, url, acceptEncoding string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if acceptEncoding != "" {
		r.Header.Set("Accept-Encoding", acceptEncoding)
	}
	rec := httptest.NewRecorder()
	assets.handler().ServeHTTP(rec, r)
	return rec
}

func TestAssets_AStylesheetIsSentGzippedToARequestThatAcceptsGzip(t *testing.T) {
	assets := fixtureAssets(t, map[string]string{"app.css": compressible})
	url, err := assets.Path("app.css")
	if err != nil {
		t.Fatal(err)
	}

	rec := getAsset(t, assets, url, "gzip, deflate, br")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding is %q, want gzip", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/css") {
		t.Errorf("Content-Type is %q, want text/css", got)
	}
	if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Errorf("Vary is %q, want Accept-Encoding", got)
	}
	if rec.Body.Len() >= len(compressible) {
		t.Errorf("sent %d bytes for a %d-byte file", rec.Body.Len(), len(compressible))
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("the body is not gzip: %v", err)
	}
	body, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("reading the gzipped body: %v", err)
	}
	if string(body) != compressible {
		t.Errorf("the body decompresses to %d bytes that differ from the file", len(body))
	}
}

func TestAssets_AcceptEncodingDecidesWhetherTheFileIsGzipped(t *testing.T) {
	assets := fixtureAssets(t, map[string]string{"app.css": compressible})

	for _, c := range []struct {
		acceptEncoding string
		gzipped        bool
	}{
		{"", false},
		{"gzip;q=0", false},
		{"*;q=0", false},
		{"*, gzip;q=0", false},
		{"GZIP", true},
		{"br, gzip;q=0.5", true},
		{"*", true},
		{"br;q=1, *;q=0.1", true},
	} {
		t.Run(c.acceptEncoding, func(t *testing.T) {
			rec := getAsset(t, assets, "/static/app.css", c.acceptEncoding)
			if got := rec.Header().Get("Content-Encoding") == "gzip"; got != c.gzipped {
				t.Errorf("gzipped = %v, want %v", got, c.gzipped)
			}
			if !c.gzipped && rec.Body.String() != compressible {
				t.Errorf("the uncompressed body is %d bytes that differ from the file", rec.Body.Len())
			}
			if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
				t.Errorf("Vary is %q, want Accept-Encoding", got)
			}
		})
	}
}

func TestAssets_AGzippedResponseRevalidatesAgainstItsOwnETag(t *testing.T) {
	assets := fixtureAssets(t, map[string]string{"app.css": compressible})

	plain := getAsset(t, assets, "/static/app.css", "").Header().Get("ETag")
	gzipped := getAsset(t, assets, "/static/app.css", "gzip").Header().Get("ETag")
	if gzipped == "" || gzipped == plain {
		t.Fatalf("the gzipped ETag is %q and the plain one %q, want two different ETags", gzipped, plain)
	}

	again := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/static/app.css", nil)
	again.Header.Set("Accept-Encoding", "gzip")
	again.Header.Set("If-None-Match", gzipped)
	rec := httptest.NewRecorder()
	assets.handler().ServeHTTP(rec, again)
	if rec.Code != http.StatusNotModified {
		t.Errorf("got %d, want %d", rec.Code, http.StatusNotModified)
	}
}

func TestAssets_AFileGzipDoesNotShrinkIsSentWithoutContentEncoding(t *testing.T) {
	font := make([]byte, 512)
	if _, err := rand.Read(font); err != nil {
		t.Fatal(err)
	}
	assets := fixtureAssets(t, map[string]string{"fonts/fraunces.woff2": string(font)})

	rec := getAsset(t, assets, "/static/fonts/fraunces.woff2", "gzip")
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding is %q, want none for a file gzip makes no smaller", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), font) {
		t.Error("the body differs from the file")
	}
}

func TestAssets_TheHashComesBeforeTheExtension(t *testing.T) {
	assets := fixtureAssets(t, map[string]string{"vendor/htmx.min.js": "var htmx"})

	url, err := assets.Path("vendor/htmx.min.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, "/static/vendor/htmx.min.") || !strings.HasSuffix(url, ".js") {
		t.Fatalf("got %q, want the hash between the name and the .js", url)
	}
	if len(url) != len("/static/vendor/htmx.min..js")+hashLength {
		t.Errorf("%q does not carry %d characters of hash", url, hashLength)
	}
}

func TestAssets_ChangingAFileChangesItsURL(t *testing.T) {
	before := fixtureAssets(t, map[string]string{"app.css": ".app { color: red }"})
	after := fixtureAssets(t, map[string]string{"app.css": ".app { color: blue }"})

	was, _ := before.Path("app.css")
	now, _ := after.Path("app.css")
	if was == now {
		t.Fatalf("both editions of app.css are served at %q, so a cached copy would be the old one forever", was)
	}
}

func TestAssets_PathRefusesAFileTheTreeDoesNotHold(t *testing.T) {
	assets := fixtureAssets(t, map[string]string{"app.css": "body {}"})

	if _, err := assets.Path("apps.css"); err == nil {
		t.Fatal("a misspelt name returned a URL, so the page would arrive with an empty href and no complaint")
	}
}

func TestAssets_ServesTheHashedPathWithTheHeadersThatMakeItCacheable(t *testing.T) {
	const content = ".app { display: flex }"
	assets := fixtureAssets(t, map[string]string{"app.css": content})
	url, err := assets.Path("app.css")
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	assets.handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, url, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d for %q, want %d", rec.Code, url, http.StatusOK)
	}
	if rec.Body.String() != content {
		t.Errorf("got %q, want %q", rec.Body.String(), content)
	}
	if got := rec.Header().Get("Cache-Control"); got != hashedCacheControl {
		t.Errorf("Cache-Control is %q, want %q", got, hashedCacheControl)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/css") {
		t.Errorf("Content-Type is %q, want text/css", got)
	}
}

func TestAssets_ServesOnlyTheTwoURLsAFileHas(t *testing.T) {
	assets := fixtureAssets(t, map[string]string{"app.css": "body {}"})
	hashed, err := assets.Path("app.css")
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		"/static/app.000000000000.css",
		strings.TrimSuffix(hashed, ".css"),
		"/static/apps.css",
		"/static/",
		"/static/../templates/layout.html",
	} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			assets.handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
			if rec.Code != http.StatusNotFound {
				t.Errorf("got %d, want %d", rec.Code, http.StatusNotFound)
			}
		})
	}
}

func TestAssets_ServesThePlainNameForRelativeStylesheetURLs(t *testing.T) {
	const woff2 = "wOF2 pretend"
	assets := fixtureAssets(t, map[string]string{"fonts/fraunces.woff2": woff2})

	rec := httptest.NewRecorder()
	assets.handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/static/fonts/fraunces.woff2", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != woff2 {
		t.Errorf("got %q, want %q", rec.Body.String(), woff2)
	}
	if got := rec.Header().Get("Content-Type"); got != "font/woff2" {
		t.Errorf("Content-Type is %q, want font/woff2", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != plainCacheControl {
		t.Errorf("Cache-Control is %q, want %q, because a plain name can serve different bytes tomorrow", got, plainCacheControl)
	}
}

func TestAssets_APlainNameRevalidatesToNotModified(t *testing.T) {
	assets := fixtureAssets(t, map[string]string{"fonts/fraunces.woff2": "wOF2 pretend"})

	first := httptest.NewRecorder()
	assets.handler().ServeHTTP(first, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/static/fonts/fraunces.woff2", nil))
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag, so an expired copy costs the whole font again")
	}

	again := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/static/fonts/fraunces.woff2", nil)
	again.Header.Set("If-None-Match", etag)
	rec := httptest.NewRecorder()
	assets.handler().ServeHTTP(rec, again)

	if rec.Code != http.StatusNotModified {
		t.Errorf("got %d, want %d", rec.Code, http.StatusNotModified)
	}
}

// Renaming a font would break the page, and nothing else would say so.
func TestAssets_EveryFileTheStylesheetsReferenceIsServed(t *testing.T) {
	assets := testAssets()
	sheets, err := fs.Glob(web.Static, "*.css")
	if err != nil {
		t.Fatal(err)
	}

	found := 0
	for _, sheet := range sheets {
		content, err := fs.ReadFile(web.Static, sheet)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range cssURL.FindAllStringSubmatch(string(content), -1) {
			reference := match[1]
			if strings.Contains(reference, ":") || strings.HasPrefix(reference, "//") {
				t.Errorf("%s names %s, which is not this origin", sheet, reference)
				continue
			}
			found++

			url := assetPrefix + path.Join(path.Dir(sheet), reference)
			rec := httptest.NewRecorder()
			assets.handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, url, nil))
			if rec.Code != http.StatusOK {
				t.Errorf("%s names %s, and %s answers %d", sheet, reference, url, rec.Code)
			}
		}
	}
	if found == 0 {
		t.Error("no stylesheet names a file, so nothing here was checked")
	}
}
