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
	want := "frame-ancestors 'self' https://meet.google.com https://*.google.com;"
	if csp != want {
		t.Fatalf("CSP: got %q want %q", csp, want)
	}
	if strings.Contains(csp, "frame-src") {
		t.Fatalf("CSP must not include frame-src: %q", csp)
	}
}

func TestMeetEmbedMiddleware_stripsDownstreamXFrameOptionsDeny(t *testing.T) {
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

func TestMeetEmbedMiddleware_stripsDownstreamXFrameOptionsSameOrigin(t *testing.T) {
	h := meetEmbedMiddleware("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Frame-Options"); got != "" {
		t.Fatalf("X-Frame-Options: got %q want empty", got)
	}
}

func TestMeetEmbedMiddleware_overridesDownstreamFrameSrcNone(t *testing.T) {
	h := meetEmbedMiddleware("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-src 'none'")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	if strings.Contains(csp, "frame-src 'none'") {
		t.Fatalf("CSP must not contain frame-src 'none': %q", csp)
	}
	if !strings.Contains(csp, "https://meet.google.com") || !strings.Contains(csp, "https://*.google.com") {
		t.Fatalf("CSP missing Meet frame-ancestors after override: %q", csp)
	}
}

func TestMeetEmbedMiddleware_allowsSameOriginIframeTest(t *testing.T) {
	h := meetEmbedMiddleware("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "'self'") {
		t.Fatalf("CSP missing 'self' (same-origin iframe test page would be blocked): %q", csp)
	}
}

func TestMeetEmbedMiddleware_preservesUnrelatedSecurityHeaders(t *testing.T) {
	h := meetEmbedMiddleware("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	for _, k := range []string{"Strict-Transport-Security", "Referrer-Policy", "X-Content-Type-Options"} {
		if rec.Header().Get(k) == "" {
			t.Fatalf("middleware dropped %s", k)
		}
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
