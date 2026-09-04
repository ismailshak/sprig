package http

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/ismailshak/sprig/web"
)

const layoutTemplate = "layout.html"

// Templates is the tree under web/templates, parsed into one set per file in
// pages/. Each set holds the layout, every file in partials/ and that one
// page, so a fragment and a page load execute the same definition.
//
// The layout declares the blocks "main" and "title" and every page redefines
// them, so two pages cannot share a set. The last one parsed would answer for
// both.
type Templates struct {
	logger *slog.Logger
	dir    string
	funcs  template.FuncMap
	pages  map[string]*template.Template
}

// ParseTemplates reads the template tree and returns an error for a template
// that does not parse. An empty dir parses the tree compiled into the binary
// once. A dir names a directory to read instead, re-read on every render, so
// an edit during development needs no restart.
//
// A template reaches assets through the asset function, which turns a file's
// name into the hashed URL.
func ParseTemplates(logger *slog.Logger, dir string, assets *Assets) (*Templates, error) {
	t := &Templates{logger: logger, dir: dir, funcs: template.FuncMap{"asset": assets.Path}}
	pages, err := parsePages(t.fs(), t.funcs)
	if err != nil {
		return nil, err
	}
	t.pages = pages
	return t, nil
}

func (t *Templates) fs() fs.FS {
	if t.dir == "" {
		return web.Templates
	}
	return os.DirFS(t.dir)
}

// parsePages parses the layout and the partials before cloning that set per
// page, so a layout that does not parse is an error in a tree with no pages.
func parsePages(fsys fs.FS, funcs template.FuncMap) (map[string]*template.Template, error) {
	partials, err := fs.Glob(fsys, "partials/*.html")
	if err != nil {
		return nil, err
	}
	base, err := template.New(layoutTemplate).Funcs(funcs).ParseFS(fsys, append([]string{layoutTemplate}, partials...)...)
	if err != nil {
		return nil, fmt.Errorf("parsing the layout and partials: %w", err)
	}

	files, err := fs.Glob(fsys, "pages/*.html")
	if err != nil {
		return nil, err
	}
	pages := make(map[string]*template.Template, len(files))
	for _, file := range files {
		set, err := base.Clone()
		if err != nil {
			return nil, fmt.Errorf("cloning the layout for %s: %w", file, err)
		}
		if _, err := set.ParseFS(fsys, file); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", file, err)
		}
		pages[strings.TrimSuffix(path.Base(file), ".html")] = set
	}
	return pages, nil
}

// view is what a route renders, the page for a navigation and the fragment
// for an htmx request. A view with no fragment answers both with the page.
type view struct {
	page     string
	fragment string
}

// render writes v to w. It executes into a buffer first, because a template
// that fails halfway has already written the top of the page, and that much
// would otherwise reach the browser under a 200.
func (t *Templates) render(w http.ResponseWriter, r *http.Request, v view, data any) {
	pages := t.pages
	if t.dir != "" {
		reloaded, err := parsePages(t.fs(), t.funcs)
		if err != nil {
			t.fail(w, r, err)
			return
		}
		pages = reloaded
	}

	// A view is a literal in a handler, so a page that is not in the set is a
	// typo, and it surfaces on the first request for that route rather than
	// at startup. If routes ever declare their view the way they declare
	// their capability, New can check every page name against the set instead.
	set, ok := pages[v.page]
	if !ok {
		t.fail(w, r, fmt.Errorf("no page named %q", v.page))
		return
	}

	name := layoutTemplate
	if v.fragment != "" && isHTMX(r) {
		name = v.fragment
	}

	var buf bytes.Buffer
	if err := set.ExecuteTemplate(&buf, name, data); err != nil {
		t.fail(w, r, fmt.Errorf("executing %s in %s: %w", name, v.page, err))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// A failed write means the browser hung up, which nothing here can act on.
	_, _ = w.Write(buf.Bytes())
}

func (t *Templates) fail(w http.ResponseWriter, r *http.Request, err error) {
	serverError(t.logger, w, r, "render", err)
}

// htmx sets HX-Request on every request it sends, and a navigation carries no
// such header.
func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}
