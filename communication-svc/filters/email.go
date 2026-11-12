package filters

// EmailFilter narrows and pages GET /email.
type EmailFilter struct {
	Status string
	Type   string
	To     string
	Limit  int
	Offset int
}

// Where returns the WHERE clause and its arguments.
func (f *EmailFilter) Where() (string, []any) {
	var w where

	if f.Status != "" {
		w.add("status = $%d", f.Status)
	}

	if f.Type != "" {
		w.add("type = $%d", f.Type)
	}

	if f.To != "" {
		w.add("lower(to_addr) LIKE '%%' || lower($%d) || '%%'", f.To)
	}

	return w.build()
}

// Page returns the LIMIT/OFFSET clause, with its arguments appended to args.
func (f *EmailFilter) Page(args []any) (string, []any) { return page(args, f.Limit, f.Offset) }
