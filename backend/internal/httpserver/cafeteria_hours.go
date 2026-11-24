package httpserver

import (
	"regexp"
	"slices"
	"strings"
	"time"

	"mogumogu/backend/internal/store"
)

type menuFilter struct {
	meal string
	day  time.Time // the day the user will eat
}

// storeHours are a store's regular opening hours, fixed in code rather than read
// from the co-op's site, whose timetable lists no hours for some stores
// (plans/fixed-hours.md). Sundays are closed, Saturdays only where listed, and
// national holidays count as weekdays. Term breaks and closures are not covered:
// the card links to the official page.
type storeHours struct {
	anchor            string // the store on its campus's official hours page
	weekday, saturday []store.OpenInterval
}

func spans(pairs ...string) []store.OpenInterval {
	var out []store.OpenInterval
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, store.OpenInterval{Start: pairs[i], End: pairs[i+1]})
	}
	return out
}

var regularHours = map[string]storeHours{
	"650111": {"central_cafeteria", spans("08:00", "15:15"), nil},
	"650112": {"dining_yoshida", spans("11:00", "14:00"), nil},
	"650113": {"dining_northern", spans("11:00", "20:00"), spans("11:00", "14:00")},
	"650115": {"dining_southern", spans("11:00", "14:00"), nil},
	"650116": {"uji_shokudo", spans("11:30", "14:00", "18:00", "20:00"), nil},
	"650118": {"cafe_lune", spans("11:00", "21:15"), spans("11:00", "14:00")},
	"650120": {"selene", spans("11:00", "14:00", "18:00", "20:00"), spans("11:00", "14:00")},
}

// openEveryDay makes every store open on its weekday hours every day; it is set
// only in E2E builds (hours_e2e.go), which run on the real date, weekends included.
var openEveryDay bool

// hoursOn returns a store's regular hours on day (empty when closed, nil for an
// unknown store) and the official page showing them.
func hoursOn(shop, campus string, day time.Time) ([]store.OpenInterval, string) {
	url := "https://www.s-coop.net/shop_guide/" + campus + "_shop/"
	h, ok := regularHours[shop]
	if !ok {
		return nil, url
	}
	url += "#" + h.anchor
	if openEveryDay {
		return h.weekday, url
	}
	switch day.Weekday() {
	case time.Sunday:
		return []store.OpenInterval{}, url
	case time.Saturday:
		return append([]store.OpenInterval{}, h.saturday...), url
	default:
		return h.weekday, url
	}
}

var categoryClock = regexp.MustCompile(`([0-9]{1,2}:[0-9]{2})\s*[~〜～－-]\s*([0-9]{1,2}:[0-9]{2})`)

func intersection(a, b []store.OpenInterval) []store.OpenInterval {
	var windows []store.OpenInterval
	for _, x := range a {
		for _, y := range b {
			start, end := max(x.Start, y.Start), min(x.End, y.End)
			if start < end {
				windows = append(windows, store.OpenInterval{Start: start, End: end})
			}
		}
	}
	return mergeIntervals(windows)
}

func mergeIntervals(windows []store.OpenInterval) []store.OpenInterval {
	slices.SortFunc(windows, func(a, b store.OpenInterval) int { return strings.Compare(a.Start, b.Start) })
	merged := windows[:0]
	for _, w := range windows {
		if n := len(merged); n > 0 && w.Start <= merged[n-1].End {
			merged[n-1].End = max(merged[n-1].End, w.End)
		} else {
			merged = append(merged, w)
		}
	}
	return merged
}

func displayHours(windows []store.OpenInterval) []string {
	out := make([]string, 0, len(windows))
	for _, w := range windows {
		out = append(out, w.Start+"–"+w.End)
	}
	return out
}

func (q menuFilter) mealWindow() store.OpenInterval {
	switch q.meal {
	case "breakfast":
		return store.OpenInterval{Start: "08:00", End: "10:00"}
	case "lunch":
		return store.OpenInterval{Start: "10:00", End: "15:00"}
	case "dinner":
		// From 16:00, so a store closing just after lunch (中央食堂 at 15:15) is no dinner option.
		return store.OpenInterval{Start: "16:00", End: "24:00"}
	default:
		return store.OpenInterval{Start: "00:00", End: "24:00"}
	}
}

// Only categories actually offering the dish count. One dish can appear in
// several categories with different service windows.
func (q menuFilter) itemWindows(shop string, categories []string, hours []store.OpenInterval) []store.OpenInterval {
	day := q.day.Weekday()
	businessDay := day != time.Saturday && day != time.Sunday
	meal := []store.OpenInterval{q.mealWindow()}
	if hours != nil {
		meal = intersection(meal, hours)
	}
	var windows []store.OpenInterval
	for _, category := range categories {
		breakfast := strings.Contains(category, "朝食プレート")
		if breakfast != (q.meal == "breakfast") || breakfast && (shop != "650111" || !businessDay) {
			continue
		}
		if (strings.HasPrefix(category, "夜") && q.meal != "dinner") ||
			(strings.Contains(category, "平日") && !businessDay) ||
			(strings.Contains(category, "土曜日営業") && day != time.Saturday) {
			continue
		}
		active := meal
		matches := categoryClock.FindAllStringSubmatch(category, -1)
		if len(matches) > 0 {
			var limits []store.OpenInterval
			for _, m := range matches {
				start, end := m[1], m[2]
				if len(start) == 4 {
					start = "0" + start
				}
				if len(end) == 4 {
					end = "0" + end
				}
				limits = append(limits, store.OpenInterval{Start: start, End: end})
			}
			active = intersection(meal, limits)
		}
		windows = append(windows, active...)
	}
	return mergeIntervals(windows)
}
