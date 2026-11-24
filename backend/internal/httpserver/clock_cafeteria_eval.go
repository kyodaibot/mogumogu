//go:build cafeteria_eval

package httpserver

import (
	"os"
	"time"
)

func init() {
	fixed, err := time.Parse(time.RFC3339, os.Getenv("EVAL_CONTEXT_TIME"))
	if err != nil {
		panic("cafeteria evaluation requires an RFC3339 EVAL_CONTEXT_TIME")
	}
	assistantContextTime = func(time.Time) time.Time { return fixed }
}
