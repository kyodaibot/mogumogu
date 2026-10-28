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
		w.Write([]byte(`{"reply":"你好","recommendations":[],"ask_meal_type":true}`))
	}))
	defer srv.Close()

	reply, err := NewClient(srv.URL).Reply(context.Background(), Request{
		Message: "吃什么", Token: "tok",
		Context: Context{TargetDate: "2026-10-27", MealsRange: Range{From: "2026-10-21", To: "2026-10-27"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reply.Reply != "你好" || !reply.AskMealType || reply.Recommendations == nil {
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
