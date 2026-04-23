package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const cookieName = "session"

var ErrInvalidToken = errors.New("invalid or expired token")

// TokenClaims is the signed session JWT payload.
type TokenClaims struct {
	Name    string `json:"name"`
	Picture string `json:"picture,omitempty"`
	jwt.RegisteredClaims
}

// TokenIssuer signs and verifies HMAC session tokens.
type TokenIssuer struct {
	secret []byte
	ttl    time.Duration
	issuer string
}

// NewTokenIssuer constructs an issuer. secret must be non-empty for production use.
func NewTokenIssuer(secret string, ttl time.Duration) *TokenIssuer {
	return &TokenIssuer{
		secret: []byte(secret),
		ttl:    ttl,
		issuer: "GoScrumPoker",
	}
}

// TTL returns token lifetime.
func (t *TokenIssuer) TTL() time.Duration {
	return t.ttl
}

// Mint creates a JWT for the given Google subject and display fields.
func (t *TokenIssuer) Mint(sub, name, picture string) (string, error) {
	now := time.Now()
	claims := TokenClaims{
		Name:    name,
		Picture: picture,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    t.issuer,
			Subject:   sub,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(t.ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(t.secret)
}

// Parse validates a compact JWT string and returns claims.
func (t *TokenIssuer) Parse(tokenString string) (*TokenClaims, error) {
	tok, err := jwt.ParseWithClaims(tokenString, &TokenClaims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrInvalidToken
		}
		return t.secret, nil
	})
	if err != nil || !tok.Valid {
		return nil, ErrInvalidToken
	}
	claims, ok := tok.Claims.(*TokenClaims)
	if !ok {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
