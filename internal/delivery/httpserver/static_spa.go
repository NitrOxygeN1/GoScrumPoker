package httpserver

import (
	"bytes"
	"html"
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
	"privacy":          "privacy.html",
	"terms":            "terms.html",
	"support":          "support.html",
	"help":             "help.html",
	"draft-opt-out":    "draft-opt-out.html",
	"meet-debug":       "meet-debug.html",
	"meet-iframe-test": "meet-iframe-test.html",
}

// newStaticHandler serves the Vite dist. HTML is served with http.ServeContent over an
// in-memory copy of index.html, not http.ServeFile, so the standard library never
// issues redirects that resolve to a scheme-relative "Location: //" in the address bar.
//
// meetCloudProjectNumber, when non-empty, is injected as
// <meta name="gsp-cloud-project-number" content="..."> into the served index.html so the
// SPA can initialize the Meet Web Add-ons SDK with no rebuild.
func newStaticHandler(webRoot, meetCloudProjectNumber string) http.Handler {
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
	indexData = injectRuntimeConfig(indexData, meetCloudProjectNumber)

	// Use server-startup time so a process restart (e.g. after changing
	// GOOGLE_CLOUD_PROJECT_NUMBER on Render) invalidates browser caches of index.html.
	modTime := time.Now()

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

func staticFileHandler(webRoot, meetCloudProjectNumber string) http.Handler {
	return newStaticHandler(webRoot, meetCloudProjectNumber)
}

// injectRuntimeConfig inserts a runtime <meta> tag into <head> so the SPA can read
// configuration that depends on deploy-time env vars without a frontend rebuild.
// Currently emits gsp-cloud-project-number for the Meet Web Add-ons SDK.
func injectRuntimeConfig(indexData []byte, meetCloudProjectNumber string) []byte {
	pn := strings.TrimSpace(meetCloudProjectNumber)
	if pn == "" {
		return indexData
	}
	meta := []byte(`<meta name="gsp-cloud-project-number" content="` + html.EscapeString(pn) + `">`)
	idx := bytes.Index(indexData, []byte("</head>"))
	if idx < 0 {
		return indexData
	}
	out := make([]byte, 0, len(indexData)+len(meta))
	out = append(out, indexData[:idx]...)
	out = append(out, meta...)
	out = append(out, indexData[idx:]...)
	return out
}
