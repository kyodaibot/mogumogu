package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Session is what a verified session token says about the caller.
type Session struct {
	UserID    int64
	JTI       string
	ExpiresAt time.Time
}

// Sessions issues and verifies HS256 session JWTs.
type Sessions struct {
	secret   []byte
	issuer   string
	audience string
	ttl      time.Duration
	now      func() time.Time
}

func NewSessions(secret []byte, issuer, audience string, ttl time.Duration) *Sessions {
	return &Sessions{secret: secret, issuer: issuer, audience: audience, ttl: ttl, now: time.Now}
}

// TTL is how long issued tokens (and the session cookie) stay valid.
func (s *Sessions) TTL() time.Duration { return s.ttl }

// Issue creates a token for userID with a fresh random jti.
func (s *Sessions) Issue(userID int64) (string, Session, error) {
	jti, err := randomToken(16)
	if err != nil {
		return "", Session{}, err
	}
	now := s.now().Truncate(time.Second)
	sess := Session{UserID: userID, JTI: jti, ExpiresAt: now.Add(s.ttl)}
	claims := jwt.RegisteredClaims{
		Subject:   strconv.FormatInt(userID, 10),
		Issuer:    s.issuer,
		Audience:  jwt.ClaimStrings{s.audience},
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(sess.ExpiresAt),
		ID:        jti,
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		return "", Session{}, err
	}
	return token, sess, nil
}

// ErrInvalidSession covers every reason a token is rejected; callers do not
// need to tell them apart, and responses must not.
var ErrInvalidSession = errors.New("invalid session token")

// Verify checks the signature, algorithm, exp, iss, aud and the presence of
// sub and jti. Revocation is checked separately against the database.
func (s *Sessions) Verify(token string) (Session, error) {
	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(token, &claims,
		func(*jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(s.issuer),
		jwt.WithAudience(s.audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithTimeFunc(s.now),
	)
	if err != nil || claims.ID == "" || claims.ExpiresAt == nil {
		return Session{}, ErrInvalidSession
	}
	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || userID <= 0 {
		return Session{}, ErrInvalidSession
	}
	return Session{UserID: userID, JTI: claims.ID, ExpiresAt: claims.ExpiresAt.Time}, nil
}

// randomToken returns n random bytes from crypto/rand, base64url encoded.
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
