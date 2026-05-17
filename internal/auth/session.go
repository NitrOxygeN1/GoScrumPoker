package auth

import (
	"net/http"
	"time"

	"github.com/gorilla/sessions"
)

const sessionName = "session"

// SessionManager signs and verifies the session cookie via gorilla/sessions.
type SessionManager struct {
	store *sessions.CookieStore
}

// NewSessionManager creates a cookie session store. secret must be non-empty.
// For Meet iframe embed, pass sameSite=http.SameSiteNoneMode and secure=true.
func NewSessionManager(secret string, maxAge time.Duration, secure bool, sameSite http.SameSite) *SessionManager {
	if sameSite == 0 {
		sameSite = http.SameSiteLaxMode
	}
	store := sessions.NewCookieStore([]byte(secret))
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   int(maxAge.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
	}
	return &SessionManager{store: store}
}

func (m *SessionManager) applyOptions(sess *sessions.Session) {
	if m.store.Options != nil {
		sess.Options = m.store.Options
	}
}

// Get returns the session for the request (may be new/empty).
func (m *SessionManager) Get(r *http.Request) (*sessions.Session, error) {
	return m.store.Get(r, sessionName)
}

// SaveUser writes the authenticated user into the session cookie.
func (m *SessionManager) SaveUser(w http.ResponseWriter, r *http.Request, p Profile) error {
	sess, err := m.Get(r)
	if err != nil {
		return err
	}
	m.applyOptions(sess)
	sess.Values["google_id"] = p.GoogleID
	sess.Values["email"] = p.Email
	sess.Values["display_name"] = p.DisplayName
	sess.Values["avatar"] = p.Avatar
	return sess.Save(r, w)
}

// Clear removes the session cookie.
func (m *SessionManager) Clear(w http.ResponseWriter, r *http.Request) error {
	sess, err := m.Get(r)
	if err != nil {
		return err
	}
	m.applyOptions(sess)
	sess.Options.MaxAge = -1
	for k := range sess.Values {
		delete(sess.Values, k)
	}
	return sess.Save(r, w)
}

// ProfileFromRequest loads the user from the session (and profile store when available).
func (m *SessionManager) ProfileFromRequest(r *http.Request, profiles *ProfileStore) (Profile, bool) {
	sess, err := m.Get(r)
	if err != nil || sess == nil {
		return Profile{}, false
	}
	googleID, _ := sess.Values["google_id"].(string)
	if googleID == "" {
		return Profile{}, false
	}
	if profiles != nil {
		if p, ok := profiles.Get(googleID); ok {
			return p, true
		}
	}
	email, _ := sess.Values["email"].(string)
	displayName, _ := sess.Values["display_name"].(string)
	avatar, _ := sess.Values["avatar"].(string)
	return Profile{
		GoogleID:    googleID,
		Email:       email,
		DisplayName: displayName,
		Avatar:      avatar,
	}, true
}
