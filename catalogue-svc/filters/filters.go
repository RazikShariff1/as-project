// Package filters holds the query-filter structs for list endpoints.
package filters

import (
	"fmt"
	"strings"
)

// where accumulates SQL conditions with Postgres $n placeholders.
type where struct {
	clauses []string
	args    []any
}

func (w *where) add(condition string, arg any) {
	w.args = append(w.args, arg)
	w.clauses = append(w.clauses, fmt.Sprintf(condition, len(w.args)))
}

// raw adds a condition that takes no argument, e.g. "deleted_at IS NULL".
func (w *where) raw(condition string) {
	w.clauses = append(w.clauses, condition)
}

func (w *where) build() (query string, args []any) {
	if len(w.clauses) == 0 {
		return "", w.args
	}

	return " WHERE " + strings.Join(w.clauses, " AND "), w.args
}

// page appends LIMIT/OFFSET placeholders after the existing args.
func page(args []any, limit, offset int) (query string, out []any) {
	out = append(args, limit, offset)

	return fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(out)-1, len(out)), out
}
