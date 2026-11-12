package render

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRender(t *testing.T) {
	data := map[string]any{
		"name":    "Sam <b>",
		"vip":     true,
		"invoice": "<table>x</table>", "noteHTML": "<b>hi</b>",
		"items": []any{map[string]any{"n": "rice", "q": 2.0}, map[string]any{"n": "dal", "q": 1.0}},
	}

	tests := []struct {
		name, template, want string
		escape               bool
	}{
		{"variable", "Hi {{name}}", "Hi Sam <b>", false},
		{"escaped", "Hi {{name}}", "Hi Sam &lt;b&gt;", true},
		{"loop", "{{%items}}[{{n}}x{{q}}]{{/items}}", "[ricex2][dalx1]", false},
		{"missing loop data renders nothing", "a{{%rows}}x{{/rows}}b", "ab", false},
		{"flag on", "{{#vip}}gold{{/vip}}", "gold", false},
		{"flag off", "{{#other}}gold{{/other}}", "", false},
		{"exists", "{{#name Exists}}yes{{/name Exists}}", "yes", false},
		{"not exists", "{{#nope Exists}}yes{{/nope Exists}}", "", false},
		{"invoice is raw html", "{{invoice}}", "<table>x</table>", true},
		{"a field ending in HTML is raw html", "{{noteHTML}}", "<b>hi</b>", true},
		{"a value that looks like a tag is not expanded", "{{x}}", "{​{name}}", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := data
			if tc.name == "a value that looks like a tag is not expanded" {
				d = map[string]any{"x": "{{name}}", "name": "leak"}
			}

			got, err := Render(tc.template, d, tc.escape)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestRenderErrors(t *testing.T) {
	_, err := Render("Hi {{who}}", map[string]any{}, false)
	assert.ErrorContains(t, err, "missing field `who`")

	_, err = Render("{{%items}}x", map[string]any{"items": []any{}}, false)
	assert.ErrorContains(t, err, "not closed")

	_, err = Render("{{%items}}x{{/items}}", map[string]any{"items": "nope"}, false)
	assert.ErrorContains(t, err, "array")
}
