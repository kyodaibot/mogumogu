package httpserver

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"mogumogu/backend/internal/assistant"
	"mogumogu/backend/internal/store"
	"mogumogu/backend/internal/testdb"
)

type memoryReply struct {
	MemoryUpdates []memoryUpdateJSON `json:"memory_updates"`
	Proposal      *proposalJSON      `json:"proposal"`
}

func newMemory(t *testing.T, b *browser, content string) memoryJSON {
	t.Helper()
	var m memoryJSON
	b.expect(201, "POST", "/api/memories", map[string]string{"content": content}).json(t, &m)
	return m
}

func memoryContents(t *testing.T, b *browser) []string {
	t.Helper()
	var list struct{ Items []memoryJSON }
	b.expect(200, "GET", "/api/memories", nil).json(t, &list)
	out := []string{}
	for _, m := range list.Items {
		out = append(out, m.Content)
	}
	return out
}

func proUser(t *testing.T, env assistantEnv, name string) *browser {
	t.Helper()
	b := signUp(t, env.srv, name)
	if err := env.store.SetUserTier(context.Background(), name, store.TierPro); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestIntegrationMemories(t *testing.T) {
	db := testdb.New(t)
	env := newAssistantEnv(t, db, DailyLimits{Normal: 30, Pro: 100, Site: 300})
	ctx := context.Background()

	normal := signUp(t, env.srv, "normal")
	if m := memoryContents(t, normal); len(m) != 0 {
		t.Fatalf("new user: %v", m)
	}
	if r := normal.do("POST", "/api/memories", map[string]string{"content": "辛いものが苦手"}); r.status != 403 || r.errCode(t) != "pro_required" {
		t.Fatalf("normal create: %d %s", r.status, r.body)
	}

	pro := proUser(t, env, "pro")
	spicy := newMemory(t, pro, " 辛いものが苦手 ")
	if spicy.Content != "辛いものが苦手" || spicy.CreatedAt == "" || spicy.UpdatedAt == "" {
		t.Fatalf("created: %+v", spicy)
	}
	for name, tc := range map[string]struct {
		content any
		reason  string
	}{
		"duplicate": {"辛いものが苦手", "duplicate_item"},
		"newline":   {"辛いもの\n苦手", "invalid_format"},
		"tab":       {"辛いもの\t苦手", "invalid_format"},
		"too long":  {strings.Repeat("あ", 101), "too_long"},
		"blank":     {"  ", "required"},
		"missing":   {nil, "required"},
	} {
		r := pro.do("POST", "/api/memories", map[string]any{"content": tc.content})
		if r.status != 422 || !strings.Contains(string(r.body), `"reason":"`+tc.reason+`"`) {
			t.Errorf("%s: %d %s", name, r.status, r.body)
		}
	}
	newMemory(t, pro, strings.Repeat("あ", 100))

	var updated memoryJSON
	pro.expect(200, "PUT", fmt.Sprintf("/api/memories/%d", spicy.ID), map[string]string{"content": "辛いものは少しなら大丈夫"}).json(t, &updated)
	if updated.Content != "辛いものは少しなら大丈夫" || updated.CreatedAt != spicy.CreatedAt {
		t.Fatalf("updated: %+v", updated)
	}
	if r := pro.do("PUT", fmt.Sprintf("/api/memories/%d", spicy.ID), map[string]string{"content": strings.Repeat("あ", 100)}); r.status != 422 {
		t.Fatalf("update to a duplicate: %d %s", r.status, r.body)
	}

	// Another user's memory is as missing as one that does not exist.
	other := proUser(t, env, "other")
	path := fmt.Sprintf("/api/memories/%d", spicy.ID)
	other.expect(404, "PUT", path, map[string]string{"content": "x"})
	other.expect(404, "DELETE", path, nil)
	other.expect(404, "PUT", "/api/memories/0", map[string]string{"content": "x"})

	// At most 30.
	for i := len(memoryContents(t, pro)); i < maxMemories; i++ {
		newMemory(t, pro, fmt.Sprintf("メモ%d", i))
	}
	r := pro.do("POST", "/api/memories", map[string]string{"content": "31番目"})
	if r.status != 422 || !strings.Contains(string(r.body), `"reason":"limit_reached"`) {
		t.Fatalf("31st: %d %s", r.status, r.body)
	}
	if m := memoryContents(t, pro); len(m) != maxMemories || m[0] != "辛いものは少しなら大丈夫" {
		t.Fatalf("list: %v", m)
	}

	// Downgraded: the memories can still be read and deleted, not added or changed.
	if err := env.store.SetUserTier(ctx, "pro", store.TierNormal); err != nil {
		t.Fatal(err)
	}
	pro.expect(403, "PUT", path, map[string]string{"content": "x"})
	pro.expect(204, "DELETE", path, nil)
	pro.expect(404, "DELETE", path, nil)
	if m := memoryContents(t, pro); len(m) != maxMemories-1 {
		t.Fatalf("after delete: %d", len(m))
	}
}

func TestIntegrationAssistantMemories(t *testing.T) {
	db := testdb.New(t)
	env := newAssistantEnv(t, db, DailyLimits{Normal: 30, Pro: 100, Site: 300})
	ctx := context.Background()
	b := proUser(t, env, "pro")
	spicy := newMemory(t, b, "辛いものが苦手")
	pork := newMemory(t, b, "豚骨ラーメンが好き")
	fish := newMemory(t, b, "魚が好き")

	env.ai.memoryChanges = []assistant.MemoryChange{
		{Op: "create", Content: ptr(" ベジタリアン ")},
		{Op: "update", MemoryID: &pork.ID, Content: ptr("醤油ラーメンが好き")},
		{Op: "delete", MemoryID: &spicy.ID},
		// Skipped: already there, an unknown id, a newline, no id, an unknown op,
		// the memory deleted above.
		{Op: "create", Content: ptr("ベジタリアン")},
		{Op: "delete", MemoryID: ptr(int64(999999))},
		{Op: "create", Content: ptr("a\nb")},
		{Op: "update", Content: ptr("x")},
		{Op: "remember", Content: ptr("x")},
		{Op: "update", MemoryID: &spicy.ID, Content: ptr("x")},
	}
	var got memoryReply
	b.expect(200, "POST", "/api/assistant/messages", message("2026-10-31", "覚えて")).json(t, &got)
	sent := env.ai.last().Context
	if !sent.AllowMemory || fmtJSON(t, sent.Memories) !=
		fmt.Sprintf(`[{"id":%d,"content":"辛いものが苦手"},{"id":%d,"content":"豚骨ラーメンが好き"},{"id":%d,"content":"魚が好き"}]`, spicy.ID, pork.ID, fish.ID) {
		t.Fatalf("context: allow_memory=%v memories=%s", sent.AllowMemory, fmtJSON(t, sent.Memories))
	}
	u := got.MemoryUpdates
	if len(u) != 3 || u[0].Op != "create" || u[0].Content != "ベジタリアン" || u[0].ID == 0 ||
		u[1].Op != "update" || u[1].ID != pork.ID || u[1].Content != "醤油ラーメンが好き" || *u[1].Before != "豚骨ラーメンが好き" ||
		u[2].Op != "delete" || u[2].ID != spicy.ID || u[2].Content != "辛いものが苦手" || u[2].Before != nil {
		t.Fatalf("memory_updates: %s", fmtJSON(t, u))
	}
	if m := memoryContents(t, b); fmtJSON(t, m) != `["醤油ラーメンが好き","魚が好き","ベジタリアン"]` {
		t.Fatalf("memories: %v", m)
	}

	// The user edits a memory on the Profile page while the reply is being
	// made: the assistant's change to it is skipped (optimistic lock).
	env.ai.memoryChanges = []assistant.MemoryChange{
		{Op: "update", MemoryID: &fish.ID, Content: ptr("魚は苦手")},
		{Op: "delete", MemoryID: &fish.ID},
	}
	user, err := env.store.UserByUsername(ctx, "pro")
	if err != nil {
		t.Fatal(err)
	}
	env.ai.during = func() {
		if _, err := env.store.UpdateMemory(ctx, user.ID, fish.ID, "白身魚が好き", nil); err != nil {
			t.Error(err)
		}
	}
	got = memoryReply{}
	b.expect(200, "POST", "/api/assistant/messages", message("2026-10-31", "魚は苦手")).json(t, &got)
	env.ai.during = nil
	if len(got.MemoryUpdates) != 0 {
		t.Fatalf("written over the user's edit: %s", fmtJSON(t, got.MemoryUpdates))
	}
	if m := memoryContents(t, b); !strings.Contains(fmtJSON(t, m), "白身魚が好き") {
		t.Fatalf("memories: %v", m)
	}

	// A message that is not answered saves nothing: here the proposal with
	// it is invalid, so the message gets 503.
	env.ai.memoryChanges = []assistant.MemoryChange{{Op: "create", Content: ptr("保存されない")}}
	env.ai.changes = []assistant.Change{createChange("2026-10-31", "brunch", "x", nil)}
	b.expect(503, "POST", "/api/assistant/messages", message("2026-10-31", "覚えて"))
	env.ai.changes = nil
	if m := memoryContents(t, b); strings.Contains(fmtJSON(t, m), "保存されない") {
		t.Fatalf("saved with an unanswered message: %v", m)
	}

	// Downgraded: the memories stay but are not sent, and changes are ignored.
	if err := env.store.SetUserTier(ctx, "pro", store.TierNormal); err != nil {
		t.Fatal(err)
	}
	got = memoryReply{}
	b.expect(200, "POST", "/api/assistant/messages", message("2026-10-31", "覚えて")).json(t, &got)
	sent = env.ai.last().Context
	if sent.AllowMemory || fmtJSON(t, sent.Memories) != `[]` {
		t.Fatalf("normal context: allow_memory=%v memories=%s", sent.AllowMemory, fmtJSON(t, sent.Memories))
	}
	if got.MemoryUpdates == nil || len(got.MemoryUpdates) != 0 {
		t.Fatalf("normal memory_updates: %s", fmtJSON(t, got.MemoryUpdates))
	}
	if m := memoryContents(t, b); len(m) != 3 {
		t.Fatalf("memories after downgrade: %v", m)
	}
}
