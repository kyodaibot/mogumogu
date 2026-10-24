// Package store reads and writes business data through GORM. Every method
// that touches user data takes the caller's user ID and filters by it, so one
// user can never read or change another user's rows.
package store

import (
	"database/sql/driver"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

func (User) TableName() string { return "users" }

type Profile struct {
	UserID           int64 `gorm:"primaryKey;autoIncrement:false"`
	HeightCM         float64
	WeightKG         float64
	Age              int
	HasExerciseHabit bool
	Allergens        NullStringArray
	Goal             string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (Profile) TableName() string { return "profiles" }

type ScheduleEvent struct {
	ID        int64
	UserID    int64
	Title     string
	StartsAt  time.Time
	EndsAt    time.Time
	Notes     *string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (ScheduleEvent) TableName() string { return "schedule_events" }

type MealRecord struct {
	ID       int64
	UserID   int64
	Date     time.Time // date column; see tz.DateOf
	MealType string
	Content  string
	// nil means unknown; it is never treated as 0.
	Calories  *float64
	ProteinG  *float64
	FatG      *float64
	CarbsG    *float64
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (MealRecord) TableName() string { return "meal_records" }

type RevokedToken struct {
	JTI       string `gorm:"primaryKey;column:jti"`
	ExpiresAt time.Time
}

func (RevokedToken) TableName() string { return "revoked_tokens" }

// NullStringArray maps a nullable PostgreSQL text[]. Valid=false is SQL NULL;
// Valid=true with no items is an empty array. The two mean different things
// for allergens ("not filled in" vs "none").
type NullStringArray struct {
	Items []string
	Valid bool
}

// GormDataType stops GORM from inspecting the struct's own fields.
func (NullStringArray) GormDataType() string { return "text[]" }

var pgTypes = pgtype.NewMap()

func (a *NullStringArray) Scan(src any) error {
	if src == nil {
		*a = NullStringArray{}
		return nil
	}
	var items []string
	if err := pgTypes.SQLScanner(&items).Scan(src); err != nil {
		return err
	}
	if items == nil {
		items = []string{}
	}
	*a = NullStringArray{Items: items, Valid: true}
	return nil
}

// Value encodes the array as a PostgreSQL array literal, quoting every element.
func (a NullStringArray) Value() (driver.Value, error) {
	if !a.Valid {
		return nil, nil
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, s := range a.Items {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		b.WriteString(strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s))
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String(), nil
}

var (
	ErrNotFound      = errors.New("not found")
	ErrUsernameTaken = errors.New("username taken")
)
