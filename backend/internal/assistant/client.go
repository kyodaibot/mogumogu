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
	TargetDate string `json:"target_date"`
	Now        string `json:"now"`
	Timezone   string `json:"timezone"`
	Locale     string `json:"locale"` // the reply's language: "ja" or "en"
	// Whether the assistant may stage changes to meal records (Pro only).
	AllowProposal bool  `json:"allow_proposal"`
	Profile       any   `json:"profile"`
	Events        any   `json:"events"`
	EventsRange   Range `json:"events_range"`
	Meals         any   `json:"meals"`
	MealsRange    Range `json:"meals_range"`
}

type Request struct {
	Message string  `json:"message"`
	Context Context `json:"context"`
	// Earlier turns of the conversation, oldest first. Sent by the browser and
	// only checked for shape, so the AI service treats it as untrusted.
	History []Turn `json:"history"`
	// Token lets the agent's query tools read this user's data (auth.AgentTokens).
	Token string `json:"token"`
}

// Turn is one answered message of the conversation history
// (project-review 3.6): what the user asked about which day, and what the
// assistant replied and recommended.
type Turn struct {
	Date            string     `json:"date"`
	Message         string     `json:"message"`
	Reply           string     `json:"reply"`
	Recommendations []TurnDish `json:"recommendations"`
	// Summary of the change proposal in the reply, if there was one.
	Changes []TurnChange `json:"changes"`
}

// TurnChange is what the history keeps of a proposed change: the record
// after a create or update, the record removed by a delete.
type TurnChange struct {
	Op       string `json:"op"`
	Date     string `json:"date"`
	MealType string `json:"meal_type"`
	Content  string `json:"content"`
}

// TurnDish is what the history keeps of a recommendation.
type TurnDish struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Nutrition holds the four values of an estimate, already rounded by the AI
// service.
type Nutrition struct {
	Calories float64 `json:"calories"`
	ProteinG float64 `json:"protein_g"`
	FatG     float64 `json:"fat_g"`
	CarbsG   float64 `json:"carbs_g"`
}

// Estimate is a rough nutrition estimate (GLOSSARY: 营养估算). The AI service
// validates it; Go passes it on unchanged.
type Estimate struct {
	Serving Nutrition `json:"serving"`
	Per100g Nutrition `json:"per_100g"`
}

type Recommendation struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Reason      string   `json:"reason"`
	Estimate    Estimate `json:"estimate"`
}

type Reply struct {
	Reply           string           `json:"reply"`
	Recommendations []Recommendation `json:"recommendations"`
	AskMealType     bool             `json:"ask_meal_type"`
	// The meal the recommendations are for; null without recommendations.
	MealType *string `json:"meal_type"`
	// Changes to meal records the assistant staged; Go checks and saves them
	// as a proposal. Empty when it staged none.
	Changes []Change `json:"changes"`
}

// Change is one staged change to the user's meal records (project-review 4).
// Go checks it like a request to the public API before saving it.
type Change struct {
	Op string `json:"op"` // create, update or delete
	// The record to update or delete.
	MealID *int64 `json:"meal_id"`
	// create: the new record. update: only the fields that change.
	Meal *ChangeMeal `json:"meal"`
}

type ChangeMeal struct {
	Date     *string `json:"date"`
	MealType *string `json:"meal_type"`
	Content  *string `json:"content"`
	// Absent: unchanged (update) or none (create). null: no nutrition.
	Nutrition OptionalNutrition `json:"nutrition"`
}

// OptionalNutrition tells an absent nutrition field from a null one.
type OptionalNutrition struct {
	Set   bool
	Value *Nutrition
}

func (o *OptionalNutrition) UnmarshalJSON(b []byte) error {
	o.Set = true
	return json.Unmarshal(b, &o.Value)
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
