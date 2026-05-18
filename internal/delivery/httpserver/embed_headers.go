package httpserver

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// meetFrameAncestors lists parents allowed to embed this app (Google Meet side panel / main stage).
// 'self' is intentionally omitted: only meet.google.com and *.google.com may frame the app.
const meetFrameAncestors = "https://meet.google.com https://*.google.com"

// meetEmbedMiddleware sets CSP frame-ancestors for Google Meet and strips X-Frame-Options
// (DENY/SAMEORIGIN would block embedding). It also removes frame-src 'none' if a downstream
// handler emits one, since both directives would prevent Meet from embedding the app.
// Applied to HTTP and WebSocket upgrade responses; other security headers are preserved.
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
			next.ServeHTTP(&meetEmbedResponseWriter{ResponseWriter: w, csp: csp}, r)
		})
	}
}

// meetEmbedResponseWriter enforces Meet-embed headers even if a downstream handler
// re-sets X-Frame-Options or overwrites the Content-Security-Policy with a value
// containing frame-src 'none' (both would block embedding in Meet).
type meetEmbedResponseWriter struct {
	http.ResponseWriter
	csp string
}

func (w *meetEmbedResponseWriter) WriteHeader(code int) {
	w.enforce()
	w.ResponseWriter.WriteHeader(code)
}

func (w *meetEmbedResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	w.enforce()
	return h.Hijack()
}

// enforce removes X-Frame-Options and any downstream CSP that bans framing,
// then re-asserts the Meet-friendly frame-ancestors policy.
func (w *meetEmbedResponseWriter) enforce() {
	h := w.ResponseWriter.Header()
	h.Del("X-Frame-Options")
	if current := h.Get("Content-Security-Policy"); current != "" && cspBlocksFraming(current) {
		h.Set("Content-Security-Policy", w.csp)
	}
}

// cspBlocksFraming reports whether the given CSP header value would prevent the
// app from being embedded in a Meet iframe (frame-src 'none' or a frame-ancestors
// directive that omits Google Meet origins).
func cspBlocksFraming(csp string) bool {
	lower := strings.ToLower(csp)
	if strings.Contains(lower, "frame-src 'none'") {
		return true
	}
	if strings.Contains(lower, "frame-ancestors") &&
		!strings.Contains(lower, "https://meet.google.com") {
		return true
	}
	return false
}
