package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"

	"mogumogu/backend/internal/assistant"
	"mogumogu/backend/internal/store"
	"mogumogu/backend/internal/tz"
)

// A user keeps at most maxMemories memories of at most maxMemoryChars
// characters each (plans/archive/memories.md); all of them go into every context.
const (
	maxMemories    = 30
	maxMemoryChars = 100
)

type memoryInput struct {
	Content *string `json:"content"`
}

type memoryJSON struct {
	ID        int64  `json:"id"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func toMemoryJSON(m store.Memory) memoryJSON {
	return memoryJSON{
		ID: m.ID, Content: m.Content,
		CreatedAt: tz.FormatInstant(m.CreatedAt), UpdatedAt: tz.FormatInstant(m.UpdatedAt),
	}
}

// memoryContent checks a memory's text: one line, without control characters.
func (v *validator) memoryContent(field string, p *string) string {
	s := v.text(field, p, 1, maxMemoryChars)
	if strings.ContainsFunc(s, unicode.IsControl) {
		v.add(field, "invalid_format")
	}
	return s
}

// requirePro answers 403 pro_required unless the session's user is Pro now.
func (a *api) requirePro(c *gin.Context) (store.User, bool) {
	sess, _ := sessionOf(c)
	user, err := a.store.UserByID(c.Request.Context(), sess.UserID)
	if err != nil {
		abortInternal(c, a.logger, err)
		return store.User{}, false
	}
	if user.Tier != store.TierPro {
		abortError(c, http.StatusForbidden, "pro_required", "This feature needs a Pro account.")
		return store.User{}, false
	}
	return user, true
}

// memoryWriteError maps the store's refusals to the response (docs/api.md 10).
func (a *api) memoryWriteError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrMemoryDuplicate):
		abortValidation(c, []fieldError{{Field: "content", Reason: "duplicate_item"}})
	case errors.Is(err, store.ErrMemoryFull):
		abortValidation(c, []fieldError{{Field: "content", Reason: "limit_reached"}})
	default:
		a.storeError(c, err)
	}
}

func bindMemory(c *gin.Context) (string, bool) {
	var in memoryInput
	if !decodeJSON(c, &in) {
		return "", false
	}
	var v validator
	content := v.memoryContent("content", in.Content)
	if v.failed() {
		abortValidation(c, v.errs)
		return "", false
	}
	return content, true
}

// listMemories is open to every tier: a downgraded user can still see and
// delete what was saved while they were Pro.
func (a *api) listMemories(c *gin.Context) {
	sess, _ := sessionOf(c)
	memories, err := a.store.ListMemories(c.Request.Context(), sess.UserID)
	if err != nil {
		abortInternal(c, a.logger, err)
		return
	}
	items := make([]memoryJSON, 0, len(memories))
	for _, m := range memories {
		items = append(items, toMemoryJSON(m))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *api) createMemory(c *gin.Context) {
	user, ok := a.requirePro(c)
	if !ok {
		return
	}
	content, ok := bindMemory(c)
	if !ok {
		return
	}
	m, err := a.store.CreateMemory(c.Request.Context(), user.ID, content, maxMemories)
	if err != nil {
		a.memoryWriteError(c, err)
		return
	}
	c.JSON(http.StatusCreated, toMemoryJSON(m))
}

func (a *api) updateMemory(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	user, ok := a.requirePro(c)
	if !ok {
		return
	}
	content, ok := bindMemory(c)
	if !ok {
		return
	}
	m, err := a.store.UpdateMemory(c.Request.Context(), user.ID, id, content, nil)
	if err != nil {
		a.memoryWriteError(c, err)
		return
	}
	c.JSON(http.StatusOK, toMemoryJSON(m))
}

func (a *api) deleteMemory(c *gin.Context) {
	sess, _ := sessionOf(c)
	id, ok := parseID(c)
	if !ok {
		return
	}
	if _, err := a.store.DeleteMemory(c.Request.Context(), sess.UserID, id, nil); err != nil {
		a.storeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// --- memories the assistant changes ---

// memoryUpdateJSON is one memory the assistant changed with a reply. Content
// is the memory after a create or update and the removed one after a delete;
// Before is the content an update replaced.
type memoryUpdateJSON struct {
	Op      string  `json:"op"`
	ID      int64   `json:"id"`
	Content string  `json:"content"`
	Before  *string `json:"before"`
}

// contextMemory is a memory as the AI service sees it.
type contextMemory struct {
	ID      int64  `json:"id"`
	Content string `json:"content"`
}

// saveMemories writes the memory changes that came with a reply, without a
// confirmation (plans/archive/memories.md). A change that fails a check is skipped
// and the rest still run; only the ones written are returned. known are the
// memories that were in the context: updates and deletes may only name them,
// and only while they are unchanged since (the optimistic lock).
func (a *api) saveMemories(ctx context.Context, user store.User, changes []assistant.MemoryChange, known []store.Memory) []memoryUpdateJSON {
	out := []memoryUpdateJSON{}
	if len(changes) == 0 {
		return out
	}
	if user.Tier != store.TierPro {
		a.logger.Warn("memory changes ignored", slog.Int64("user_id", user.ID), slog.String("reason", "not_pro"))
		return out
	}
	byID := make(map[int64]store.Memory, len(known))
	for _, m := range known {
		byID[m.ID] = m
	}
	for i, ch := range changes {
		if i == maxMemories {
			a.logger.Warn("memory changes ignored", slog.Int64("user_id", user.ID), slog.String("reason", "too_many"))
			break
		}
		u, err := a.saveMemory(ctx, user.ID, ch, byID)
		if err != nil {
			// The reason only; never the content.
			a.logger.Info("memory change skipped", slog.Int64("user_id", user.ID),
				slog.String("op", ch.Op), slog.String("reason", err.Error()))
			continue
		}
		// The audit line: who changed which memory.
		a.logger.Info("memory updated", slog.Int64("user_id", user.ID),
			slog.String("op", u.Op), slog.Int64("memory_id", u.ID))
		out = append(out, u)
	}
	return out
}

var errInvalidMemoryChange = errors.New("invalid memory change")

func (a *api) saveMemory(ctx context.Context, userID int64, ch assistant.MemoryChange, known map[int64]store.Memory) (memoryUpdateJSON, error) {
	var v validator
	if ch.Op == store.OpCreate {
		content := v.memoryContent("content", ch.Content)
		if v.failed() || ch.MemoryID != nil {
			return memoryUpdateJSON{}, errInvalidMemoryChange
		}
		m, err := a.store.CreateMemory(ctx, userID, content, maxMemories)
		if err != nil {
			return memoryUpdateJSON{}, err
		}
		return memoryUpdateJSON{Op: ch.Op, ID: m.ID, Content: m.Content}, nil
	}

	if ch.MemoryID == nil {
		return memoryUpdateJSON{}, errInvalidMemoryChange
	}
	before, ok := known[*ch.MemoryID]
	if !ok {
		return memoryUpdateJSON{}, store.ErrNotFound
	}
	switch ch.Op {
	case store.OpUpdate:
		content := v.memoryContent("content", ch.Content)
		if v.failed() {
			return memoryUpdateJSON{}, errInvalidMemoryChange
		}
		m, err := a.store.UpdateMemory(ctx, userID, before.ID, content, &before.UpdatedAt)
		if err != nil {
			return memoryUpdateJSON{}, err
		}
		return memoryUpdateJSON{Op: ch.Op, ID: m.ID, Content: m.Content, Before: &before.Content}, nil
	case store.OpDelete:
		m, err := a.store.DeleteMemory(ctx, userID, before.ID, &before.UpdatedAt)
		if err != nil {
			return memoryUpdateJSON{}, err
		}
		return memoryUpdateJSON{Op: ch.Op, ID: m.ID, Content: m.Content}, nil
	}
	return memoryUpdateJSON{}, errInvalidMemoryChange
}
