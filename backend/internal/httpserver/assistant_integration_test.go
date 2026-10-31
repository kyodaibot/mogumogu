package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"mogumogu/backend/internal/assistant"
	"mogumogu/backend/internal/auth"
	"mogumogu/backend/internal/store"
	"mogumogu/backend/internal/testdb"
	"mogumogu/backend/internal/tz"
)

// fakeAI stands in for the AI service and records what Go sent it.
type fakeAI struct {
	mu       sync.Mutex
	requests []assistant.Request
	fail     bool
}

func (f *fakeAI) Reply(_ context.Context, req assistant.Request) (assistant.Reply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	if f.fail {
		return assistant.Reply{}, errors.New("ai service: status 503 invalid_output")
	}
	lunch := "lunch"
	return assistant.Reply{
		Reply: "这是三个方案：",
		Recommendations: []assistant.Recommendation{{Name: "a", Description: "b", Reason: "c", Estimate: assistant.Estimate{
			Serving: assistant.Nutrition{Calories: 650, ProteinG: 35, FatG: 18, CarbsG: 85},
			Per100g: assistant.Nutrition{Calories: 163, ProteinG: 9, FatG: 4, CarbsG: 21},
		}}},
		MealType: &lunch,
	}, nil
}

func (f *fakeAI) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeAI) last() assistant.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

type assistantEnv struct {
	srv      *httptest.Server
	internal *httptest.Server
	ai       *fakeAI
	tokens   *auth.AgentTokens
	store    *store.Store
}

func newAssistantEnv(t *testing.T, db *testdb.DB, limits DailyLimits) assistantEnv {
	t.Helper()
	tokens, err := auth.NewAgentTokens()
	if err != nil {
		t.Fatal(err)
	}
	ai := &fakeAI{}
	st := store.New(db.Gorm)
	srv := httptest.NewServer(NewRouter(Deps{
		Logger:           discardLogger(),
		DB:               db.SQL,
		Store:            st,
		Sessions:         auth.NewSessions(itJWTSecret, "mogumogu", "mogumogu-web", time.Hour),
		CSRF:             auth.NewCSRF(itCSRFSecret),
		LoginLimiter:     auth.NewLimiter(5, 15*time.Minute),
		RegisterLimiter:  auth.NewLimiter(1000, time.Hour),
		AssistantLimiter: auth.NewLimiter(6, time.Minute),
		AllowedOrigins:   []string{testOrigin},
		Assistant:        ai,
		AgentTokens:      tokens,
		AssistantLimits:  limits,
	}))
	internal := httptest.NewServer(NewInternalRouter(InternalDeps{Logger: discardLogger(), Store: st, AgentTokens: tokens}))
	t.Cleanup(srv.Close)
	t.Cleanup(internal.Close)
	return assistantEnv{srv: srv, internal: internal, ai: ai, tokens: tokens, store: st}
}

func fmtJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func message(date, text string) map[string]string {
	return map[string]string{"date": date, "message": text}
}

func TestIntegrationAssistantContext(t *testing.T) {
	env := newAssistantEnv(t, testdb.New(t), DailyLimits{Normal: 30, Pro: 100, Site: 300})
	b := signUp(t, env.srv, "asker")
	today := tz.Today(time.Now())
	day := func(n int) string { return today.AddDays(n).String() }
	at := func(n int, clock string) string { return day(n) + "T" + clock + ":00+09:00" }

	// No profile yet: the context says so with null.
	b.expect(200, "POST", "/api/assistant/messages", message(day(0), "午餐吃什么"))
	if p := env.ai.last().Context.Profile; p != nil {
		t.Fatalf("profile without one saved: %#v", p)
	}

	b.expect(200, "PUT", "/api/profile", map[string]any{
		"height_cm": 170, "weight_kg": 60, "age": 21, "has_exercise_habit": true,
		"goal": "build_muscle", "allergens": []string{"虾"},
	})
	for _, e := range []struct{ title, start, end string }{
		{"前一天", at(-1, "10:00"), at(-1, "11:00")},
		{"当天", at(0, "13:00"), at(0, "16:00")},
		{"后一天", at(1, "09:00"), at(1, "10:00")},
		{"两天前", at(-2, "09:00"), at(-2, "10:00")},
		{"两天后", at(2, "09:00"), at(2, "10:00")},
	} {
		b.expect(201, "POST", "/api/schedule-events", map[string]string{"title": e.title, "starts_at": e.start, "ends_at": e.end})
	}
	for _, m := range []struct{ date, content string }{
		{day(-7), "八天前"}, {day(-6), "七天前"}, {day(0), "今天"}, {day(1), "明天的计划"},
	} {
		b.expect(201, "POST", "/api/meal-records", map[string]string{"date": m.date, "meal_type": "lunch", "content": m.content})
	}

	var got assistant.Reply
	b.expect(200, "POST", "/api/assistant/messages", message(day(0), "  午餐吃什么  ")).json(t, &got)
	if got.Reply != "这是三个方案：" || len(got.Recommendations) != 1 || got.MealType == nil || *got.MealType != "lunch" ||
		got.Recommendations[0].Estimate.Serving.Calories != 650 || got.Recommendations[0].Estimate.Per100g.CarbsG != 21 {
		t.Fatalf("reply passed through: %+v", got)
	}

	req := env.ai.last()
	ctx := req.Context
	if req.Message != "午餐吃什么" || ctx.TargetDate != day(0) || ctx.Timezone != "Asia/Tokyo" || !strings.HasSuffix(ctx.Now, "+09:00") ||
		ctx.Locale != "en" {
		t.Fatalf("request: %q %+v", req.Message, ctx)
	}
	if !strings.Contains(fmtJSON(t, ctx.Profile), `"allergens":["虾"]`) {
		t.Fatalf("profile: %s", fmtJSON(t, ctx.Profile))
	}
	if ctx.EventsRange != (assistant.Range{From: day(-1), To: day(1)}) {
		t.Fatalf("events range: %+v", ctx.EventsRange)
	}
	if s := fmtJSON(t, ctx.Events); !strings.Contains(s, "前一天") || !strings.Contains(s, "后一天") ||
		strings.Contains(s, "两天前") || strings.Contains(s, "两天后") {
		t.Fatalf("events: %s", s)
	}
	if ctx.MealsRange != (assistant.Range{From: day(-6), To: day(0)}) {
		t.Fatalf("meals range: %+v", ctx.MealsRange)
	}
	if s := fmtJSON(t, ctx.Meals); !strings.Contains(s, "七天前") || !strings.Contains(s, "今天") ||
		strings.Contains(s, "八天前") || strings.Contains(s, "明天的计划") {
		t.Fatalf("meals: %s", s)
	}

	// A future target date: meals still end today, events follow the target.
	b.expect(200, "POST", "/api/assistant/messages", message(day(10), "晚餐吃什么"))
	ctx = env.ai.last().Context
	if ctx.MealsRange != (assistant.Range{From: day(-6), To: day(0)}) || ctx.EventsRange.From != day(9) {
		t.Fatalf("future target: meals %+v events %+v", ctx.MealsRange, ctx.EventsRange)
	}

	// The interface language is passed on for the reply.
	b.expect(200, "POST", "/api/assistant/messages", map[string]string{"date": day(0), "message": "昼食は？", "locale": "ja"})
	if l := env.ai.last().Context.Locale; l != "ja" {
		t.Fatalf("locale: %q", l)
	}

	// The token Go hands over belongs to this user.
	if _, err := env.tokens.Verify(env.ai.last().Token); err != nil {
		t.Fatalf("token: %v", err)
	}
}

func TestIntegrationAssistantValidation(t *testing.T) {
	env := newAssistantEnv(t, testdb.New(t), DailyLimits{Normal: 30, Pro: 100, Site: 300})

	anon := newBrowser(t, env.srv)
	anon.expect(200, "GET", "/api/auth/csrf", nil)
	if r := anon.do("POST", "/api/assistant/messages", message("2026-10-27", "hi")); r.status != 401 {
		t.Fatalf("anonymous: %d", r.status)
	}

	b := signUp(t, env.srv, "validator")
	for name, body := range map[string]any{
		"no date":    map[string]string{"message": "hi"},
		"bad date":   message("2026-02-30", "hi"),
		"blank":      message("2026-10-27", "   "),
		"501 chars":  message("2026-10-27", strings.Repeat("长", 501)),
		"no message": map[string]string{"date": "2026-10-27"},
		"bad locale": map[string]string{"date": "2026-10-27", "message": "hi", "locale": "zh"},
	} {
		if r := b.do("POST", "/api/assistant/messages", body); r.status != 422 {
			t.Errorf("%s: %d %s", name, r.status, r.body)
		}
	}
	b.expect(200, "POST", "/api/assistant/messages", message("2026-10-27", strings.Repeat("长", 500)))
	if env.ai.calls() != 1 {
		t.Fatalf("AI service called %d times, want 1", env.ai.calls())
	}
}

func TestIntegrationAssistantQuota(t *testing.T) {
	env := newAssistantEnv(t, testdb.New(t), DailyLimits{Normal: 3, Pro: 100, Site: 5})
	alice := signUp(t, env.srv, "alice")
	bob := signUp(t, env.srv, "bob")
	send := func(b *browser) resp {
		return b.do("POST", "/api/assistant/messages", message("2026-10-27", "吃什么"))
	}

	for i := range 3 {
		if r := send(alice); r.status != 200 {
			t.Fatalf("alice message %d: %d", i+1, r.status)
		}
	}
	r := send(alice)
	var e errResp
	r.json(t, &e)
	if r.status != 429 || e.Error.Code != "quota_exceeded" || e.Error.Limit != 3 || r.header.Get("Retry-After") == "" {
		t.Fatalf("alice over her limit: %d %s", r.status, r.body)
	}
	if env.ai.calls() != 3 {
		t.Fatalf("AI service called %d times after a rejected message, want 3", env.ai.calls())
	}

	// A failed answer is refunded: bob can still send two, which reach the site limit.
	env.ai.fail = true
	if r := send(bob); r.status != 503 || r.errCode(t) != "ai_unavailable" {
		t.Fatalf("failed answer: %d %s", r.status, r.body)
	}
	env.ai.fail = false
	for i := range 2 {
		if r := send(bob); r.status != 200 {
			t.Fatalf("bob message %d: %d %s", i+1, r.status, r.body)
		}
	}
	// The site's limit, not bob's: no number for him.
	r = send(bob)
	e = errResp{}
	r.json(t, &e)
	if r.status != 429 || e.Error.Code != "quota_exceeded" || e.Error.Limit != 0 {
		t.Fatalf("site limit: %d %s", r.status, r.body)
	}
}

func TestIntegrationAssistantTiers(t *testing.T) {
	env := newAssistantEnv(t, testdb.New(t), DailyLimits{Normal: 1, Pro: 3, Site: 100})
	// Six messages in all, which is also the per-minute limit.
	carol := signUp(t, env.srv, "carol")
	send := func() resp {
		return carol.do("POST", "/api/assistant/messages", message("2026-10-27", "吃什么"))
	}
	limitOf := func(r resp) int {
		t.Helper()
		var e errResp
		r.json(t, &e)
		if r.status != 429 || e.Error.Code != "quota_exceeded" {
			t.Fatalf("want quota_exceeded: %d %s", r.status, r.body)
		}
		return e.Error.Limit
	}
	tierOf := func() string {
		t.Helper()
		var me meJSON
		carol.expect(200, "GET", "/api/auth/me", nil).json(t, &me)
		return me.User.Tier
	}
	ctx := context.Background()

	if tier := tierOf(); tier != "normal" {
		t.Fatalf("new user's tier: %q", tier)
	}
	carol.expect(200, "POST", "/api/assistant/messages", message("2026-10-27", "吃什么"))
	if limit := limitOf(send()); limit != 1 {
		t.Fatalf("normal limit: %d", limit)
	}

	// Upgraded the same day: the messages already sent still count, and the
	// change applies without logging in again.
	if err := env.store.SetUserTier(ctx, "carol", store.TierPro); err != nil {
		t.Fatal(err)
	}
	if tier := tierOf(); tier != "pro" {
		t.Fatalf("tier after upgrade: %q", tier)
	}
	for range 2 {
		carol.expect(200, "POST", "/api/assistant/messages", message("2026-10-27", "吃什么"))
	}
	if limit := limitOf(send()); limit != 3 {
		t.Fatalf("pro limit: %d", limit)
	}

	if err := env.store.SetUserTier(ctx, "carol", store.TierNormal); err != nil {
		t.Fatal(err)
	}
	if limit := limitOf(send()); limit != 1 {
		t.Fatalf("limit after downgrade: %d", limit)
	}
	if env.ai.calls() != 3 {
		t.Fatalf("AI service called %d times, want 3", env.ai.calls())
	}

	if err := env.store.SetUserTier(ctx, "nobody", store.TierPro); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
	// The database refuses tiers that do not exist, whatever the caller.
	if err := env.store.SetUserTier(ctx, "carol", store.Tier("gold")); err == nil {
		t.Fatal("tier gold was stored")
	}
}

// turn is one entry of a request's history, as the browser sends it.
func turn(date, msg, reply string, dishes ...string) map[string]any {
	recs := []map[string]string{}
	for _, d := range dishes {
		recs = append(recs, map[string]string{"name": d, "description": d + "の説明"})
	}
	return map[string]any{"date": date, "message": msg, "reply": reply, "recommendations": recs}
}

func withHistory(date, msg string, history ...map[string]any) map[string]any {
	return map[string]any{"date": date, "message": msg, "history": history}
}

func TestIntegrationAssistantHistory(t *testing.T) {
	env := newAssistantEnv(t, testdb.New(t), DailyLimits{Normal: 30, Pro: 100, Site: 300})
	b := signUp(t, env.srv, "talker")

	// A new conversation: the AI service gets an empty history, not null.
	b.expect(200, "POST", "/api/assistant/messages", message("2026-10-31", "明日の昼食は？"))
	if h := env.ai.last().History; h == nil || len(h) != 0 {
		t.Fatalf("no history: %#v", h)
	}

	// Earlier turns are passed on in order, trimmed like the message.
	b.expect(200, "POST", "/api/assistant/messages", withHistory("2026-11-01", "ご飯なしで",
		turn("2026-10-31", " 明日の昼食は？ ", "3つ提案します。", "親子丼", "焼き鮭定食", "カレー"),
		turn("2026-11-01", "あさっては？", "何食ですか？"),
	))
	h := env.ai.last().History
	want := []assistant.Turn{
		{Date: "2026-10-31", Message: "明日の昼食は？", Reply: "3つ提案します。", Recommendations: []assistant.TurnDish{
			{Name: "親子丼", Description: "親子丼の説明"}, {Name: "焼き鮭定食", Description: "焼き鮭定食の説明"}, {Name: "カレー", Description: "カレーの説明"},
		}},
		{Date: "2026-11-01", Message: "あさっては？", Reply: "何食ですか？", Recommendations: []assistant.TurnDish{}},
	}
	if fmtJSON(t, h) != fmtJSON(t, want) {
		t.Fatalf("history:\n got %s\nwant %s", fmtJSON(t, h), fmtJSON(t, want))
	}

	// The longest valid conversation: nine turns at every length limit. With
	// 4-byte characters it is over the general 64 KB body limit.
	long := func(n int) string { return strings.Repeat("😀", n) }
	full := make([]map[string]any, 9)
	for i := range full {
		full[i] = map[string]any{"date": "2026-10-31", "message": long(500), "reply": long(1000), "recommendations": []map[string]string{
			{"name": long(80), "description": long(200)}, {"name": long(80), "description": long(200)}, {"name": long(80), "description": long(200)},
		}}
	}
	if body := fmtJSON(t, withHistory("2026-10-31", long(500), full...)); len(body) <= 64<<10 {
		t.Fatalf("test body is only %d bytes", len(body))
	}
	b.expect(200, "POST", "/api/assistant/messages", withHistory("2026-10-31", long(500), full...))
	calls := env.ai.calls()

	tooBig := map[string]any{"date": "2026-10-31", "message": "hi", "padding": strings.Repeat("x", 128<<10)}
	if r := b.do("POST", "/api/assistant/messages", tooBig); r.status != 413 {
		t.Fatalf("over 128 KB: %d", r.status)
	}

	ok := turn("2026-10-31", "q", "a")
	for name, c := range map[string]struct {
		body  map[string]any
		field string
	}{
		"ten turns":         {withHistory("2026-10-31", "hi", append(full, ok)...), "history"},
		"no date":           {withHistory("2026-10-31", "hi", map[string]any{"message": "q", "reply": "a"}), "history[0].date"},
		"bad date":          {withHistory("2026-10-31", "hi", ok, turn("2026-02-30", "q", "a")), "history[1].date"},
		"blank message":     {withHistory("2026-10-31", "hi", turn("2026-10-31", "  ", "a")), "history[0].message"},
		"long reply":        {withHistory("2026-10-31", "hi", turn("2026-10-31", "q", long(1001))), "history[0].reply"},
		"two dishes":        {withHistory("2026-10-31", "hi", turn("2026-10-31", "q", "a", "x", "y")), "history[0].recommendations"},
		"long dish name":    {withHistory("2026-10-31", "hi", turn("2026-10-31", "q", "a", "x", "y", long(81))), "history[0].recommendations[2].name"},
		"blank description": {withHistory("2026-10-31", "hi", map[string]any{"date": "2026-10-31", "message": "q", "reply": "a", "recommendations": []map[string]string{{"name": "x", "description": " "}, {"name": "x", "description": "d"}, {"name": "x", "description": "d"}}}), "history[0].recommendations[0].description"},
	} {
		r := b.do("POST", "/api/assistant/messages", c.body)
		var e struct {
			Error struct {
				Details []fieldError `json:"details"`
			} `json:"error"`
		}
		r.json(t, &e)
		if r.status != 422 || len(e.Error.Details) == 0 || e.Error.Details[0].Field != c.field {
			t.Errorf("%s: %d %s", name, r.status, r.body)
		}
	}
	if env.ai.calls() != calls {
		t.Fatalf("rejected requests reached the AI service")
	}
}

func TestIntegrationAssistantRateLimit(t *testing.T) {
	db := testdb.New(t)
	env := newAssistantEnv(t, db, DailyLimits{Normal: 30, Pro: 100, Site: 300})
	alice := signUp(t, env.srv, "alice")
	bob := signUp(t, env.srv, "bob")
	send := func(b *browser) resp {
		return b.do("POST", "/api/assistant/messages", message("2026-10-27", "吃什么"))
	}

	for i := range 6 {
		if r := send(alice); r.status != 200 {
			t.Fatalf("message %d: %d %s", i+1, r.status, r.body)
		}
	}
	r := send(alice)
	if r.status != 429 || r.errCode(t) != "rate_limited" || r.header.Get("Retry-After") == "" {
		t.Fatalf("seventh message within a minute: %d %s", r.status, r.body)
	}

	// The refused message neither reached the AI service nor used the quota.
	var used int
	if err := db.SQL.QueryRow(`SELECT COALESCE(SUM(messages), 0) FROM assistant_usage`).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if env.ai.calls() != 6 || used != 6 {
		t.Fatalf("after a refused message: %d calls, %d counted", env.ai.calls(), used)
	}

	// The limit is per user.
	if r := send(bob); r.status != 200 {
		t.Fatalf("another user: %d %s", r.status, r.body)
	}
}

func TestIntegrationAgentInternalAPI(t *testing.T) {
	env := newAssistantEnv(t, testdb.New(t), DailyLimits{Normal: 30, Pro: 100, Site: 300})
	alice := signUp(t, env.srv, "alice")
	bob := signUp(t, env.srv, "bob")
	alice.expect(201, "POST", "/api/meal-records", map[string]string{"date": "2026-10-03", "meal_type": "dinner", "content": "alice 的烤鱼"})
	bob.expect(201, "POST", "/api/meal-records", map[string]string{"date": "2026-10-03", "meal_type": "dinner", "content": "bob 的拉面"})

	// Each user's message yields a token for that user only.
	alice.expect(200, "POST", "/api/assistant/messages", message("2026-10-27", "吃什么"))
	aliceToken := env.ai.last().Token

	get := func(path, token string) resp {
		req, _ := http.NewRequest("GET", env.internal.URL+path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return resp{status: res.StatusCode, body: body}
	}

	r := get("/internal/agent/meals?from=2026-10-01&to=2026-10-31", aliceToken)
	if r.status != 200 || !strings.Contains(string(r.body), "alice 的烤鱼") || strings.Contains(string(r.body), "bob") {
		t.Fatalf("alice's meals: %d %s", r.status, r.body)
	}
	if r := get("/internal/agent/events?from=2026-10-01&to=2026-10-31", aliceToken); r.status != 200 {
		t.Fatalf("events: %d %s", r.status, r.body)
	}
	if r := get("/internal/agent/meals?from=2026-10-01&to=2026-11-01", aliceToken); r.status != 422 {
		t.Fatalf("32 days: %d %s", r.status, r.body)
	}

	parts := strings.Split(aliceToken, ".")
	for name, token := range map[string]string{
		"missing":        "",
		"forged":         "1.9999999999.AAAA",
		"other user":     "2." + parts[1] + "." + parts[2],
		"session cookie": alice.cookie(sessionCookie, "/"),
	} {
		if r := get("/internal/agent/meals?from=2026-10-01&to=2026-10-31", token); r.status != 401 {
			t.Errorf("%s token: %d", name, r.status)
		}
	}

	// The internal API is not part of the public router.
	if r := alice.do("GET", "/internal/agent/meals?from=2026-10-01&to=2026-10-31", nil); r.status != 404 {
		t.Fatalf("public router serves the internal API: %d", r.status)
	}
}

func TestIntegrationAssistantQuotaConcurrent(t *testing.T) {
	env := newAssistantEnv(t, testdb.New(t), DailyLimits{Normal: 3, Pro: 100, Site: 300})
	b := signUp(t, env.srv, "racer")

	var wg sync.WaitGroup
	var mu sync.Mutex
	statuses := map[int]int{}
	for range 10 {
		wg.Go(func() {
			r := b.do("POST", "/api/assistant/messages", message("2026-10-27", "吃什么"))
			mu.Lock()
			statuses[r.status]++
			mu.Unlock()
		})
	}
	wg.Wait()
	if statuses[200] != 3 || statuses[429] != 7 {
		t.Fatalf("10 concurrent messages with a limit of 3: %v", statuses)
	}
}
