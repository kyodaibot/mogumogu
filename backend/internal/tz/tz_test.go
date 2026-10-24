package tz

import (
	"testing"
	"time"
)

func mustDate(t *testing.T, s string) Date {
	t.Helper()
	d, err := ParseDate(s)
	if err != nil {
		t.Fatalf("ParseDate(%q): %v", s, err)
	}
	return d
}

func TestParseDate(t *testing.T) {
	for _, ok := range []string{"2026-10-23", "2024-02-29", "2026-12-31", "2026-01-01"} {
		if d := mustDate(t, ok); d.String() != ok {
			t.Errorf("round trip %q -> %q", ok, d.String())
		}
	}
	for _, bad := range []string{"", "2026-02-30", "2025-02-29", "2026-13-01", "2026-1-5", "2026/10/23", "2026-10-23T00:00:00Z", " 2026-10-23"} {
		if _, err := ParseDate(bad); err == nil {
			t.Errorf("ParseDate(%q) should fail", bad)
		}
	}
}

func TestRangeBoundsUseTokyoMidnight(t *testing.T) {
	tests := []struct {
		from, to           string
		wantStart, wantEnd string
	}{
		{"2026-10-23", "2026-10-23", "2026-10-22T15:00:00Z", "2026-10-23T15:00:00Z"},
		// Month end and year end roll over correctly.
		{"2026-10-31", "2026-12-31", "2026-10-30T15:00:00Z", "2026-12-31T15:00:00Z"},
		// Leap day.
		{"2024-02-28", "2024-02-29", "2024-02-27T15:00:00Z", "2024-02-29T15:00:00Z"},
	}
	for _, tt := range tests {
		start, end := RangeBounds(mustDate(t, tt.from), mustDate(t, tt.to))
		if got := start.UTC().Format(time.RFC3339); got != tt.wantStart {
			t.Errorf("%s..%s start = %s, want %s", tt.from, tt.to, got, tt.wantStart)
		}
		if got := end.UTC().Format(time.RFC3339); got != tt.wantEnd {
			t.Errorf("%s..%s end = %s, want %s", tt.from, tt.to, got, tt.wantEnd)
		}
	}
}

func TestToday(t *testing.T) {
	// 2026-10-23 15:30 UTC is already 2026-10-24 00:30 in Tokyo.
	now := time.Date(2026, 10, 23, 15, 30, 0, 0, time.UTC)
	if got := Today(now).String(); got != "2026-10-24" {
		t.Fatalf("Today = %s, want 2026-10-24", got)
	}
}

func TestDaysUntil(t *testing.T) {
	if n := mustDate(t, "2024-02-01").DaysUntil(mustDate(t, "2024-03-01")); n != 29 {
		t.Fatalf("DaysUntil across leap February = %d, want 29", n)
	}
	if n := mustDate(t, "2026-10-23").DaysUntil(mustDate(t, "2026-10-22")); n != -1 {
		t.Fatalf("DaysUntil backwards = %d, want -1", n)
	}
}

func TestParseInstant(t *testing.T) {
	got, err := ParseInstant("2026-10-23T09:00:00.987+09:00")
	if err != nil {
		t.Fatal(err)
	}
	if FormatInstant(got) != "2026-10-23T09:00:00+09:00" {
		t.Fatalf("fractional seconds not dropped: %s", FormatInstant(got))
	}
	if FormatInstant(mustInstant(t, "2026-10-23T00:30:00Z")) != "2026-10-23T09:30:00+09:00" {
		t.Fatal("UTC input not converted to +09:00")
	}
	for _, bad := range []string{"2026-10-23T09:00:00", "2026-10-23 09:00:00+09:00", "2026-10-23", ""} {
		if _, err := ParseInstant(bad); err == nil {
			t.Errorf("ParseInstant(%q) should fail", bad)
		}
	}
}

func mustInstant(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := ParseInstant(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
