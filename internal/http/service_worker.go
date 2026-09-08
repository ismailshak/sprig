package http

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"
)

const (
	// serviceWorkerPath is at the root because a worker only sees requests
	// under the directory it was served from.
	serviceWorkerPath = "/service-worker.js"

	serviceWorkerFile = "service-worker.js"

	// offlinePath is the page the worker serves when a navigation fails and it
	// has no cached copy.
	offlinePath = "/offline"

	// notificationIcon is the static file shown beside a push notification.
	notificationIcon = "icons/icon-192.png"
)

// serviceWorker holds the script served at /service-worker.js: the file from
// the static tree with six constants written above it.
//
// The shell holds the plain URL of every file a stylesheet names with url()
// as well as the hashed URLs, because a url() reference resolves to the plain
// URL and no build step rewrites CSS. The version covers the templates too,
// so editing a page installs a new worker.
type serviceWorker struct {
	// script is nil when the static tree has no service-worker.js or no
	// notification icon. The tree is compiled into the binary, so that only
	// happens in a broken build.
	script []byte
	etag   string
}

func newServiceWorker(assets *Assets, templates *Templates) *serviceWorker {
	body, ok := assets.content(serviceWorkerFile)
	if !ok {
		return &serviceWorker{}
	}
	icon, err := assets.Path(notificationIcon)
	if err != nil {
		return &serviceWorker{}
	}

	shell := shellURLs(assets)

	sum := sha256.New()
	sum.Write(body)
	for _, url := range shell {
		sum.Write([]byte(url + "\n"))
	}
	sum.Write([]byte(templates.digest))
	version := hex.EncodeToString(sum.Sum(nil))[:hashLength]

	// Marshal cannot fail on a []string, so the error is dropped.
	shellJSON, _ := json.Marshal(shell)

	var script bytes.Buffer
	fmt.Fprintf(&script, "const VERSION = %q;\n", version)
	fmt.Fprintf(&script, "const SHELL = %s;\n", shellJSON)
	fmt.Fprintf(&script, "const OFFLINE = %q;\n", offlinePath)
	fmt.Fprintf(&script, "const SIGN_IN = %q;\n", signInPath)
	fmt.Fprintf(&script, "const GARDENS = %q;\n", gardensPath)
	// The hashed URL is one the shell cache holds, so the icon is available
	// with no network.
	fmt.Fprintf(&script, "const ICON = %q;\n\n", icon)
	script.Write(body)

	return &serviceWorker{script: script.Bytes(), etag: `"` + version + `"`}
}

// shellURLs returns the URLs the worker caches at install, sorted so the list
// and the version are the same from one start to the next.
func shellURLs(assets *Assets) []string {
	urls := []string{offlinePath}
	for name, hashed := range assets.paths {
		urls = append(urls, hashed)
		if path.Ext(name) != ".css" {
			continue
		}
		content, _ := assets.content(name)
		for _, match := range cssURL.FindAllStringSubmatch(string(content), -1) {
			reference := match[1]
			if strings.Contains(reference, ":") || strings.HasPrefix(reference, "//") {
				continue
			}
			urls = append(urls, assetPrefix+path.Join(path.Dir(name), reference))
		}
	}
	slices.Sort(urls)
	return slices.Compact(urls)
}

// serve writes the script. no-cache makes the browser revalidate on every
// check for a new worker rather than reuse a cached copy.
func (s *serviceWorker) serve(w http.ResponseWriter, r *http.Request) {
	if s.script == nil {
		notFound(w)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", s.etag)
	http.ServeContent(w, r, serviceWorkerFile, time.Time{}, bytes.NewReader(s.script))
}

func offline(templates *Templates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		templates.render(w, r, view{page: "offline"}, nil)
	}
}
