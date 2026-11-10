package migrations

import (
	_ "embed"

	"gofr.dev/pkg/gofr/migration"
)

//go:embed init.sql
var initSQL string

// All returns migrations keyed by version (a timestamp). Add new ones as new keys; never edit applied ones.
func All() map[int64]migration.Migrate {
	return map[int64]migration.Migrate{
		20261110000001: {UP: func(d migration.Datasource) error {
			_, err := d.SQL.Exec(initSQL)
			return err
		}},
	}
}
