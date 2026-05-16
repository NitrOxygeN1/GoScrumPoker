package auth

import "net/http"

// CookieSameSite picks a SameSite mode for auth cookies. Meet iframe embedding needs
// SameSite=None with Secure cookies so sessions persist when the app is framed on meet.google.com.
func CookieSameSite(secure, meetEmbed bool) http.SameSite {
	if secure && meetEmbed {
		return http.SameSiteNoneMode
	}
	return http.SameSiteLaxMode
}
