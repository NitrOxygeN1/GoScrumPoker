package auth

import "net/http"

// MeetCookieOptions returns session/auth cookie flags for Google Meet iframe embedding.
// Cross-site iframe cookies require SameSite=None and Secure.
func MeetCookieOptions(meetEmbed, secure bool) (sameSite http.SameSite, cookieSecure bool) {
	if meetEmbed {
		return http.SameSiteNoneMode, true
	}
	return http.SameSiteLaxMode, secure
}

// CookieSameSite is kept for callers that only need the SameSite value.
func CookieSameSite(secure, meetEmbed bool) http.SameSite {
	sameSite, _ := MeetCookieOptions(meetEmbed, secure)
	return sameSite
}
