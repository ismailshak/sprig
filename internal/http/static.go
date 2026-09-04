package http

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

const (
	assetPrefix  = "/static/"
	assetPattern = "GET " + assetPrefix + "{path...}"

	// A URL carrying a content hash cannot serve different bytes later, so
	// immutable is safe to claim.
	hashedCacheControl = "public, max-age=31536000, immutable"

	// A plain name can serve different bytes later, so it promises an hour and
	// revalidates against the ETag after that.
	plainCacheControl = "public, max-age=3600"

	// Twelve hex characters are six bytes of the hash, which will not collide
	// across a tree of this size.
	hashLength = 12
)

// The built-in extension table has .css and .js but not .woff2, and the
// distroless image has no mime.types file for the mime package to read.
// Without the entry the container serves a font as application/octet-stream.
func init() {
	if err := mime.AddExtensionType(".woff2", "font/woff2"); err != nil {
		panic(err)
	}
}

// Assets is web/static, read into memory at startup and served at two URLs
// per file. No build step writes a manifest, so the hash is computed here.
//
// The plain name is served as well, because a stylesheet reaches its
// neighbours by a relative url() and nothing rewrites CSS. tokens.css asks for
// fonts/fraunces-v38-latin-600.woff2 and gets it whichever URL the stylesheet
// itself arrived at.
type Assets struct {
	// paths maps a name in the tree to its hashed URL, and files maps both of
	// a file's URLs back to it.
	paths map[string]string
	files map[string]asset
}

type asset struct {
	name    string
	content []byte
	etag    string
	hashed  bool
}

// NewAssets reads every file under fsys. It keeps the bytes rather than the
// FS, because hashing has already read them.
func NewAssets(fsys fs.FS) (*Assets, error) {
	a := &Assets{paths: map[string]string{}, files: map[string]asset{}}
	err := fs.WalkDir(fsys, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		content, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(content)
		digest := hex.EncodeToString(sum[:])
		etag := `"` + digest[:hashLength] + `"`

		hashed := assetPrefix + insertHash(name, digest)
		a.paths[name] = hashed
		a.files[hashed] = asset{name: name, content: content, etag: etag, hashed: true}
		a.files[assetPrefix+name] = asset{name: name, content: content, etag: etag}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading the static tree: %w", err)
	}
	return a, nil
}

// insertHash puts the hash before the extension, so the name still ends in
// .css or .woff2 and http.ServeContent reads the content type from it.
func insertHash(name, digest string) string {
	ext := path.Ext(name)
	return strings.TrimSuffix(name, ext) + "." + digest[:hashLength] + ext
}

// Path is the asset function a template calls. It returns an error for a name
// the tree does not hold, because the other answer is an empty href on a page
// that arrives with no styles.
func (a *Assets) Path(name string) (string, error) {
	url, ok := a.paths[name]
	if !ok {
		return "", fmt.Errorf("no static file named %q", name)
	}
	return url, nil
}

// handler answers the two URLs each file has and nothing else. A misspelt
// name misses the map and gets a 404 rather than a directory listing or a file
// from somewhere else in the tree.
func (a *Assets) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, ok := a.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		cacheControl := plainCacheControl
		if f.hashed {
			cacheControl = hashedCacheControl
		}
		w.Header().Set("Cache-Control", cacheControl)
		// ServeContent answers a 304 from this header.
		w.Header().Set("ETag", f.etag)
		// The zero time leaves out Last-Modified, and the ETag already carries
		// the revalidation.
		http.ServeContent(w, r, f.name, time.Time{}, bytes.NewReader(f.content))
	})
}
