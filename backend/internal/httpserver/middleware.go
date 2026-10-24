package httpserver

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/gin-gonic/gin"

	"mogumogu/backend/internal/auth"
)

const (
	sessionCookie = "mogumogu_session"
	csrfCookie    = "mogumogu_csrf"
	csrfHeader    = "X-CSRF-Token"
	ctxSession    = "session"
)

// requestLogger logs one line per request. Only the path is logged, not the
// query string, body, cookies or headers, so none of those reach the logs.
func requestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Info("request",
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", c.Writer.Status()),
			slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
		)
	}
}

// resolveSession attaches the caller's session when the request carries a
// valid session cookie whose token is not revoked and whose user still
// exists. It never rejects a request by itself; requireAuth does.
func (a *api) resolveSession(c *gin.Context) {
	token, err := c.Cookie(sessionCookie)
	if err != nil || token == "" {
		c.Next()
		return
	}
	sess, err := a.sessions.Verify(token)
	if err != nil {
		c.Next()
		return
	}
	active, err := a.store.SessionActive(c.Request.Context(), sess.UserID, sess.JTI)
	if err != nil {
		abortInternal(c, a.logger, err)
		return
	}
	if active {
		c.Set(ctxSession, sess)
	}
	c.Next()
}

func sessionOf(c *gin.Context) (auth.Session, bool) {
	v, ok := c.Get(ctxSession)
	if !ok {
		return auth.Session{}, false
	}
	return v.(auth.Session), true
}

// requireAuth rejects requests without a valid session.
func (a *api) requireAuth(c *gin.Context) {
	if _, ok := sessionOf(c); !ok {
		abortError(c, http.StatusUnauthorized, "unauthenticated", "Authentication required.")
		return
	}
	c.Next()
}

// csrfProtect implements docs/api.md 2.4 for every state-changing request:
// allowed Origin, double submit (header equals cookie), valid signature, and a
// token bound to the caller's current session (or anonymous when logged out).
func (a *api) csrfProtect(c *gin.Context) {
	switch c.Request.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		c.Next()
		return
	}
	fail := func() {
		abortError(c, http.StatusForbidden, "csrf_failed", "CSRF validation failed.")
	}

	if origin := c.GetHeader("Origin"); origin == "" || !slices.Contains(a.allowedOrigins, origin) {
		fail()
		return
	}
	header := c.GetHeader(csrfHeader)
	cookie, err := c.Cookie(csrfCookie)
	if header == "" || err != nil || subtle.ConstantTimeCompare([]byte(header), []byte(cookie)) != 1 {
		fail()
		return
	}
	jti := ""
	if sess, ok := sessionOf(c); ok {
		jti = sess.JTI
	}
	if a.csrf.Verify(header, jti) != nil {
		fail()
		return
	}
	c.Next()
}

// --- cookies ---

func (a *api) setCookie(c *gin.Context, name, value, path string, maxAge time.Duration, httpOnly bool, sameSite http.SameSite) {
	ck := &http.Cookie{
		Name: name, Value: value, Path: path,
		HttpOnly: httpOnly, Secure: a.cookieSecure, SameSite: sameSite,
	}
	if maxAge > 0 {
		ck.MaxAge = int(maxAge.Seconds())
	} else {
		ck.MaxAge = -1 // delete
	}
	http.SetCookie(c.Writer, ck)
}

func (a *api) setSessionCookie(c *gin.Context, token string) {
	a.setCookie(c, sessionCookie, token, "/api", a.sessions.TTL(), true, http.SameSiteLaxMode)
}

func (a *api) clearSessionCookie(c *gin.Context) {
	a.setCookie(c, sessionCookie, "", "/api", 0, true, http.SameSiteLaxMode)
}

// issueCSRF creates a token for the given session (or an anonymous one when
// sess is nil), stores it in the CSRF cookie and returns it.
func (a *api) issueCSRF(c *gin.Context, sess *auth.Session) (string, error) {
	var (
		token  string
		err    error
		maxAge = auth.AnonymousCSRFTTL
	)
	if sess != nil {
		token, err = a.csrf.IssueForSession(sess.JTI)
		maxAge = time.Until(sess.ExpiresAt)
	} else {
		token, err = a.csrf.IssueAnonymous()
	}
	if err != nil {
		return "", err
	}
	// Not HttpOnly: the frontend reads it to fill the X-CSRF-Token header.
	a.setCookie(c, csrfCookie, token, "/", maxAge, false, http.SameSiteStrictMode)
	return token, nil
}
