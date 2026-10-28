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
	return assistant.Reply{
		Reply:           "这是三个方案：",
		Recommendations: []assistant.Recommendation{{Name: "a", Description: "b", Reason: "c"}},
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
}

func newAssistantEnv(t *testing.T, db *testdb.DB, userLimit, siteLimit int) assistantEnv {
	t.Helper()
	tokens, err := auth.NewAgentTokens()
	if err != nil {
		t.Fatal(err)
	}
	ai := &fakeAI{}
	st := store.New(db.Gorm)
	srv := httptest.NewServer(NewRouter(Deps{
		Logger:             discardLogger(),
		DB:                 db.SQL,
		Store:              st,
		Sessions:           auth.NewSessions(itJWTSecret, "mogumogu", "mogumogu-web", time.Hour),
		CSRF:               auth.NewCSRF(itCSRFSecret),
		LoginLimiter:       auth.NewLimiter(5, 15*time.Minute),
		RegisterLimiter:    auth.NewLimiter(1000, time.Hour),
		AllowedOrigins:     []string{testOrigin},
		Assistant:          ai,
		AgentTokens:        tokens,
		AssistantUserLimit: userLimit,
		AssistantSiteLimit: siteLimit,
	}))
	internal := httptest.NewServer(NewInternalRouter(InternalDeps{Logger: discardLogger(), Store: st, AgentTokens: tokens}))
	t.Cleanup(srv.Close)
	t.Cleanup(internal.Close)
	return assistantEnv{srv: srv, internal: internal, ai: ai, tokens: tokens}
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
	env := newAssistantEnv(t, testdb.New(t), 30, 300)
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
	if got.Reply != "这是三个方案：" || len(got.Recommendations) != 1 {
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
	env := newAssistantEnv(t, testdb.New(t), 30, 300)

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
	env := newAssistantEnv(t, testdb.New(t), 3, 5)
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
	if r.status != 429 || r.errCode(t) != "rate_limited" || r.header.Get("Retry-After") == "" {
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
	if r := send(bob); r.status != 429 {
		t.Fatalf("site limit: %d %s", r.status, r.body)
	}
}

func TestIntegrationAgentInternalAPI(t *testing.T) {
	env := newAssistantEnv(t, testdb.New(t), 30, 300)
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
	env := newAssistantEnv(t, testdb.New(t), 3, 300)
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
