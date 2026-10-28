// Package tz holds the product time zone (Asia/Tokyo) and the date and time
// formats used by the API.
package tz

import (
	"errors"
	"time"
	// Embed the zone database so Asia/Tokyo loads even where the OS has none
	// (for example a minimal container image).
	_ "time/tzdata"
)

const dateLayout = "2006-01-02"

// Tokyo is the product time zone. Every calendar date in the API is a date in
// this zone.
var Tokyo = mustLoad("Asia/Tokyo")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// Date is a calendar date without a time of day, stored as midnight UTC so
// that it compares and subtracts cleanly.
type Date struct{ t time.Time }

// ParseDate parses YYYY-MM-DD and rejects dates that do not exist.
func ParseDate(s string) (Date, error) {
	t, err := time.ParseInLocation(dateLayout, s, time.UTC)
	if err != nil {
		return Date{}, errors.New("invalid date")
	}
	return Date{t}, nil
}

// DateOf returns the calendar date of a value read from a PostgreSQL date
// column (which the driver returns as midnight UTC).
func DateOf(t time.Time) Date {
	return Date{time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)}
}

// Today returns the current date in Tokyo.
func Today(now time.Time) Date {
	y, m, d := now.In(Tokyo).Date()
	return Date{time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
}

func (d Date) String() string { return d.t.Format(dateLayout) }

// Time returns the date as midnight UTC, the form stored in a date column.
func (d Date) Time() time.Time { return d.t }

func (d Date) Before(o Date) bool { return d.t.Before(o.t) }

// AddDays returns the date n days later (earlier if n is negative).
func (d Date) AddDays(n int) Date { return Date{d.t.AddDate(0, 0, n)} }

// DaysUntil returns the number of days from d to o (negative if o is earlier).
func (d Date) DaysUntil(o Date) int { return int(o.t.Sub(d.t) / (24 * time.Hour)) }

// Start returns the instant this date begins in Tokyo.
func (d Date) Start() time.Time {
	y, m, day := d.t.Date()
	return time.Date(y, m, day, 0, 0, 0, 0, Tokyo)
}

// RangeBounds returns the half-open interval [from 00:00, the day after to
// 00:00) in Tokyo, the interval an inclusive from..to date query covers.
func RangeBounds(from, to Date) (start, end time.Time) {
	y, m, d := to.t.Date()
	return from.Start(), time.Date(y, m, d+1, 0, 0, 0, 0, Tokyo)
}

// ParseInstant parses an RFC 3339 timestamp, which must carry a UTC offset,
// and drops fractional seconds.
func ParseInstant(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, errors.New("invalid timestamp")
	}
	return t.Truncate(time.Second), nil
}

// FormatInstant formats t in RFC 3339 with the Tokyo offset (+09:00).
func FormatInstant(t time.Time) string {
	return t.In(Tokyo).Format(time.RFC3339)
}
