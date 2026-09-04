//go:build !dev

package http

import (
	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// devRoutes is empty in a production build. The development sign-in lives in
// a file only the dev build tag compiles, so the binary the image is built
// from has no route that grants a session without a credential.
func devRoutes(*auth.Sessions, *store.Queries, *Templates) []route {
	return nil
}
