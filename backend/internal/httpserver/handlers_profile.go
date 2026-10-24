package httpserver

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"mogumogu/backend/internal/store"
	"mogumogu/backend/internal/tz"
)

type profileInput struct {
	HeightCM         *float64  `json:"height_cm"`
	WeightKG         *float64  `json:"weight_kg"`
	Age              *int      `json:"age"`
	HasExerciseHabit *bool     `json:"has_exercise_habit"`
	Allergens        *[]string `json:"allergens"`
	Goal             *string   `json:"goal"`
}

type profileJSON struct {
	HeightCM         float64   `json:"height_cm"`
	WeightKG         float64   `json:"weight_kg"`
	Age              int       `json:"age"`
	HasExerciseHabit bool      `json:"has_exercise_habit"`
	Allergens        *[]string `json:"allergens"` // null: not filled in; []: none
	Goal             string    `json:"goal"`
	UpdatedAt        string    `json:"updated_at"`
}

func toProfileJSON(p store.Profile) profileJSON {
	out := profileJSON{
		HeightCM: p.HeightCM, WeightKG: p.WeightKG, Age: p.Age,
		HasExerciseHabit: p.HasExerciseHabit, Goal: p.Goal,
		UpdatedAt: tz.FormatInstant(p.UpdatedAt),
	}
	if p.Allergens.Valid {
		items := p.Allergens.Items
		out.Allergens = &items
	}
	return out
}

func (a *api) getProfile(c *gin.Context) {
	sess, _ := sessionOf(c)
	p, err := a.store.GetProfile(c.Request.Context(), sess.UserID)
	if errors.Is(err, store.ErrNotFound) {
		abortError(c, http.StatusNotFound, "profile_not_found", "Profile has not been created yet.")
		return
	}
	if err != nil {
		abortInternal(c, a.logger, err)
		return
	}
	c.JSON(http.StatusOK, toProfileJSON(p))
}

func (a *api) putProfile(c *gin.Context) {
	sess, _ := sessionOf(c)
	var in profileInput
	if !decodeJSON(c, &in) {
		return
	}

	var v validator
	p := store.Profile{UserID: sess.UserID}
	if h := v.decimal("height_cm", in.HeightCM, 50, 300, true); h != nil {
		p.HeightCM = *h
	}
	if w := v.decimal("weight_kg", in.WeightKG, 10, 500, true); w != nil {
		p.WeightKG = *w
	}
	switch {
	case in.Age == nil:
		v.add("age", "required")
	case *in.Age < 10 || *in.Age > 120:
		v.add("age", "out_of_range")
	default:
		p.Age = *in.Age
	}
	if in.HasExerciseHabit == nil {
		v.add("has_exercise_habit", "required")
	} else {
		p.HasExerciseHabit = *in.HasExerciseHabit
	}
	p.Allergens = validateAllergens(&v, in.Allergens)
	p.Goal = v.oneOf("goal", in.Goal, "build_muscle", "lose_fat")
	if v.failed() {
		abortValidation(c, v.errs)
		return
	}

	saved, err := a.store.PutProfile(c.Request.Context(), p)
	if err != nil {
		abortInternal(c, a.logger, err)
		return
	}
	c.JSON(http.StatusOK, toProfileJSON(saved))
}

// validateAllergens keeps the null / [] / list distinction: nil (or omitted)
// is "not filled in", an empty list is "no known allergens".
func validateAllergens(v *validator, in *[]string) store.NullStringArray {
	if in == nil {
		return store.NullStringArray{}
	}
	if len(*in) > 20 {
		v.add("allergens", "too_long")
		return store.NullStringArray{}
	}
	items := make([]string, 0, len(*in))
	seen := map[string]bool{}
	for i, raw := range *in {
		field := fmt.Sprintf("allergens[%d]", i)
		s := strings.TrimSpace(raw)
		v.length(field, s, 1, 50)
		key := strings.ToLower(s)
		if s != "" && seen[key] {
			v.add(field, "duplicate_item")
		}
		seen[key] = true
		items = append(items, s)
	}
	return store.NullStringArray{Items: items, Valid: true}
}
