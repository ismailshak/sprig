// Package web embeds the templates the server renders and the files it serves
// unchanged.
package web

import (
	"embed"
	"io/fs"
)

//go:embed templates static
var embedded embed.FS

// Templates is web/templates rooted at the directory itself, so a template is
// named by its path below that directory rather than by "templates/...".
var Templates = mustSub(embedded, "templates")

// Static is web/static rooted the same way, so a stylesheet is "app.css" and
// the vendored script is "vendor/htmx-2.0.10.min.js".
var Static = mustSub(embedded, "static")

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err) // the tree is compiled in, so a failure here is a broken build
	}
	return sub
}
