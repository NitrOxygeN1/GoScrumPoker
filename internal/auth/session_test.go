package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Touch on a fresh request (no session cookie) must not set a cookie — we
// never want to create an empty session just because Touch was called.
func TestSessionManager_Touch_noCookie_noop(t *testing.T) {
	m := NewSessionManager("test-secret-test-secret-test-secret-123", time.Hour, false, http.SameSiteLaxMode)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	if err := m.Touch(w, r); err != nil {
		t.Fatalf("Touch error: %v", err)
	}
	if got := w.Header().Get("Set-Cookie"); got != "" {
		t.Fatalf("Touch must not create a session; got Set-Cookie=%q", got)
	}
}

// Touch on an existing authenticated session must re-emit the session cookie
// with the configured Max-Age so the expiration slides forward.
func TestSessionManager_Touch_existingSession_refreshes(t *testing.T) {
	m := NewSessionManager("test-secret-test-secret-test-secret-123", 7*24*time.Hour, false, http.SameSiteLaxMode)

	// Step 1: create a session by signing in a profile.
	saveReq := httptest.NewRequest(http.MethodGet, "/", nil)
	saveResp := httptest.NewRecorder()
	if err := m.SaveUser(saveResp, saveReq, Profile{GoogleID: "g-1", DisplayName: "Jane", Email: "j@example.com"}); err != nil {
		t.Fatalf("SaveUser: %v", err)
	}
	setCookie := saveResp.Header().Get("Set-Cookie")
	if setCookie == "" {
		t.Fatal("SaveUser did not emit a session cookie")
	}

	// Step 2: replay that cookie on a new request and Touch it.
	touchReq := httptest.NewRequest(http.MethodGet, "/", nil)
	touchReq.Header.Set("Cookie", cookieHeaderValue(setCookie))
	touchResp := httptest.NewRecorder()
	if err := m.Touch(touchResp, touchReq); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	refreshed := touchResp.Header().Get("Set-Cookie")
	if refreshed == "" {
		t.Fatal("Touch must re-emit the session cookie to slide expiration")
	}
	if !strings.Contains(refreshed, "Max-Age=") {
		t.Fatalf("refreshed cookie missing Max-Age: %q", refreshed)
	}
}

// cookieHeaderValue strips the cookie attributes (Path/Max-Age/...) from a
// Set-Cookie value and returns just "name=value" for use in a Cookie header.
func cookieHeaderValue(setCookie string) string {
	if i := strings.Index(setCookie, ";"); i >= 0 {
		return setCookie[:i]
	}
	return setCookie
}
