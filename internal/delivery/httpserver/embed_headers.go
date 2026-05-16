package httpserver

import (
	"net/http"
	"strings"
)

// defaultMeetFrameAncestors allows embedding in Google Meet and related Workspace hosts.
const defaultMeetFrameAncestors = "'self' https://meet.google.com https://*.google.com https://accounts.google.com"

// meetEmbedMiddleware sets CSP frame-ancestors for Google Meet iframes and does not send
// X-Frame-Options (which would block embedding when set to DENY or SAMEORIGIN).
func meetEmbedMiddleware(extraFrameAncestors string) func(http.Handler) http.Handler {
	ancestors := defaultMeetFrameAncestors
	if v := strings.TrimSpace(extraFrameAncestors); v != "" {
		ancestors = ancestors + " " + v
	}
	csp := "frame-ancestors " + ancestors + ";"

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Del("X-Frame-Options")
			w.Header().Set("Content-Security-Policy", csp)
			next.ServeHTTP(w, r)
		})
	}
}
