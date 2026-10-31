package httpserver

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"mogumogu/backend/internal/assistant"
	"mogumogu/backend/internal/store"
	"mogumogu/backend/internal/testdb"
)

type proposalReply struct {
	Reply    string        `json:"reply"`
	Proposal *proposalJSON `json:"proposal"`
}

type resultsJSON struct {
	Results []store.ChangeResult `json:"results"`
}

func ptr[T any](v T) *T { return &v }

func createChange(date, mealType, content string, n *assistant.Nutrition) assistant.Change {
	return assistant.Change{Op: "create", Meal: &assistant.ChangeMeal{
		Date: &date, MealType: &mealType, Content: &content,
		Nutrition: assistant.OptionalNutrition{Set: n != nil, Value: n},
	}}
}

// newMeal creates a record through the public API and returns its ID.
func newMeal(t *testing.T, b *browser, date, mealType, content string) int64 {
	t.Helper()
	var m mealJSON
	b.expect(201, "POST", "/api/meal-records", map[string]string{"date": date, "meal_type": mealType, "content": content}).json(t, &m)
	return m.ID
}

func mealContents(t *testing.T, b *browser, date string) []string {
	t.Helper()
	var list struct{ Items []mealJSON }
	b.expect(200, "GET", "/api/meal-records?from="+date+"&to="+date, nil).json(t, &list)
	var out []string
	for _, m := range list.Items {
		out = append(out, m.MealType+":"+m.Content)
	}
	return out
}

func TestIntegrationProposals(t *testing.T) {
	db := testdb.New(t)
	env := newAssistantEnv(t, db, DailyLimits{Normal: 30, Pro: 100, Site: 300})
	ctx := context.Background()
	b := signUp(t, env.srv, "pro")
	if err := env.store.SetUserTier(ctx, "pro", store.TierPro); err != nil {
		t.Fatal(err)
	}
	const day = "2026-10-30"
	curry := newMeal(t, b, day, "dinner", "カレー")
	salad := newMeal(t, b, day, "lunch", "サラダ")
	toast := newMeal(t, b, day, "breakfast", "トースト")

	env.ai.changes = []assistant.Change{
		createChange("2026-10-31", "lunch", " ラーメン ", &assistant.Nutrition{Calories: 500, ProteinG: 20, FatG: 15, CarbsG: 70}),
		{Op: "update", MealID: &curry, Meal: &assistant.ChangeMeal{Content: ptr("ハヤシライス"), Nutrition: assistant.OptionalNutrition{Set: true}}},
		{Op: "delete", MealID: &salad},
		{Op: "update", MealID: &toast, Meal: &assistant.ChangeMeal{MealType: ptr("snack")}},
	}
	var got proposalReply
	b.expect(200, "POST", "/api/assistant/messages", message("2026-10-31", "記録して")).json(t, &got)
	if !env.ai.last().Context.AllowProposal {
		t.Fatal("a pro user's message does not allow proposals")
	}
	p := got.Proposal
	if p == nil || len(p.Changes) != 4 {
		t.Fatalf("proposal: %s", fmtJSON(t, got))
	}
	create, update, del := p.Changes[0], p.Changes[1], p.Changes[2]
	if create.Before != nil || create.After.Content != "ラーメン" || create.After.ID != 0 || *create.After.Calories != 500 {
		t.Fatalf("create: %s", fmtJSON(t, create))
	}
	if update.Before.Content != "カレー" || update.After.Content != "ハヤシライス" || update.After.Date != day ||
		update.After.MealType != "dinner" || update.After.Calories != nil {
		t.Fatalf("update: %s", fmtJSON(t, update))
	}
	if del.Before.ID != salad || del.After != nil {
		t.Fatalf("delete: %s", fmtJSON(t, del))
	}
	expires, err := time.Parse(time.RFC3339, p.ExpiresAt)
	if err != nil || time.Until(expires) < 119*time.Minute || time.Until(expires) > 121*time.Minute {
		t.Fatalf("expires_at %q: %v", p.ExpiresAt, err)
	}
	// Nothing is written until the user confirms.
	if m := mealContents(t, b, "2026-10-31"); len(m) != 0 {
		t.Fatalf("written before execution: %v", m)
	}

	// The toast is edited on the calendar after the proposal: its change fails.
	b.expect(200, "PUT", fmt.Sprintf("/api/meal-records/%d", toast), map[string]string{"date": day, "meal_type": "breakfast", "content": "トースト2枚"})

	exec := fmt.Sprintf("/api/assistant/proposals/%d/execute", p.ID)
	for name, body := range map[string]any{
		"none":         map[string][]int{"accepted": {}},
		"missing":      map[string]any{},
		"duplicate":    map[string][]int{"accepted": {0, 0}},
		"out of range": map[string][]int{"accepted": {0, 4}},
		"negative":     map[string][]int{"accepted": {-1}},
	} {
		if r := b.do("POST", exec, body); r.status != 422 {
			t.Errorf("%s: %d %s", name, r.status, r.body)
		}
	}

	// The salad is left unchecked.
	var res resultsJSON
	b.expect(200, "POST", exec, map[string][]int{"accepted": {3, 1, 0}}).json(t, &res)
	want := []store.ChangeResult{
		{Status: "done"}, {Status: "done", MealID: curry}, {Status: "skipped"}, {Status: "failed", Reason: "changed"},
	}
	if res.Results[0].MealID == 0 {
		t.Fatalf("created record has no id: %+v", res.Results)
	}
	want[0].MealID = res.Results[0].MealID
	if fmtJSON(t, res.Results) != fmtJSON(t, want) {
		t.Fatalf("results:\n got %s\nwant %s", fmtJSON(t, res.Results), fmtJSON(t, want))
	}
	if m := mealContents(t, b, "2026-10-31"); fmtJSON(t, m) != `["lunch:ラーメン"]` {
		t.Fatalf("created: %v", m)
	}
	if m := mealContents(t, b, day); fmtJSON(t, m) != `["breakfast:トースト2枚","lunch:サラダ","dinner:ハヤシライス"]` {
		t.Fatalf("after execution: %v", m)
	}

	// Once only.
	if r := b.do("POST", exec, map[string][]int{"accepted": {2}}); r.status != 409 || r.errCode(t) != "proposal_executed" {
		t.Fatalf("second execution: %d %s", r.status, r.body)
	}

	// A record deleted after the proposal: not_found.
	env.ai.changes = []assistant.Change{{Op: "delete", MealID: &salad}}
	b.expect(200, "POST", "/api/assistant/messages", message(day, "サラダを消して")).json(t, &got)
	b.expect(204, "DELETE", fmt.Sprintf("/api/meal-records/%d", salad), nil)
	res = resultsJSON{}
	b.expect(200, "POST", fmt.Sprintf("/api/assistant/proposals/%d/execute", got.Proposal.ID), map[string][]int{"accepted": {0}}).json(t, &res)
	if res.Results[0] != (store.ChangeResult{Status: "failed", Reason: "not_found"}) {
		t.Fatalf("deleted record: %+v", res.Results)
	}

	// Another user's proposal looks missing.
	env.ai.changes = []assistant.Change{createChange(day, "snack", "りんご", nil)}
	b.expect(200, "POST", "/api/assistant/messages", message(day, "りんご食べた")).json(t, &got)
	other := signUp(t, env.srv, "other")
	if err := env.store.SetUserTier(ctx, "other", store.TierPro); err != nil {
		t.Fatal(err)
	}
	exec = fmt.Sprintf("/api/assistant/proposals/%d/execute", got.Proposal.ID)
	if r := other.do("POST", exec, map[string][]int{"accepted": {0}}); r.status != 404 {
		t.Fatalf("another user's proposal: %d %s", r.status, r.body)
	}

	// After a downgrade the proposal cannot run.
	if err := env.store.SetUserTier(ctx, "pro", store.TierNormal); err != nil {
		t.Fatal(err)
	}
	if r := b.do("POST", exec, map[string][]int{"accepted": {0}}); r.status != 403 || r.errCode(t) != "pro_required" {
		t.Fatalf("after downgrade: %d %s", r.status, r.body)
	}
	if err := env.store.SetUserTier(ctx, "pro", store.TierPro); err != nil {
		t.Fatal(err)
	}

	// Two hours later it has expired, and is still kept.
	if _, err := db.SQL.Exec(`UPDATE proposals SET created_at = now() - interval '121 minutes' WHERE id = $1`, got.Proposal.ID); err != nil {
		t.Fatal(err)
	}
	if r := b.do("POST", exec, map[string][]int{"accepted": {0}}); r.status != 410 || r.errCode(t) != "proposal_expired" {
		t.Fatalf("expired: %d %s", r.status, r.body)
	}
	var kept, executed int
	if err := db.SQL.QueryRow(`SELECT count(*), count(executed_at) FROM proposals`).Scan(&kept, &executed); err != nil {
		t.Fatal(err)
	}
	if kept != 3 || executed != 2 {
		t.Fatalf("audit rows: %d kept, %d executed", kept, executed)
	}
}

func TestIntegrationProposalsRejected(t *testing.T) {
	db := testdb.New(t)
	env := newAssistantEnv(t, db, DailyLimits{Normal: 30, Pro: 100, Site: 300})
	ctx := context.Background()
	normal := signUp(t, env.srv, "normal")
	const day = "2026-10-30"
	normalMeal := newMeal(t, normal, day, "lunch", "うどん")

	// A normal user's message does not allow proposals; changes staged anyway are refused.
	normal.expect(200, "POST", "/api/assistant/messages", message(day, "うどん食べた"))
	if env.ai.last().Context.AllowProposal {
		t.Fatal("a normal user's message allows proposals")
	}
	ok := createChange(day, "dinner", "鍋", nil)
	env.ai.changes = []assistant.Change{ok}
	if r := normal.do("POST", "/api/assistant/messages", message(day, "鍋食べた")); r.status != 503 || r.errCode(t) != "ai_unavailable" {
		t.Fatalf("normal user's proposal: %d %s", r.status, r.body)
	}

	eleven := make([]assistant.Change, 11)
	for i := range eleven {
		eleven[i] = ok
	}
	// Each case is a new Pro user with one record (proMeal), so the
	// per-minute message limit is not reached.
	n := 0
	for name, changes := range map[string]func(proMeal int64) []assistant.Change{
		"another user's record": func(int64) []assistant.Change { return []assistant.Change{{Op: "delete", MealID: &normalMeal}} },
		"missing record":        func(int64) []assistant.Change { return []assistant.Change{{Op: "delete", MealID: ptr(int64(99999))}} },
		"same record twice": func(id int64) []assistant.Change {
			return []assistant.Change{{Op: "delete", MealID: &id}, {Op: "update", MealID: &id, Meal: &assistant.ChangeMeal{Content: ptr("x")}}}
		},
		"eleven changes": func(int64) []assistant.Change { return eleven },
		"bad meal type":  func(int64) []assistant.Change { return []assistant.Change{createChange(day, "brunch", "x", nil)} },
		"blank content":  func(int64) []assistant.Change { return []assistant.Change{createChange(day, "lunch", "  ", nil)} },
		"too many calories": func(int64) []assistant.Change {
			return []assistant.Change{createChange(day, "lunch", "x", &assistant.Nutrition{Calories: 10001})}
		},
		"update without fields": func(id int64) []assistant.Change { return []assistant.Change{{Op: "update", MealID: &id}} },
		"unchanged update": func(id int64) []assistant.Change {
			return []assistant.Change{{Op: "update", MealID: &id, Meal: &assistant.ChangeMeal{Content: ptr("そば")}}}
		},
		"delete with fields": func(id int64) []assistant.Change {
			return []assistant.Change{{Op: "delete", MealID: &id, Meal: &assistant.ChangeMeal{Content: ptr("x")}}}
		},
		"create with an id": func(id int64) []assistant.Change {
			return []assistant.Change{{Op: "create", MealID: &id, Meal: ok.Meal}}
		},
		"unknown op": func(id int64) []assistant.Change { return []assistant.Change{{Op: "upsert", MealID: &id}} },
	} {
		n++
		username := fmt.Sprintf("pro%d", n)
		pro := signUp(t, env.srv, username)
		if err := env.store.SetUserTier(ctx, username, store.TierPro); err != nil {
			t.Fatal(err)
		}
		proMeal := newMeal(t, pro, day, "lunch", "そば")
		env.ai.changes = changes(proMeal)
		if r := pro.do("POST", "/api/assistant/messages", message(day, "変更して")); r.status != 503 || r.errCode(t) != "ai_unavailable" {
			t.Errorf("%s: %d %s", name, r.status, r.body)
		}
		if m := mealContents(t, pro, day); fmtJSON(t, m) != `["lunch:そば"]` {
			t.Fatalf("%s: records changed: %v", name, m)
		}
	}

	// None was saved, and every refused answer was refunded.
	var proposals, used int
	if err := db.SQL.QueryRow(`SELECT count(*) FROM proposals`).Scan(&proposals); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL.QueryRow(`SELECT COALESCE(SUM(messages), 0) FROM assistant_usage`).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if proposals != 0 || used != 1 {
		t.Fatalf("after refused proposals: %d saved, %d messages counted", proposals, used)
	}
}

func TestIntegrationProposalHistory(t *testing.T) {
	env := newAssistantEnv(t, testdb.New(t), DailyLimits{Normal: 30, Pro: 100, Site: 300})
	b := signUp(t, env.srv, "talker")

	change := func(op, content string) map[string]string {
		return map[string]string{"op": op, "date": "2026-10-31", "meal_type": "lunch", "content": content}
	}
	t1 := turn("2026-10-31", "お昼にラーメン食べた", "記録します。")
	t1["changes"] = []map[string]string{change("create", " ラーメン ")}
	b.expect(200, "POST", "/api/assistant/messages", withHistory("2026-10-31", "やっぱりうどん", t1))
	got := env.ai.last().History[0].Changes
	if fmtJSON(t, got) != `[{"op":"create","date":"2026-10-31","meal_type":"lunch","content":"ラーメン"}]` {
		t.Fatalf("history changes: %s", fmtJSON(t, got))
	}

	eleven := make([]map[string]string, 11)
	for i := range eleven {
		eleven[i] = change("create", "x")
	}
	for name, c := range map[string]struct {
		changes any
		field   string
	}{
		"eleven":   {eleven, "history[0].changes"},
		"bad op":   {[]map[string]string{change("upsert", "x")}, "history[0].changes[0].op"},
		"no date":  {[]map[string]string{{"op": "delete", "meal_type": "lunch", "content": "x"}}, "history[0].changes[0].date"},
		"too long": {[]map[string]string{change("create", strings.Repeat("長", 201))}, "history[0].changes[0].content"},
	} {
		bad := turn("2026-10-31", "q", "a")
		bad["changes"] = c.changes
		r := b.do("POST", "/api/assistant/messages", withHistory("2026-10-31", "hi", bad))
		var e errResp
		r.json(t, &e)
		if r.status != 422 || len(e.Error.Details) == 0 || e.Error.Details[0].Field != c.field {
			t.Errorf("%s: %d %s", name, r.status, r.body)
		}
	}
}
