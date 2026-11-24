package httpserver

import (
	"errors"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"mogumogu/backend/internal/assistant"
	"mogumogu/backend/internal/store"
)

type menuNutrition struct {
	Calories float64  `json:"calories"`
	ProteinG *float64 `json:"protein_g"`
	FatG     *float64 `json:"fat_g"`
	CarbsG   *float64 `json:"carbs_g"`
}

type menuCandidate struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Price      *int                 `json:"price_yen"`
	Nutrition  menuNutrition        `json:"nutrition"`
	Categories []string             `json:"categories"`
	Allergens  []allergenLabel      `json:"allergens"`
	URL        string               `json:"-"`
	Windows    []store.OpenInterval `json:"windows"`
}
type candidateStore struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Campus    string          `json:"campus"`
	MenuDate  string          `json:"menu_date"` // differs from the query's when this store fell back
	Hours     []string        `json:"hours"`
	HoursURL  string          `json:"hours_url"`
	CheckedAt string          `json:"checked_at"`
	Items     []menuCandidate `json:"items"`
}
type missingStore struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	LastCheckedAt string `json:"last_checked_at,omitempty"`
}
type menuResult struct {
	ID          string           `json:"query_id"`
	Date        string           `json:"menu_date"`
	Status      string           `json:"status"`
	Stores      []candidateStore `json:"stores"`
	Unavailable []missingStore   `json:"unavailable"`
	Meal        string           `json:"-"`
}
type cafeteriaCard struct {
	Kind             string      `json:"kind"`
	Name             string      `json:"name"`
	Reason           string      `json:"reason"`
	Meal             string      `json:"meal_type"`
	Date             string      `json:"menu_date"` // the day the user will eat
	BreakfastOptions bool        `json:"breakfast_options"`
	Stores           []cardStore `json:"stores"`
}
type cardStore struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Campus    string         `json:"campus"`
	MenuDate  string         `json:"menu_date"`
	Hours     []string       `json:"hours"`
	HoursURL  string         `json:"hours_url"`
	CheckedAt string         `json:"checked_at"`
	Items     []cardItem     `json:"items"`
	Nutrition *menuNutrition `json:"nutrition"`
	Price     *int           `json:"price_yen"`
}
type riceOption struct {
	Size string `json:"size"`
	cardItem
}

type cardItem struct {
	RiceOptions []riceOption    `json:"rice_options,omitempty"`
	Code        string          `json:"code"`
	Name        string          `json:"name"`
	Portions    int             `json:"portions"`
	URL         string          `json:"url"`
	Nutrition   menuNutrition   `json:"nutrition"`
	Price       *int            `json:"price_yen"`
	Allergens   []allergenLabel `json:"allergens"`
}

// Only standalone rice has interchangeable sizes. Bowls and curry also carry size prefixes.
var riceSizes = []string{"ミニ", "小", "中", "大", "特大"}

func riceSize(name string) string {
	for _, size := range riceSizes {
		if name == "ライス"+size || name == "（"+size+"）ライス" {
			return size
		}
	}
	return ""
}

func (c menuCandidate) cardItem(portions int) cardItem {
	return cardItem{Code: c.ID, Name: c.Name, Portions: portions, URL: c.URL,
		Price: c.Price, Nutrition: c.Nutrition, Allergens: c.Allergens}
}

func (s candidateStore) riceOptions(selected cardItem, windows []store.OpenInterval) []riceOption {
	options := map[string]riceOption{}
	for _, candidate := range s.Items {
		size := riceSize(candidate.Name)
		if size == "" || (candidate.Allergens == nil) != (selected.Allergens == nil) ||
			!slices.Equal(candidate.Allergens, selected.Allergens) ||
			!slices.Equal(intersection(windows, candidate.Windows), windows) {
			continue
		}
		if _, found := options[size]; !found || candidate.ID == selected.Code {
			options[size] = riceOption{size, candidate.cardItem(1)}
		}
	}
	var ordered []riceOption
	for _, size := range riceSizes {
		if option, ok := options[size]; ok {
			ordered = append(ordered, option)
		}
	}
	return ordered
}

var errMenuSelection = errors.New("assistant returned an invalid cafeteria selection")

func sumNutrient(total, perServing *float64, portions int) *float64 {
	if total == nil || perServing == nil {
		return nil
	}
	s := math.Round((*total+*perServing*float64(portions))*1000) / 1000
	return &s
}

// storeOffer verifies that this location serves every component at a common
// time. Unknown nutrition stays unknown for that field of the whole meal.
func (s candidateStore) storeOffer(portions []assistant.MenuPortion, breakfast bool) (cardStore, bool) {
	offer := cardStore{ID: s.ID, Name: s.Name, Campus: s.Campus, MenuDate: s.MenuDate, HoursURL: s.HoursURL, CheckedAt: s.CheckedAt, Hours: []string{}}
	total := menuNutrition{ProteinG: new(float64), FatG: new(float64), CarbsG: new(float64)}
	price := new(int)
	var windows []store.OpenInterval
	for i, p := range portions {
		index := slices.IndexFunc(s.Items, func(c menuCandidate) bool { return c.ID == p.ID })
		if index < 0 {
			return cardStore{}, false
		}
		item := s.Items[index]
		if riceSize(item.Name) != "" {
			p.Portions = 1
		}
		offer.Items = append(offer.Items, item.cardItem(p.Portions))
		if i == 0 {
			windows = item.Windows
		} else {
			windows = intersection(windows, item.Windows)
		}
		if price != nil && item.Price != nil {
			*price += *item.Price * p.Portions
		} else {
			price = nil
		}
		total.Calories += item.Nutrition.Calories * float64(p.Portions)
		total.ProteinG = sumNutrient(total.ProteinG, item.Nutrition.ProteinG, p.Portions)
		total.FatG = sumNutrient(total.FatG, item.Nutrition.FatG, p.Portions)
		total.CarbsG = sumNutrient(total.CarbsG, item.Nutrition.CarbsG, p.Portions)
	}
	if len(windows) == 0 {
		return cardStore{}, false
	}
	if len(s.Hours) > 0 {
		offer.Hours = displayHours(windows)
	}
	if !breakfast {
		for i, item := range offer.Items {
			if riceSize(item.Name) != "" {
				offer.Items[i].RiceOptions = s.riceOptions(item, windows)
			}
		}
		total.Calories = math.Round(total.Calories*1000) / 1000
		offer.Nutrition, offer.Price = &total, price
	}
	return offer, true
}

func (q menuResult) cards(selections []assistant.MenuSelection) ([]cafeteriaCard, error) {
	breakfast := q.Meal == "breakfast"
	if len(selections) > 3 || breakfast && len(selections) > 1 {
		return nil, errMenuSelection
	}
	byBundle := map[string]int{}
	var cards []cafeteriaCard
	for _, sel := range selections {
		if len(sel.Items) == 0 || len(sel.Items) > 12 || len(sel.StoreIDs) == 0 || len(sel.StoreIDs) > 7 ||
			strings.TrimSpace(sel.Reason) == "" || utf8.RuneCountInString(sel.Reason) > 200 {
			return nil, errMenuSelection
		}
		var keyParts []string
		seen := map[string]bool{}
		for _, p := range sel.Items {
			if p.ID == "" || p.Portions < 1 || p.Portions > 10 || breakfast && p.Portions != 1 || seen[p.ID] {
				return nil, errMenuSelection
			}
			seen[p.ID] = true
			keyParts = append(keyParts, p.ID+"="+strconv.Itoa(p.Portions))
		}
		sort.Strings(keyParts)
		key := strings.Join(keyParts, ",")
		cardIndex, existing := byBundle[key]
		if !existing {
			cardIndex = len(cards)
			byBundle[key] = cardIndex
			cards = append(cards, cafeteriaCard{Kind: "cafeteria", Reason: sel.Reason, Meal: q.Meal, Date: q.Date, BreakfastOptions: breakfast})
		}
		card := &cards[cardIndex]
		for _, id := range sel.StoreIDs {
			storeIndex := slices.IndexFunc(q.Stores, func(s candidateStore) bool { return s.ID == id })
			if storeIndex < 0 {
				return nil, errMenuSelection
			}
			offer, ok := q.Stores[storeIndex].storeOffer(sel.Items, breakfast)
			if !ok {
				return nil, errMenuSelection
			}
			if slices.ContainsFunc(card.Stores, func(s cardStore) bool { return s.ID == id }) {
				continue
			}
			card.Stores = append(card.Stores, offer)
			if card.Name == "" {
				if breakfast {
					card.Name = "朝食プレート"
				} else {
					names := make([]string, 0, len(offer.Items))
					for _, i := range offer.Items {
						names = append(names, i.Name)
					}
					card.Name = strings.Join(names, " + ")
				}
			}
		}
	}
	return cards, nil
}
