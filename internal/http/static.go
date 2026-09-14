package http

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strconv"
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
	gzipped []byte
	// gzippedETag differs from etag because a cache stores the two encodings
	// as two responses.
	gzippedETag string
	contentType string
}

// NewAssets reads every file under fsys and compresses each one with gzip. It
// keeps the bytes rather than the FS, because hashing has already read them.
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
		gzipped, err := gzipIfSmaller(content)
		if err != nil {
			return fmt.Errorf("compressing %s: %w", name, err)
		}
		contentType := mime.TypeByExtension(path.Ext(name))
		if contentType == "" {
			contentType = http.DetectContentType(content)
		}

		plain := asset{
			name:        name,
			content:     content,
			contentType: contentType,
			etag:        `"` + digest[:hashLength] + `"`,
			gzipped:     gzipped,
			gzippedETag: `"` + digest[:hashLength] + `-gzip"`,
		}
		hashed := plain
		hashed.hashed = true

		url := assetPrefix + insertHash(name, digest)
		a.paths[name] = url
		a.files[url] = hashed
		a.files[assetPrefix+name] = plain
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading the static tree: %w", err)
	}
	return a, nil
}

// gzipIfSmaller returns content compressed with gzip, or nil when the
// compressed bytes are not smaller than content.
func gzipIfSmaller(content []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(content); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	if buf.Len() >= len(content) {
		return nil, nil
	}
	return buf.Bytes(), nil
}

// acceptsGzip reports whether the request's Accept-Encoding allows gzip. It
// does when gzip is listed with a q-value above zero, or when gzip is not
// listed and * is.
func acceptsGzip(r *http.Request) bool {
	star := false
	for _, header := range r.Header.Values("Accept-Encoding") {
		for coding := range strings.SplitSeq(header, ",") {
			name, params, _ := strings.Cut(coding, ";")
			allowed := qValue(params) > 0
			switch strings.ToLower(strings.TrimSpace(name)) {
			case "gzip", "x-gzip":
				return allowed
			case "*":
				star = allowed
			}
		}
	}
	return star
}

// qValue returns the q parameter among params, the part of one Accept-Encoding
// coding after its first semicolon. It returns 1 when there is none or it does
// not parse.
func qValue(params string) float64 {
	for param := range strings.SplitSeq(params, ";") {
		name, value, _ := strings.Cut(strings.TrimSpace(param), "=")
		if !strings.EqualFold(name, "q") {
			continue
		}
		if q, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			return q
		}
	}
	return 1
}

// insertHash puts the hash before the extension, as in app.3f2a9c1b04de.css.
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
// reaches a file outside the tree. A file with a gzipped form is sent gzipped
// to a request that accepts gzip.
func (a *Assets) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, ok := a.files[r.URL.Path]
		if !ok {
			// Plain text rather than the page, because a browser asks for an
			// asset from a link or a script tag and never shows the response.
			http.Error(w, notFoundText, http.StatusNotFound)
			return
		}
		cacheControl := plainCacheControl
		if f.hashed {
			cacheControl = hashedCacheControl
		}
		w.Header().Set("Cache-Control", cacheControl)
		content, etag := f.content, f.etag
		if f.gzipped != nil {
			// Without Vary, a cache keyed on the URL alone could give the
			// gzipped bytes to a client that did not ask for them.
			w.Header().Set("Vary", "Accept-Encoding")
			if acceptsGzip(r) {
				content, etag = f.gzipped, f.gzippedETag
				w.Header().Set("Content-Encoding", "gzip")
			}
		}
		// For a file with no known extension, ServeContent sniffs the type from
		// the bytes it sends. Those can be the gzipped bytes.
		w.Header().Set("Content-Type", f.contentType)
		// ServeContent compares this with If-None-Match and returns 304 on a match.
		w.Header().Set("ETag", etag)
		// A zero modification time leaves Last-Modified off the response.
		// Revalidation here goes through the ETag.
		http.ServeContent(w, r, f.name, time.Time{}, bytes.NewReader(content))
	})
}
