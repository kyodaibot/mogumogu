package httpserver

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"mogumogu/backend/internal/store"
	"mogumogu/backend/internal/tz"
)

const (
	maxRangeDays     = 62
	maxEventDuration = 24 * time.Hour
)

// dateRange parses the inclusive from/to query parameters (docs/api.md 6.2).
func dateRange(c *gin.Context) (tz.Date, tz.Date, bool) {
	rawFrom, rawTo := c.Query("from"), c.Query("to")
	if rawFrom == "" || rawTo == "" {
		abortError(c, http.StatusBadRequest, "invalid_request", "Query parameters from and to are required.")
		return tz.Date{}, tz.Date{}, false
	}
	var v validator
	from, err := tz.ParseDate(rawFrom)
	if err != nil {
		v.add("from", "invalid_format")
	}
	to, err := tz.ParseDate(rawTo)
	if err != nil {
		v.add("to", "invalid_format")
	}
	if !v.failed() {
		switch {
		case to.Before(from):
			v.add("to", "must_not_be_before_from")
		case from.DaysUntil(to)+1 > maxRangeDays:
			v.add("to", "range_too_large")
		}
	}
	if v.failed() {
		abortValidation(c, v.errs)
		return tz.Date{}, tz.Date{}, false
	}
	return from, to, true
}

// storeError maps store errors for single-record endpoints. Another user's
// record is indistinguishable from a missing one.
func (a *api) storeError(c *gin.Context, err error) {
	if errors.Is(err, store.ErrNotFound) {
		abortNotFound(c)
		return
	}
	abortInternal(c, a.logger, err)
}

// --- schedule events ---

type eventInput struct {
	Title    *string `json:"title"`
	StartsAt *string `json:"starts_at"`
	EndsAt   *string `json:"ends_at"`
	Notes    *string `json:"notes"`
}

type eventJSON struct {
	ID        int64   `json:"id"`
	Title     string  `json:"title"`
	StartsAt  string  `json:"starts_at"`
	EndsAt    string  `json:"ends_at"`
	Notes     *string `json:"notes"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

func toEventJSON(e store.ScheduleEvent) eventJSON {
	return eventJSON{
		ID: e.ID, Title: e.Title, Notes: e.Notes,
		StartsAt: tz.FormatInstant(e.StartsAt), EndsAt: tz.FormatInstant(e.EndsAt),
		CreatedAt: tz.FormatInstant(e.CreatedAt), UpdatedAt: tz.FormatInstant(e.UpdatedAt),
	}
}

func instant(v *validator, field string, p *string) (time.Time, bool) {
	if p == nil {
		v.add(field, "required")
		return time.Time{}, false
	}
	t, err := tz.ParseInstant(*p)
	if err != nil {
		v.add(field, "invalid_format")
		return time.Time{}, false
	}
	return t, true
}

// bindEvent decodes and validates an event body; it writes the error response
// and returns false on failure.
func bindEvent(c *gin.Context, userID int64) (store.ScheduleEvent, bool) {
	var in eventInput
	if !decodeJSON(c, &in) {
		return store.ScheduleEvent{}, false
	}
	var v validator
	e := store.ScheduleEvent{UserID: userID}
	e.Title = v.text("title", in.Title, 1, 100)
	start, okStart := instant(&v, "starts_at", in.StartsAt)
	end, okEnd := instant(&v, "ends_at", in.EndsAt)
	if okStart && okEnd {
		switch {
		case !end.After(start):
			v.add("ends_at", "must_be_after_starts_at")
		case end.Sub(start) > maxEventDuration:
			v.add("ends_at", "duration_too_long")
		}
	}
	e.StartsAt, e.EndsAt = start, end
	e.Notes = v.optionalText("notes", in.Notes, 1000)
	if v.failed() {
		abortValidation(c, v.errs)
		return store.ScheduleEvent{}, false
	}
	return e, true
}

func (a *api) listEvents(c *gin.Context) {
	sess, _ := sessionOf(c)
	from, to, ok := dateRange(c)
	if !ok {
		return
	}
	start, end := tz.RangeBounds(from, to)
	events, err := a.store.ListEvents(c.Request.Context(), sess.UserID, start, end)
	if err != nil {
		abortInternal(c, a.logger, err)
		return
	}
	items := make([]eventJSON, 0, len(events))
	for _, e := range events {
		items = append(items, toEventJSON(e))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *api) getEvent(c *gin.Context) {
	sess, _ := sessionOf(c)
	id, ok := parseID(c)
	if !ok {
		return
	}
	e, err := a.store.GetEvent(c.Request.Context(), sess.UserID, id)
	if err != nil {
		a.storeError(c, err)
		return
	}
	c.JSON(http.StatusOK, toEventJSON(e))
}

func (a *api) createEvent(c *gin.Context) {
	sess, _ := sessionOf(c)
	e, ok := bindEvent(c, sess.UserID)
	if !ok {
		return
	}
	created, err := a.store.CreateEvent(c.Request.Context(), e)
	if err != nil {
		abortInternal(c, a.logger, err)
		return
	}
	c.JSON(http.StatusCreated, toEventJSON(created))
}

func (a *api) updateEvent(c *gin.Context) {
	sess, _ := sessionOf(c)
	id, ok := parseID(c)
	if !ok {
		return
	}
	e, ok := bindEvent(c, sess.UserID)
	if !ok {
		return
	}
	e.ID = id
	updated, err := a.store.UpdateEvent(c.Request.Context(), e)
	if err != nil {
		a.storeError(c, err)
		return
	}
	c.JSON(http.StatusOK, toEventJSON(updated))
}

func (a *api) deleteEvent(c *gin.Context) {
	sess, _ := sessionOf(c)
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := a.store.DeleteEvent(c.Request.Context(), sess.UserID, id); err != nil {
		a.storeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// --- meal records ---

type mealInput struct {
	Date     *string  `json:"date"`
	MealType *string  `json:"meal_type"`
	Content  *string  `json:"content"`
	Calories *float64 `json:"calories"`
	ProteinG *float64 `json:"protein_g"`
	FatG     *float64 `json:"fat_g"`
	CarbsG   *float64 `json:"carbs_g"`
}

// Nutrition fields are pointers without omitempty: null (unknown) is always
// written out and never turned into 0.
type mealJSON struct {
	ID        int64    `json:"id"`
	Date      string   `json:"date"`
	MealType  string   `json:"meal_type"`
	Content   string   `json:"content"`
	Calories  *float64 `json:"calories"`
	ProteinG  *float64 `json:"protein_g"`
	FatG      *float64 `json:"fat_g"`
	CarbsG    *float64 `json:"carbs_g"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

func toMealJSON(m store.MealRecord) mealJSON {
	return mealJSON{
		ID: m.ID, Date: tz.DateOf(m.Date).String(), MealType: m.MealType, Content: m.Content,
		Calories: m.Calories, ProteinG: m.ProteinG, FatG: m.FatG, CarbsG: m.CarbsG,
		CreatedAt: tz.FormatInstant(m.CreatedAt), UpdatedAt: tz.FormatInstant(m.UpdatedAt),
	}
}

func bindMeal(c *gin.Context, userID int64) (store.MealRecord, bool) {
	var in mealInput
	if !decodeJSON(c, &in) {
		return store.MealRecord{}, false
	}
	var v validator
	m := store.MealRecord{UserID: userID}
	if in.Date == nil {
		v.add("date", "required")
	} else if d, err := tz.ParseDate(*in.Date); err != nil {
		v.add("date", "invalid_format")
	} else {
		m.Date = d.Time()
	}
	m.MealType = v.oneOf("meal_type", in.MealType, "breakfast", "lunch", "dinner", "snack")
	m.Content = v.text("content", in.Content, 1, 200)
	m.Calories = v.decimal("calories", in.Calories, 0, 10000, false)
	m.ProteinG = v.decimal("protein_g", in.ProteinG, 0, 1000, false)
	m.FatG = v.decimal("fat_g", in.FatG, 0, 1000, false)
	m.CarbsG = v.decimal("carbs_g", in.CarbsG, 0, 1000, false)
	if v.failed() {
		abortValidation(c, v.errs)
		return store.MealRecord{}, false
	}
	return m, true
}

func (a *api) listMeals(c *gin.Context) {
	sess, _ := sessionOf(c)
	from, to, ok := dateRange(c)
	if !ok {
		return
	}
	meals, err := a.store.ListMeals(c.Request.Context(), sess.UserID, from.Time(), to.Time())
	if err != nil {
		abortInternal(c, a.logger, err)
		return
	}
	items := make([]mealJSON, 0, len(meals))
	for _, m := range meals {
		items = append(items, toMealJSON(m))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *api) getMeal(c *gin.Context) {
	sess, _ := sessionOf(c)
	id, ok := parseID(c)
	if !ok {
		return
	}
	m, err := a.store.GetMeal(c.Request.Context(), sess.UserID, id)
	if err != nil {
		a.storeError(c, err)
		return
	}
	c.JSON(http.StatusOK, toMealJSON(m))
}

func (a *api) createMeal(c *gin.Context) {
	sess, _ := sessionOf(c)
	m, ok := bindMeal(c, sess.UserID)
	if !ok {
		return
	}
	created, err := a.store.CreateMeal(c.Request.Context(), m)
	if err != nil {
		abortInternal(c, a.logger, err)
		return
	}
	c.JSON(http.StatusCreated, toMealJSON(created))
}

func (a *api) updateMeal(c *gin.Context) {
	sess, _ := sessionOf(c)
	id, ok := parseID(c)
	if !ok {
		return
	}
	m, ok := bindMeal(c, sess.UserID)
	if !ok {
		return
	}
	m.ID = id
	updated, err := a.store.UpdateMeal(c.Request.Context(), m)
	if err != nil {
		a.storeError(c, err)
		return
	}
	c.JSON(http.StatusOK, toMealJSON(updated))
}

func (a *api) deleteMeal(c *gin.Context) {
	sess, _ := sessionOf(c)
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := a.store.DeleteMeal(c.Request.Context(), sess.UserID, id); err != nil {
		a.storeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
