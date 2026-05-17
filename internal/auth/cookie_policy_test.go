package auth

import (
	"net/http"
	"testing"
)

func TestMeetCookieOptions_embed(t *testing.T) {
	sameSite, secure := MeetCookieOptions(true, false)
	if sameSite != http.SameSiteNoneMode {
		t.Fatalf("SameSite: got %v want None", sameSite)
	}
	if !secure {
		t.Fatal("Secure must be true when Meet embed is enabled")
	}
}

func TestMeetCookieOptions_standalone(t *testing.T) {
	sameSite, secure := MeetCookieOptions(false, true)
	if sameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite: got %v want Lax", sameSite)
	}
	if !secure {
		t.Fatal("Secure should follow config")
	}
}
