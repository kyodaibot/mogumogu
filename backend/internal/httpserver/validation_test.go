package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"mogumogu/backend/internal/auth"
)

// call runs handler for one request and returns the recorder.
func call(t *testing.T, method, body string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	r := gin.New()
	r.Handle(method, "/x", handler)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, "/x", strings.NewReader(body)))
	return w
}

type errResp struct {
	Error struct {
		Code    string       `json:"code"`
		Details []fieldError `json:"details"`
	} `json:"error"`
}

func parseErr(t *testing.T, w *httptest.ResponseRecorder) errResp {
	t.Helper()
	var e errResp
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("error body is not JSON: %s", w.Body.String())
	}
	return e
}

func hasDetail(e errResp, field, reason string) bool {
	for _, d := range e.Error.Details {
		if d.Field == field && d.Reason == reason {
			return true
		}
	}
	return false
}

func TestDecodeJSON(t *testing.T) {
	handler := func(c *gin.Context) {
		var in eventInput
		if decodeJSON(c, &in) {
			c.Status(http.StatusNoContent)
		}
	}
	tests := []struct {
		name, body string
		wantStatus int
		wantCode   string
	}{
		{"valid", `{"title":"x"}`, 204, ""},
		{"empty body", ``, 400, "invalid_request"},
		{"malformed", `{"title":`, 400, "invalid_request"},
		{"unknown field", `{"title":"x","user_id":2}`, 400, "invalid_request"},
		{"wrong type", `{"title":5}`, 400, "invalid_request"},
		{"two objects", `{"title":"x"}{"title":"y"}`, 400, "invalid_request"},
		{"array", `[]`, 400, "invalid_request"},
		{"too large", `{"title":"` + strings.Repeat("a", maxBodyBytes) + `"}`, 413, "payload_too_large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := call(t, http.MethodPost, tt.body, handler)
			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", w.Code, tt.wantStatus, w.Body.String())
			}
			if tt.wantCode != "" && parseErr(t, w).Error.Code != tt.wantCode {
				t.Fatalf("code = %s, want %s", parseErr(t, w).Error.Code, tt.wantCode)
			}
		})
	}
}

func TestBindEventValidation(t *testing.T) {
	handler := func(c *gin.Context) {
		if _, ok := bindEvent(c, 1); ok {
			c.Status(http.StatusNoContent)
		}
	}
	valid := func(start, end string) string {
		return `{"title":"上课","starts_at":"` + start + `","ends_at":"` + end + `"}`
	}
	tests := []struct {
		name, body    string
		field, reason string // empty: expect success
	}{
		{"valid", valid("2026-10-23T09:00:00+09:00", "2026-10-23T10:00:00+09:00"), "", ""},
		{"crosses midnight", valid("2026-10-23T23:00:00+09:00", "2026-10-24T01:00:00+09:00"), "", ""},
		{"exactly 24h", valid("2026-10-23T09:00:00+09:00", "2026-10-24T09:00:00+09:00"), "", ""},
		{"24h plus 1s", valid("2026-10-23T09:00:00+09:00", "2026-10-24T09:00:01+09:00"), "ends_at", "duration_too_long"},
		{"end equals start", valid("2026-10-23T09:00:00+09:00", "2026-10-23T09:00:00+09:00"), "ends_at", "must_be_after_starts_at"},
		{"end before start", valid("2026-10-23T09:00:00+09:00", "2026-10-23T08:00:00+09:00"), "ends_at", "must_be_after_starts_at"},
		{"no offset", valid("2026-10-23T09:00:00", "2026-10-23T10:00:00+09:00"), "starts_at", "invalid_format"},
		{"missing title", `{"starts_at":"2026-10-23T09:00:00+09:00","ends_at":"2026-10-23T10:00:00+09:00"}`, "title", "required"},
		{"blank title", `{"title":"   ","starts_at":"2026-10-23T09:00:00+09:00","ends_at":"2026-10-23T10:00:00+09:00"}`, "title", "required"},
		{"long title", `{"title":"` + strings.Repeat("あ", 101) + `","starts_at":"2026-10-23T09:00:00+09:00","ends_at":"2026-10-23T10:00:00+09:00"}`, "title", "too_long"},
		{"100 char title", `{"title":"` + strings.Repeat("あ", 100) + `","starts_at":"2026-10-23T09:00:00+09:00","ends_at":"2026-10-23T10:00:00+09:00"}`, "", ""},
		{"long notes", `{"title":"x","starts_at":"2026-10-23T09:00:00+09:00","ends_at":"2026-10-23T10:00:00+09:00","notes":"` + strings.Repeat("n", 1001) + `"}`, "notes", "too_long"},
		{"missing times", `{"title":"x"}`, "starts_at", "required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := call(t, http.MethodPost, tt.body, handler)
			if tt.field == "" {
				if w.Code != http.StatusNoContent {
					t.Fatalf("expected success, got %d %s", w.Code, w.Body.String())
				}
				return
			}
			if w.Code != http.StatusUnprocessableEntity || !hasDetail(parseErr(t, w), tt.field, tt.reason) {
				t.Fatalf("want 422 %s/%s, got %d %s", tt.field, tt.reason, w.Code, w.Body.String())
			}
		})
	}
}

func TestBindMealValidation(t *testing.T) {
	var got []any
	handler := func(c *gin.Context) {
		if m, ok := bindMeal(c, 1); ok {
			got = []any{m.Calories, m.ProteinG}
			c.Status(http.StatusNoContent)
		}
	}
	base := `"date":"2026-10-23","meal_type":"lunch","content":"鸡胸肉沙拉"`
	tests := []struct {
		name, body    string
		field, reason string
	}{
		{"nutrition omitted", `{` + base + `}`, "", ""},
		{"nutrition null and zero", `{` + base + `,"calories":null,"protein_g":0}`, "", ""},
		{"future date allowed", `{"date":"2099-01-01","meal_type":"snack","content":"x"}`, "", ""},
		{"negative", `{` + base + `,"fat_g":-1}`, "fat_g", "out_of_range"},
		{"too large", `{` + base + `,"calories":10000.1}`, "calories", "out_of_range"},
		{"two decimals", `{` + base + `,"carbs_g":1.25}`, "carbs_g", "too_many_decimals"},
		{"bad meal type", `{"date":"2026-10-23","meal_type":"brunch","content":"x"}`, "meal_type", "invalid_value"},
		{"bad date", `{"date":"2026-02-30","meal_type":"lunch","content":"x"}`, "date", "invalid_format"},
		{"missing content", `{"date":"2026-10-23","meal_type":"lunch"}`, "content", "required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := call(t, http.MethodPost, tt.body, handler)
			if tt.field == "" {
				if w.Code != http.StatusNoContent {
					t.Fatalf("expected success, got %d %s", w.Code, w.Body.String())
				}
				return
			}
			if w.Code != http.StatusUnprocessableEntity || !hasDetail(parseErr(t, w), tt.field, tt.reason) {
				t.Fatalf("want 422 %s/%s, got %d %s", tt.field, tt.reason, w.Code, w.Body.String())
			}
		})
	}

	// null stays nil and 0 stays a pointer to 0: never conflated.
	call(t, http.MethodPost, `{`+base+`,"calories":null,"protein_g":0}`, handler)
	if cal, _ := got[0].(*float64); cal != nil {
		t.Fatalf("null calories became %v", *cal)
	}
	if p, _ := got[1].(*float64); p == nil || *p != 0 {
		t.Fatalf("zero protein not kept as 0: %v", p)
	}
}

func TestValidateAllergens(t *testing.T) {
	var v validator
	if a := validateAllergens(&v, nil); a.Valid {
		t.Fatal("nil allergens should stay NULL")
	}
	empty := []string{}
	if a := validateAllergens(&v, &empty); !a.Valid || len(a.Items) != 0 || v.failed() {
		t.Fatal("[] should be a valid empty list")
	}
	list := []string{" peanut ", "Shrimp", "shrimp", ""}
	a := validateAllergens(&v, &list)
	if a.Items[0] != "peanut" {
		t.Fatalf("item not trimmed: %q", a.Items[0])
	}
	want := map[fieldError]bool{{"allergens[2]", "duplicate_item"}: true, {"allergens[3]", "required"}: true}
	if len(v.errs) != 2 || !want[v.errs[0]] || !want[v.errs[1]] {
		t.Fatalf("errors = %+v", v.errs)
	}
	tooMany := make([]string, 21)
	var v2 validator
	validateAllergens(&v2, &tooMany)
	if len(v2.errs) != 1 || v2.errs[0] != (fieldError{"allergens", "too_long"}) {
		t.Fatalf("21 items: %+v", v2.errs)
	}
}

func TestProfileNumberRanges(t *testing.T) {
	f := func(x float64) *float64 { return &x }
	tests := []struct {
		x      float64
		lo, hi float64
		reason string
	}{
		{50, 50, 300, ""}, {300, 50, 300, ""}, {49.9, 50, 300, "out_of_range"}, {300.1, 50, 300, "out_of_range"},
		{172.5, 50, 300, ""}, {172.55, 50, 300, "too_many_decimals"}, {10, 10, 500, ""}, {500, 10, 500, ""},
	}
	for _, tt := range tests {
		var v validator
		v.decimal("x", f(tt.x), tt.lo, tt.hi, true)
		got := ""
		if v.failed() {
			got = v.errs[0].Reason
		}
		if got != tt.reason {
			t.Errorf("%v in [%v,%v]: got %q, want %q", tt.x, tt.lo, tt.hi, got, tt.reason)
		}
	}
}

func TestDateRange(t *testing.T) {
	handler := func(c *gin.Context) {
		if _, _, ok := dateRange(c, maxRangeDays); ok {
			c.Status(http.StatusNoContent)
		}
	}
	tests := []struct {
		query         string
		status        int
		field, reason string
	}{
		{"from=2026-10-01&to=2026-10-31", 204, "", ""},
		{"from=2026-10-23&to=2026-10-23", 204, "", ""},
		{"from=2026-09-28&to=2026-11-28", 204, "", ""}, // exactly 62 days
		{"from=2026-09-28&to=2026-11-29", 422, "to", "range_too_large"},
		{"from=2026-10-23&to=2026-10-22", 422, "to", "must_not_be_before_from"},
		{"from=2026-10-23&to=bad", 422, "to", "invalid_format"},
		{"from=2026-10-23", 400, "", ""},
		{"", 400, "", ""},
	}
	for _, tt := range tests {
		r := gin.New()
		r.GET("/x", handler)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x?"+tt.query, nil))
		if w.Code != tt.status {
			t.Errorf("%q: status %d, want %d", tt.query, w.Code, tt.status)
			continue
		}
		if tt.field != "" && !hasDetail(parseErr(t, w), tt.field, tt.reason) {
			t.Errorf("%q: missing %s/%s in %s", tt.query, tt.field, tt.reason, w.Body.String())
		}
	}
}

// TestCSRFMiddleware exercises the rejections that happen before any handler
// or database access. Accepted requests are covered by integration tests.
func TestCSRFMiddleware(t *testing.T) {
	csrf := auth.NewCSRF([]byte("test-csrf-secret-placeholder-0123456789"))
	a := &api{csrf: csrf, allowedOrigins: []string{"http://localhost:5173"}, logger: discardLogger()}
	r := gin.New()
	r.POST("/x", a.csrfProtect, func(c *gin.Context) { c.Status(http.StatusNoContent) })
	r.GET("/x", a.csrfProtect, func(c *gin.Context) { c.Status(http.StatusNoContent) })

	anon, _ := csrf.IssueAnonymous()
	sessionTok, _ := csrf.IssueForSession("some-jti")

	type req struct {
		method, origin, header, cookie string
	}
	tests := []struct {
		name string
		req  req
		want int
	}{
		{"valid anonymous", req{"POST", "http://localhost:5173", anon, anon}, 204},
		{"GET needs nothing", req{"GET", "", "", ""}, 204},
		{"no origin", req{"POST", "", anon, anon}, 403},
		{"foreign origin", req{"POST", "http://evil.example", anon, anon}, 403},
		{"no header", req{"POST", "http://localhost:5173", "", anon}, 403},
		{"no cookie", req{"POST", "http://localhost:5173", anon, ""}, 403},
		{"header differs from cookie", req{"POST", "http://localhost:5173", anon, sessionTok}, 403},
		{"literal anonymous", req{"POST", "http://localhost:5173", "anonymous", "anonymous"}, 403},
		{"session token without session", req{"POST", "http://localhost:5173", sessionTok, sessionTok}, 403},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hr := httptest.NewRequest(tt.req.method, "/x", nil)
			if tt.req.origin != "" {
				hr.Header.Set("Origin", tt.req.origin)
			}
			if tt.req.header != "" {
				hr.Header.Set(csrfHeader, tt.req.header)
			}
			if tt.req.cookie != "" {
				hr.AddCookie(&http.Cookie{Name: csrfCookie, Value: tt.req.cookie})
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, hr)
			if w.Code != tt.want {
				t.Fatalf("status %d, want %d (%s)", w.Code, tt.want, w.Body.String())
			}
			if tt.want == 403 && parseErr(t, w).Error.Code != "csrf_failed" {
				t.Fatalf("code = %s", parseErr(t, w).Error.Code)
			}
		})
	}
}
