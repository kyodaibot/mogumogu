//go:build e2e

package httpserver

// E2E runs on the real date: keep the cafeteria cards the same on weekends.
func init() { openEveryDay = true }
