package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const oauthStateCookie = "oauth_state"

// Service wires Google OAuth2, JWT sessions, and profile storage.
type Service struct {
	oauth      *oauth2.Config
	jwt        *TokenIssuer
	profiles   *ProfileStore
	log        zerolog.Logger
	secure     bool
	postLogin  string
	enabled    bool
}

// NewService returns nil if OAuth/JWT is not fully configured (endpoints stay public-only).
func NewService(log zerolog.Logger, profiles *ProfileStore, clientID, clientSecret, redirectURL, jwtSecret string, jwtTTL time.Duration, postLogin string, cookieSecure bool) *Service {
	if clientID == "" || clientSecret == "" || redirectURL == "" || jwtSecret == "" {
		return nil
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
		jwt:       NewTokenIssuer(jwtSecret, jwtTTL),
		profiles:  profiles,
		log:       log,
		secure:    cookieSecure,
		postLogin: postLogin,
		enabled:   true,
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
	r.Get("/auth/google", s.handleGoogleLogin)
	r.Get("/auth/google/callback", s.handleGoogleCallback)
	r.Post("/auth/logout", s.handleLogout)
	r.With(s.RequireAuth).Get("/api/me", s.handleMe)
}

// RequireAuth ensures a valid session JWT and attaches Profile to the request context.
func (s *Service) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.Enabled() {
			http.Error(w, `{"error":"authentication not configured"}`, http.StatusServiceUnavailable)
			return
		}
		raw := readSessionCookie(r)
		if raw == "" {
			writeJSONAuthError(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		claims, err := s.jwt.Parse(raw)
		if err != nil {
			writeJSONAuthError(w, http.StatusUnauthorized, "invalid session")
			return
		}
		sub := claims.Subject
		prof, ok := s.profiles.Get(sub)
		if !ok {
			prof = Profile{ID: sub, Name: claims.Name, Avatar: claims.Picture}
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
		SameSite: http.SameSiteLaxMode,
	})
	url := s.oauth.AuthCodeURL(state, oauth2.AccessTypeOffline)
	http.Redirect(w, r, url, http.StatusFound)
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
	clearOAuthStateCookie(w, s.secure)

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
		ID:     googleID,
		Name:   gu.Name,
		Avatar: gu.Picture,
		Email:  gu.Email,
	}
	s.profiles.Upsert(prof)

	jwtStr, err := s.jwt.Mint(prof.ID, prof.Name, prof.Avatar)
	if err != nil {
		s.log.Error().Err(err).Msg("jwt mint failed")
		http.Redirect(w, r, s.postLogin+"?login=error", http.StatusFound)
		return
	}

	maxAge := int(s.jwt.TTL() / time.Second)
	if maxAge < 60 {
		maxAge = 3600
	}
	writeSessionCookie(w, jwtStr, maxAge, s.secure)
	http.Redirect(w, r, s.postLogin+"?login=ok", http.StatusFound)
}

func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	clearSessionCookie(w, s.secure)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (s *Service) handleMe(w http.ResponseWriter, r *http.Request) {
	p := MustUser(r.Context())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(p)
}

func clearOAuthStateCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
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
