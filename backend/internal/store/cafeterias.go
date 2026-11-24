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
	SnapshotID               *int64
	MenuDate                 string
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
// A store without a snapshot for date uses its latest one since oldest
// (oldest == date allows no other day). Opening hours are fixed in code
// (httpserver/cafeteria_hours.go), not read from cafeteria_hours.
func (s *Store) CafeteriaMenu(ctx context.Context, date, oldest, campus, cafeteria string) ([]CafeteriaMenuRow, error) {
	var rows []CafeteriaMenuRow
	err := s.db.WithContext(ctx).Raw(`
		SELECT c.id,c.name,c.campus,
			s.id AS snapshot_id,to_char(s.menu_date,'YYYY-MM-DD') AS menu_date,s.checked_at,
			(SELECT max(m.checked_at) FROM menu_snapshots m WHERE m.cafeteria_id=c.id AND m.menu_date <= ?) AS last_checked_at,
			i.dish_code,i.name AS dish_name,i.source_url,i.categories,i.allergens,i.member_price_yen AS price,
			i.calories,i.protein_g,i.fat_g,i.carbs_g
		FROM cafeterias c
		LEFT JOIN LATERAL (
			SELECT id,menu_date,checked_at FROM menu_snapshots WHERE cafeteria_id=c.id AND menu_date BETWEEN ? AND ?
			ORDER BY menu_date DESC,version DESC LIMIT 1
		) s ON true
		LEFT JOIN menu_items i ON i.snapshot_id=s.id AND i.status='unmarked'
		WHERE (?='' OR c.id=? OR c.name=?) AND (?='all' OR c.campus=?)
		ORDER BY c.id,i.dish_code`, date, oldest, date, cafeteria, cafeteria, cafeteria, campus, campus).Scan(&rows).Error
	return rows, err
}
