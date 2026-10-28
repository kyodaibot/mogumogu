package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
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
)

// Replier is the AI service; *assistant.Client satisfies it.
type Replier interface {
	Reply(ctx context.Context, req assistant.Request) (assistant.Reply, error)
}

type assistantInput struct {
	Date    *string `json:"date"`
	Message *string `json:"message"`
}

// postAssistantMessage answers one message to the meal assistant
// (docs/api.md 9): check the quota, assemble the context, call the AI service.
func (a *api) postAssistantMessage(c *gin.Context) {
	sess, _ := sessionOf(c)
	var in assistantInput
	if !decodeJSON(c, &in) {
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
	if v.failed() {
		abortValidation(c, v.errs)
		return
	}

	ctx := c.Request.Context()
	now := time.Now()
	today := tz.Today(now)
	err := a.store.TakeAssistantQuota(ctx, sess.UserID, today.Time(), a.assistantUserLimit, a.assistantSiteLimit)
	if errors.Is(err, store.ErrQuotaExceeded) {
		abortRateLimited(c, today.AddDays(1).Start().Sub(now))
		return
	}
	if err != nil {
		abortInternal(c, a.logger, err)
		return
	}

	reply, err := a.askAssistant(ctx, sess.UserID, target, message, now)
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
func (a *api) askAssistant(ctx context.Context, userID int64, target tz.Date, message string, now time.Time) (assistant.Reply, error) {
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
		Token:   a.agentTokens.Issue(userID),
		Context: assistant.Context{
			TargetDate:  target.String(),
			Now:         tz.FormatInstant(now),
			Timezone:    tz.Tokyo.String(),
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
