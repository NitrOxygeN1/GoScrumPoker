package auth

import "context"

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
