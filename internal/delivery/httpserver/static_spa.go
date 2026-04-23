package httpserver

import (
	"net/http"
	"path"
	"path/filepath"
	"strings"
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

// newStaticHandler serves the Vite dist with http.ServeFile / http.ServeContent
// only, never http.FileServer (avoids 301 to Location: // for directory "/" ).
func newStaticHandler(webRoot string) http.Handler {
	abs, err := filepath.Abs(webRoot)
	if err != nil {
		abs = webRoot
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
			http.ServeFile(w, r, filepath.Join(abs, "index.html"))
			return
		}
		if strings.HasPrefix(up, "/assets/") {
			serveUnderStaticRoot(w, r, abs, rel)
			return
		}
		if path.Ext(rel) != "" {
			serveUnderStaticRoot(w, r, abs, rel)
			return
		}
		http.ServeFile(w, r, filepath.Join(abs, "index.html"))
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
