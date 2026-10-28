// Package assistant calls the internal AI service (ai/), which runs the meal
// assistant's agent. Request and reply shapes match ai/src/mogumogu_ai/schemas.py.
package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Timeout bounds one message, including every model call and tool query.
const Timeout = 30 * time.Second

const maxReplyBytes = 64 << 10

type Range struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Context is the data Go assembles for every message (project-review 3.6).
// Profile, Events and Meals hold the same JSON objects as the public API.
type Context struct {
	TargetDate  string `json:"target_date"`
	Now         string `json:"now"`
	Timezone    string `json:"timezone"`
	Profile     any    `json:"profile"`
	Events      any    `json:"events"`
	EventsRange Range  `json:"events_range"`
	Meals       any    `json:"meals"`
	MealsRange  Range  `json:"meals_range"`
}

type Request struct {
	Message string  `json:"message"`
	Context Context `json:"context"`
	// Token lets the agent's query tools read this user's data (auth.AgentTokens).
	Token string `json:"token"`
}

type Recommendation struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Reason      string `json:"reason"`
}

type Reply struct {
	Reply           string           `json:"reply"`
	Recommendations []Recommendation `json:"recommendations"`
	AskMealType     bool             `json:"ask_meal_type"`
}

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{Timeout: Timeout}}
}

// Reply sends one message. Any failure (unreachable, timeout, a non-200 from
// the service) is an error; the caller shows it as ai_unavailable.
func (c *Client) Reply(ctx context.Context, req Request) (Reply, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return Reply{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/reply", bytes.NewReader(body))
	if err != nil {
		return Reply{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(httpReq)
	if err != nil {
		return Reply{}, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxReplyBytes))
	if err != nil {
		return Reply{}, err
	}
	if res.StatusCode != http.StatusOK {
		// The service answers {"error": "<code>"}; the code carries no user data.
		var e struct{ Error string }
		_ = json.Unmarshal(data, &e)
		return Reply{}, fmt.Errorf("ai service: status %d %s", res.StatusCode, e.Error)
	}
	var out Reply
	if err := json.Unmarshal(data, &out); err != nil {
		return Reply{}, fmt.Errorf("ai service: invalid reply: %w", err)
	}
	if out.Recommendations == nil {
		out.Recommendations = []Recommendation{}
	}
	return out, nil
}
