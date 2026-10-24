package store

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Store struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Store {
	return &Store{db: db}
}

func notFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}

// --- users ---

func (s *Store) CreateUser(ctx context.Context, username, passwordHash string) (User, error) {
	u := User{Username: username, PasswordHash: passwordHash}
	err := s.db.WithContext(ctx).Select("Username", "PasswordHash").Create(&u).Error
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return User{}, ErrUsernameTaken
	}
	if err != nil {
		return User{}, err
	}
	// Re-read so created_at comes from the database.
	return s.UserByID(ctx, u.ID)
}

func (s *Store) UserByUsername(ctx context.Context, username string) (User, error) {
	var u User
	err := s.db.WithContext(ctx).Where("username = ?", username).Take(&u).Error
	return u, notFound(err)
}

func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	var u User
	err := s.db.WithContext(ctx).Where("id = ?", id).Take(&u).Error
	return u, notFound(err)
}

// --- revoked tokens ---

func (s *Store) RevokeToken(ctx context.Context, jti string, expiresAt time.Time) error {
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&RevokedToken{JTI: jti, ExpiresAt: expiresAt}).Error
}

// SessionActive reports whether a verified token may still be used: its user
// still exists and its jti has not been revoked. One query per request.
func (s *Store) SessionActive(ctx context.Context, userID int64, jti string) (bool, error) {
	var active bool
	err := s.db.WithContext(ctx).Raw(
		`SELECT EXISTS (SELECT 1 FROM users WHERE id = ?)
		    AND NOT EXISTS (SELECT 1 FROM revoked_tokens WHERE jti = ?)`,
		userID, jti,
	).Scan(&active).Error
	return active, err
}

// DeleteExpiredRevocations removes rows for tokens that have expired anyway.
func (s *Store) DeleteExpiredRevocations(ctx context.Context, now time.Time) error {
	return s.db.WithContext(ctx).Where("expires_at < ?", now).Delete(&RevokedToken{}).Error
}

// --- profiles ---

func (s *Store) HasProfile(ctx context.Context, userID int64) (bool, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&Profile{}).Where("user_id = ?", userID).Count(&n).Error
	return n > 0, err
}

func (s *Store) GetProfile(ctx context.Context, userID int64) (Profile, error) {
	var p Profile
	err := s.db.WithContext(ctx).Where("user_id = ?", userID).Take(&p).Error
	return p, notFound(err)
}

// PutProfile creates the profile or replaces every editable field.
func (s *Store) PutProfile(ctx context.Context, p Profile) (Profile, error) {
	now := time.Now()
	p.CreatedAt, p.UpdatedAt = now, now
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"height_cm", "weight_kg", "age", "has_exercise_habit", "allergens", "goal", "updated_at",
		}),
	}).Create(&p).Error
	if err != nil {
		return Profile{}, err
	}
	return s.GetProfile(ctx, p.UserID)
}

// --- schedule events ---

// ListEvents returns the user's events overlapping [start, end).
func (s *Store) ListEvents(ctx context.Context, userID int64, start, end time.Time) ([]ScheduleEvent, error) {
	var events []ScheduleEvent
	err := s.db.WithContext(ctx).
		Where("user_id = ? AND starts_at < ? AND ends_at > ?", userID, end, start).
		Order("starts_at, id").
		Find(&events).Error
	return events, err
}

func (s *Store) GetEvent(ctx context.Context, userID, id int64) (ScheduleEvent, error) {
	var e ScheduleEvent
	err := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Take(&e).Error
	return e, notFound(err)
}

func (s *Store) CreateEvent(ctx context.Context, e ScheduleEvent) (ScheduleEvent, error) {
	e.ID = 0
	if err := s.db.WithContext(ctx).Omit("ID").Create(&e).Error; err != nil {
		return ScheduleEvent{}, err
	}
	return s.GetEvent(ctx, e.UserID, e.ID)
}

// UpdateEvent replaces the editable fields of the user's event.
func (s *Store) UpdateEvent(ctx context.Context, e ScheduleEvent) (ScheduleEvent, error) {
	res := s.db.WithContext(ctx).Model(&ScheduleEvent{}).
		Where("id = ? AND user_id = ?", e.ID, e.UserID).
		Updates(map[string]any{
			"title": e.Title, "starts_at": e.StartsAt, "ends_at": e.EndsAt, "notes": e.Notes,
			"updated_at": time.Now(),
		})
	if res.Error != nil {
		return ScheduleEvent{}, res.Error
	}
	if res.RowsAffected == 0 {
		return ScheduleEvent{}, ErrNotFound
	}
	return s.GetEvent(ctx, e.UserID, e.ID)
}

func (s *Store) DeleteEvent(ctx context.Context, userID, id int64) error {
	res := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&ScheduleEvent{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- meal records ---

// ListMeals returns the user's records with from <= date <= to.
func (s *Store) ListMeals(ctx context.Context, userID int64, from, to time.Time) ([]MealRecord, error) {
	var meals []MealRecord
	err := s.db.WithContext(ctx).
		Where("user_id = ? AND date BETWEEN ? AND ?", userID, from, to).
		Order("date").
		Order("array_position(ARRAY['breakfast','lunch','dinner','snack'], meal_type)").
		Order("created_at, id").
		Find(&meals).Error
	return meals, err
}

func (s *Store) GetMeal(ctx context.Context, userID, id int64) (MealRecord, error) {
	var m MealRecord
	err := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Take(&m).Error
	return m, notFound(err)
}

func (s *Store) CreateMeal(ctx context.Context, m MealRecord) (MealRecord, error) {
	m.ID = 0
	if err := s.db.WithContext(ctx).Omit("ID").Create(&m).Error; err != nil {
		return MealRecord{}, err
	}
	return s.GetMeal(ctx, m.UserID, m.ID)
}

// UpdateMeal replaces the editable fields of the user's record. nil nutrition
// values are written as NULL.
func (s *Store) UpdateMeal(ctx context.Context, m MealRecord) (MealRecord, error) {
	res := s.db.WithContext(ctx).Model(&MealRecord{}).
		Where("id = ? AND user_id = ?", m.ID, m.UserID).
		Updates(map[string]any{
			"date": m.Date, "meal_type": m.MealType, "content": m.Content,
			"calories": m.Calories, "protein_g": m.ProteinG, "fat_g": m.FatG, "carbs_g": m.CarbsG,
			"updated_at": time.Now(),
		})
	if res.Error != nil {
		return MealRecord{}, res.Error
	}
	if res.RowsAffected == 0 {
		return MealRecord{}, ErrNotFound
	}
	return s.GetMeal(ctx, m.UserID, m.ID)
}

func (s *Store) DeleteMeal(ctx context.Context, userID, id int64) error {
	res := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&MealRecord{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
