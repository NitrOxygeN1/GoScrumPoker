package httpserver

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// meetFrameAncestors lists parents allowed to embed this app (Google Meet side panel / main stage).
const meetFrameAncestors = "'self' https://meet.google.com https://*.google.com"

// meetEmbedMiddleware sets CSP frame-ancestors for Google Meet and strips X-Frame-Options
// (DENY/SAMEORIGIN would block embedding). Applied to HTTP and WebSocket upgrade responses.
func meetEmbedMiddleware(extraFrameAncestors string) func(http.Handler) http.Handler {
	ancestors := meetFrameAncestors
	if v := strings.TrimSpace(extraFrameAncestors); v != "" {
		ancestors = ancestors + " " + v
	}
	csp := "frame-ancestors " + ancestors + ";"

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Del("X-Frame-Options")
			w.Header().Set("Content-Security-Policy", csp)
			next.ServeHTTP(&meetEmbedResponseWriter{ResponseWriter: w}, r)
		})
	}
}

// meetEmbedResponseWriter removes X-Frame-Options if a downstream handler sets it.
type meetEmbedResponseWriter struct {
	http.ResponseWriter
}

func (w *meetEmbedResponseWriter) WriteHeader(code int) {
	w.ResponseWriter.Header().Del("X-Frame-Options")
	w.ResponseWriter.WriteHeader(code)
}

func (w *meetEmbedResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	w.ResponseWriter.Header().Del("X-Frame-Options")
	return h.Hijack()
}
