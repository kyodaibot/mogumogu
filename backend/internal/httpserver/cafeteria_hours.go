package httpserver

import (
	"regexp"
	"slices"
	"strings"
	"time"

	"mogumogu/backend/internal/store"
)

type menuFilter struct {
	meal, clock string
	day         time.Time // the day the user will eat
	holiday     bool
	// reference: today's menu stands in for tomorrow, whose hours are not imported yet.
	reference bool
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
		return store.OpenInterval{Start: "15:00", End: "24:00"}
	default:
		return store.OpenInterval{Start: "00:00", End: "24:00"}
	}
}

// Only categories actually offering the dish count. One dish can appear in
// several categories with different service windows.
func (q menuFilter) itemWindows(shop string, categories []string, hours []store.OpenInterval) []store.OpenInterval {
	day := q.day.Weekday()
	businessDay := day != time.Saturday && day != time.Sunday && !q.holiday
	meal := []store.OpenInterval{q.mealWindow()}
	known := hours != nil
	if known {
		meal = intersection(meal, hours)
	}
	var windows []store.OpenInterval
	for _, category := range categories {
		breakfast := strings.Contains(category, "朝食プレート")
		if breakfast != (q.meal == "breakfast") || breakfast && (shop != "650111" || !businessDay || !known && !q.reference) {
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
		for _, w := range active {
			if q.clock == "" || q.clock >= w.Start && q.clock < w.End {
				windows = append(windows, w)
			}
		}
	}
	return mergeIntervals(windows)
}

func validClock(clock string) bool {
	t, err := time.Parse("15:04", clock)
	return err == nil && t.Format("15:04") == clock
}
