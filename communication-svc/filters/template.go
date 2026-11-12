package filters

// TemplateFilter narrows and pages GET /communications.
type TemplateFilter struct {
	Name     string
	Language string
	Status   string
	Limit    int
	Offset   int
}

// Where returns the WHERE clause and its arguments.
func (f *TemplateFilter) Where() (string, []any) {
	var w where

	if f.Name != "" {
		w.add("name = $%d", f.Name)
	}

	if f.Language != "" {
		w.add("language = $%d", f.Language)
	}

	if f.Status != "" {
		w.add("status = $%d", f.Status)
	}

	return w.build()
}

// Page returns the LIMIT/OFFSET clause, with its arguments appended to args.
func (f *TemplateFilter) Page(args []any) (string, []any) { return page(args, f.Limit, f.Offset) }
