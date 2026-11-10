// Package migrations holds the schema, applied by GoFr on startup.
package migrations

import (
	_ "embed"

	"gofr.dev/pkg/gofr/migration"
)

//go:embed init.sql
var initSQL string

//go:embed align.sql
var alignSQL string

//go:embed logs.sql
var logsSQL string

//go:embed completed.sql
var completedSQL string

// All returns migrations keyed by version (a timestamp). Add new ones as new keys; never edit applied ones.
func All() map[int64]migration.Migrate {
	return map[int64]migration.Migrate{
		20261108000001: {UP: func(d migration.Datasource) error {
			_, err := d.SQL.Exec(initSQL)
			return err
		}},
		20261110000001: {UP: func(d migration.Datasource) error {
			_, err := d.SQL.Exec(alignSQL)
			return err
		}},
		20261110000002: {UP: func(d migration.Datasource) error {
			_, err := d.SQL.Exec(logsSQL)
			return err
		}},
		20261110000003: {UP: func(d migration.Datasource) error {
			_, err := d.SQL.Exec(completedSQL)
			return err
		}},
	}
}
