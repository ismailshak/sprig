// Package db embeds the migrations the server applies to its own schema at startup.
package db

import (
	"embed"
	"io/fs"
)

// Without all:, embed skips the .gitkeep that is currently the only file here.
//
//go:embed all:migrations
var embedded embed.FS

// Migrations is db/migrations rooted at the directory itself, because goose
// reads migrations from the top of the tree it is given.
var Migrations = mustSub(embedded, "migrations")

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err) // the tree is compiled in, so a failure here is a broken build
	}
	return sub
}
