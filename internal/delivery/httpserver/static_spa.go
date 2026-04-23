package httpserver

import (
	"net/http"
	"path"
	"strings"
)

// distFS serves files from a Vite `dist` directory. Unknown paths without a file
// extension (client-side routes) fall back to index.html. Missing /assets/* and
// other static files (e.g. favicon) return 404 from the file server.
type distFS struct{ root string }

func (d distFS) Open(name string) (http.File, error) {
	fs := http.Dir(d.root)
	if name == "" || name == "." || name == "/" {
		return fs.Open("index.html")
	}
	f, err := fs.Open(name)
	if err == nil {
		st, serr := f.Stat()
		if serr == nil && st.IsDir() {
			_ = f.Close()
			return fs.Open("index.html")
		}
		return f, nil
	}
	if name == "index.html" {
		return nil, err
	}
	ext := path.Ext(name)
	if ext != "" && !strings.EqualFold(ext, ".html") {
		return nil, err
	}
	if strings.HasPrefix(name, "assets/") {
		return nil, err
	}
	return fs.Open("index.html")
}

func staticFileHandler(webRoot string) http.Handler {
	return http.FileServer(distFS{root: webRoot})
}
