package http

import (
	"bytes"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ismailshak/sprig/web"
)

// renderShell parses the layout and the partials directly rather than going
// through Templates, so nothing but the shell is rendered.
func renderShell(t *testing.T, assets *Assets, tab string) string {
	t.Helper()
	set, err := template.New(layoutTemplate).
		Funcs(template.FuncMap{"asset": assets.Path}).
		ParseFS(web.Templates, layoutTemplate, "partials/*.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := set.Parse(`{{define "main"}}{{end}}{{define "nav"}}{{template "tabs" $.Tab}}{{end}}`); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := set.ExecuteTemplate(&buf, layoutTemplate, struct{ Tab string }{tab}); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestShell_EveryAssetItReferencesIsServed(t *testing.T) {
	assets := testAssets()
	shell := readHTML(renderShell(t, assets, "Today"))

	var references []string
	for _, e := range shell.all(hasAttr("href")) {
		references = append(references, e.attr("href"))
	}
	for _, e := range shell.all(hasAttr("src")) {
		references = append(references, e.attr("src"))
	}
	if len(references) == 0 {
		t.Fatal("the shell names no stylesheet and no script")
	}

	stylesheets := 0
	for _, url := range references {
		if !strings.HasPrefix(url, assetPrefix) {
			continue
		}
		if strings.HasSuffix(url, ".css") {
			stylesheets++
		}

		rec := httptest.NewRecorder()
		assets.handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, url, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("the shell asks for %s and the server answers %d", url, rec.Code)
		}
	}
	if stylesheets != 3 {
		t.Errorf("the shell links %d stylesheets, want palettes, tokens and app", stylesheets)
	}
}

// A CDN would put a third-party origin in the critical path of a self-hosted
// app.
func TestShell_LoadsHTMXFromThisOrigin(t *testing.T) {
	shell := readHTML(renderShell(t, testAssets(), "Today"))

	htmx := false
	for _, script := range shell.all(isTag("script"), hasAttr("src")) {
		src := script.attr("src")
		if !strings.HasPrefix(src, assetPrefix) {
			t.Errorf("a script is loaded from %q rather than from this app", src)
		}
		htmx = htmx || strings.Contains(src, "htmx")
	}
	if !htmx {
		t.Fatal("the shell loads no htmx script")
	}
	if shell.first(hasAttr("hx-boost")) != nil {
		t.Error("the shell boosts navigation, which makes every page load a partial swap")
	}
}

// An installed app needs the manifest. iOS takes the Home Screen icon from
// the apple-touch-icon link and not from the manifest.
func TestShell_LinksTheManifestAndTheAppleTouchIcon(t *testing.T) {
	shell := readHTML(renderShell(t, testAssets(), "Today"))

	for _, rel := range []string{"manifest", "apple-touch-icon"} {
		if shell.first(isTag("link"), attrIs("rel", rel)) == nil {
			t.Errorf("the shell has no %s link, so the app cannot be installed", rel)
		}
	}
}

func TestShell_TheCurrentTabIsMarkedCurrentAndLinksToItsOwnPage(t *testing.T) {
	for tab, href := range map[string]string{
		"Today":    "/",
		"Plants":   "/plants",
		"Activity": "/activity",
		"More":     "/more",
	} {
		t.Run(tab, func(t *testing.T) {
			shell := readHTML(renderShell(t, testAssets(), tab))

			if links := shell.first(isTag("nav")).all(isTag("a")); len(links) != 4 {
				t.Errorf("the bar has %d links, want all four tabs", len(links))
			}
			current := shell.all(attrIs("aria-current", "page"))
			if len(current) != 1 {
				t.Fatalf("%d tabs are marked current, want 1", len(current))
			}
			if got := current[0].attr("href"); got != href {
				t.Errorf("the tab marked current links to %s, want the %s tab at %s", got, tab, href)
			}
		})
	}
}

func TestShell_APageUnderPlantsMarksThePlantsTabAndLinksToTheList(t *testing.T) {
	shell := readHTML(renderShell(t, testAssets(), "Under Plants"))

	if got := shell.first(attrIs("aria-current", "true")).attr("href"); got != "/plants" {
		t.Errorf("the tab marked is the one linking to %q, want the Plants tab linking to the list", got)
	}
	if shell.first(attrIs("aria-current", "page")) != nil {
		t.Error("a tab is marked as the current page, and the reader is on a page under Plants rather than on the list")
	}
}

func TestShell_APageUnderMoreMarksTheMoreTabAndLinksToTheIndex(t *testing.T) {
	shell := readHTML(renderShell(t, testAssets(), "Under More"))

	if got := shell.first(attrIs("aria-current", "true")).attr("href"); got != "/more" {
		t.Errorf("the tab marked is the one linking to %q, want the More tab linking to the index", got)
	}
	if shell.first(attrIs("aria-current", "page")) != nil {
		t.Error("a tab is marked as the current page, and the reader is on a page under More rather than on the index")
	}
}

// Sign in, invite and setup come before there is a garden, and a tab bar there
// would offer four pages a stranger cannot open.
func TestShell_APageThatDefinesNoTabBarGetsNone(t *testing.T) {
	assets := testAssets()
	set, err := template.New(layoutTemplate).
		Funcs(template.FuncMap{"asset": assets.Path}).
		ParseFS(web.Templates, layoutTemplate, "partials/*.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := set.Parse(`{{define "main"}}<p>Sign in</p>{{end}}`); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := set.ExecuteTemplate(&buf, layoutTemplate, nil); err != nil {
		t.Fatal(err)
	}
	if readHTML(buf.String()).first(isTag("nav")) != nil {
		t.Error("a page that defines no tab bar was given one anyway")
	}
}
