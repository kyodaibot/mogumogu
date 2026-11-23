package httpserver

import (
	"crypto/rand"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"mogumogu/backend/internal/assistant"
	"mogumogu/backend/internal/store"
	"mogumogu/backend/internal/tz"
)

// MenuRequests shares a short-lived query between the public request and its
// internal tool call. Releasing the request also releases all menu data.
type MenuRequests struct{ active sync.Map }

type menuRequest struct {
	target tz.Date
	now    time.Time
	campus string
	mu     sync.Mutex
	result menuResult
}

func (r *menuRequest) render(reply assistant.Reply) ([]cafeteriaCard, error) {
	if len(reply.Recommendations) > 0 {
		return nil, errMenuSelection
	}
	if len(reply.MenuSelections) == 0 {
		return nil, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if reply.MenuQueryID == "" || reply.MenuQueryID != r.result.ID || reply.MealType == nil ||
		*reply.MealType != r.result.Meal || r.result.Status != "ready" {
		return nil, errMenuSelection
	}
	return r.result.cards(reply.MenuSelections)
}

func (a *api) agentMenu(c *gin.Context) {
	token, _ := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
	value, ok := a.menuRequests.active.Load(token)
	if !ok {
		abortError(c, http.StatusForbidden, "inactive_request", "The menu request has ended.")
		return
	}
	req := value.(*menuRequest)
	date, err := tz.ParseDate(c.Query("date"))
	meal, clock, campus, cafeteria := c.Query("meal_type"), c.Query("time"), c.Query("campus"), strings.TrimSpace(c.Query("cafeteria"))
	var v validator
	if err != nil || date != req.target {
		v.add("date", "invalid_value")
	}
	if !slices.Contains(mealTypes, meal) {
		v.add("meal_type", "invalid_value")
	}
	if clock != "" && !validClock(clock) {
		v.add("time", "invalid_format")
	}
	if campus != "" && !slices.Contains([]string{"yoshida", "uji", "katsura", "all"}, campus) {
		v.add("campus", "invalid_value")
	}
	if b := c.Query("use_today"); b != "" && b != "true" && b != "false" {
		v.add("use_today", "invalid_value")
	}
	v.length("cafeteria", cafeteria, 0, 80)
	if v.failed() {
		abortValidation(c, v.errs)
		return
	}

	today := tz.Today(req.now)
	// Weekday rules always follow the day the user will eat.
	filter := menuFilter{meal: meal, clock: clock, day: date.Time()}
	q := menuResult{ID: rand.Text(), Date: date.String(), Meal: meal, Stores: []candidateStore{}, Unavailable: []missingStore{}}
	// The only cross-date fallback is tomorrow, on explicit request.
	if today.Before(date) {
		if today.AddDays(1) == date && c.Query("use_today") == "true" {
			date = today
			q.Date, q.Reference = date.String(), true
			filter.reference = true
		} else {
			q.Status = "await_update"
		}
	}
	if q.Status == "" {
		if campus == "" {
			campus = req.campus
		}
		if cafeteria != "" {
			campus = "all"
		}
		rows, err := a.store.CafeteriaMenu(c.Request.Context(), q.Date, campus, cafeteria)
		if err != nil {
			abortInternal(c, a.logger, err)
			return
		}
		q.populate(rows, filter)
	}
	req.mu.Lock()
	req.result = q
	req.mu.Unlock()
	c.JSON(http.StatusOK, q)
}

func (q *menuResult) populate(rows []store.CafeteriaMenuRow, filter menuFilter) {
	anySnapshot := false
	byShop := map[string]int{}
	for _, r := range rows {
		if r.SnapshotID == nil {
			missing := missingStore{ID: r.ID, Name: r.Name}
			if r.LastCheckedAt != nil {
				missing.LastCheckedAt = tz.FormatInstant(*r.LastCheckedAt)
			}
			q.Unavailable = append(q.Unavailable, missing)
			continue
		}
		anySnapshot = true
		if r.DishCode == nil {
			continue
		}
		// Today's opening hours and holiday flag say nothing about tomorrow.
		hours := r.Intervals.V
		if filter.reference {
			hours = nil
		} else {
			filter.holiday = r.IsHoliday
		}
		windows := filter.itemWindows(r.ID, r.Categories.Items, hours)
		if len(windows) == 0 {
			continue
		}
		index, ok := byShop[r.ID]
		if !ok {
			index = len(q.Stores)
			byShop[r.ID] = index
			url := r.HoursURL
			if url == "" {
				url = "https://www.s-coop.net/shop_guide/" + r.Campus + "_shop/"
			}
			s := candidateStore{ID: r.ID, Name: r.Name, Campus: r.Campus, HoursURL: url, Hours: []string{}, CheckedAt: tz.FormatInstant(*r.CheckedAt)}
			if hours != nil {
				s.Hours = displayHours(intersection(hours, []store.OpenInterval{filter.mealWindow()}))
			}
			q.Stores = append(q.Stores, s)
		}
		s := &q.Stores[index]
		s.Items = append(s.Items, menuCandidate{ID: *r.DishCode, Name: r.DishName, URL: r.SourceURL, Price: r.Price,
			Categories: r.Categories.Items, Allergens: allergenLabels(r.Allergens.Items, r.Allergens.Valid), Windows: windows,
			Nutrition: menuNutrition{Calories: r.Calories, ProteinG: r.ProteinG, FatG: r.FatG, CarbsG: r.CarbsG}})
	}
	switch {
	case len(q.Stores) > 0:
		q.Status = "ready"
	case anySnapshot:
		q.Status = "no_candidates"
	default:
		q.Status = "no_snapshot"
	}
}
