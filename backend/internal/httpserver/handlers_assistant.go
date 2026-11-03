package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"mogumogu/backend/internal/assistant"
	"mogumogu/backend/internal/store"
	"mogumogu/backend/internal/tz"
)

const (
	// Days of meals before the target date (inclusive) sent with every message.
	mealWindowDays = 7
	// The agent's query tools may read at most this many days at once.
	agentMaxRangeDays = 31
	ctxAgentUser      = "agent_user"

	// A conversation has at most 10 turns: 9 in the history plus this one.
	maxHistoryTurns = 9
	// Lengths in characters of a message, a reply and a recommended dish;
	// the AI service and the browser have the same (contract/limits.json).
	maxMessageChars         = 500
	maxReplyChars           = 1000
	maxDishNameChars        = 80
	maxDishDescriptionChars = 200
	// Nine turns of Japanese at their length limits come to about 60 KB.
	assistantMaxBodyBytes = 128 << 10
)

// Replier is the AI service; *assistant.Client satisfies it.
type Replier interface {
	Reply(ctx context.Context, req assistant.Request) (assistant.Reply, error)
}

type assistantInput struct {
	Date    *string `json:"date"`
	Message *string `json:"message"`
	// Language of the interface, which the reply follows: "ja" or "en".
	// Optional; English when absent.
	Locale *string `json:"locale"`
	// Earlier turns, oldest first. Optional; absent for a new conversation.
	History []turnInput `json:"history"`
}

type turnInput struct {
	Date            *string     `json:"date"`
	Message         *string     `json:"message"`
	Reply           *string     `json:"reply"`
	Recommendations []dishInput `json:"recommendations"`
	// The change proposal of that reply, as a summary. Optional.
	Changes []turnChangeInput `json:"changes"`
}

type turnChangeInput struct {
	Op       *string `json:"op"`
	Date     *string `json:"date"`
	MealType *string `json:"meal_type"`
	Content  *string `json:"content"`
}

type dishInput struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// history checks the turns the browser sent. They are only checked for shape
// and the same limits the AI service's own output has; their content is not
// trusted (project-review 3.6).
func (v *validator) history(in []turnInput) []assistant.Turn {
	if len(in) > maxHistoryTurns {
		v.add("history", "too_long")
		return nil
	}
	out := make([]assistant.Turn, 0, len(in))
	for i, t := range in {
		field := func(name string) string { return fmt.Sprintf("history[%d].%s", i, name) }
		turn := assistant.Turn{Recommendations: []assistant.TurnDish{}}
		if t.Date == nil {
			v.add(field("date"), "required")
		} else if d, err := tz.ParseDate(*t.Date); err != nil {
			v.add(field("date"), "invalid_format")
		} else {
			turn.Date = d.String()
		}
		turn.Message = v.text(field("message"), t.Message, 1, maxMessageChars)
		turn.Reply = v.text(field("reply"), t.Reply, 1, maxReplyChars)
		// A reply has no recommendations or exactly three.
		if n := len(t.Recommendations); n != 0 && n != 3 {
			v.add(field("recommendations"), "invalid_value")
		}
		for j, r := range t.Recommendations {
			prefix := fmt.Sprintf("%s[%d].", field("recommendations"), j)
			turn.Recommendations = append(turn.Recommendations, assistant.TurnDish{
				Name:        v.text(prefix+"name", r.Name, 1, maxDishNameChars),
				Description: v.text(prefix+"description", r.Description, 1, maxDishDescriptionChars),
			})
		}
		if len(t.Changes) > maxProposalChanges {
			v.add(field("changes"), "too_long")
		}
		turn.Changes = []assistant.TurnChange{}
		for j, ch := range t.Changes {
			prefix := fmt.Sprintf("%s[%d].", field("changes"), j)
			tc := assistant.TurnChange{
				Op:       v.oneOf(prefix+"op", ch.Op, store.OpCreate, store.OpUpdate, store.OpDelete),
				MealType: v.oneOf(prefix+"meal_type", ch.MealType, mealTypes...),
				Content:  v.text(prefix+"content", ch.Content, 1, maxMealContentChars),
			}
			if ch.Date == nil {
				v.add(prefix+"date", "required")
			} else if d, err := tz.ParseDate(*ch.Date); err != nil {
				v.add(prefix+"date", "invalid_format")
			} else {
				tc.Date = d.String()
			}
			turn.Changes = append(turn.Changes, tc)
		}
		out = append(out, turn)
	}
	return out
}

// assistantReplyJSON is the answer to a message (docs/api.md 9.1).
type assistantReplyJSON struct {
	Reply           string                     `json:"reply"`
	Recommendations []assistant.Recommendation `json:"recommendations"`
	AskMealType     bool                       `json:"ask_meal_type"`
	MealType        *string                    `json:"meal_type"`
	// The changes to meal records the assistant proposes; null without any.
	Proposal *proposalJSON `json:"proposal"`
	// The memories the assistant saved with this reply; empty without any.
	MemoryUpdates []memoryUpdateJSON `json:"memory_updates"`
	// The web pages the reply cites; empty without any.
	Sources []sourceJSON `json:"sources"`
}

type sourceJSON struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

const (
	maxSources          = 5
	maxSourceURLBytes   = 2048
	maxSourceTitleRunes = 200
)

// checkSources keeps the sources the browser may show as links: http and
// https URLs only (never javascript: or data:), at most maxSources. The rest
// are dropped and logged; the reply itself is still returned.
func (a *api) checkSources(user store.User, sources []assistant.Source) []sourceJSON {
	out := []sourceJSON{}
	if len(sources) == 0 {
		return out
	}
	if user.Tier != store.TierPro {
		a.logger.Warn("sources ignored", slog.Int64("user_id", user.ID), slog.String("reason", "not_pro"))
		return out
	}
	dropped := 0
	for _, s := range sources {
		u, err := url.Parse(s.URL)
		if len(out) == maxSources || len(s.URL) > maxSourceURLBytes || err != nil ||
			(u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			dropped++
			continue
		}
		title := []rune(strings.TrimSpace(s.Title))
		if len(title) > maxSourceTitleRunes {
			title = title[:maxSourceTitleRunes]
		}
		out = append(out, sourceJSON{Title: string(title), URL: s.URL})
	}
	if dropped > 0 {
		a.logger.Warn("sources dropped", slog.Int64("user_id", user.ID), slog.Int("count", dropped))
	}
	return out
}

// postAssistantMessage answers one message to the meal assistant
// (docs/api.md 9): check the quota, assemble the context, call the AI service.
func (a *api) postAssistantMessage(c *gin.Context) {
	sess, _ := sessionOf(c)
	var in assistantInput
	if !decodeJSONLimit(c, &in, assistantMaxBodyBytes) {
		return
	}
	var v validator
	var target tz.Date
	if in.Date == nil {
		v.add("date", "required")
	} else if d, err := tz.ParseDate(*in.Date); err != nil {
		v.add("date", "invalid_format")
	} else {
		target = d
	}
	message := v.text("message", in.Message, 1, maxMessageChars)
	locale := "en"
	if in.Locale != nil {
		switch *in.Locale {
		case "ja", "en":
			locale = *in.Locale
		default:
			v.add("locale", "invalid_value")
		}
	}
	history := v.history(in.History)
	if v.failed() {
		abortValidation(c, v.errs)
		return
	}

	// Too many messages in a short time are refused before the daily quota,
	// so they do not use it up.
	limitKey := strconv.FormatInt(sess.UserID, 10)
	if blocked, retry := a.assistantLimiter.Blocked(limitKey); blocked {
		abortRateLimited(c, retry)
		return
	}
	a.assistantLimiter.Add(limitKey)

	ctx := c.Request.Context()
	now := time.Now()
	today := tz.Today(now)
	// The tier is read on every message, so a change takes effect at once.
	user, err := a.store.UserByID(ctx, sess.UserID)
	if err != nil {
		abortInternal(c, a.logger, err)
		return
	}
	userLimit := a.assistantLimits.forTier(user.Tier)
	err = a.store.TakeAssistantQuota(ctx, sess.UserID, today.Time(), userLimit, a.assistantLimits.Site)
	switch {
	case errors.Is(err, store.ErrUserQuotaExceeded):
		abortQuotaExceeded(c, today.AddDays(1).Start().Sub(now), userLimit)
		return
	case errors.Is(err, store.ErrSiteQuotaExceeded):
		// Not the user's own limit, so no number to show them.
		abortQuotaExceeded(c, today.AddDays(1).Start().Sub(now), 0)
		return
	}
	if err != nil {
		abortInternal(c, a.logger, err)
		return
	}

	reply, memories, err := a.askAssistant(ctx, user, target, message, locale, history, now)
	var proposal *proposalJSON
	if err == nil {
		proposal, err = a.saveProposal(ctx, user, reply.Changes)
	}
	if err != nil {
		// The message was not answered, so it does not count. The request may
		// have been cancelled; the refund must still happen.
		if rerr := a.store.RefundAssistantQuota(context.WithoutCancel(ctx), sess.UserID, today.Time()); rerr != nil {
			a.logger.Error("assistant quota refund failed", slog.String("error", rerr.Error()))
		}
		a.logger.Warn("assistant unavailable", slog.String("error", err.Error()))
		abortError(c, http.StatusServiceUnavailable, "ai_unavailable", "The meal assistant is unavailable. Try again later.")
		return
	}
	// Only once the message is answered, so a refunded message saves nothing.
	updates := a.saveMemories(ctx, user, reply.MemoryChanges, memories)
	c.JSON(http.StatusOK, assistantReplyJSON{
		Reply: reply.Reply, Recommendations: reply.Recommendations,
		AskMealType: reply.AskMealType, MealType: reply.MealType, Proposal: proposal,
		MemoryUpdates: updates, Sources: a.checkSources(user, reply.Sources),
	})
}

// askAssistant assembles the initial context (project-review 3.6) and calls
// the AI service. It also returns the memories in that context, which the
// memory changes in the reply are checked against.
func (a *api) askAssistant(ctx context.Context, user store.User, target tz.Date, message, locale string, history []assistant.Turn, now time.Time) (assistant.Reply, []store.Memory, error) {
	userID := user.ID
	d := contextData{Target: target, Now: now, Locale: locale, Pro: user.Tier == store.TierPro}
	p, err := a.store.GetProfile(ctx, userID)
	switch {
	case err == nil:
		d.Profile = &p
	case !errors.Is(err, store.ErrNotFound):
		return assistant.Reply{}, nil, err
	}

	// A downgraded user keeps their memories, but they are not used.
	if d.Pro {
		if d.Memories, err = a.store.ListMemories(ctx, userID); err != nil {
			return assistant.Reply{}, nil, err
		}
	}

	d.EventsFrom, d.EventsTo = target.AddDays(-1), target.AddDays(1)
	start, end := tz.RangeBounds(d.EventsFrom, d.EventsTo)
	if d.Events, err = a.store.ListEvents(ctx, userID, start, end); err != nil {
		return assistant.Reply{}, nil, err
	}

	// Future records are plans, not meals eaten: the window ends today at the latest.
	d.MealsTo = target
	if today := tz.Today(now); today.Before(target) {
		d.MealsTo = today
	}
	d.MealsFrom = d.MealsTo.AddDays(-(mealWindowDays - 1))
	if d.Meals, err = a.store.ListMeals(ctx, userID, d.MealsFrom.Time(), d.MealsTo.Time()); err != nil {
		return assistant.Reply{}, nil, err
	}

	reply, err := a.assistant.Reply(ctx, assistant.Request{
		Message: message,
		History: history,
		Token:   a.agentTokens.Issue(userID),
		UserID:  userID,
		Tier:    string(user.Tier),
		Context: assistantContext(d),
	})
	return reply, d.Memories, err
}

// contextData is what askAssistant reads for one message.
type contextData struct {
	Target               tz.Date
	Now                  time.Time
	Locale               string
	Pro                  bool
	Profile              *store.Profile // nil before the user saves one
	Memories             []store.Memory
	Events               []store.ScheduleEvent
	EventsFrom, EventsTo tz.Date
	Meals                []store.MealRecord
	MealsFrom, MealsTo   tz.Date
}

// assistantContext turns it into the context the AI service reads, the
// format of contract/request.json.
func assistantContext(d contextData) assistant.Context {
	var profile any
	if d.Profile != nil {
		profile = toProfileJSON(*d.Profile)
	}
	memoryItems := make([]contextMemory, 0, len(d.Memories))
	for _, m := range d.Memories {
		memoryItems = append(memoryItems, contextMemory{ID: m.ID, Content: m.Content})
	}
	eventItems := make([]eventJSON, 0, len(d.Events))
	for _, e := range d.Events {
		eventItems = append(eventItems, toEventJSON(e))
	}
	mealItems := make([]mealJSON, 0, len(d.Meals))
	for _, m := range d.Meals {
		mealItems = append(mealItems, toMealJSON(m))
	}
	return assistant.Context{
		TargetDate: d.Target.String(),
		Now:        tz.FormatInstant(d.Now),
		Timezone:   tz.Tokyo.String(),
		Locale:     d.Locale,
		// Checked again when the reply comes back and when the proposal runs.
		AllowProposal: d.Pro,
		// Checked again when the reply comes back.
		AllowMemory: d.Pro,
		// Checked again when the reply comes back (its sources).
		AllowSearch: d.Pro,
		Memories:    memoryItems,
		Profile:     profile,
		Events:      eventItems,
		EventsRange: assistant.Range{From: d.EventsFrom.String(), To: d.EventsTo.String()},
		Meals:       mealItems,
		MealsRange:  assistant.Range{From: d.MealsFrom.String(), To: d.MealsTo.String()},
	}
}

// --- internal API for the agent's query tools ---

// agentAuth accepts only a valid agent token and records whose data it reads.
func (a *api) agentAuth(c *gin.Context) {
	token, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
	userID, err := a.agentTokens.Verify(token)
	if !ok || err != nil {
		abortError(c, http.StatusUnauthorized, "unauthenticated", "Authentication required.")
		return
	}
	c.Set(ctxAgentUser, userID)
	c.Next()
}

func (a *api) agentEvents(c *gin.Context) {
	a.writeEvents(c, c.GetInt64(ctxAgentUser), agentMaxRangeDays)
}

func (a *api) agentMeals(c *gin.Context) {
	a.writeMeals(c, c.GetInt64(ctxAgentUser), agentMaxRangeDays)
}
