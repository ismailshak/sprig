// Package web embeds the HTML templates and the static files.
package web

import (
	"embed"
	"io/fs"
)

//go:embed templates static
var embedded embed.FS

// Templates is the web/templates directory with the "templates/" prefix
// stripped, so a template is named "pages/plant.html" and not
// "templates/pages/plant.html".
var Templates = mustSub(embedded, "templates")

// Static is the web/static directory with the "static/" prefix stripped, so
// the stylesheet is "app.css" and htmx is "vendor/htmx.min.js".
var Static = mustSub(embedded, "static")

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err) // the directory is compiled in, so this can only fail on a broken build
	}
	return sub
}
