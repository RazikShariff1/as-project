// Package render fills an email template with data. The syntax is the reference communication-service's:
//
//	{{field}}                      the value of field
//	{{%list}}...{{/list}}          repeated for each object in the array list; inside, {{field}} is the object's field
//	{{#flag}}...{{/flag}}          kept only when flag is true (or "true")
//	{{#field Exists}}...{{/field Exists}}   kept only when field is present in data
//
// A {{field}} with no value is an error, so a half-filled email is never sent.
//
// In a body, values are HTML-escaped, except fields that carry ready-made HTML: the reference's {{invoice}}, and any field whose name
// ends in HTML. The caller builds those and must escape what it puts in them.
package render

import (
	"errors"
	"fmt"
	"html"
	"regexp"
	"strings"
)

var (
	loopRegex     = regexp.MustCompile(`{{%(\w+)}}`)
	decisionRegex = regexp.MustCompile(`{{#(\w+(?: \w+)?)}}`)
	variableRegex = regexp.MustCompile(`{{(\w+)}}`)
)

// Render returns the template with data filled in. With escapeHTML the values are HTML-escaped, for a body.
func Render(template string, data map[string]any, escapeHTML bool) (string, error) {
	r := renderer{template: template, data: data, escape: escapeHTML}

	if err := r.renderLoops(); err != nil {
		return "", err
	}

	r.renderConditionalBlocks()

	if err := r.replaceVariables(); err != nil {
		return "", err
	}

	return r.template, nil
}

type renderer struct {
	template string
	data     map[string]any
	escape   bool
}

func (r *renderer) renderLoops() error {
	for {
		matches := loopRegex.FindStringSubmatch(r.template)
		if matches == nil {
			return nil
		}

		field := matches[1]
		closed := regexp.MustCompile(`(?s){{%` + field + `}}(.*?){{/` + field + `}}`)

		if !closed.MatchString(r.template) {
			return errors.New("loop `" + field + "` is not closed")
		}

		if r.data[field] == nil {
			r.template = closed.ReplaceAllLiteralString(r.template, "")

			continue
		}

		if err := r.processLoop(closed, field); err != nil {
			return err
		}
	}
}

func (r *renderer) processLoop(closed *regexp.Regexp, field string) error {
	content := closed.FindStringSubmatch(r.template)[1]

	items, ok := r.data[field].([]any)
	if !ok {
		return errors.New("field `" + field + "` is expected to be an array of objects")
	}

	var sb strings.Builder

	for _, item := range items {
		fields, ok := item.(map[string]any)
		if !ok {
			continue
		}

		inner := renderer{template: content, data: fields, escape: r.escape}
		inner.renderConditionalBlocks()

		if err := inner.replaceVariables(); err != nil {
			return err
		}

		sb.WriteString(inner.template)
	}

	r.template = closed.ReplaceAllLiteralString(r.template, sb.String())

	return nil
}

func (r *renderer) renderConditionalBlocks() {
	for {
		matches := decisionRegex.FindStringSubmatch(r.template)
		if matches == nil {
			return
		}

		field := matches[1]
		closed := regexp.MustCompile(`(?s){{#` + field + `}}(.*?){{/` + field + `}}`)

		if !closed.MatchString(r.template) {
			// An unclosed block: drop the opening tag so the loop ends; the stray text then renders as is.
			r.template = strings.Replace(r.template, matches[0], "", 1)

			continue
		}

		keep := false

		if name, isExists := strings.CutSuffix(field, " Exists"); isExists {
			keep = r.data[name] != nil
		} else {
			keep = r.data[field] == true || r.data[field] == "true"
		}

		if keep {
			r.template = closed.ReplaceAllString(r.template, "$1")
		} else {
			r.template = closed.ReplaceAllLiteralString(r.template, "")
		}
	}
}

// replaceVariables substitutes every {{field}} in one pass, so a value that itself looks like {{x}} is never expanded.
func (r *renderer) replaceVariables() error {
	var missing string

	out := variableRegex.ReplaceAllStringFunc(r.template, func(tag string) string {
		field := tag[2 : len(tag)-2]

		value, ok := r.data[field]
		if !ok || value == nil {
			missing = field

			return tag
		}

		s := fmt.Sprintf("%v", value)
		if r.escape && !isHTML(field) {
			s = html.EscapeString(s)
		}

		// A value is data, never template: break up anything that looks like a tag in case a later stage looks at it.
		return strings.ReplaceAll(s, "{{", "{​{")
	})

	if missing != "" {
		return errors.New("missing field `" + missing + "` to render template")
	}

	r.template = out

	return nil
}

// isHTML reports whether a field carries HTML the caller built, which is inserted as is.
func isHTML(field string) bool { return field == "invoice" || strings.HasSuffix(field, "HTML") }
