package http

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ismailshak/sprig/web"
)

// testTemplates parses the template tree once per test binary, because nearly
// every handler test calls it and a parse reads and hashes every template. The
// tree is compiled into the binary. A parse failure here is a broken build
// rather than a failing test.
var testTemplates = sync.OnceValue(func() *Templates {
	templates, err := ParseTemplates(slog.New(slog.NewJSONHandler(io.Discard, nil)), "", testAssets())
	if err != nil {
		panic(err)
	}
	return templates
})

// The template tree is the real one rather than a fixture, so a template
// referencing a missing file fails here as well as in a browser.
var testAssets = sync.OnceValue(func() *Assets {
	assets, err := NewAssets(web.Static)
	if err != nil {
		panic(err)
	}
	return assets
})

// The fixture's rows come from a partial rather than inline markup, so one
// definition serves both a swap and a page load.
const (
	fixtureLayout = `<!doctype html>
<title>{{block "title" .}}sprig{{end}}</title>
<body>{{block "main" .}}{{end}}</body>
`
	fixtureRow = `{{define "care-row"}}<li id="care-{{.Plant}}-{{.Care}}">{{.Plant}} needs {{.Care}}</li>{{end}}
`
	fixturePage = `{{define "title"}}sprig — today{{end}}
{{define "main"}}<ul id="care-rows">{{range .Rows}}{{template "care-row" .}}{{end}}</ul>{{end}}
`
)

type fixtureRowData struct{ Plant, Care string }

func writeTree(t *testing.T, layout, page, partial string) string {
	t.Helper()

	dir := t.TempDir()
	for _, d := range []string{"pages", "partials"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o750); err != nil {
			t.Fatalf("making %s: %v", d, err)
		}
	}
	files := map[string]string{
		"layout.html":            layout,
		"pages/today.html":       page,
		"partials/care-row.html": partial,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(content), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	return dir
}

func fixtureTemplates(t *testing.T) (*Templates, *bytes.Buffer) {
	t.Helper()

	var logged bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logged, nil))
	templates, err := ParseTemplates(logger, writeTree(t, fixtureLayout, fixturePage, fixtureRow), testAssets())
	if err != nil {
		t.Fatalf("parsing the fixture tree: %v", err)
	}
	return templates, &logged
}

func renderTo(t *testing.T, templates *Templates, htmx bool, v view, data any) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	templates.render(rec, req, v, data)
	return rec
}

func TestRender_TheFragmentIsTheSameMarkupThePageContains(t *testing.T) {
	templates, _ := fixtureTemplates(t)
	row := fixtureRowData{Plant: "Doris", Care: "water"}
	v := view{page: "today", fragment: "care-row"}

	fragment := renderTo(t, templates, true, v, row).Body.String()
	page := renderTo(t, templates, false, v, struct{ Rows []fixtureRowData }{[]fixtureRowData{row}}).Body.String()

	swapped := readHTML(fragment).byID("care-Doris-water")
	if swapped == nil {
		t.Fatalf("the fragment has no element under the id a swap targets:\n%s", fragment)
	}
	if got := readHTML(page).byID("care-Doris-water"); got.String() != swapped.String() {
		t.Errorf("the page's row is not the fragment.\nfragment:\n%s\npage:\n%s", fragment, page)
	}
}

func TestRender_ANavigationGetsTheWholePage(t *testing.T) {
	templates, _ := fixtureTemplates(t)

	rec := renderTo(t, templates, false, view{page: "today", fragment: "care-row"}, struct{ Rows []fixtureRowData }{})

	body := rec.Body.String()
	if readHTML(body).first(isTag("body")) == nil {
		t.Errorf("a request without HX-Request got something other than a page:\n%s", body)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", got)
	}
}

// Otherwise every htmx attribute in the templates would need to know whether
// its route has a fragment.
func TestRender_AViewWithNoFragmentGivesHTMXTheWholePage(t *testing.T) {
	templates, _ := fixtureTemplates(t)

	rec := renderTo(t, templates, true, view{page: "today"}, struct{ Rows []fixtureRowData }{})

	if readHTML(rec.Body.String()).first(isTag("body")) == nil {
		t.Errorf("an htmx request to a page with no fragment got:\n%s", rec.Body.String())
	}
}

// The layout writes the top of the page before the range that fails, so
// without the buffer the browser would get that much under a 200.
func TestRender_ATemplateThatFailsHalfwayWritesNothing(t *testing.T) {
	templates, logged := fixtureTemplates(t)

	rec := renderTo(t, templates, false, view{page: "today"}, struct{ Rows int }{3})

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if readHTML(rec.Body.String()).first() != nil {
		t.Errorf("the failed render wrote part of the page:\n%s", rec.Body.String())
	}
	if n := strings.Count(logged.String(), `"level":"ERROR"`); n != 1 {
		t.Errorf("the failure produced %d error lines, want 1", n)
	}
}

func TestRender_AnUnknownPageIs500AndNotAPanic(t *testing.T) {
	templates, logged := fixtureTemplates(t)

	rec := renderTo(t, templates, false, view{page: "plants"}, nil)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	var line struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(logged.String())), &line); err != nil {
		t.Fatalf("the log line was not JSON: %v", err)
	}
	if !strings.Contains(line.Error, "plants") {
		t.Errorf("the log line %q does not name the missing page", line.Error)
	}
}

func TestParseTemplates_ADirectoryOnDiskIsRereadPerRender(t *testing.T) {
	dir := writeTree(t, fixtureLayout, `{{define "main"}}first{{end}}`, fixtureRow)
	templates, err := ParseTemplates(slog.New(slog.NewJSONHandler(io.Discard, nil)), dir, testAssets())
	if err != nil {
		t.Fatalf("parsing the tree: %v", err)
	}

	if body := renderTo(t, templates, false, view{page: "today"}, nil).Body.String(); !strings.Contains(text(body), "first") {
		t.Fatalf("the first render did not read the file on disk:\n%s", body)
	}

	page := filepath.Join(dir, "pages", "today.html")
	if err := os.WriteFile(page, []byte(`{{define "main"}}second{{end}}`), 0o600); err != nil {
		t.Fatalf("rewriting the page: %v", err)
	}

	if body := renderTo(t, templates, false, view{page: "today"}, nil).Body.String(); !strings.Contains(text(body), "second") {
		t.Errorf("the second render did not pick up the edit:\n%s", body)
	}
}

// textWithoutAnnouncement returns the text a person reads in a swap's response
// with the announcement left out. A test of what the page says then cannot
// match the sentence the response announces instead.
func textWithoutAnnouncement(body string) string {
	page := readHTML(body)
	if announced := page.first(attrIs("hx-swap-oob", "innerHTML:#status")); announced != nil {
		announced.children = nil
	}
	return page.text()
}

// announcement returns the text of the element a swap's response puts into
// the layout's live region, or "" when the response has none.
func announcement(body string) string {
	return readHTML(body).first(attrIs("hx-swap-oob", "innerHTML:#status")).text()
}

func TestRender_AFragmentEndsWithItsAnnouncementAndAPageHasNone(t *testing.T) {
	templates := testTemplates()
	v := view{page: "today", fragment: "care-settled", announce: "All done for today."}
	data := careSettled{Head: todayHead{Clear: true}}

	fragment := renderTo(t, templates, true, v, data).Body.String()
	page := renderTo(t, templates, false, v, todayPage{}).Body.String()

	var last *element
	for _, child := range readHTML(fragment).children {
		if e, ok := child.(*element); ok {
			last = e
		}
	}
	if last.attr("hx-swap-oob") != "innerHTML:#status" || last.text() != "All done for today." {
		t.Errorf("the fragment does not end with the announcement:\n%s", fragment)
	}
	if readHTML(page).first(hasAttr("hx-swap-oob")) != nil {
		t.Errorf("the whole page has an out-of-band element:\n%s", page)
	}
}

func TestRender_AnAnnouncementIsEscaped(t *testing.T) {
	templates := testTemplates()
	v := view{page: "today", fragment: "care-settled", announce: "<b>Nigel</b>"}

	fragment := renderTo(t, templates, true, v, careSettled{Head: todayHead{Clear: true}}).Body.String()

	announced := readHTML(fragment).first(attrIs("hx-swap-oob", "innerHTML:#status"))
	if announced.text() != "<b>Nigel</b>" || announced.first(isTag("b")) != nil {
		t.Errorf("the announcement was not escaped:\n%s", fragment)
	}
}

func TestParseTemplates_ATreeThatDoesNotParseIsAStartupError(t *testing.T) {
	dir := writeTree(t, fixtureLayout, `{{define "main"}}{{range .Rows}}{{end}}`, fixtureRow)

	if _, err := ParseTemplates(slog.New(slog.NewJSONHandler(io.Discard, nil)), dir, testAssets()); err == nil {
		t.Fatal("a page with an unclosed range parsed without an error")
	}
}
