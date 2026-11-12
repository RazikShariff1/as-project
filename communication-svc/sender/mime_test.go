package sender

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPlainText(t *testing.T) {
	got := PlainText(`<div style="x"><p style="a">Dear Asha,</p><br/><p>Thanks &amp; welcome</p>` +
		`<table><tr><th>Item</th><th>Qty</th></tr><tr><td>Rice &lt;5 kg&gt;</td><td>2</td></tr></table>` +
		`<p><b>Payment:</b> COD</p><p>1 Main St<br/>Pune</p></div>`)

	assert.Equal(t, "Dear Asha,\n\nThanks & welcome\nItem | Qty\nRice <5 kg> | 2\nPayment: COD\n1 Main St\nPune", got)
}
