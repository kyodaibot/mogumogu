// Package migrations embeds the numbered SQL migrations so the migrate command
// and integration tests apply exactly the same files.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
