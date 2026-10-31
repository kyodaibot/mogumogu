package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/gin-gonic/gin"

	"mogumogu/backend/internal/assistant"
	"mogumogu/backend/internal/store"
	"mogumogu/backend/internal/tz"
)

const (
	// A proposal holds at most this many changes (plans/change-proposals.md).
	maxProposalChanges = 10
	// A proposal can run for this long after the reply that made it.
	proposalTTL = 2 * time.Hour
)

// errInvalidProposal means the AI service staged changes Go refuses; the
// message is then answered like any other AI service failure.
var errInvalidProposal = errors.New("invalid proposal")

// proposedMealJSON is a meal record in a proposal. ID is absent for a record
// still to be created.
type proposedMealJSON struct {
	ID       int64    `json:"id,omitempty"`
	Date     string   `json:"date"`
	MealType string   `json:"meal_type"`
	Content  string   `json:"content"`
	Calories *float64 `json:"calories"`
	ProteinG *float64 `json:"protein_g"`
	FatG     *float64 `json:"fat_g"`
	CarbsG   *float64 `json:"carbs_g"`
}

// changeJSON is one change: before is null for a create, after for a delete.
type changeJSON struct {
	Op     string            `json:"op"`
	Before *proposedMealJSON `json:"before"`
	After  *proposedMealJSON `json:"after"`
}

type proposalJSON struct {
	ID        int64        `json:"id"`
	ExpiresAt string       `json:"expires_at"`
	Changes   []changeJSON `json:"changes"`
}

func toProposedMeal(m *store.MealRecord) *proposedMealJSON {
	if m == nil {
		return nil
	}
	return &proposedMealJSON{
		ID: m.ID, Date: tz.DateOf(m.Date).String(), MealType: m.MealType, Content: m.Content,
		Calories: m.Calories, ProteinG: m.ProteinG, FatG: m.FatG, CarbsG: m.CarbsG,
	}
}

func toProposalJSON(p store.Proposal) *proposalJSON {
	changes := make([]changeJSON, 0, len(p.Changes.V))
	for _, c := range p.Changes.V {
		changes = append(changes, changeJSON{Op: c.Op, Before: toProposedMeal(c.Before), After: toProposedMeal(c.After)})
	}
	return &proposalJSON{ID: p.ID, ExpiresAt: tz.FormatInstant(p.CreatedAt.Add(proposalTTL)), Changes: changes}
}

// mealInputOf is a meal record as the fields of a request body.
func mealInputOf(m store.MealRecord) mealInput {
	date, mealType, content := tz.DateOf(m.Date).String(), m.MealType, m.Content
	return mealInput{
		Date: &date, MealType: &mealType, Content: &content,
		Calories: m.Calories, ProteinG: m.ProteinG, FatG: m.FatG, CarbsG: m.CarbsG,
	}
}

// overlay sets the fields a staged change gives on top of in.
func overlay(in mealInput, m assistant.ChangeMeal) mealInput {
	if m.Date != nil {
		in.Date = m.Date
	}
	if m.MealType != nil {
		in.MealType = m.MealType
	}
	if m.Content != nil {
		in.Content = m.Content
	}
	if m.Nutrition.Set {
		in.Calories, in.ProteinG, in.FatG, in.CarbsG = nil, nil, nil, nil
		if n := m.Nutrition.Value; n != nil {
			in.Calories, in.ProteinG, in.FatG, in.CarbsG = &n.Calories, &n.ProteinG, &n.FatG, &n.CarbsG
		}
	}
	return in
}

func sameMeal(a, b store.MealRecord) bool {
	eq := func(x, y *float64) bool { return (x == nil && y == nil) || (x != nil && y != nil && *x == *y) }
	return tz.DateOf(a.Date) == tz.DateOf(b.Date) && a.MealType == b.MealType && a.Content == b.Content &&
		eq(a.Calories, b.Calories) && eq(a.ProteinG, b.ProteinG) && eq(a.FatG, b.FatG) && eq(a.CarbsG, b.CarbsG)
}

// saveProposal checks the changes the assistant staged, as the public API
// would check them, and saves them with each record's current content (the
// "before" on the card, and the optimistic lock). Nil when there are none.
func (a *api) saveProposal(ctx context.Context, user store.User, changes []assistant.Change) (*proposalJSON, error) {
	if len(changes) == 0 {
		return nil, nil
	}
	if user.Tier != store.TierPro {
		return nil, fmt.Errorf("%w: proposals are for pro users", errInvalidProposal)
	}
	if len(changes) > maxProposalChanges {
		return nil, fmt.Errorf("%w: %d changes", errInvalidProposal, len(changes))
	}
	var v validator
	out := make([]store.ProposalChange, 0, len(changes))
	seen := map[int64]bool{}
	for i, c := range changes {
		prefix := fmt.Sprintf("changes[%d].", i)
		if c.Op == store.OpCreate {
			if c.Meal == nil || c.MealID != nil {
				v.add(prefix+"meal", "invalid_value")
				continue
			}
			m := checkMeal(&v, prefix+"meal.", overlay(mealInput{}, *c.Meal))
			out = append(out, store.ProposalChange{Op: c.Op, After: &m})
			continue
		}
		if c.Op != store.OpUpdate && c.Op != store.OpDelete {
			v.add(prefix+"op", "invalid_value")
			continue
		}
		if c.MealID == nil || seen[*c.MealID] || (c.Op == store.OpUpdate) != (c.Meal != nil) {
			v.add(prefix+"meal_id", "invalid_value")
			continue
		}
		seen[*c.MealID] = true
		// Another user's record is as missing as one that does not exist.
		before, err := a.store.GetMeal(ctx, user.ID, *c.MealID)
		if errors.Is(err, store.ErrNotFound) {
			v.add(prefix+"meal_id", "not_found")
			continue
		}
		if err != nil {
			return nil, err
		}
		change := store.ProposalChange{Op: c.Op, Before: &before}
		if c.Op == store.OpUpdate {
			after := checkMeal(&v, prefix+"meal.", overlay(mealInputOf(before), *c.Meal))
			if !v.failed() && sameMeal(before, after) {
				v.add(prefix+"meal", "unchanged")
			}
			after.ID = before.ID
			change.After = &after
		}
		out = append(out, change)
	}
	if v.failed() {
		// Field names and reasons only; never the content.
		return nil, fmt.Errorf("%w: %v", errInvalidProposal, v.errs)
	}
	p, err := a.store.CreateProposal(ctx, user.ID, out)
	if err != nil {
		return nil, err
	}
	return toProposalJSON(p), nil
}

type executeInput struct {
	// Indexes of the changes the user kept checked.
	Accepted []int `json:"accepted"`
}

// executeProposal runs a proposal once the user confirms it (docs/api.md 9.4).
func (a *api) executeProposal(c *gin.Context) {
	sess, _ := sessionOf(c)
	id, ok := parseID(c)
	if !ok {
		return
	}
	var in executeInput
	if !decodeJSON(c, &in) {
		return
	}
	accepted := slices.Clone(in.Accepted)
	slices.Sort(accepted)
	var v validator
	switch {
	case len(accepted) == 0:
		v.add("accepted", "required")
	case len(slices.Compact(slices.Clone(accepted))) != len(accepted):
		v.add("accepted", "invalid_value")
	}
	if v.failed() {
		abortValidation(c, v.errs)
		return
	}

	ctx := c.Request.Context()
	// A downgraded user cannot run proposals made while they were Pro.
	user, err := a.store.UserByID(ctx, sess.UserID)
	if err != nil {
		abortInternal(c, a.logger, err)
		return
	}
	if user.Tier != store.TierPro {
		abortError(c, http.StatusForbidden, "pro_required", "This feature needs a Pro account.")
		return
	}
	p, err := a.store.ExecuteProposal(ctx, user.ID, id, accepted, time.Now().Add(-proposalTTL))
	switch {
	case errors.Is(err, store.ErrNotFound):
		abortNotFound(c)
		return
	case errors.Is(err, store.ErrProposalExecuted):
		abortError(c, http.StatusConflict, "proposal_executed", "The proposal has already been executed.")
		return
	case errors.Is(err, store.ErrProposalExpired):
		abortError(c, http.StatusGone, "proposal_expired", "The proposal has expired.")
		return
	case errors.Is(err, store.ErrNoSuchChange):
		abortValidation(c, []fieldError{{Field: "accepted", Reason: "invalid_value"}})
		return
	case err != nil:
		abortInternal(c, a.logger, err)
		return
	}

	counts := map[string]int{}
	for _, r := range p.Results.V {
		counts[r.Status]++
	}
	// The audit line: who ran which proposal, and how it went.
	a.logger.Info("proposal executed", slog.Int64("proposal_id", p.ID), slog.Int64("user_id", user.ID),
		slog.Int("done", counts[store.ResultDone]), slog.Int("failed", counts[store.ResultFailed]),
		slog.Int("skipped", counts[store.ResultSkipped]))
	c.JSON(http.StatusOK, gin.H{"results": p.Results.V})
}
