package httpserver

import (
	"encoding/json"
	"fmt"
	"mogumogu/backend/internal/auth"
	"mogumogu/backend/internal/testdb"
	"mogumogu/backend/internal/tz"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mogumogu/backend/internal/assistant"
	"mogumogu/backend/internal/store"
)

func TestCafeteriaServiceWindows(t *testing.T) {
	monday := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	open := []store.OpenInterval{{Start: "08:00", End: "15:30"}, {Start: "17:00", End: "20:30"}}
	tests := []struct {
		meal, clock, shop, category string
		want                        bool
	}{
		{"breakfast", "09:59", "650111", "朝食プレート", true},
		{"breakfast", "10:00", "650111", "朝食プレート", false},
		{"breakfast", "09:00", "650113", "朝食プレート", false},
		{"lunch", "12:00", "650111", "朝食プレート", false},
		{"lunch", "12:00", "650111", "夜の主菜メニュー", false},
		{"dinner", "18:00", "650113", "夜の主菜メニュー", true},
		{"dinner", "16:00", "650113", "主菜", false},
		{"dinner", "17:15", "650118", "オーダー 平日11:00~14:00 17:30~20:00　土曜閉店", false},
		{"lunch", "13:55", "650118", "オーダー 平日11:00~14:00 17:30~20:00　土曜閉店", true},
	}
	for _, c := range tests {
		q := menuFilter{meal: c.meal, clock: c.clock, day: monday}
		if got := q.itemWindows(c.shop, []string{c.category}, open); (len(got) > 0) != c.want {
			t.Errorf("%+v: %v", c, got)
		}
	}
}

// A Friday reference for Saturday follows Saturday's rules, without today's hours.
func TestCafeteriaReferenceUsesTheDayEaten(t *testing.T) {
	saturday := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	monday := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	weekdayOnly := []string{"オーダー 平日11:00~14:00"}
	if got := (menuFilter{meal: "lunch", day: saturday, reference: true}).itemWindows("650118", weekdayOnly, nil); len(got) != 0 {
		t.Fatalf("weekday category on Saturday: %v", got)
	}
	if got := (menuFilter{meal: "lunch", day: monday, reference: true}).itemWindows("650118", weekdayOnly, nil); len(got) != 1 {
		t.Fatalf("weekday category on Monday: %v", got)
	}
	plate := []string{"朝食プレート"}
	if got := (menuFilter{meal: "breakfast", day: monday, reference: true}).itemWindows("650111", plate, nil); len(got) != 1 {
		t.Fatalf("breakfast reference on a weekday: %v", got)
	}
	if got := (menuFilter{meal: "breakfast", day: saturday, reference: true}).itemWindows("650111", plate, nil); len(got) != 0 {
		t.Fatalf("breakfast reference on Saturday: %v", got)
	}
	if got := (menuFilter{meal: "breakfast", day: monday}).itemWindows("650111", plate, nil); len(got) != 0 {
		t.Fatalf("breakfast today needs confirmed hours: %v", got)
	}
}

func TestCafeteriaCardUsesOnlyReviewedStoreAndSharedServingHours(t *testing.T) {
	yen := 100
	a := menuCandidate{ID: "rice", Name: "Rice", Price: &yen, Nutrition: menuNutrition{Calories: 150}, Windows: []store.OpenInterval{{Start: "11:00", End: "14:00"}}}
	b := menuCandidate{ID: "tofu", Name: "Tofu", Price: &yen, Nutrition: menuNutrition{Calories: 50}, Windows: []store.OpenInterval{{Start: "12:00", End: "15:00"}}}
	q := menuResult{ID: "query", Date: "2026-11-16", Meal: "lunch", Stores: []candidateStore{
		{ID: "A", Hours: []string{"11:00–15:00"}, Items: []menuCandidate{a, b}}, {ID: "B", Hours: []string{"11:00–15:00"}, Items: []menuCandidate{a, b}},
	}}
	sel := assistant.MenuSelection{Reason: "Lunch", StoreIDs: []string{"A"}, Items: []assistant.MenuPortion{{ID: "tofu", Portions: 1}, {ID: "rice", Portions: 2}}}
	cards, err := q.cards([]assistant.MenuSelection{sel})
	if err != nil || len(cards) != 1 || len(cards[0].Stores) != 1 {
		t.Fatalf("cards: %+v %v", cards, err)
	}
	got := cards[0].Stores[0]
	if got.ID != "A" || got.Nutrition.Calories != 350 || got.Nutrition.ProteinG != nil || *got.Price != 300 || len(got.Hours) != 1 || got.Hours[0] != "12:00–14:00" {
		t.Fatalf("card store: %+v", got)
	}
	sel.Items[0].ID = "model-invented"
	if _, err = q.cards([]assistant.MenuSelection{sel}); err == nil {
		t.Fatal("invented id was accepted")
	}
}

func TestIntegrationBreakfastMenuCard(t *testing.T) {
	db := testdb.New(t)
	env := newAssistantEnv(t, db, DailyLimits{Normal: 20, Pro: 20, Site: 100})
	b := signUp(t, env.srv, "breakfast")
	const date = "2020-11-16" // Monday
	_, err := db.SQL.Exec(`
		INSERT INTO cafeteria_hours(cafeteria_id,menu_date,intervals,source_url,checked_at)
		VALUES ('650111','2020-11-16','[{"start":"08:00","end":"15:30"}]','https://www.s-coop.net/',now());
		WITH s AS (
			INSERT INTO menu_snapshots(cafeteria_id,menu_date,version,checked_at,digest)
			VALUES ('650111','2020-11-16',1,'2020-11-16T08:00:00+09:00','fixture') RETURNING id
		)
		INSERT INTO menu_items(snapshot_id,dish_code,name,categories,status,member_price_yen,source_url,calories,allergens)
		SELECT s.id, dish.code,dish.name,ARRAY['朝食プレート'],dish.status,300,'https://west2-univ.jp/sp/detail.php?t=650111&c='||dish.code,dish.calories,ARRAY['egg']
		FROM s CROSS JOIN (VALUES ('one','朝 A','unmarked',401),('two','朝 B','unmarked',425),('gone','朝 C','sold_out',444)) dish(code,name,status,calories)
	`)
	if err != nil {
		t.Fatal(err)
	}
	// Perform the same HTTP call the Python tool makes, using the token from the public request.
	env.ai.menuReply = func(req assistant.Request) (assistant.Reply, error) {
		call, err := http.NewRequest("GET", env.internal.URL+"/internal/agent/menu?date="+date+"&meal_type=breakfast&time=09:30", nil)
		if err != nil {
			return assistant.Reply{}, err
		}
		call.Header.Set("Authorization", "Bearer "+req.Token)
		res, err := http.DefaultClient.Do(call)
		if err != nil {
			return assistant.Reply{}, err
		}
		defer res.Body.Close()
		var menu menuResult
		if err = json.NewDecoder(res.Body).Decode(&menu); err != nil {
			return assistant.Reply{}, err
		}
		if res.StatusCode != 200 || menu.Status != "ready" || len(menu.Stores) != 1 || len(menu.Stores[0].Items) != 2 {
			return assistant.Reply{}, fmt.Errorf("menu query status %d: %+v", res.StatusCode, menu)
		}
		var alternatives []assistant.MenuPortion
		for _, item := range menu.Stores[0].Items {
			alternatives = append(alternatives, assistant.MenuPortion{ID: item.ID, Portions: 1})
		}
		meal := "breakfast"
		return assistant.Reply{Reply: "朝食", MealType: &meal, Recommendations: []assistant.Recommendation{}, MenuQueryID: menu.ID,
			MenuSelections: []assistant.MenuSelection{{Reason: "Breakfast before class", StoreIDs: []string{"650111"}, Items: alternatives}}}, nil
	}
	var reply assistantReplyJSON
	b.expect(200, "POST", "/api/assistant/messages", message(date, "朝食")).json(t, &reply)
	if len(reply.CafeteriaCards) != 1 {
		t.Fatalf("cards: %+v", reply)
	}
	card := reply.CafeteriaCards[0]
	if !card.BreakfastOptions || card.Name != "朝食プレート" || card.Date != date || card.Reference || len(card.Stores) != 1 {
		t.Fatalf("card: %+v", card)
	}
	s := card.Stores[0]
	if s.Name != "中央食堂" || len(s.Hours) != 1 || s.Hours[0] != "08:00–10:00" || len(s.Items) != 2 ||
		s.CheckedAt != "2020-11-16T08:00:00+09:00" || s.Nutrition != nil || s.Items[1].Nutrition.Calories != 425 ||
		s.Items[0].Nutrition.ProteinG != nil || len(s.Items[0].Allergens) != 1 || s.Items[0].Allergens[0] != (allergenLabel{"卵", "egg"}) {
		t.Fatalf("store: %+v", s)
	}
	// A later request cannot replay this query (even within the token's TTL).
	env.ai.menuReply = func(req assistant.Request) (assistant.Reply, error) {
		meal := "breakfast"
		return assistant.Reply{Reply: "old", MealType: &meal, MenuQueryID: "unissued", MenuSelections: []assistant.MenuSelection{{Reason: "replay"}}}, nil
	}
	b.expect(503, "POST", "/api/assistant/messages", message(date, "もう一度"))
}

func TestIntegrationMenuScopeAndTomorrow(t *testing.T) {
	db := testdb.New(t)
	scope := &MenuRequests{}
	tokens, err := auth.NewAgentTokens()
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(db.Gorm)
	internal := NewInternalRouter(InternalDeps{Logger: discardLogger(), AgentTokens: tokens, MenuRequests: scope, Store: st})
	token := tokens.Issue(12)
	now := time.Date(2026, 11, 16, 11, 45, 0, 0, tz.Tokyo)
	today := tz.Today(now)
	req := &menuRequest{target: today.AddDays(1), now: now, campus: "uji"}
	scope.active.Store(token, req)
	get := func(query string) (int, menuResult) {
		t.Helper()
		req := httptest.NewRequest("GET", "/internal/agent/menu?"+query, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		internal.ServeHTTP(w, req)
		var out menuResult
		if w.Code == 200 {
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
		}
		return w.Code, out
	}
	base := "date=2026-11-17&meal_type=lunch"
	if status, q := get(base); status != 200 || q.Status != "await_update" || len(q.Stores) != 0 {
		t.Fatalf("future without consent: %d %+v", status, q)
	}
	if status, _ := get("date=2026-11-16&meal_type=lunch"); status != 422 {
		t.Fatalf("different calendar target: %d", status)
	}
	if _, err := db.SQL.Exec(`
		INSERT INTO menu_snapshots(cafeteria_id,menu_date,version,checked_at,digest) VALUES
		('650116','2026-11-15',1,'2026-11-15T08:00:00+09:00','old'),
		('650111','2026-11-16',1,'2026-11-16T08:00:00+09:00','previous'),
		('650111','2026-11-16',2,'2026-11-16T10:30:00+09:00','new');
		INSERT INTO menu_items(snapshot_id,dish_code,name,categories,member_price_yen,status,source_url,calories)
		SELECT id,'dish-'||version,'dish',ARRAY['主菜'],300,'unmarked','https://west2-univ.jp/',350 FROM menu_snapshots
	`); err != nil {
		t.Fatal(err)
	}
	if status, q := get(base + "&use_today=true"); status != 200 || q.Status != "no_snapshot" || q.Date != "2026-11-16" || len(q.Unavailable) != 1 || q.Unavailable[0].ID != "650116" || q.Unavailable[0].LastCheckedAt != "2026-11-15T08:00:00+09:00" {
		t.Fatalf("home campus today, no stale fallback: %d %+v", status, q)
	}
	status, q := get(base + "&use_today=true&cafeteria=650111")
	if status != 200 || q.Status != "ready" || len(q.Stores) != 1 || len(q.Stores[0].Items) != 1 {
		t.Fatalf("specified store: %d %+v", status, q)
	}
	if q.Stores[0].Items[0].ID != "dish-2" || len(q.Stores[0].Hours) != 0 || q.Stores[0].HoursURL == "" {
		t.Fatalf("latest revision and unknown hours: %+v", q.Stores[0])
	}
	lunch := "lunch"
	r := assistant.Reply{MealType: &lunch, MenuQueryID: q.ID, MenuSelections: []assistant.MenuSelection{{Reason: "one", Items: []assistant.MenuPortion{{ID: "dish-2", Portions: 1}}, StoreIDs: []string{"650111"}}}}
	cards, err := req.render(r)
	if err != nil || len(cards) != 1 || !cards[0].Reference || cards[0].Date != "2026-11-16" {
		t.Fatalf("tomorrow's reference: %+v %v", cards, err)
	}
	if _, err := db.SQL.Exec(`INSERT INTO cafeteria_hours(cafeteria_id, menu_date, intervals, source_url, checked_at) VALUES ('650111','2026-11-16','[]','https://www.s-coop.net/',now())`); err != nil {
		t.Fatal(err)
	}
	// Closed today says nothing about tomorrow: the reference keeps the dish with unknown hours.
	if status, q := get(base + "&use_today=true&campus=all"); status != 200 || q.Status != "ready" || len(q.Stores[0].Hours) != 0 {
		t.Fatalf("today's closure applied to tomorrow: %d %+v", status, q)
	}
	if _, err = req.render(r); err == nil {
		t.Fatal("earlier menu query reused")
	}
}

func TestAllergenLabels(t *testing.T) {
	got := allergenLabels([]string{"egg", "wheat_origin", "new_marker"}, true)
	want := []allergenLabel{{"卵", "egg"}, {"小麦由来", "derived from wheat"}, {"new_marker", "new_marker"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("labels: %v", got)
	}
	if allergenLabels(nil, false) != nil || allergenLabels(nil, true) == nil {
		t.Fatal("unknown and none must stay apart")
	}
}
