package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMeetEmbedMiddleware_headers(t *testing.T) {
	h := meetEmbedMiddleware("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Frame-Options"); got != "" {
		t.Fatalf("X-Frame-Options: got %q want empty", got)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors") {
		t.Fatalf("CSP missing frame-ancestors: %q", csp)
	}
	if !strings.Contains(csp, "https://meet.google.com") {
		t.Fatalf("CSP missing meet.google.com: %q", csp)
	}
	if !strings.Contains(csp, "https://*.google.com") {
		t.Fatalf("CSP missing *.google.com: %q", csp)
	}
}

func TestMeetEmbedMiddleware_stripsDownstreamXFrameOptions(t *testing.T) {
	h := meetEmbedMiddleware("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Frame-Options"); got != "" {
		t.Fatalf("X-Frame-Options: got %q want empty", got)
	}
}

func TestMeetEmbedMiddleware_extraAncestors(t *testing.T) {
	h := meetEmbedMiddleware("https://addon.example.com")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "https://addon.example.com") {
		t.Fatalf("CSP missing extra ancestor: %q", csp)
	}
}
