package http

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/ismailshak/sprig/web"
)

var (
	shellConstant = regexp.MustCompile(`(?m)^const SHELL = (\[.*\]);$`)
	iconConstant  = regexp.MustCompile(`(?m)^const ICON = "(.*)";$`)
	// hashedURL matches the content hash the static handler puts before a
	// file's extension.
	hashedURL = regexp.MustCompile(fmt.Sprintf(`\.[0-9a-f]{%d}\.[a-z0-9]+$`, hashLength))
)

// serveWorker serves the worker for the real static tree and returns the
// response with the SHELL list read out of it.
func serveWorker(t *testing.T) (*httptest.ResponseRecorder, []string) {
	t.Helper()
	rec := httptest.NewRecorder()
	newServiceWorker(testAssets(), testTemplates()).serve(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, serviceWorkerPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	match := shellConstant.FindStringSubmatch(rec.Body.String())
	if match == nil {
		t.Fatalf("the script declares no SHELL list:\n%s", rec.Body.String())
	}
	var shell []string
	if err := json.Unmarshal([]byte(match[1]), &shell); err != nil {
		t.Fatalf("SHELL is not a JSON list: %v", err)
	}
	return rec, shell
}

func TestServiceWorker_IsServedAsAScriptThatIsRevalidatedOnEveryCheck(t *testing.T) {
	rec, _ := serveWorker(t)

	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/javascript") {
		t.Errorf("Content-Type = %q, want text/javascript", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	if rec.Header().Get("ETag") == "" {
		t.Error("no ETag, so a check for a new worker downloads the script every time")
	}
}

func TestServiceWorker_TheShellIsEveryStaticFileAndTheOfflinePage(t *testing.T) {
	assets := testAssets()
	_, shell := serveWorker(t)

	cached := map[string]bool{}
	for _, url := range shell {
		cached[url] = true
	}
	if !cached[offlinePath] {
		t.Errorf("the shell does not hold the offline page %s", offlinePath)
	}
	if err := fs.WalkDir(web.Static, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		hashed, err := assets.Path(name)
		if err != nil {
			return err
		}
		if !cached[hashed] {
			t.Errorf("the shell does not hold %s at %s", name, hashed)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// The stylesheet asks for a font by its plain URL, so the plain URL has to be
// cached as well as the hashed one.
func TestServiceWorker_TheShellHoldsTheFontsAtTheURLTheStylesheetAsksFor(t *testing.T) {
	_, shell := serveWorker(t)

	plainFonts := 0
	for _, url := range shell {
		if strings.HasPrefix(url, assetPrefix+"fonts/") && !hashedURL.MatchString(url) {
			plainFonts++
		}
	}
	if plainFonts == 0 {
		t.Errorf("no plain font URL in the shell:\n%s", strings.Join(shell, "\n"))
	}
}

func TestServiceWorker_EveryShellURLIsServed(t *testing.T) {
	assets := testAssets()
	_, shell := serveWorker(t)

	for _, url := range shell {
		if url == offlinePath {
			continue
		}
		rec := httptest.NewRecorder()
		assets.handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, url, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("the shell names %s and the server answers %d", url, rec.Code)
		}
	}
}

func TestServiceWorker_TheNotificationIconIsAURLTheServerServes(t *testing.T) {
	assets := testAssets()
	rec, _ := serveWorker(t)

	match := iconConstant.FindStringSubmatch(rec.Body.String())
	if match == nil || match[1] == "" {
		t.Fatalf("the script declares no ICON:\n%s", rec.Body.String())
	}
	served := httptest.NewRecorder()
	assets.handler().ServeHTTP(served, httptest.NewRequestWithContext(t.Context(), http.MethodGet, match[1], nil))
	if served.Code != http.StatusOK {
		t.Errorf("a notification is shown with %s and the server answers %d", match[1], served.Code)
	}
}

func TestServiceWorker_ChangingAStaticFileChangesTheVersion(t *testing.T) {
	templates := testTemplates()
	before := fixtureAssets(t, map[string]string{serviceWorkerFile: "// worker", notificationIcon: "png", "app.css": "body{}"})
	after := fixtureAssets(t, map[string]string{serviceWorkerFile: "// worker", notificationIcon: "png", "app.css": "body{margin:0}"})

	if a, b := newServiceWorker(before, templates).etag, newServiceWorker(after, templates).etag; a == b {
		t.Errorf("the version is %s before and after app.css changed", a)
	}
}

func TestServiceWorker_ChangingATemplateChangesTheVersion(t *testing.T) {
	assets := fixtureAssets(t, map[string]string{serviceWorkerFile: "// worker", notificationIcon: "png"})
	before := &Templates{digest: "one"}
	after := &Templates{digest: "two"}

	if a, b := newServiceWorker(assets, before).etag, newServiceWorker(assets, after).etag; a == b {
		t.Errorf("the version is %s before and after a template changed", a)
	}
}

// The shell URLs are read out of a map. A version that differed from one
// start to the next would throw away every client's caches on a deploy that
// changed nothing.
func TestServiceWorker_TheVersionIsTheSameForTheSameFiles(t *testing.T) {
	assets, templates := testAssets(), testTemplates()

	if a, b := newServiceWorker(assets, templates).etag, newServiceWorker(assets, templates).etag; a != b {
		t.Errorf("two builds of the same files give %s and %s", a, b)
	}
}

func TestServiceWorker_IsNotFoundWhenTheStaticTreeHasNoScript(t *testing.T) {
	assets := fixtureAssets(t, map[string]string{"app.css": "body{}"})

	rec := httptest.NewRecorder()
	newServiceWorker(assets, testTemplates()).serve(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, serviceWorkerPath, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestOffline_RendersWithNoSession(t *testing.T) {
	rec := httptest.NewRecorder()
	offline(testTemplates()).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, offlinePath, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	page := text(rec.Body.String())
	for _, want := range []string{"You’re offline", "Try again"} {
		if !strings.Contains(page, want) {
			t.Errorf("the page does not say %q:\n%s", want, page)
		}
	}
}

type manifest struct {
	ID    string `json:"id"`
	Icons []struct {
		Src     string `json:"src"`
		Sizes   string `json:"sizes"`
		Purpose string `json:"purpose"`
	} `json:"icons"`
}

func readManifest(t *testing.T) manifest {
	t.Helper()
	content, err := fs.ReadFile(web.Static, "manifest.webmanifest")
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(content, &m); err != nil {
		t.Fatalf("the manifest is not JSON: %v", err)
	}
	return m
}

// Android needs a 192 and a 512 icon to offer install, and a maskable one to
// fill its own icon shape.
func TestManifest_NamesTheIconsAnInstallNeedsAndEveryOneIsServed(t *testing.T) {
	assets := testAssets()
	m := readManifest(t)

	sizes := map[string]bool{}
	maskable := false
	for _, icon := range m.Icons {
		sizes[icon.Sizes] = true
		maskable = maskable || icon.Purpose == "maskable"
		rec := httptest.NewRecorder()
		assets.handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, icon.Src, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("the manifest names %s and the server answers %d", icon.Src, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != "image/png" {
			t.Errorf("%s is served as %q, want image/png", icon.Src, got)
		}
	}
	for _, want := range []string{"192x192", "512x512"} {
		if !sizes[want] {
			t.Errorf("no %s icon", want)
		}
	}
	if !maskable {
		t.Error("no maskable icon")
	}
}

// The id is what the browser identifies an installed app by.
func TestManifest_HasAnID(t *testing.T) {
	if id := readManifest(t).ID; id != "/" {
		t.Errorf("id = %q, want /", id)
	}
}

func TestManifest_IsServedWithItsOwnContentType(t *testing.T) {
	assets := testAssets()
	url, err := assets.Path("manifest.webmanifest")
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	assets.handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, url, nil))
	if got := rec.Header().Get("Content-Type"); got != "application/manifest+json" {
		t.Errorf("Content-Type = %q, want application/manifest+json", got)
	}
}
