package auth

import (
	"context"
	"net/http"
)

type ctxKey int

const userCtxKey ctxKey = 1

// UserFromContext returns the authenticated profile placed by RequireAuth.
func UserFromContext(ctx context.Context) (Profile, bool) {
	p, ok := ctx.Value(userCtxKey).(Profile)
	return p, ok
}

// MustUser returns the profile or panics if middleware misconfigured (should not happen on protected routes).
func MustUser(ctx context.Context) Profile {
	p, ok := UserFromContext(ctx)
	if !ok {
		panic("auth: no user in context")
	}
	return p
}

func withUser(ctx context.Context, p Profile) context.Context {
	return context.WithValue(ctx, userCtxKey, p)
}

// writeSessionCookie sets the HttpOnly session JWT.
func writeSessionCookie(w http.ResponseWriter, token string, maxAge int, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSessionCookie removes the session cookie.
func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// readSessionCookie returns the raw JWT from the request.
func readSessionCookie(r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return c.Value
}
