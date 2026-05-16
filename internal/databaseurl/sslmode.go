package databaseurl

import (
	"net/url"
	"regexp"
	"strings"
)

var sslmodeKV = regexp.MustCompile(`(?i)\bsslmode\s*=\s*('[^']*'|\S+)`)

// RequireSSL returns databaseURL with sslmode set to require (libpq URL or keyword/value form).
func RequireSSL(databaseURL string) string {
	s := strings.TrimSpace(databaseURL)
	if s == "" {
		return s
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://") {
		u, err := url.Parse(s)
		if err != nil {
			return s
		}
		q := u.Query()
		q.Set("sslmode", "require")
		u.RawQuery = q.Encode()
		return u.String()
	}
	if sslmodeKV.MatchString(s) {
		return sslmodeKV.ReplaceAllString(s, "sslmode=require")
	}
	return s + " sslmode=require"
}
