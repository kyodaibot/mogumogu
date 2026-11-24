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
	lunchOnly := []store.OpenInterval{{Start: "11:00", End: "14:00"}}
	tests := []struct {
		meal, shop, category string
		hours                []store.OpenInterval
		want                 bool
	}{
		{"breakfast", "650111", "朝食プレート", open, true},
		{"breakfast", "650113", "朝食プレート", open, false},
		{"lunch", "650111", "朝食プレート", open, false},
		{"lunch", "650111", "夜の主菜メニュー", open, false},
		{"dinner", "650113", "夜の主菜メニュー", open, true},
		{"dinner", "650113", "主菜", open, true},
		{"dinner", "650113", "主菜", lunchOnly, false},
		{"lunch", "650113", "主菜", []store.OpenInterval{}, false}, // regularly closed that day
		{"lunch", "650113", "主菜", nil, true},                     // hours unknown
		{"lunch", "650118", "オーダー 平日11:00~14:00 17:30~20:00　土曜閉店", open, true},
	}
	for _, c := range tests {
		q := menuFilter{meal: c.meal, day: monday}
		if got := q.itemWindows(c.shop, []string{c.category}, c.hours); (len(got) > 0) != c.want {
			t.Errorf("%+v: %v", c, got)
		}
	}
}

// Weekday rules follow the day eaten; national holidays count as weekdays.
func TestCafeteriaRulesFollowTheDayEaten(t *testing.T) {
	saturday := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	monday := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	weekdayOnly := []string{"オーダー 平日11:00~14:00"}
	if got := (menuFilter{meal: "lunch", day: saturday}).itemWindows("650118", weekdayOnly, nil); len(got) != 0 {
		t.Fatalf("weekday category on Saturday: %v", got)
	}
	if got := (menuFilter{meal: "lunch", day: monday}).itemWindows("650118", weekdayOnly, nil); len(got) != 1 {
		t.Fatalf("weekday category on Monday: %v", got)
	}
	plate := []string{"朝食プレート"}
	if got := (menuFilter{meal: "breakfast", day: monday}).itemWindows("650111", plate, nil); len(got) != 1 {
		t.Fatalf("breakfast on a weekday with unknown hours: %v", got)
	}
	if got := (menuFilter{meal: "breakfast", day: saturday}).itemWindows("650111", plate, nil); len(got) != 0 {
		t.Fatalf("breakfast on Saturday: %v", got)
	}
}

func TestRegularHoursByWeekday(t *testing.T) {
	monday := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC) // a national holiday: a weekday
	saturday := monday.AddDate(0, 0, 5)
	sunday := monday.AddDate(0, 0, 6)
	tests := []struct {
		shop string
		day  time.Time
		want string
	}{
		{"650111", monday, "[{08:00 15:15}]"},
		{"650111", saturday, "[]"},
		{"650113", saturday, "[{11:00 14:00}]"},
		{"650120", monday, "[{11:00 14:00} {18:00 20:00}]"},
		{"650118", sunday, "[]"},
	}
	for _, c := range tests {
		got, url := hoursOn(c.shop, "yoshida", c.day)
		if fmt.Sprint(got) != c.want || got == nil || url == "" {
			t.Errorf("%s on %s: %v %s", c.shop, c.day.Weekday(), got, url)
		}
	}
	if got, url := hoursOn("999999", "uji", monday); got != nil || url != "https://www.s-coop.net/shop_guide/uji_shop/" {
		t.Errorf("unknown store: %v %s", got, url)
	}
	// 中央食堂 serves breakfast, not dinner (it closes at 15:15).
	hours, _ := hoursOn("650111", "yoshida", monday)
	if got := (menuFilter{meal: "breakfast", day: monday}).itemWindows("650111", []string{"朝食プレート"}, hours); fmt.Sprint(got) != "[{08:00 10:00}]" {
		t.Errorf("中央食堂 breakfast: %v", got)
	}
	if got := (menuFilter{meal: "dinner", day: monday}).itemWindows("650111", []string{"主菜"}, hours); len(got) != 0 {
		t.Errorf("中央食堂 dinner: %v", got)
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
		call, err := http.NewRequest("GET", env.internal.URL+"/internal/agent/menu?date="+date+"&meal_type=breakfast", nil)
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
	if !card.BreakfastOptions || card.Name != "朝食プレート" || card.Date != date || len(card.Stores) != 1 {
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
	now := time.Date(2026, 11, 16, 11, 45, 0, 0, tz.Tokyo) // Monday
	req := &menuRequest{now: now, campus: "uji"}
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
	base := "date=2026-11-17&meal_type=lunch" // tomorrow
	if status, q := get(base); status != 200 || q.Status != "no_snapshot" || q.Date != "2026-11-17" || len(q.Unavailable) != 1 {
		t.Fatalf("tomorrow with no menu at all: %d %+v", status, q)
	}
	if status, _ := get("date=tomorrow&meal_type=lunch"); status != 422 {
		t.Fatalf("invalid date: %d", status)
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
	// Tomorrow uses the latest recent menu, also for the home campus.
	if status, q := get(base); status != 200 || q.Status != "ready" || q.Date != "2026-11-17" || q.Stores[0].ID != "650116" || q.Stores[0].MenuDate != "2026-11-15" {
		t.Fatalf("home campus tomorrow: %d %+v", status, q)
	}
	status, q := get(base + "&cafeteria=650111")
	if status != 200 || q.Status != "ready" || len(q.Stores) != 1 || len(q.Stores[0].Items) != 1 {
		t.Fatalf("specified store: %d %+v", status, q)
	}
	if q.Stores[0].Items[0].ID != "dish-2" || q.Stores[0].MenuDate != "2026-11-16" || fmt.Sprint(q.Stores[0].Hours) != "[10:00–15:00]" || q.Stores[0].HoursURL == "" {
		t.Fatalf("latest revision of today's menu, tomorrow's regular hours: %+v", q.Stores[0])
	}
	lunch := "lunch"
	r := assistant.Reply{MealType: &lunch, MenuQueryID: q.ID, MenuSelections: []assistant.MenuSelection{{Reason: "one", Items: []assistant.MenuPortion{{ID: "dish-2", Portions: 1}}, StoreIDs: []string{"650111"}}}}
	cards, err := req.render(r)
	if err != nil || len(cards) != 1 || cards[0].Date != "2026-11-17" || cards[0].Stores[0].MenuDate != "2026-11-16" {
		t.Fatalf("tomorrow's card: %+v %v", cards, err)
	}
	// Tomorrow's own menu, once imported, takes over.
	if _, err := db.SQL.Exec(`
		WITH s AS (INSERT INTO menu_snapshots(cafeteria_id,menu_date,version,checked_at,digest)
			VALUES ('650116','2026-11-17',1,'2026-11-17T08:00:00+09:00','tomorrow') RETURNING id)
		INSERT INTO menu_items(snapshot_id,dish_code,name,categories,member_price_yen,status,source_url,calories)
		SELECT id,'dish-t','dish',ARRAY['主菜'],300,'unmarked','https://west2-univ.jp/',350 FROM s
	`); err != nil {
		t.Fatal(err)
	}
	if status, q := get(base); status != 200 || q.Status != "ready" || q.Stores[0].MenuDate != "2026-11-17" {
		t.Fatalf("tomorrow's own menu: %d %+v", status, q)
	}
	// Closed on the day eaten (Sunday), whichever menu is used.
	if status, q := get("date=2026-11-22&meal_type=lunch&cafeteria=650111"); status != 200 || q.Status != "no_candidates" {
		t.Fatalf("closed on Sunday: %d %+v", status, q)
	}
	// A past day uses only its own menu.
	if status, q := get("date=2026-11-15&meal_type=lunch&cafeteria=650111"); status != 200 || q.Status != "no_snapshot" {
		t.Fatalf("past day: %d %+v", status, q)
	}
	if _, err = req.render(r); err == nil {
		t.Fatal("earlier menu query reused")
	}
}

// Today, a store without today's menu falls back to its latest one from the
// previous three days; today's regular hours apply.
func TestIntegrationMenuFallsBackToRecentMenu(t *testing.T) {
	db := testdb.New(t)
	scope := &MenuRequests{}
	tokens, err := auth.NewAgentTokens()
	if err != nil {
		t.Fatal(err)
	}
	internal := NewInternalRouter(InternalDeps{Logger: discardLogger(), AgentTokens: tokens, MenuRequests: scope, Store: store.New(db.Gorm)})
	token := tokens.Issue(12)
	now := time.Date(2026, 11, 16, 11, 45, 0, 0, tz.Tokyo) // Monday
	req := &menuRequest{now: now, campus: "yoshida"}
	scope.active.Store(token, req)
	if _, err := db.SQL.Exec(`
		INSERT INTO menu_snapshots(cafeteria_id,menu_date,version,checked_at,digest) VALUES
		('650111','2026-11-16',1,'2026-11-16T08:00:00+09:00','today'),
		('650112','2026-11-14',1,'2026-11-14T08:00:00+09:00','older'),
		('650112','2026-11-15',1,'2026-11-15T10:30:00+09:00','yesterday'),
		('650113','2026-11-12',1,'2026-11-12T10:30:00+09:00','too old');
		INSERT INTO menu_items(snapshot_id,dish_code,name,categories,member_price_yen,status,source_url,calories)
		SELECT id,'curry','カレー',ARRAY['主菜'],400,'unmarked','https://west2-univ.jp/',700 FROM menu_snapshots
	`); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/internal/agent/menu?date=2026-11-16&meal_type=lunch", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	internal.ServeHTTP(w, r)
	var q menuResult
	if err := json.Unmarshal(w.Body.Bytes(), &q); w.Code != 200 || err != nil {
		t.Fatalf("menu query: %d %s", w.Code, w.Body)
	}
	if q.Status != "ready" || q.Date != "2026-11-16" || len(q.Stores) != 2 {
		t.Fatalf("query: %+v", q)
	}
	// Today's menu first; the fallback is the latest snapshot, with today's regular hours.
	today, fallback := q.Stores[0], q.Stores[1]
	if today.ID != "650111" || today.MenuDate != "2026-11-16" || len(today.Hours) != 1 {
		t.Fatalf("today's store: %+v", today)
	}
	if fallback.ID != "650112" || fallback.MenuDate != "2026-11-15" || fmt.Sprint(fallback.Hours) != "[11:00–14:00]" || fallback.CheckedAt != "2026-11-15T10:30:00+09:00" {
		t.Fatalf("fallback store: %+v", fallback)
	}
	unavailable := map[string]missingStore{}
	for _, m := range q.Unavailable {
		unavailable[m.ID] = m
	}
	if len(unavailable) != 3 || unavailable["650113"].LastCheckedAt != "2026-11-12T10:30:00+09:00" || unavailable["650118"].LastCheckedAt != "" {
		t.Fatalf("unavailable: %+v", q.Unavailable)
	}
	lunch := "lunch"
	cards, err := req.render(assistant.Reply{MealType: &lunch, MenuQueryID: q.ID, MenuSelections: []assistant.MenuSelection{
		{Reason: "curry", Items: []assistant.MenuPortion{{ID: "curry", Portions: 1}}, StoreIDs: []string{"650111", "650112"}}}})
	if err != nil || len(cards) != 1 || cards[0].Date != "2026-11-16" || len(cards[0].Stores) != 2 ||
		cards[0].Stores[0].MenuDate != "2026-11-16" || cards[0].Stores[1].MenuDate != "2026-11-15" {
		t.Fatalf("card: %+v %v", cards, err)
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
