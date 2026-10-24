package httpserver

// Integration tests: the full router against a real, temporary PostgreSQL
// database (see internal/testdb). They are skipped without TEST_DATABASE_URL.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"mogumogu/backend/internal/auth"
	"mogumogu/backend/internal/database"
	"mogumogu/backend/internal/store"
	"mogumogu/backend/internal/testdb"
)

const testOrigin = "http://localhost:5173"

var (
	itJWTSecret  = []byte("integration-jwt-secret-placeholder-0123")
	itCSRFSecret = []byte("integration-csrf-secret-placeholder-012")
)

// newServer starts the full API on db with fresh limiters.
func newServer(t *testing.T, db *testdb.DB) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(NewRouter(Deps{
		Logger:          discardLogger(),
		DB:              db.SQL,
		Store:           store.New(db.Gorm),
		Sessions:        auth.NewSessions(itJWTSecret, "mogumogu", "mogumogu-web", time.Hour),
		CSRF:            auth.NewCSRF(itCSRFSecret),
		LoginLimiter:    auth.NewLimiter(5, 15*time.Minute),
		RegisterLimiter: auth.NewLimiter(1000, time.Hour),
		AllowedOrigins:  []string{testOrigin},
	}))
	t.Cleanup(srv.Close)
	return srv
}

// browser is an HTTP client with its own cookie jar that behaves like the
// frontend: it sends Origin and copies the CSRF cookie into X-CSRF-Token.
type browser struct {
	t   *testing.T
	srv *httptest.Server
	jar *cookiejar.Jar
	hc  *http.Client
	// overrideCSRF, when set, is sent instead of the cookie value.
	overrideCSRF string
}

func newBrowser(t *testing.T, srv *httptest.Server) *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{t: t, srv: srv, jar: jar, hc: &http.Client{Jar: jar}}
}

func (b *browser) cookie(name, path string) string {
	u, _ := url.Parse(b.srv.URL + path)
	for _, c := range b.jar.Cookies(u) {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func (b *browser) csrfCookie() string { return b.cookie(csrfCookie, "/") }

type resp struct {
	status int
	header http.Header
	body   []byte
}

func (r resp) json(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, r.body)
	}
}

func (r resp) errCode(t *testing.T) string {
	t.Helper()
	var e errResp
	r.json(t, &e)
	return e.Error.Code
}

func (b *browser) do(method, path string, body any) resp {
	b.t.Helper()
	var rd io.Reader
	switch v := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(v)
	default:
		buf, _ := json.Marshal(v)
		rd = bytes.NewReader(buf)
	}
	req, _ := http.NewRequest(method, b.srv.URL+path, rd)
	if method != http.MethodGet {
		req.Header.Set("Origin", testOrigin)
		token := b.csrfCookie()
		if b.overrideCSRF != "" {
			token = b.overrideCSRF
		}
		if token != "" {
			req.Header.Set(csrfHeader, token)
		}
	}
	res, err := b.hc.Do(req)
	if err != nil {
		b.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return resp{status: res.StatusCode, header: res.Header, body: data}
}

func (b *browser) expect(want int, method, path string, body any) resp {
	b.t.Helper()
	r := b.do(method, path, body)
	if r.status != want {
		b.t.Fatalf("%s %s: status %d, want %d (%s)", method, path, r.status, want, r.body)
	}
	return r
}

// signUp fetches a CSRF token and registers username.
func signUp(t *testing.T, srv *httptest.Server, username string) *browser {
	t.Helper()
	b := newBrowser(t, srv)
	b.expect(200, "GET", "/api/auth/csrf", nil)
	b.expect(201, "POST", "/api/auth/register", map[string]string{"username": username, "password": "correct horse battery"})
	return b
}

func TestIntegrationAuthFlow(t *testing.T) {
	db := testdb.New(t)
	srv := newServer(t, db)
	b := newBrowser(t, srv)

	// Anonymous CSRF tokens are random and signed.
	var c1, c2 struct {
		CSRFToken string `json:"csrf_token"`
	}
	b.expect(200, "GET", "/api/auth/csrf", nil).json(t, &c1)
	newBrowser(t, srv).expect(200, "GET", "/api/auth/csrf", nil).json(t, &c2)
	if !strings.HasPrefix(c1.CSRFToken, "v1.a.") || c1.CSRFToken == c2.CSRFToken {
		t.Fatalf("anonymous tokens %q / %q", c1.CSRFToken, c2.CSRFToken)
	}
	if b.csrfCookie() != c1.CSRFToken {
		t.Fatal("csrf cookie does not match the response body")
	}
	anonToken := c1.CSRFToken

	// Not logged in yet.
	if r := b.do("GET", "/api/auth/me", nil); r.status != 401 || r.errCode(t) != "unauthenticated" {
		t.Fatalf("me before login: %d %s", r.status, r.body)
	}

	// Register logs in and rotates the CSRF token to one bound to the session.
	var reg struct {
		User       userJSON `json:"user"`
		HasProfile bool     `json:"has_profile"`
		CSRFToken  string   `json:"csrf_token"`
	}
	b.expect(201, "POST", "/api/auth/register", map[string]string{"username": "Taro_01", "password": "correct horse battery"}).json(t, &reg)
	if reg.User.Username != "taro_01" || reg.HasProfile || !strings.HasPrefix(reg.CSRFToken, "v1.s.") || reg.CSRFToken == anonToken {
		t.Fatalf("register response: %+v", reg)
	}
	if b.csrfCookie() != reg.CSRFToken {
		t.Fatal("register did not overwrite the csrf cookie")
	}
	if b.cookie(sessionCookie, "/api") == "" {
		t.Fatal("no session cookie after register")
	}

	var me meJSON
	b.expect(200, "GET", "/api/auth/me", nil).json(t, &me)
	if me.User.ID != reg.User.ID || me.HasProfile || me.CSRFToken != "" {
		t.Fatalf("me: %+v", me)
	}

	// The pre-login anonymous token no longer works.
	b.overrideCSRF = anonToken
	if r := b.do("PUT", "/api/profile", map[string]any{}); r.status != 403 || r.errCode(t) != "csrf_failed" {
		t.Fatalf("old anonymous token: %d %s", r.status, r.body)
	}
	b.overrideCSRF = "anonymous"
	if r := b.do("PUT", "/api/profile", map[string]any{}); r.status != 403 {
		t.Fatalf("literal anonymous token: %d", r.status)
	}
	b.overrideCSRF = ""

	// Duplicate username, case-insensitively.
	other := newBrowser(t, srv)
	other.expect(200, "GET", "/api/auth/csrf", nil)
	if r := other.do("POST", "/api/auth/register", map[string]string{"username": "TARO_01", "password": "another password"}); r.status != 409 || r.errCode(t) != "username_taken" {
		t.Fatalf("duplicate register: %d %s", r.status, r.body)
	}

	// Logout revokes the token: a copy of the old cookie stops working.
	oldSession := b.cookie(sessionCookie, "/api")
	b.expect(204, "POST", "/api/auth/logout", nil)
	if b.cookie(sessionCookie, "/api") != "" {
		t.Fatal("session cookie not cleared")
	}
	if !strings.HasPrefix(b.csrfCookie(), "v1.a.") {
		t.Fatal("logout did not issue a new anonymous csrf token")
	}
	req, _ := http.NewRequest("GET", srv.URL+"/api/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: oldSession})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("revoked token: status %d, want 401", res.StatusCode)
	}
	var revoked int64
	db.Gorm.Model(&store.RevokedToken{}).Count(&revoked)
	if revoked != 1 {
		t.Fatalf("revoked_tokens rows = %d", revoked)
	}

	// Login again with any case.
	var login meJSON
	b.expect(200, "POST", "/api/auth/login", map[string]string{"username": "TARO_01", "password": "correct horse battery"}).json(t, &login)
	if login.User.ID != reg.User.ID || !strings.HasPrefix(login.CSRFToken, "v1.s.") {
		t.Fatalf("login: %+v", login)
	}
	b.expect(200, "GET", "/api/auth/me", nil)

	// The password hash is Argon2id, never the password.
	var u store.User
	db.Gorm.Where("id = ?", reg.User.ID).Take(&u)
	if !strings.HasPrefix(u.PasswordHash, "$argon2id$") || strings.Contains(u.PasswordHash, "correct horse") {
		t.Fatalf("stored hash: %s", u.PasswordHash)
	}
}

func TestIntegrationLoginRateLimit(t *testing.T) {
	srv := newServer(t, testdb.New(t))
	signUp(t, srv, "hanako")

	b := newBrowser(t, srv)
	b.expect(200, "GET", "/api/auth/csrf", nil)
	bad := map[string]string{"username": "hanako", "password": "wrong password"}
	for i := range 5 {
		if r := b.do("POST", "/api/auth/login", bad); r.status != 401 || r.errCode(t) != "invalid_credentials" {
			t.Fatalf("attempt %d: %d %s", i+1, r.status, r.body)
		}
	}
	r := b.do("POST", "/api/auth/login", map[string]string{"username": "hanako", "password": "correct horse battery"})
	if r.status != 429 || r.errCode(t) != "rate_limited" || r.header.Get("Retry-After") == "" {
		t.Fatalf("6th attempt: %d %s Retry-After=%q", r.status, r.body, r.header.Get("Retry-After"))
	}
	// Unknown users get the same answer as a wrong password.
	if r := b.do("POST", "/api/auth/login", map[string]string{"username": "nobody", "password": "whatever1"}); r.status != 401 || r.errCode(t) != "invalid_credentials" {
		t.Fatalf("unknown user: %d %s", r.status, r.body)
	}
}

func TestIntegrationCSRFBoundToSession(t *testing.T) {
	srv := newServer(t, testdb.New(t))
	a := signUp(t, srv, "user_a")
	b := signUp(t, srv, "user_b")

	// B presents A's (valid, signed) session token: rejected.
	b.overrideCSRF = a.csrfCookie()
	if r := b.do("POST", "/api/schedule-events", map[string]any{}); r.status != 403 || r.errCode(t) != "csrf_failed" {
		t.Fatalf("other session's token: %d %s", r.status, r.body)
	}
	b.overrideCSRF = ""

	// Wrong Origin.
	req, _ := http.NewRequest("POST", srv.URL+"/api/auth/logout", nil)
	req.Header.Set("Origin", "http://evil.example")
	req.Header.Set(csrfHeader, a.csrfCookie())
	if res, _ := a.hc.Do(req); res.StatusCode != 403 {
		t.Fatalf("foreign origin: %d", res.StatusCode)
	}
	// Unauthenticated write: 401 comes before CSRF.
	if r := newBrowser(t, srv).do("POST", "/api/schedule-events", map[string]any{}); r.status != 401 {
		t.Fatalf("unauthenticated write: %d", r.status)
	}
}

func TestIntegrationProfile(t *testing.T) {
	srv := newServer(t, testdb.New(t))
	b := signUp(t, srv, "profile_user")

	if r := b.do("GET", "/api/profile", nil); r.status != 404 || r.errCode(t) != "profile_not_found" {
		t.Fatalf("missing profile: %d %s", r.status, r.body)
	}

	put := func(allergens any) map[string]any {
		body := map[string]any{"height_cm": 172.5, "weight_kg": 65, "age": 21, "has_exercise_habit": true, "goal": "build_muscle"}
		if allergens != "omit" {
			body["allergens"] = allergens
		}
		var got map[string]any
		b.expect(200, "PUT", "/api/profile", body).json(t, &got)
		return got
	}

	if got := put("omit"); got["allergens"] != nil || got["height_cm"] != 172.5 || got["weight_kg"] != float64(65) {
		t.Fatalf("omitted allergens: %v", got)
	}
	if got := put([]string{}); fmt.Sprint(got["allergens"]) != "[]" {
		t.Fatalf("empty allergens: %#v", got["allergens"])
	}
	if got := put([]string{"shrimp", `pea"nut\x`, "卵"}); fmt.Sprint(got["allergens"]) != `[shrimp pea"nut\x 卵]` {
		t.Fatalf("allergens round trip: %#v", got["allergens"])
	}
	var stored map[string]any
	b.expect(200, "GET", "/api/profile", nil).json(t, &stored)
	if fmt.Sprint(stored["allergens"]) != `[shrimp pea"nut\x 卵]` || stored["goal"] != "build_muscle" {
		t.Fatalf("GET profile: %v", stored)
	}
	if got := put(nil); got["allergens"] != nil {
		t.Fatalf("null allergens: %#v", got["allergens"])
	}

	var me meJSON
	b.expect(200, "GET", "/api/auth/me", nil).json(t, &me)
	if !me.HasProfile {
		t.Fatal("has_profile still false")
	}

	r := b.do("PUT", "/api/profile", map[string]any{"height_cm": 301, "weight_kg": 9, "age": 9, "has_exercise_habit": true, "goal": "bulk"})
	var e errResp
	r.json(t, &e)
	for _, f := range []string{"height_cm", "weight_kg", "age", "goal"} {
		found := false
		for _, d := range e.Error.Details {
			found = found || d.Field == f
		}
		if !found {
			t.Errorf("no validation detail for %s: %s", f, r.body)
		}
	}
}

type eventOut struct {
	ID       int64   `json:"id"`
	Title    string  `json:"title"`
	StartsAt string  `json:"starts_at"`
	EndsAt   string  `json:"ends_at"`
	Notes    *string `json:"notes"`
}

func listEventTitles(t *testing.T, b *browser, from, to string) []string {
	t.Helper()
	var out struct{ Items []eventOut }
	b.expect(200, "GET", "/api/schedule-events?from="+from+"&to="+to, nil).json(t, &out)
	titles := []string{}
	for _, e := range out.Items {
		titles = append(titles, e.Title)
	}
	return titles
}

func TestIntegrationScheduleEvents(t *testing.T) {
	srv := newServer(t, testdb.New(t))
	b := signUp(t, srv, "planner")

	create := func(title, start, end string) eventOut {
		var e eventOut
		b.expect(201, "POST", "/api/schedule-events", map[string]any{"title": title, "starts_at": start, "ends_at": end}).json(t, &e)
		return e
	}
	create("morning", "2026-10-23T09:00:00+09:00", "2026-10-23T10:30:00+09:00")
	cross := create("overnight", "2026-10-23T23:00:00+09:00", "2026-10-24T01:00:00+09:00")
	create("until midnight", "2026-10-23T22:00:00+09:00", "2026-10-24T00:00:00+09:00")
	// Given in UTC: 2026-10-24 00:00 Tokyo, so it belongs to the 24th only.
	create("from midnight", "2026-10-23T15:00:00Z", "2026-10-23T16:00:00Z")

	if cross.StartsAt != "2026-10-23T23:00:00+09:00" || cross.Notes != nil {
		t.Fatalf("created event: %+v", cross)
	}
	if got := strings.Join(listEventTitles(t, b, "2026-10-23", "2026-10-23"), ","); got != "morning,until midnight,overnight" {
		t.Errorf("events on 10/23: %s", got)
	}
	if got := strings.Join(listEventTitles(t, b, "2026-10-24", "2026-10-24"), ","); got != "overnight,from midnight" {
		t.Errorf("events on 10/24 (ending exactly at 00:00 must not appear): %s", got)
	}
	if got := len(listEventTitles(t, b, "2026-10-22", "2026-10-22")); got != 0 {
		t.Errorf("events on 10/22: %d", got)
	}

	// Update replaces every field; omitted notes become null.
	notes := "bring laptop"
	var upd eventOut
	b.expect(200, "PUT", fmt.Sprintf("/api/schedule-events/%d", cross.ID), map[string]any{
		"title": "late lab", "starts_at": "2026-10-23T20:00:00+09:00", "ends_at": "2026-10-23T21:00:00+09:00", "notes": notes,
	}).json(t, &upd)
	if upd.Title != "late lab" || upd.Notes == nil || *upd.Notes != notes {
		t.Fatalf("updated: %+v", upd)
	}
	b.expect(200, "PUT", fmt.Sprintf("/api/schedule-events/%d", cross.ID), map[string]any{
		"title": "late lab", "starts_at": "2026-10-23T20:00:00+09:00", "ends_at": "2026-10-23T21:00:00+09:00",
	}).json(t, &upd)
	if upd.Notes != nil {
		t.Fatal("omitted notes on PUT should become null")
	}

	if r := b.do("POST", "/api/schedule-events", map[string]any{"title": "x", "starts_at": "2026-10-23T09:00:00+09:00", "ends_at": "2026-10-24T09:00:01+09:00"}); r.status != 422 {
		t.Fatalf("25h event: %d", r.status)
	}
	if r := b.do("POST", "/api/schedule-events", map[string]any{"title": "x", "starts_at": "2026-10-23T09:00:00+09:00", "ends_at": "2026-10-23T10:00:00+09:00", "user_id": 99}); r.status != 400 {
		t.Fatalf("user_id in body: %d", r.status)
	}

	b.expect(204, "DELETE", fmt.Sprintf("/api/schedule-events/%d", cross.ID), nil)
	b.expect(404, "GET", fmt.Sprintf("/api/schedule-events/%d", cross.ID), nil)
	b.expect(404, "DELETE", fmt.Sprintf("/api/schedule-events/%d", cross.ID), nil)
	b.expect(404, "GET", "/api/schedule-events/abc", nil)
}

func TestIntegrationMealRecords(t *testing.T) {
	db := testdb.New(t)
	srv := newServer(t, db)
	b := signUp(t, srv, "eater")

	create := func(body map[string]any) map[string]any {
		var m map[string]any
		b.expect(201, "POST", "/api/meal-records", body).json(t, &m)
		return m
	}
	create(map[string]any{"date": "2026-10-23", "meal_type": "dinner", "content": "拉面", "calories": 800.5})
	lunch := create(map[string]any{"date": "2026-10-23", "meal_type": "lunch", "content": "鸡胸肉沙拉", "calories": nil, "protein_g": 0})
	create(map[string]any{"date": "2026-10-23", "meal_type": "breakfast", "content": "toast"})
	create(map[string]any{"date": "2026-10-24", "meal_type": "breakfast", "content": "rice"})
	create(map[string]any{"date": "2099-12-31", "meal_type": "snack", "content": "future plan"})

	// null and 0 come back exactly as sent; omitted fields are null.
	if lunch["calories"] != nil || lunch["protein_g"] != float64(0) || lunch["fat_g"] != nil || lunch["date"] != "2026-10-23" {
		t.Fatalf("lunch: %v", lunch)
	}
	id := int64(lunch["id"].(float64))
	var dbCalories, dbProtein *float64
	db.SQL.QueryRow("SELECT calories, protein_g FROM meal_records WHERE id = $1", id).Scan(&dbCalories, &dbProtein)
	if dbCalories != nil || dbProtein == nil || *dbProtein != 0 {
		t.Fatalf("stored calories=%v protein=%v", dbCalories, dbProtein)
	}

	var day struct{ Items []map[string]any }
	b.expect(200, "GET", "/api/meal-records?from=2026-10-23&to=2026-10-23", nil).json(t, &day)
	var order []string
	for _, m := range day.Items {
		order = append(order, m["meal_type"].(string))
	}
	if strings.Join(order, ",") != "breakfast,lunch,dinner" {
		t.Fatalf("order on 10/23: %v", order)
	}
	b.expect(200, "GET", "/api/meal-records?from=2026-10-23&to=2026-10-24", nil).json(t, &day)
	if len(day.Items) != 4 {
		t.Fatalf("two-day range: %d items", len(day.Items))
	}

	// PUT with nutrition omitted clears it to null, not 0.
	var upd map[string]any
	b.expect(200, "PUT", fmt.Sprintf("/api/meal-records/%d", id), map[string]any{"date": "2026-10-23", "meal_type": "lunch", "content": "salad"}).json(t, &upd)
	if upd["protein_g"] != nil || upd["content"] != "salad" {
		t.Fatalf("updated: %v", upd)
	}
	b.expect(204, "DELETE", fmt.Sprintf("/api/meal-records/%d", id), nil)
	b.expect(404, "GET", fmt.Sprintf("/api/meal-records/%d", id), nil)
}

// TestIntegrationIsolation: user B can neither see nor change anything of A's.
func TestIntegrationIsolation(t *testing.T) {
	srv := newServer(t, testdb.New(t))
	a := signUp(t, srv, "alice")
	b := signUp(t, srv, "bob")

	var ev, meal struct {
		ID int64 `json:"id"`
	}
	a.expect(201, "POST", "/api/schedule-events", map[string]any{"title": "alice event", "starts_at": "2026-10-23T09:00:00+09:00", "ends_at": "2026-10-23T10:00:00+09:00"}).json(t, &ev)
	a.expect(201, "POST", "/api/meal-records", map[string]any{"date": "2026-10-23", "meal_type": "lunch", "content": "alice lunch"}).json(t, &meal)
	a.expect(200, "PUT", "/api/profile", map[string]any{"height_cm": 160, "weight_kg": 50, "age": 30, "has_exercise_habit": false, "allergens": []string{"peanut"}, "goal": "lose_fat"})

	evPath := fmt.Sprintf("/api/schedule-events/%d", ev.ID)
	mealPath := fmt.Sprintf("/api/meal-records/%d", meal.ID)
	evBody := map[string]any{"title": "hacked", "starts_at": "2026-10-23T09:00:00+09:00", "ends_at": "2026-10-23T10:00:00+09:00"}
	mealBody := map[string]any{"date": "2026-10-23", "meal_type": "lunch", "content": "hacked"}

	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{"GET", evPath, nil}, {"PUT", evPath, evBody}, {"DELETE", evPath, nil},
		{"GET", mealPath, nil}, {"PUT", mealPath, mealBody}, {"DELETE", mealPath, nil},
	} {
		if r := b.do(tc.method, tc.path, tc.body); r.status != 404 || r.errCode(t) != "not_found" {
			t.Errorf("bob %s %s: %d %s", tc.method, tc.path, r.status, r.body)
		}
	}
	if got := listEventTitles(t, b, "2026-10-23", "2026-10-23"); len(got) != 0 {
		t.Errorf("bob sees events: %v", got)
	}
	var bobMeals struct{ Items []any }
	b.expect(200, "GET", "/api/meal-records?from=2026-10-23&to=2026-10-23", nil).json(t, &bobMeals)
	if len(bobMeals.Items) != 0 {
		t.Errorf("bob sees meals: %v", bobMeals.Items)
	}
	if r := b.do("GET", "/api/profile", nil); r.status != 404 {
		t.Errorf("bob sees a profile: %d %s", r.status, r.body)
	}

	// Alice's data is untouched.
	var aEv eventOut
	a.expect(200, "GET", evPath, nil).json(t, &aEv)
	var aMeal map[string]any
	a.expect(200, "GET", mealPath, nil).json(t, &aMeal)
	if aEv.Title != "alice event" || aMeal["content"] != "alice lunch" {
		t.Fatalf("alice's data changed: %+v %v", aEv, aMeal)
	}
}

// TestIntegrationPersistence: data written through one server is read back by
// a new server on a new connection pool, with the same session cookie.
func TestIntegrationPersistence(t *testing.T) {
	db := testdb.New(t)
	srv1 := newServer(t, db)
	b := signUp(t, srv1, "persist")
	b.expect(200, "PUT", "/api/profile", map[string]any{"height_cm": 170, "weight_kg": 60, "age": 25, "has_exercise_habit": true, "allergens": nil, "goal": "lose_fat"})
	b.expect(201, "POST", "/api/meal-records", map[string]any{"date": "2026-10-23", "meal_type": "snack", "content": "apple", "calories": 0})

	gdb, sqlDB, err := database.OpenURL(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	srv2 := newServer(t, &testdb.DB{Gorm: gdb, SQL: sqlDB, URL: db.URL})

	b2 := newBrowser(t, srv2)
	u, _ := url.Parse(srv2.URL + "/api")
	b2.jar.SetCookies(u, []*http.Cookie{{Name: sessionCookie, Value: b.cookie(sessionCookie, "/api"), Path: "/api"}})

	var p map[string]any
	b2.expect(200, "GET", "/api/profile", nil).json(t, &p)
	var meals struct{ Items []map[string]any }
	b2.expect(200, "GET", "/api/meal-records?from=2026-10-23&to=2026-10-23", nil).json(t, &meals)
	if p["goal"] != "lose_fat" || len(meals.Items) != 1 || meals.Items[0]["calories"] != float64(0) {
		t.Fatalf("after reconnect: profile=%v meals=%v", p, meals.Items)
	}
}
