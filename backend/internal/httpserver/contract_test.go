package httpserver

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"slices"
	"testing"
	"time"

	"mogumogu/backend/internal/assistant"
	"mogumogu/backend/internal/contracttest"
	"mogumogu/backend/internal/store"
	"mogumogu/backend/internal/tz"
)

// The limits the AI service and the browser share with Go (contract/README.md).
// When one fails, change contract/limits.json and the other tiers too.
func TestLimitsMatchTheContract(t *testing.T) {
	l := contracttest.ReadLimits(t)
	for _, c := range []struct {
		name      string
		got, want int
	}{
		{"message_chars", maxMessageChars, l.MessageChars},
		{"reply_chars", maxReplyChars, l.ReplyChars},
		{"dish_name_chars", maxDishNameChars, l.DishNameChars},
		{"dish_description_chars", maxDishDescriptionChars, l.DishDescriptionChars},
		{"meal_content_chars", maxMealContentChars, l.MealContentChars},
		{"history_turns", maxHistoryTurns, l.HistoryTurns},
		{"changes_per_reply", maxProposalChanges, l.ChangesPerReply},
		{"memories", maxMemories, l.Memories},
		{"memory_chars", maxMemoryChars, l.MemoryChars},
		{"query_span_days", agentMaxRangeDays, l.QuerySpanDays},
		{"sources", maxSources, l.Sources},
		{"serving_nutrition_max.calories", maxCalories, int(l.ServingNutritionMax["calories"])},
	} {
		if c.got != c.want {
			t.Errorf("%s: Go has %d, the contract %d", c.name, c.got, c.want)
		}
	}
	for _, k := range []string{"protein_g", "fat_g", "carbs_g"} {
		if l.ServingNutritionMax[k] != maxNutrientG {
			t.Errorf("serving_nutrition_max.%s: Go has %d, the contract %g", k, maxNutrientG, l.ServingNutritionMax[k])
		}
	}
	if !slices.Equal(mealTypes, l.MealTypes) {
		t.Errorf("meal_types: Go has %v, the contract %v", mealTypes, l.MealTypes)
	}
}

var update = flag.Bool("update", false, "rewrite contract/request.json from the current code")

// contract/request.json is what Go sends the AI service for one message,
// built by the same code as askAssistant from fixed records. The AI service's
// tests check that it reads it (contract/README.md).
func TestRequestMatchesTheContract(t *testing.T) {
	ptr := func(f float64) *float64 { return &f }
	notes := "研究室の飲み会"
	at := func(s string) time.Time {
		v, err := tz.ParseInstant(s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	day := func(s string) tz.Date {
		d, err := tz.ParseDate(s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	stamp := at("2026-10-20T08:00:00+09:00")
	ctx := assistantContext(contextData{
		Target: day("2026-10-27"), Now: at("2026-10-27T11:30:00+09:00"), Locale: "ja", Pro: true,
		Profile: &store.Profile{
			HeightCM: 170, WeightKG: 60.5, Age: 21, HasExerciseHabit: true, Goal: "build_muscle",
			Allergens: store.NullStringArray{Items: []string{"えび", "ピーナッツ"}, Valid: true},
			CreatedAt: stamp, UpdatedAt: stamp,
		},
		Memories: []store.Memory{
			{ID: 3, Content: "辛いものが苦手", CreatedAt: stamp, UpdatedAt: stamp},
			{ID: 4, Content: "甘いものを控えたい", CreatedAt: stamp, UpdatedAt: stamp},
		},
		Events: []store.ScheduleEvent{
			{ID: 11, Title: "ゼミ", StartsAt: at("2026-10-27T13:00:00+09:00"), EndsAt: at("2026-10-27T15:00:00+09:00"), CreatedAt: stamp, UpdatedAt: stamp},
			{ID: 12, Title: "飲み会", StartsAt: at("2026-10-27T19:00:00+09:00"), EndsAt: at("2026-10-27T21:00:00+09:00"), Notes: &notes, CreatedAt: stamp, UpdatedAt: stamp},
		},
		EventsFrom: day("2026-10-26"), EventsTo: day("2026-10-28"),
		Meals: []store.MealRecord{
			{ID: 21, Date: day("2026-10-26").Time(), MealType: "dinner", Content: "カレーライス", Calories: ptr(750), ProteinG: ptr(20), FatG: ptr(25), CarbsG: ptr(110), CreatedAt: stamp, UpdatedAt: stamp},
			{ID: 22, Date: day("2026-10-27").Time(), MealType: "breakfast", Content: "トーストとコーヒー", CreatedAt: stamp, UpdatedAt: stamp},
		},
		MealsFrom: day("2026-10-21"), MealsTo: day("2026-10-27"),
	})
	req := assistant.Request{
		Message: "今日の昼食は何を食べたらいい？",
		History: []assistant.Turn{{
			Date: "2026-10-26", Message: "夕食は？", Reply: "3つ提案します。",
			Recommendations: []assistant.TurnDish{
				{Name: "鮭の塩焼き定食", Description: "焼き鮭とご飯"},
				{Name: "親子丼", Description: "鶏肉と卵"},
				{Name: "豆腐ハンバーグ", Description: "豆腐と鶏ひき肉"},
			},
			Changes: []assistant.TurnChange{{Op: "create", Date: "2026-10-26", MealType: "dinner", Content: "カレーライス"}},
		}},
		Token: "contract-token", UserID: 7, Tier: "pro", Context: ctx,
	}
	got, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := contracttest.Path("request.json")
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("the request Go sends changed. If on purpose, run\n  cd backend && go test ./internal/httpserver -run TestRequestMatchesTheContract -update\nthen make sure the AI service still reads it (pytest tests/test_contract.py).\ngot:\n%s", got)
	}
}
