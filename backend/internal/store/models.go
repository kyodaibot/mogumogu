// Package store reads and writes business data through GORM. Every method
// that touches user data takes the caller's user ID and filters by it, so one
// user can never read or change another user's rows.
package store

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	Tier         Tier
	CreatedAt    time.Time
}

// Tier is the account tier; it decides the daily assistant quota.
type Tier string

const (
	TierNormal Tier = "normal"
	TierPro    Tier = "pro"
)

// ParseTier accepts the names stored in users.tier.
func ParseTier(s string) (Tier, bool) {
	switch t := Tier(s); t {
	case TierNormal, TierPro:
		return t, true
	}
	return "", false
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

// Proposal is a change proposal (GLOSSARY: 变更提案): changes to the user's
// meal records the assistant staged. They run once, when the user confirms.
type Proposal struct {
	ID         int64
	UserID     int64
	Changes    JSON[[]ProposalChange]
	CreatedAt  time.Time
	ExecutedAt *time.Time
	// One result per change; empty until the proposal runs.
	Results JSON[[]ChangeResult]
}

func (Proposal) TableName() string { return "proposals" }

// Kinds of change in a proposal.
const (
	OpCreate = "create"
	OpUpdate = "update"
	OpDelete = "delete"
)

// ProposalChange is one change. Before is the record when the change was
// proposed (update, delete); its UpdatedAt is the optimistic lock. After is
// the record to write (create, update).
type ProposalChange struct {
	Op     string      `json:"op"`
	Before *MealRecord `json:"before,omitempty"`
	After  *MealRecord `json:"after,omitempty"`
}

// What happened to one change when its proposal ran.
const (
	ResultDone    = "done"
	ResultFailed  = "failed"
	ResultSkipped = "skipped" // the user unchecked it
)

// Why a change failed.
const (
	ReasonChanged  = "changed" // the record was edited after the proposal
	ReasonNotFound = "not_found"
)

type ChangeResult struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
	// The record created or changed.
	MealID int64 `json:"meal_id,omitempty"`
}

// JSON maps a jsonb column to a Go value.
type JSON[T any] struct {
	V T
}

func (JSON[T]) GormDataType() string { return "jsonb" }

func (j *JSON[T]) Scan(src any) error {
	switch s := src.(type) {
	case nil:
		*j = JSON[T]{}
		return nil
	case []byte:
		return json.Unmarshal(s, &j.V)
	case string:
		return json.Unmarshal([]byte(s), &j.V)
	}
	return fmt.Errorf("jsonb: unsupported type %T", src)
}

func (j JSON[T]) Value() (driver.Value, error) {
	b, err := json.Marshal(j.V)
	return string(b), err
}

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

// ExecuteProposal returns these when the proposal cannot run.
var (
	ErrProposalExecuted = errors.New("proposal already executed")
	ErrProposalExpired  = errors.New("proposal expired")
	ErrNoSuchChange     = errors.New("no such change in the proposal")
)
