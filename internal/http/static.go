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
	"regexp"
	"strings"
	"time"
)

const (
	assetPrefix  = "/static/"
	assetPattern = "GET " + assetPrefix + "{path...}"

	// A URL carrying a content hash cannot serve different bytes later, so
	// immutable is safe to claim.
	hashedCacheControl = "public, max-age=31536000, immutable"

	// A URL without a hash can serve different bytes later, so it is cached for
	// an hour and revalidated against the ETag after that.
	plainCacheControl = "public, max-age=3600"

	// Twelve hex characters are six bytes of the hash, which will not collide
	// across a tree of this size.
	hashLength = 12
)

// The built-in extension table has .css and .js but not .woff2 or
// .webmanifest, and the distroless image has no mime.types file for the mime
// package to read. Without the entries the container serves a font and the
// web app manifest as application/octet-stream.
func init() {
	for ext, typ := range map[string]string{".woff2": "font/woff2", ".webmanifest": "application/manifest+json"} {
		if err := mime.AddExtensionType(ext, typ); err != nil {
			panic(err)
		}
	}
}

// cssURL matches a url() in a stylesheet and captures the reference in it.
var cssURL = regexp.MustCompile(`url\(\s*["']?([^"')]+)["']?\s*\)`)

// Assets is web/static, read into memory at startup and served at two URLs
// per file. No build step writes a manifest, so the hash is computed here.
//
// The plain name is served too, because stylesheets reference fonts by a
// relative url() and nothing rewrites CSS. tokens.css requests
// fonts/fraunces-v38-latin-600.woff2 and gets it whichever URL the stylesheet
// was loaded from.
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

// Path returns the hashed URL of a file in the static tree. Templates call it
// to write an href. An unknown name is an error rather than an empty string,
// because an empty href renders a page with no styles and nothing to say why.
func (a *Assets) Path(name string) (string, error) {
	url, ok := a.paths[name]
	if !ok {
		return "", fmt.Errorf("no static file named %q", name)
	}
	return url, nil
}

func (a *Assets) content(name string) ([]byte, bool) {
	f, ok := a.files[assetPrefix+name]
	if !ok {
		return nil, false
	}
	return f.content, true
}

// handler serves the two URLs each file has and nothing else. A name that is
// not in the map is a 404, so there is no directory listing and no path that
// reaches a file outside the tree.
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
		// ServeContent compares this with If-None-Match and returns 304 on a match.
		w.Header().Set("ETag", f.etag)
		// A zero modification time leaves Last-Modified off the response.
		// Revalidation here goes through the ETag.
		http.ServeContent(w, r, f.name, time.Time{}, bytes.NewReader(f.content))
	})
}
