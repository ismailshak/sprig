// Package db embeds the migrations the server applies at startup.
package db

import (
	"embed"
	"io/fs"
)

//go:embed migrations
var embedded embed.FS

// Migrations is the db/migrations directory with the "migrations/" prefix
// stripped, because goose reads migrations from the root of the fs.FS it is
// given.
var Migrations = mustSub(embedded, "migrations")

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err) // the directory is compiled in, so this can only fail on a broken build
	}
	return sub
}
