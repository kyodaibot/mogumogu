package httpserver

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"mogumogu/backend/internal/auth"
	"mogumogu/backend/internal/store"
	"mogumogu/backend/internal/tz"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9_]+$`)

type credentialsInput struct {
	Username *string `json:"username"`
	Password *string `json:"password"`
}

type userJSON struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	CreatedAt string `json:"created_at"`
}

type meJSON struct {
	User       userJSON `json:"user"`
	HasProfile bool     `json:"has_profile"`
	CSRFToken  string   `json:"csrf_token,omitempty"`
}

func toUserJSON(u store.User) userJSON {
	return userJSON{ID: u.ID, Username: u.Username, CreatedAt: tz.FormatInstant(u.CreatedAt)}
}

func (a *api) getCSRF(c *gin.Context) {
	var sess *auth.Session
	if s, ok := sessionOf(c); ok {
		sess = &s
	}
	token, err := a.issueCSRF(c, sess)
	if err != nil {
		abortInternal(c, a.logger, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"csrf_token": token})
}

func (a *api) register(c *gin.Context) {
	ipKey := c.ClientIP()
	if blocked, retry := a.registerLimiter.Blocked(ipKey); blocked {
		abortRateLimited(c, retry)
		return
	}
	a.registerLimiter.Add(ipKey)

	var in credentialsInput
	if !decodeJSON(c, &in) {
		return
	}
	var v validator
	username := ""
	if in.Username == nil {
		v.add("username", "required")
	} else {
		username = strings.ToLower(*in.Username)
		v.length("username", username, 3, 32)
		if username != "" && !usernamePattern.MatchString(username) {
			v.add("username", "invalid_format")
		}
	}
	if in.Password == nil {
		v.add("password", "required")
	} else {
		v.length("password", *in.Password, 8, 128)
	}
	if v.failed() {
		abortValidation(c, v.errs)
		return
	}

	hash, err := auth.HashPassword(*in.Password)
	if err != nil {
		abortInternal(c, a.logger, err)
		return
	}
	user, err := a.store.CreateUser(c.Request.Context(), username, hash)
	if errors.Is(err, store.ErrUsernameTaken) {
		abortError(c, http.StatusConflict, "username_taken", "Username is already taken.")
		return
	}
	if err != nil {
		abortInternal(c, a.logger, err)
		return
	}
	a.startSession(c, http.StatusCreated, user, false)
}

func (a *api) login(c *gin.Context) {
	var in credentialsInput
	if !decodeJSON(c, &in) {
		return
	}
	var v validator
	if in.Username == nil {
		v.add("username", "required")
	}
	if in.Password == nil {
		v.add("password", "required")
	}
	if v.failed() {
		abortValidation(c, v.errs)
		return
	}

	username := strings.ToLower(*in.Username)
	key := c.ClientIP() + "|" + username
	if blocked, retry := a.loginLimiter.Blocked(key); blocked {
		abortRateLimited(c, retry)
		return
	}
	invalid := func() {
		a.loginLimiter.Add(key)
		abortError(c, http.StatusUnauthorized, "invalid_credentials", "Invalid username or password.")
	}

	// Bound the work an oversized password can cause before hashing it.
	if utf8.RuneCountInString(*in.Password) > 128 {
		invalid()
		return
	}
	user, err := a.store.UserByUsername(c.Request.Context(), username)
	if errors.Is(err, store.ErrNotFound) {
		auth.BurnPasswordCheck(*in.Password)
		invalid()
		return
	}
	if err != nil {
		abortInternal(c, a.logger, err)
		return
	}
	ok, err := auth.VerifyPassword(*in.Password, user.PasswordHash)
	if err != nil {
		abortInternal(c, a.logger, err)
		return
	}
	if !ok {
		invalid()
		return
	}
	a.loginLimiter.Reset(key)

	hasProfile, err := a.store.HasProfile(c.Request.Context(), user.ID)
	if err != nil {
		abortInternal(c, a.logger, err)
		return
	}
	a.startSession(c, http.StatusOK, user, hasProfile)
}

// startSession issues a session JWT and a CSRF token bound to it, sets both
// cookies and returns them with the user (docs/api.md 2.4, 4.2, 4.3).
func (a *api) startSession(c *gin.Context, status int, user store.User, hasProfile bool) {
	token, sess, err := a.sessions.Issue(user.ID)
	if err != nil {
		abortInternal(c, a.logger, err)
		return
	}
	csrfToken, err := a.issueCSRF(c, &sess)
	if err != nil {
		abortInternal(c, a.logger, err)
		return
	}
	a.setSessionCookie(c, token)
	c.JSON(status, meJSON{User: toUserJSON(user), HasProfile: hasProfile, CSRFToken: csrfToken})
}

func (a *api) logout(c *gin.Context) {
	sess, _ := sessionOf(c)
	ctx := c.Request.Context()
	if err := a.store.RevokeToken(ctx, sess.JTI, sess.ExpiresAt); err != nil {
		abortInternal(c, a.logger, err)
		return
	}
	if err := a.store.DeleteExpiredRevocations(ctx, time.Now()); err != nil {
		// Housekeeping only; the logout itself succeeded.
		a.logger.Warn("deleting expired revocations failed")
	}
	a.clearSessionCookie(c)
	if _, err := a.issueCSRF(c, nil); err != nil {
		abortInternal(c, a.logger, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (a *api) me(c *gin.Context) {
	sess, _ := sessionOf(c)
	ctx := c.Request.Context()
	user, err := a.store.UserByID(ctx, sess.UserID)
	if errors.Is(err, store.ErrNotFound) {
		abortError(c, http.StatusUnauthorized, "unauthenticated", "Authentication required.")
		return
	}
	if err != nil {
		abortInternal(c, a.logger, err)
		return
	}
	hasProfile, err := a.store.HasProfile(ctx, user.ID)
	if err != nil {
		abortInternal(c, a.logger, err)
		return
	}
	c.JSON(http.StatusOK, meJSON{User: toUserJSON(user), HasProfile: hasProfile})
}
