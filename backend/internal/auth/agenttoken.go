package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Agent tokens let the AI service's query tools read one user's data through
// the internal API (docs/adr/0001):
//
//	<user_id>.<expires_unix>.<signature>
//
// The key is random per process: this backend both issues and verifies them,
// so nothing needs configuring, and a restart only invalidates tokens of
// requests in flight.
const AgentTokenTTL = 60 * time.Second

var ErrAgentToken = errors.New("agent token rejected")

type AgentTokens struct {
	key []byte
	now func() time.Time
}

func NewAgentTokens() (*AgentTokens, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return &AgentTokens{key: key, now: time.Now}, nil
}

// Issue returns a token for userID that expires after AgentTokenTTL.
func (a *AgentTokens) Issue(userID int64) string {
	payload := strconv.FormatInt(userID, 10) + "." + strconv.FormatInt(a.now().Add(AgentTokenTTL).Unix(), 10)
	return payload + "." + a.sign(payload)
}

// Verify checks the signature and expiry and returns the user ID.
func (a *AgentTokens) Verify(token string) (int64, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0, ErrAgentToken
	}
	payload := parts[0] + "." + parts[1]
	if !hmac.Equal([]byte(parts[2]), []byte(a.sign(payload))) {
		return 0, ErrAgentToken
	}
	userID, err1 := strconv.ParseInt(parts[0], 10, 64)
	exp, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil || userID <= 0 || a.now().Unix() >= exp {
		return 0, ErrAgentToken
	}
	return userID, nil
}

func (a *AgentTokens) sign(payload string) string {
	m := hmac.New(sha256.New, a.key)
	m.Write([]byte("agent." + payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
