package assistant

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"mogumogu/backend/internal/contracttest"
)

// contract/reply.json is what the AI service answers, checked by its own
// tests against what its tools stage (contract/README.md). Go must read
// every field, and tell an absent nutrition from a null one.
func TestReplyContractIsRead(t *testing.T) {
	b, err := os.ReadFile(contracttest.Path("reply.json"))
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var r Reply
	if err := dec.Decode(&r); err != nil {
		t.Fatalf("contract/reply.json: %v", err)
	}
	if len(r.Recommendations) != 3 || r.MealType == nil || *r.MealType != "dinner" || len(r.Sources) != 1 {
		t.Fatalf("reply: %+v", r)
	}
	if r.Recommendations[0].Estimate.Serving.Calories == 0 || r.Recommendations[0].Estimate.Per100g.Calories == 0 {
		t.Errorf("estimate: %+v", r.Recommendations[0].Estimate)
	}

	type want struct {
		op            string
		mealID        int64 // 0: none
		meal          bool
		nutritionSet  bool
		nutritionNull bool
	}
	wants := []want{
		{op: "create", meal: true, nutritionSet: true},
		{op: "create", meal: true, nutritionSet: true, nutritionNull: true},
		{op: "update", mealID: 21, meal: true},
		{op: "update", mealID: 22, meal: true, nutritionSet: true, nutritionNull: true},
		{op: "delete", mealID: 99},
	}
	if len(r.Changes) != len(wants) {
		t.Fatalf("changes: %d, want %d", len(r.Changes), len(wants))
	}
	for i, w := range wants {
		c := r.Changes[i]
		var id int64
		if c.MealID != nil {
			id = *c.MealID
		}
		got := want{op: c.Op, mealID: id, meal: c.Meal != nil}
		if c.Meal != nil {
			got.nutritionSet = c.Meal.Nutrition.Set
			got.nutritionNull = c.Meal.Nutrition.Set && c.Meal.Nutrition.Value == nil
		}
		if got != w {
			t.Errorf("changes[%d]: %+v, want %+v", i, got, w)
		}
	}
	if n := r.Changes[0].Meal.Nutrition.Value; n == nil || n.Calories != 700 {
		t.Errorf("changes[0] nutrition: %+v", n)
	}

	var ops []string
	for _, m := range r.MemoryChanges {
		ops = append(ops, m.Op)
	}
	if len(ops) != 3 || ops[0] != "create" || ops[1] != "update" || ops[2] != "delete" ||
		r.MemoryChanges[0].Content == nil || r.MemoryChanges[2].MemoryID == nil || *r.MemoryChanges[2].MemoryID != 4 {
		t.Errorf("memory changes: %v", ops)
	}
}
