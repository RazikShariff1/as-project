// Package migrations holds the schema migrations, applied on startup.
package migrations

import (
	_ "embed"

	"gofr.dev/pkg/gofr/migration"
)

//go:embed init.sql
var initSQL string

//go:embed owner_place_order.sql
var ownerPlaceOrderSQL string

//go:embed order_templates.sql
var orderTemplatesSQL string

// All returns migrations keyed by version (a timestamp). Add new ones as new keys; never edit applied ones.
func All() map[int64]migration.Migrate {
	return map[int64]migration.Migrate{
		20261112000001: {UP: func(d migration.Datasource) error {
			_, err := d.SQL.Exec(initSQL)
			return err
		}},
		20261112000002: {UP: func(d migration.Datasource) error {
			_, err := d.SQL.Exec(ownerPlaceOrderSQL)
			return err
		}},
		20261112000003: {UP: func(d migration.Datasource) error {
			_, err := d.SQL.Exec(orderTemplatesSQL)
			return err
		}},
	}
}
