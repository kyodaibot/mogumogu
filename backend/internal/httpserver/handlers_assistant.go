package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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
		turn.Message = v.text(field("message"), t.Message, 1, 500)
		turn.Reply = v.text(field("reply"), t.Reply, 1, 1000)
		// A reply has no recommendations or exactly three.
		if n := len(t.Recommendations); n != 0 && n != 3 {
			v.add(field("recommendations"), "invalid_value")
		}
		for j, r := range t.Recommendations {
			prefix := fmt.Sprintf("%s[%d].", field("recommendations"), j)
			turn.Recommendations = append(turn.Recommendations, assistant.TurnDish{
				Name:        v.text(prefix+"name", r.Name, 1, 80),
				Description: v.text(prefix+"description", r.Description, 1, 200),
			})
		}
		out = append(out, turn)
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
	message := v.text("message", in.Message, 1, 500)
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
	err := a.store.TakeAssistantQuota(ctx, sess.UserID, today.Time(), a.assistantUserLimit, a.assistantSiteLimit)
	if errors.Is(err, store.ErrQuotaExceeded) {
		abortQuotaExceeded(c, today.AddDays(1).Start().Sub(now))
		return
	}
	if err != nil {
		abortInternal(c, a.logger, err)
		return
	}

	reply, err := a.askAssistant(ctx, sess.UserID, target, message, locale, history, now)
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
	c.JSON(http.StatusOK, reply)
}

// askAssistant assembles the initial context (project-review 3.6) and calls
// the AI service.
func (a *api) askAssistant(ctx context.Context, userID int64, target tz.Date, message, locale string, history []assistant.Turn, now time.Time) (assistant.Reply, error) {
	var profile any
	p, err := a.store.GetProfile(ctx, userID)
	switch {
	case err == nil:
		profile = toProfileJSON(p)
	case !errors.Is(err, store.ErrNotFound):
		return assistant.Reply{}, err
	}

	evFrom, evTo := target.AddDays(-1), target.AddDays(1)
	start, end := tz.RangeBounds(evFrom, evTo)
	events, err := a.store.ListEvents(ctx, userID, start, end)
	if err != nil {
		return assistant.Reply{}, err
	}
	eventItems := make([]eventJSON, 0, len(events))
	for _, e := range events {
		eventItems = append(eventItems, toEventJSON(e))
	}

	// Future records are plans, not meals eaten: the window ends today at the latest.
	mealTo := target
	if today := tz.Today(now); today.Before(target) {
		mealTo = today
	}
	mealFrom := mealTo.AddDays(-(mealWindowDays - 1))
	meals, err := a.store.ListMeals(ctx, userID, mealFrom.Time(), mealTo.Time())
	if err != nil {
		return assistant.Reply{}, err
	}
	mealItems := make([]mealJSON, 0, len(meals))
	for _, m := range meals {
		mealItems = append(mealItems, toMealJSON(m))
	}

	return a.assistant.Reply(ctx, assistant.Request{
		Message: message,
		History: history,
		Token:   a.agentTokens.Issue(userID),
		Context: assistant.Context{
			TargetDate:  target.String(),
			Now:         tz.FormatInstant(now),
			Timezone:    tz.Tokyo.String(),
			Locale:      locale,
			Profile:     profile,
			Events:      eventItems,
			EventsRange: assistant.Range{From: evFrom.String(), To: evTo.String()},
			Meals:       mealItems,
			MealsRange:  assistant.Range{From: mealFrom.String(), To: mealTo.String()},
		},
	})
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
