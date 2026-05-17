package httpserver

import (
	"bytes"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// normalizePathMiddleware coalesces duplicate slashes (e.g. //) to a single "/".
// Go's http.FileServer is avoided for the SPA: its directory redirect uses
// path.Base("/")+ "/" which becomes Location: //, which browsers interpret as
// a scheme-relative URL and can break the home page.
func normalizePathMiddleware() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "" {
				r.URL.Path = "/"
			} else {
				c := path.Clean(r.URL.Path)
				if c == "" || c == "." {
					c = "/"
				} else if c[0] != '/' {
					c = "/" + c
				}
				r.URL.Path = c
			}
			next.ServeHTTP(w, r)
		})
	}
}

// staticHTMLPages maps clean URL paths (no extension) to HTML files in the web root.
var staticHTMLPages = map[string]string{
	"privacy": "privacy.html",
	"terms":   "terms.html",
	"support": "support.html",
	"help":    "help.html",
}

// newStaticHandler serves the Vite dist. HTML is served with http.ServeContent over an
// in-memory copy of index.html, not http.ServeFile, so the standard library never
// issues redirects that resolve to a scheme-relative "Location: //" in the address bar.
func newStaticHandler(webRoot string) http.Handler {
	abs, err := filepath.Abs(webRoot)
	if err != nil {
		abs = webRoot
	}
	indexPath := filepath.Join(abs, "index.html")
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			http.Error(w, "static ui unavailable", http.StatusServiceUnavailable)
		})
	}
	var modTime time.Time
	if st, err := os.Stat(indexPath); err == nil {
		modTime = st.ModTime()
	}

	serveIndex := func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "index.html", modTime, bytes.NewReader(indexData))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		up := path.Clean(r.URL.Path)
		if up == "" || up == "." {
			up = "/"
		} else if up[0] != '/' {
			up = "/" + up
		}
		rel := strings.TrimPrefix(up, "/")
		if rel == "" {
			serveIndex(w, r)
			return
		}
		if strings.HasPrefix(up, "/assets/") || strings.HasPrefix(up, "/static/") {
			serveUnderStaticRoot(w, r, abs, rel)
			return
		}
		if file, ok := staticHTMLPages[rel]; ok {
			serveUnderStaticRoot(w, r, abs, file)
			return
		}
		if path.Ext(rel) != "" {
			serveUnderStaticRoot(w, r, abs, rel)
			return
		}
		serveIndex(w, r)
	})
}

func serveUnderStaticRoot(w http.ResponseWriter, r *http.Request, absRoot, rel string) {
	d := http.Dir(absRoot)
	f, err := d.Open(rel)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	// *os.File from http.Dir is an io.ReadSeeker
	http.ServeContent(w, r, path.Base(rel), st.ModTime(), f)
}

func staticFileHandler(webRoot string) http.Handler {
	return newStaticHandler(webRoot)
}
