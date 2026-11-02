package httpserver

import (
	"fmt"
	"strings"
	"testing"

	"mogumogu/backend/internal/assistant"
	"mogumogu/backend/internal/testdb"
)

func TestIntegrationAssistantSources(t *testing.T) {
	env := newAssistantEnv(t, testdb.New(t), DailyLimits{Normal: 5, Pro: 5, Site: 100})
	b := proUser(t, env, "pro")
	long := "https://example.com/" + strings.Repeat("a", maxSourceURLBytes)
	env.ai.sources = []assistant.Source{
		{Title: "  メニュー  ", URL: "https://example.com/menu"},
		{Title: "x", URL: "javascript:alert(1)"},
		{Title: "x", URL: "data:text/html,hi"},
		{Title: "x", URL: "ftp://example.com/a"},
		{Title: "x", URL: "https://"},
		{Title: "x", URL: long},
		{Title: strings.Repeat("長", 300), URL: "http://example.com/long-title"},
	}
	for i := range 5 {
		env.ai.sources = append(env.ai.sources, assistant.Source{Title: "t", URL: fmt.Sprintf("https://example.com/%d", i)})
	}

	var got struct{ Sources []sourceJSON }
	b.expect(200, "POST", "/api/assistant/messages", message("2026-10-31", "北部食堂のメニュー")).json(t, &got)
	if !env.ai.last().Context.AllowSearch {
		t.Fatal("pro context: allow_search is false")
	}
	urls := []string{}
	for _, s := range got.Sources {
		urls = append(urls, s.URL)
	}
	want := "https://example.com/menu http://example.com/long-title https://example.com/0 https://example.com/1 https://example.com/2"
	if strings.Join(urls, " ") != want {
		t.Fatalf("sources: %v", urls)
	}
	if got.Sources[0].Title != "メニュー" || len([]rune(got.Sources[1].Title)) != maxSourceTitleRunes {
		t.Fatalf("titles: %q, %d runes", got.Sources[0].Title, len([]rune(got.Sources[1].Title)))
	}

	// Normal users cannot search, and sources sent anyway are dropped.
	alice := signUp(t, env.srv, "alice")
	got.Sources = nil
	r := alice.expect(200, "POST", "/api/assistant/messages", message("2026-10-31", "北部食堂のメニュー"))
	r.json(t, &got)
	if env.ai.last().Context.AllowSearch {
		t.Fatal("normal context: allow_search is true")
	}
	if got.Sources == nil || len(got.Sources) != 0 || !strings.Contains(string(r.body), `"sources":[]`) {
		t.Fatalf("normal sources: %s", r.body)
	}
}
