// Package migrations embeds the same SQL files used by the development Goose CLI.
package migrations

import "embed"

//go:embed *.sql
var Files embed.FS
