package http

import (
	"bytes"
	"html/template"
	"net/http"
	"net/http/httptest"
	"regexp"
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

var localReference = regexp.MustCompile(`(?:href|src)="([^"]+)"`)

func TestShell_EveryAssetItReferencesIsServed(t *testing.T) {
	assets := testAssets()
	shell := renderShell(t, assets, "Today")

	references := localReference.FindAllStringSubmatch(shell, -1)
	if len(references) == 0 {
		t.Fatal("the shell names no stylesheet and no script")
	}

	stylesheets := 0
	for _, match := range references {
		url := match[1]
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
	shell := renderShell(t, testAssets(), "Today")

	script := regexp.MustCompile(`<script src="([^"]+)"`).FindStringSubmatch(shell)
	if script == nil {
		t.Fatal("the shell loads no script, so htmx is not there at all")
	}
	if !strings.HasPrefix(script[1], assetPrefix) {
		t.Errorf("htmx is loaded from %q rather than from this app", script[1])
	}
	if strings.Contains(shell, "hx-boost") {
		t.Error("the shell boosts navigation, which makes every page load a partial swap")
	}
}

// An installed app needs the manifest. iOS takes the Home Screen icon from
// the apple-touch-icon link and not from the manifest.
func TestShell_LinksTheManifestAndTheAppleTouchIcon(t *testing.T) {
	shell := renderShell(t, testAssets(), "Today")

	for _, link := range []string{`rel="manifest"`, `rel="apple-touch-icon"`} {
		if !strings.Contains(shell, link) {
			t.Errorf("the shell has no %s link, so the app cannot be installed", link)
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
			shell := renderShell(t, testAssets(), tab)

			if links := strings.Count(shell, `class="nav__item" href=`); links != 4 {
				t.Errorf("the bar has %d links, want all four tabs", links)
			}
			if current := strings.Count(shell, `aria-current="page"`); current != 1 {
				t.Errorf("%d tabs are marked current, want 1", current)
			}
			if !strings.Contains(shell, `href="`+href+`" aria-current="page"`) {
				t.Errorf("the %s tab is not the one marked current, or does not link to %s", tab, href)
			}
		})
	}
}

func TestShell_APageUnderPlantsMarksThePlantsTabAndLinksToTheList(t *testing.T) {
	shell := renderShell(t, testAssets(), "Under Plants")

	if !strings.Contains(shell, `href="/plants" aria-current="true"`) {
		t.Errorf("the Plants tab is not marked, or does not link to the list:\n%s", shell)
	}
	if strings.Contains(shell, `aria-current="page"`) {
		t.Error("a tab is marked as the current page, and the reader is on a page under Plants rather than on the list")
	}
}

func TestShell_APageUnderMoreMarksTheMoreTabAndLinksToTheIndex(t *testing.T) {
	shell := renderShell(t, testAssets(), "Under More")

	if !strings.Contains(shell, `href="/more" aria-current="true"`) {
		t.Errorf("the More tab is not marked, or does not link to the index:\n%s", shell)
	}
	if strings.Contains(shell, `aria-current="page"`) {
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
	if strings.Contains(buf.String(), "nav__item") {
		t.Error("a page that defines no tab bar was given one anyway")
	}
}
