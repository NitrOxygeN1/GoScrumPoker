package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const oauthStateCookie = "oauth_state"

// Service wires Google OAuth2, gorilla session cookies, and profile storage.
type Service struct {
	oauth     *oauth2.Config
	sessions  *SessionManager
	profiles  *ProfileStore
	log       zerolog.Logger
	secure       bool
	cookieSameSite http.SameSite
	postLogin    string
	enabled      bool
}

// NewService returns nil if OAuth/session secret is not fully configured (endpoints stay public-only).
func NewService(
	log zerolog.Logger,
	profiles *ProfileStore,
	clientID, clientSecret, redirectURL, sessionSecret string,
	sessionTTL time.Duration,
	postLogin string,
	cookieSecure bool,
	cookieSameSite http.SameSite,
) *Service {
	if clientID == "" || clientSecret == "" || redirectURL == "" || sessionSecret == "" {
		return nil
	}
	if sessionTTL <= 0 {
		sessionTTL = 7 * 24 * time.Hour
	}
	return &Service{
		oauth: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Scopes: []string{
				"openid",
				"https://www.googleapis.com/auth/userinfo.email",
				"https://www.googleapis.com/auth/userinfo.profile",
			},
			Endpoint: google.Endpoint,
		},
		sessions:       NewSessionManager(sessionSecret, sessionTTL, cookieSecure, cookieSameSite),
		profiles:       profiles,
		log:            log,
		secure:         cookieSecure,
		cookieSameSite: cookieSameSite,
		postLogin:      postLogin,
		enabled:        true,
	}
}

// Enabled reports whether Google login is active.
func (s *Service) Enabled() bool {
	return s != nil && s.enabled
}

// Register mounts auth routes on the router.
func (s *Service) Register(r chi.Router) {
	if !s.Enabled() {
		return
	}
	r.Get("/auth/google/login", s.handleGoogleLogin)
	r.Get("/auth/google/callback", s.handleGoogleCallback)
	r.Get("/logout", s.handleLogout)
	r.Post("/logout", s.handleLogout)
	r.With(s.RequireAuth).Get("/api/me", s.handleMe)
}

// RequireAuth ensures a valid session and attaches Profile to the request context.
func (s *Service) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.Enabled() {
			http.Error(w, `{"error":"authentication not configured"}`, http.StatusServiceUnavailable)
			return
		}
		prof, ok := s.sessions.ProfileFromRequest(r, s.profiles)
		if !ok {
			writeJSONAuthError(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), prof)))
	})
}

func (s *Service) handleGoogleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := randomState()
	if err != nil {
		s.log.Error().Err(err).Msg("oauth state generation failed")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    state,
		Path:     "/",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: s.cookieSameSite,
	})

	// Always ask Google to show its account chooser. Without this the popup
	// silently auto-selects whichever account the browser most recently used,
	// which makes "log out and log back in" feel like a no-op for users with
	// a single signed-in Google account.
	opts := []oauth2.AuthCodeOption{
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "select_account"),
	}

	authURL := s.oauth.AuthCodeURL(state, opts...)
	if isEmbeddedRequest(r) {
		writeTopLevelRedirect(w, authURL)
		return
	}
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (s *Service) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		s.log.Info().Str("oauth_error", errParam).Msg("oauth callback error param")
		http.Redirect(w, r, s.postLogin+"?login=error", http.StatusFound)
		return
	}

	stateCookie, err := r.Cookie(oauthStateCookie)
	if err != nil || stateCookie.Value == "" || r.URL.Query().Get("state") != stateCookie.Value {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
	s.clearOAuthStateCookie(w)

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	tok, err := s.oauth.Exchange(ctx, code)
	if err != nil {
		s.log.Error().Err(err).Msg("oauth token exchange failed")
		http.Redirect(w, r, s.postLogin+"?login=error", http.StatusFound)
		return
	}

	client := s.oauth.Client(ctx, tok)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		s.log.Error().Err(err).Msg("google userinfo request failed")
		http.Redirect(w, r, s.postLogin+"?login=error", http.StatusFound)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		s.log.Error().Int("status", resp.StatusCode).Msg("google userinfo bad status")
		http.Redirect(w, r, s.postLogin+"?login=error", http.StatusFound)
		return
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		s.log.Error().Err(err).Msg("google userinfo read failed")
		http.Redirect(w, r, s.postLogin+"?login=error", http.StatusFound)
		return
	}

	var gu struct {
		ID      string `json:"id"`
		Sub     string `json:"sub"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := json.Unmarshal(body, &gu); err != nil {
		s.log.Error().Err(err).Msg("google userinfo json decode failed")
		http.Redirect(w, r, s.postLogin+"?login=error", http.StatusFound)
		return
	}
	googleID := gu.Sub
	if googleID == "" {
		googleID = gu.ID
	}
	if googleID == "" {
		s.log.Error().Msg("google userinfo missing subject/id")
		http.Redirect(w, r, s.postLogin+"?login=error", http.StatusFound)
		return
	}

	prof := Profile{
		GoogleID:    googleID,
		DisplayName: gu.Name,
		Avatar:      gu.Picture,
		Email:       gu.Email,
	}
	s.profiles.Upsert(prof)

	if err := s.sessions.SaveUser(w, r, prof); err != nil {
		s.log.Error().Err(err).Msg("session save failed")
		http.Redirect(w, r, s.postLogin+"?login=error", http.StatusFound)
		return
	}
	http.Redirect(w, r, s.postLogin+buildLoginOkQuery(prof), http.StatusFound)
}

// buildLoginOkQuery appends ?login=ok plus the freshly authenticated profile
// fields so the OAuth popup can hand off the profile to its opener via
// postMessage without depending on /api/me. The Meet iframe is a cross-site
// embedding context where Chrome's third-party-cookie restrictions often
// prevent the popup's session cookie from being read by /api/me even though
// sign-in actually succeeded; putting the data on the URL bypasses that
// entirely. The values are the user's own profile, delivered back to their
// own browser over HTTPS, so URL-history exposure is acceptable.
func buildLoginOkQuery(p Profile) string {
	v := url.Values{}
	v.Set("login", "ok")
	if p.DisplayName != "" {
		v.Set("name", p.DisplayName)
	}
	if p.Avatar != "" {
		v.Set("avatar", p.Avatar)
	}
	if p.Email != "" {
		v.Set("email", p.Email)
	}
	return "?" + v.Encode()
}

func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.sessions.Clear(w, r); err != nil {
		s.log.Error().Err(err).Msg("session clear failed")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if r.Method == http.MethodGet {
		http.Redirect(w, r, s.postLogin, http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (s *Service) handleMe(w http.ResponseWriter, r *http.Request) {
	p := MustUser(r.Context())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(p)
}

func (s *Service) clearOAuthStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: s.cookieSameSite,
	})
}

func isEmbeddedRequest(r *http.Request) bool {
	if r.URL.Query().Get("embedded") == "1" {
		return true
	}
	switch r.Header.Get("Sec-Fetch-Dest") {
	case "iframe", "embed":
		return true
	default:
		return false
	}
}

// writeTopLevelRedirect breaks out of an iframe so Google OAuth runs at top level (no popups).
func writeTopLevelRedirect(w http.ResponseWriter, url string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	escaped, _ := json.Marshal(url)
	_, _ = w.Write([]byte(`<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"><title>Signing in…</title></head><body><script>window.top.location.replace(` + string(escaped) + `);</script><p>Redirecting to Google sign-in…</p></body></html>`))
}

func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func writeJSONAuthError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
