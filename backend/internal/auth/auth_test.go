package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	jwtSecret  = []byte("test-jwt-secret-placeholder-0123456789")
	csrfSecret = []byte("test-csrf-secret-placeholder-0123456789")
	baseTime   = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
)

func TestPasswordHashAndVerify(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("unexpected hash format: %s", h)
	}
	if strings.Contains(h, "correct horse") {
		t.Fatal("hash contains the password")
	}
	if ok, err := VerifyPassword("correct horse battery", h); err != nil || !ok {
		t.Fatalf("correct password rejected: ok=%v err=%v", ok, err)
	}
	if ok, _ := VerifyPassword("wrong", h); ok {
		t.Fatal("wrong password accepted")
	}
	if h2, _ := HashPassword("correct horse battery"); h2 == h {
		t.Fatal("two hashes of the same password share a salt")
	}
	if _, err := VerifyPassword("x", "$2a$10$notargon"); err == nil {
		t.Fatal("non-argon2id hash should be an error")
	}
}

func newSessions(now time.Time) *Sessions {
	s := NewSessions(jwtSecret, "mogumogu", "mogumogu-web", time.Hour)
	s.now = func() time.Time { return now }
	return s
}

func TestSessionRoundTrip(t *testing.T) {
	s := newSessions(baseTime)
	token, issued, err := s.Issue(42)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.UserID != 42 || got.JTI == "" || got.JTI != issued.JTI || !got.ExpiresAt.Equal(baseTime.Add(time.Hour)) {
		t.Fatalf("got %+v, issued %+v", got, issued)
	}
	_, other, _ := s.Issue(42)
	if other.JTI == issued.JTI {
		t.Fatal("jti is not unique")
	}
}

func TestSessionRejects(t *testing.T) {
	s := newSessions(baseTime)
	valid, _, _ := s.Issue(42)

	sign := func(claims jwt.RegisteredClaims, method jwt.SigningMethod, key any) string {
		tok, err := jwt.NewWithClaims(method, claims).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	good := jwt.RegisteredClaims{
		Subject: "42", Issuer: "mogumogu", Audience: jwt.ClaimStrings{"mogumogu-web"},
		IssuedAt: jwt.NewNumericDate(baseTime), ExpiresAt: jwt.NewNumericDate(baseTime.Add(time.Hour)), ID: "jti-1",
	}
	with := func(f func(c *jwt.RegisteredClaims)) jwt.RegisteredClaims { c := good; f(&c); return c }

	if _, err := s.Verify(sign(good, jwt.SigningMethodHS256, jwtSecret)); err != nil {
		t.Fatalf("baseline hand-made token rejected: %v", err)
	}

	tests := map[string]string{
		"tampered signature": valid[:len(valid)-2] + "xx",
		"tampered payload":   strings.Replace(valid, ".", ".e30", 1),
		"other secret":       sign(good, jwt.SigningMethodHS256, []byte("another-secret-0123456789-0123456789")),
		"alg none":           sign(good, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType),
		"expired":            sign(with(func(c *jwt.RegisteredClaims) { c.ExpiresAt = jwt.NewNumericDate(baseTime.Add(-time.Second)) }), jwt.SigningMethodHS256, jwtSecret),
		"no exp":             sign(with(func(c *jwt.RegisteredClaims) { c.ExpiresAt = nil }), jwt.SigningMethodHS256, jwtSecret),
		"wrong issuer":       sign(with(func(c *jwt.RegisteredClaims) { c.Issuer = "someone-else" }), jwt.SigningMethodHS256, jwtSecret),
		"wrong audience":     sign(with(func(c *jwt.RegisteredClaims) { c.Audience = jwt.ClaimStrings{"other"} }), jwt.SigningMethodHS256, jwtSecret),
		"missing jti":        sign(with(func(c *jwt.RegisteredClaims) { c.ID = "" }), jwt.SigningMethodHS256, jwtSecret),
		"missing sub":        sign(with(func(c *jwt.RegisteredClaims) { c.Subject = "" }), jwt.SigningMethodHS256, jwtSecret),
		"non-numeric sub":    sign(with(func(c *jwt.RegisteredClaims) { c.Subject = "admin" }), jwt.SigningMethodHS256, jwtSecret),
		"issued in future":   sign(with(func(c *jwt.RegisteredClaims) { c.IssuedAt = jwt.NewNumericDate(baseTime.Add(time.Hour)) }), jwt.SigningMethodHS256, jwtSecret),
		"garbage":            "not-a-jwt",
	}
	for name, token := range tests {
		if _, err := s.Verify(token); err == nil {
			t.Errorf("%s: token accepted", name)
		}
	}
}

func newCSRF(now *time.Time) *CSRF {
	c := NewCSRF(csrfSecret)
	c.now = func() time.Time { return *now }
	return c
}

func TestCSRFAnonymousTokens(t *testing.T) {
	now := baseTime
	c := newCSRF(&now)

	a1, err := c.IssueAnonymous()
	if err != nil {
		t.Fatal(err)
	}
	a2, _ := c.IssueAnonymous()
	if a1 == a2 {
		t.Fatal("two anonymous tokens are identical; nonce is not random")
	}
	if !strings.HasPrefix(a1, "v1.a.") {
		t.Fatalf("unexpected anonymous token format: %s", a1)
	}
	if err := c.Verify(a1, ""); err != nil {
		t.Fatalf("fresh anonymous token rejected: %v", err)
	}

	now = baseTime.Add(AnonymousCSRFTTL - time.Second)
	if err := c.Verify(a1, ""); err != nil {
		t.Fatal("anonymous token rejected just before expiry")
	}
	now = baseTime.Add(AnonymousCSRFTTL + time.Second)
	if err := c.Verify(a1, ""); err == nil {
		t.Fatal("expired anonymous token accepted")
	}
}

func TestCSRFSessionBinding(t *testing.T) {
	now := baseTime
	c := newCSRF(&now)
	anon, _ := c.IssueAnonymous()
	s1, _ := c.IssueForSession("jti-1")

	if !strings.HasPrefix(s1, "v1.s.") || s1 == anon {
		t.Fatalf("session token %q not distinct from anonymous %q", s1, anon)
	}
	if err := c.Verify(s1, "jti-1"); err != nil {
		t.Fatalf("session token rejected for its own session: %v", err)
	}
	if err := c.Verify(s1, "jti-2"); err == nil {
		t.Fatal("session token accepted for another session")
	}
	if err := c.Verify(anon, "jti-1"); err == nil {
		t.Fatal("anonymous token accepted on a logged-in request")
	}
	if err := c.Verify(s1, ""); err == nil {
		t.Fatal("session token accepted on an anonymous request")
	}
	// Session tokens do not expire on their own; they live as long as the session.
	now = baseTime.Add(30 * 24 * time.Hour)
	if err := c.Verify(s1, "jti-1"); err != nil {
		t.Fatal("session token expired independently of its session")
	}
	if _, err := c.IssueForSession(""); err == nil {
		t.Fatal("IssueForSession accepted an empty jti")
	}
}

func TestCSRFRejectsForgedValues(t *testing.T) {
	now := baseTime
	c := newCSRF(&now)
	valid, _ := c.IssueAnonymous()
	parts := strings.Split(valid, ".")

	other := NewCSRF([]byte("another-csrf-secret-0123456789-0123"))
	other.now = c.now
	otherSecret, _ := other.IssueAnonymous()

	forged := map[string]string{
		"literal anonymous": "anonymous",
		"empty":             "",
		"made up":           "v1.a.1792764000.aGVsbG8.d29ybGQ",
		"tampered sig":      strings.Join(append(parts[:4:4], parts[4][:len(parts[4])-2]+"AA"), "."),
		"tampered nonce":    strings.Join([]string{parts[0], parts[1], parts[2], strings.Repeat("A", len(parts[3])), parts[4]}, "."),
		"changed iat":       strings.Join([]string{parts[0], parts[1], "1999999999", parts[3], parts[4]}, "."),
		"kind flipped":      strings.Join([]string{parts[0], "s", parts[2], parts[3], parts[4]}, "."),
		"other secret":      otherSecret,
		"wrong version":     "v2" + valid[2:],
	}
	for name, tok := range forged {
		if err := c.Verify(tok, ""); err == nil {
			t.Errorf("%s: forged token accepted", name)
		}
	}
}

func TestLimiter(t *testing.T) {
	now := baseTime
	l := NewLimiter(3, 15*time.Minute)
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if blocked, _ := l.Blocked("k"); blocked {
			t.Fatalf("blocked after %d events", i)
		}
		l.Add("k")
	}
	blocked, retry := l.Blocked("k")
	if !blocked || retry != 15*time.Minute {
		t.Fatalf("after limit: blocked=%v retry=%v", blocked, retry)
	}
	if blocked, _ := l.Blocked("other"); blocked {
		t.Fatal("keys are not independent")
	}

	now = baseTime.Add(10 * time.Minute)
	if _, retry := l.Blocked("k"); retry != 5*time.Minute {
		t.Fatalf("retry-after = %v, want 5m", retry)
	}
	now = baseTime.Add(15 * time.Minute)
	if blocked, _ := l.Blocked("k"); blocked {
		t.Fatal("still blocked after the window ended")
	}

	l.Add("k")
	l.Reset("k")
	if blocked, _ := l.Blocked("k"); blocked {
		t.Fatal("Reset did not clear the key")
	}
}
