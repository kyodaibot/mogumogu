package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

// CSRF tokens (docs/api.md 2.4):
//
//	v1.<kind>.<issued_at>.<nonce>.<signature>
//
// kind is "a" (anonymous, before login) or "s" (bound to a session jti). Both
// kinds carry a fresh 32-byte random nonce and an HMAC signature, so a token
// can only come from this server; there is no fixed or guessable value.
const (
	csrfVersion   = "v1"
	kindAnonymous = "a"
	kindSession   = "s"
	csrfNonceLen  = 32
	// AnonymousCSRFTTL bounds how long a pre-login token is accepted.
	AnonymousCSRFTTL = 2 * time.Hour
)

var ErrCSRF = errors.New("csrf token rejected")

type CSRF struct {
	secret []byte
	now    func() time.Time
}

func NewCSRF(secret []byte) *CSRF {
	return &CSRF{secret: secret, now: time.Now}
}

// IssueAnonymous returns a new token for a caller without a session.
func (c *CSRF) IssueAnonymous() (string, error) {
	return c.issue(kindAnonymous, "")
}

// IssueForSession returns a new token bound to the session's jti.
func (c *CSRF) IssueForSession(jti string) (string, error) {
	if jti == "" {
		return "", errors.New("session token needs a jti")
	}
	return c.issue(kindSession, jti)
}

func (c *CSRF) issue(kind, binding string) (string, error) {
	nonce, err := randomToken(csrfNonceLen)
	if err != nil {
		return "", err
	}
	iat := strconv.FormatInt(c.now().Unix(), 10)
	return strings.Join([]string{csrfVersion, kind, iat, nonce, c.sign(kind, binding, iat, nonce)}, "."), nil
}

// Verify checks a token for the caller's current state. jti is the caller's
// session jti, or "" when the caller has no valid session:
//   - with a session, only a session token bound to that jti is accepted;
//   - without one, only an anonymous token younger than AnonymousCSRFTTL is.
func (c *CSRF) Verify(token, jti string) error {
	parts := strings.Split(token, ".")
	if len(parts) != 5 || parts[0] != csrfVersion {
		return ErrCSRF
	}
	kind, iat, nonce, sig := parts[1], parts[2], parts[3], parts[4]

	wantKind, binding := kindAnonymous, ""
	if jti != "" {
		wantKind, binding = kindSession, jti
	}
	if kind != wantKind {
		return ErrCSRF
	}
	if raw, err := base64.RawURLEncoding.DecodeString(nonce); err != nil || len(raw) != csrfNonceLen {
		return ErrCSRF
	}
	if !hmac.Equal([]byte(sig), []byte(c.sign(kind, binding, iat, nonce))) {
		return ErrCSRF
	}
	issued, err := strconv.ParseInt(iat, 10, 64)
	if err != nil {
		return ErrCSRF
	}
	age := c.now().Sub(time.Unix(issued, 0))
	if age < -time.Minute { // allow small clock skew, reject tokens from the future
		return ErrCSRF
	}
	if kind == kindAnonymous && age > AnonymousCSRFTTL {
		return ErrCSRF
	}
	return nil
}

func (c *CSRF) sign(kind, binding, iat, nonce string) string {
	mac := hmac.New(sha256.New, c.secret)
	mac.Write([]byte("mogumogu-csrf|" + csrfVersion + "|" + kind + "|" + binding + "|" + iat + "|" + nonce))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
