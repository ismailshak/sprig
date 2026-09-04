// Package web embeds the templates the server renders.
package web

import (
	"embed"
	"io/fs"
)

//go:embed templates
var embedded embed.FS

// Templates is web/templates rooted at the directory itself, so a template is
// named by its path below that directory rather than by "templates/...".
var Templates = mustSub(embedded, "templates")

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err) // the tree is compiled in, so a failure here is a broken build
	}
	return sub
}
