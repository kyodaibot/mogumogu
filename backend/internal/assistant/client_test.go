package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReply(t *testing.T) {
	var got Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/reply" {
			t.Errorf("request: %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"reply":"你好","recommendations":[],"ask_meal_type":true,"meal_type":null}`))
	}))
	defer srv.Close()

	reply, err := NewClient(srv.URL).Reply(context.Background(), Request{
		Message: "吃什么", Token: "tok",
		Context: Context{TargetDate: "2026-10-27", MealsRange: Range{From: "2026-10-21", To: "2026-10-27"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reply.Reply != "你好" || !reply.AskMealType || reply.Recommendations == nil || reply.MealType != nil {
		t.Fatalf("reply: %+v", reply)
	}
	if got.Message != "吃什么" || got.Token != "tok" || got.Context.MealsRange.From != "2026-10-21" {
		t.Fatalf("request: %+v", got)
	}
}

func TestReplyErrors(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"503": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(503)
			w.Write([]byte(`{"error":"invalid_output"}`))
		},
		"not json": func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("oops")) },
	} {
		srv := httptest.NewServer(handler)
		_, err := NewClient(srv.URL).Reply(context.Background(), Request{})
		srv.Close()
		if err == nil {
			t.Errorf("%s: no error", name)
		}
		if name == "503" && !strings.Contains(err.Error(), "invalid_output") {
			t.Errorf("503: error %q lacks the service's code", err)
		}
	}

	srv := httptest.NewServer(nil)
	srv.Close()
	if _, err := NewClient(srv.URL).Reply(context.Background(), Request{}); err == nil {
		t.Error("unreachable service: no error")
	}
}

func TestReplyPassesEstimatesOn(t *testing.T) {
	// The AI service sends floats; they come out of Go as plain numbers.
	in := `{"reply":"案","ask_meal_type":false,"meal_type":"dinner","recommendations":[{"name":"n","description":"d","reason":"r",` +
		`"estimate":{"serving":{"calories":650.0,"protein_g":35.0,"fat_g":18.0,"carbs_g":85.0},` +
		`"per_100g":{"calories":163.0,"protein_g":9.0,"fat_g":4.0,"carbs_g":21.0}}}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(in)) }))
	defer srv.Close()

	reply, err := NewClient(srv.URL).Reply(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(reply)
	for _, want := range []string{`"meal_type":"dinner"`, `"serving":{"calories":650,"protein_g":35,"fat_g":18,"carbs_g":85}`, `"per_100g":{"calories":163,`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("%s lacks %s", out, want)
		}
	}
}
