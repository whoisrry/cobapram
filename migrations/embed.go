// Package migrations menyematkan file SQL goose ke dalam binary, sehingga
// image distroless tidak memerlukan file migrasi terpisah.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
