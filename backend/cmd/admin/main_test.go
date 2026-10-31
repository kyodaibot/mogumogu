package main

import (
	"strings"
	"testing"
)

func TestParseRejectsBadArguments(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"user"},
		{"users", "extra"},
		{"tier", "alice"},
		{"tier", "alice", "gold"},
		{"tier", "alice", "PRO"},
		{"tier", "alice", "pro", "extra"},
	} {
		if _, err := parse(args); err == nil {
			t.Errorf("%q: accepted", args)
		}
	}
	if _, err := parse([]string{"tier", "alice", "gold"}); err == nil || !strings.Contains(err.Error(), "normal or pro") {
		t.Errorf("unknown tier: %v", err)
	}
}

func TestParseAcceptsCommands(t *testing.T) {
	for _, args := range [][]string{{"users"}, {"tier", "Alice", "pro"}, {"tier", "alice", "normal"}} {
		if _, err := parse(args); err != nil {
			t.Errorf("%q: %v", args, err)
		}
	}
}
