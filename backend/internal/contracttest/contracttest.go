// Package contracttest reads contract/ at the repo root: the limits and
// sample messages the meal assistant's three tiers share (contract/README.md).
// Only tests import it.
package contracttest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Limits mirrors contract/limits.json.
type Limits struct {
	MessageChars         int                `json:"message_chars"`
	ReplyChars           int                `json:"reply_chars"`
	DishNameChars        int                `json:"dish_name_chars"`
	DishDescriptionChars int                `json:"dish_description_chars"`
	MealContentChars     int                `json:"meal_content_chars"`
	RecommendationCounts []int              `json:"recommendation_counts"`
	HistoryTurns         int                `json:"history_turns"`
	ChangesPerReply      int                `json:"changes_per_reply"`
	Memories             int                `json:"memories"`
	MemoryChars          int                `json:"memory_chars"`
	QuerySpanDays        int                `json:"query_span_days"`
	Sources              int                `json:"sources"`
	Searches             int                `json:"searches"`
	SearchRounds         int                `json:"search_rounds"`
	MealTypes            []string           `json:"meal_types"`
	ServingNutritionMax  map[string]float64 `json:"serving_nutrition_max"`
	AIHardLimitSeconds   int                `json:"ai_hard_limit_s"`
}

// Path is a file in contract/.
func Path(name string) string {
	_, here, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(here), "..", "..", "..", "contract", name)
}

// ReadLimits fails the test when contract/limits.json is missing, malformed
// or has a key Limits does not know.
func ReadLimits(t *testing.T) Limits {
	t.Helper()
	f, err := os.Open(Path("limits.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var l Limits
	if err := dec.Decode(&l); err != nil {
		t.Fatalf("contract/limits.json: %v", err)
	}
	return l
}
