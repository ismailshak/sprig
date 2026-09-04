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
// through Templates, because Templates holds a set per page and the tree has
// no page yet.
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

func TestShell_EveryFileItNamesIsOneTheServerServes(t *testing.T) {
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

func TestShell_TheCurrentTabIsTheOnlyOneThatIsNotALink(t *testing.T) {
	for tab, href := range map[string]string{
		"Today":    "/",
		"Plants":   "/plants",
		"Activity": "/activity",
		"More":     "/more",
	} {
		t.Run(tab, func(t *testing.T) {
			shell := renderShell(t, testAssets(), tab)

			if links := strings.Count(shell, `class="nav__item" href=`); links != 3 {
				t.Errorf("the bar has %d links, want the three tabs that are not this one", links)
			}
			if current := strings.Count(shell, `aria-current="page"`); current != 1 {
				t.Errorf("%d tabs are marked current, want 1", current)
			}
			if strings.Contains(shell, `href="`+href+`"`) {
				t.Errorf("the %s tab links to %s, which is the page it is already on", tab, href)
			}
		})
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
