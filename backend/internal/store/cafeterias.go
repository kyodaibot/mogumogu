package store

import (
	"context"
	"time"
)

// OpenInterval is a half-open local time range, with zero-padded 24-hour clocks.
type OpenInterval struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type CafeteriaMenuRow struct {
	ID, Name, Campus         string
	Intervals                JSON[[]OpenInterval]
	HoursURL                 string
	IsHoliday                bool
	SnapshotID               *int64
	CheckedAt, LastCheckedAt *time.Time
	DishCode                 *string
	DishName, SourceURL      string
	Categories, Allergens    NullStringArray
	Price                    *int
	Calories                 float64
	ProteinG, FatG, CarbsG   *float64
}

// CafeteriaMenu reads one consistent view of the latest revisions, keeping
// stores without a menu or eligible dish so the assistant can explain gaps.
func (s *Store) CafeteriaMenu(ctx context.Context, date, campus, cafeteria string) ([]CafeteriaMenuRow, error) {
	var rows []CafeteriaMenuRow
	err := s.db.WithContext(ctx).Raw(`
		SELECT c.id,c.name,c.campus,h.intervals,h.source_url AS hours_url,h.is_holiday,
			s.id AS snapshot_id,s.checked_at,
			(SELECT max(m.checked_at) FROM menu_snapshots m WHERE m.cafeteria_id=c.id AND m.menu_date <= ?) AS last_checked_at,
			i.dish_code,i.name AS dish_name,i.source_url,i.categories,i.allergens,i.member_price_yen AS price,
			i.calories,i.protein_g,i.fat_g,i.carbs_g
		FROM cafeterias c
		LEFT JOIN cafeteria_hours h ON h.cafeteria_id=c.id AND h.menu_date=?
		LEFT JOIN LATERAL (
			SELECT id,checked_at FROM menu_snapshots WHERE cafeteria_id=c.id AND menu_date=? ORDER BY version DESC LIMIT 1
		) s ON true
		LEFT JOIN menu_items i ON i.snapshot_id=s.id AND i.status='unmarked'
		WHERE (?='' OR c.id=? OR c.name=?) AND (?='all' OR c.campus=?)
		ORDER BY c.id,i.dish_code`, date, date, date, cafeteria, cafeteria, cafeteria, campus, campus).Scan(&rows).Error
	return rows, err
}
