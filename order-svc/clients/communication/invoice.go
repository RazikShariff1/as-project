package communication

import (
	"fmt"
	"html"
	"strings"

	"github.com/zopping/order-svc/models"
)

const (
	cellStyle  = "padding:6px 10px;border:1px solid #dddddd;"
	rightStyle = cellStyle + "text-align:right;"
)

// Invoice renders the order as the HTML the emails put in {{invoice}}: the items, the totals, how it is paid and where it goes.
// names maps a variant id to its display name; a variant without one shows as "Item #id". Everything from the order or the
// shopper is escaped, because the template inserts this block as is.
func Invoice(o *models.Order, names map[int64]string, currency string) string {
	var b strings.Builder

	money := func(v float64) string { return html.EscapeString(currency) + fmt.Sprintf("%.2f", v) }

	b.WriteString(`<table style="border-collapse:collapse;width:100%;font-size:14px;">`)
	b.WriteString(`<tr style="background:#f5f5f5;"><th style="` + cellStyle + `text-align:left;">Item</th><th style="` + rightStyle +
		`">Qty</th><th style="` + rightStyle + `">Price</th><th style="` + rightStyle + `">Total</th></tr>`)

	for _, it := range o.Items {
		name := names[it.VariantID]
		if name == "" {
			name = fmt.Sprintf("Item #%d", it.VariantID)
		}

		fmt.Fprintf(&b, `<tr><td style="%s">%s</td><td style="%s">%d</td><td style="%s">%s</td><td style="%s">%s</td></tr>`,
			cellStyle, html.EscapeString(name), rightStyle, it.Quantity, rightStyle, money(it.UnitPrice), rightStyle, money(it.LineTotal))
	}

	row := func(label, value string, bold bool) {
		weight := "normal"
		if bold {
			weight = "bold"
		}

		fmt.Fprintf(&b, `<tr style="font-weight:%s;"><td colspan="3" style="%s">%s</td><td style="%s">%s</td></tr>`,
			weight, rightStyle, label, rightStyle, value)
	}

	row("Subtotal", money(o.Subtotal), false)

	if o.DiscountTotal > 0 {
		row("Discount", "-"+money(o.DiscountTotal), false)
	}

	row("Total", money(o.Total), true)
	b.WriteString("</table>")

	if o.PaymentMethod != "" {
		fmt.Fprintf(&b, `<p><b>Payment:</b> %s</p>`, html.EscapeString(o.PaymentMethod))
	}

	if addr := address(o.DeliveryAddress); addr != "" {
		fmt.Fprintf(&b, `<p><b>Delivery address:</b><br/>%s</p>`, addr)
	}

	return b.String()
}

// address joins the non-empty address lines with line breaks, each escaped.
func address(a models.Address) string {
	cityLine := strings.TrimSpace(strings.Join(nonEmpty(a.City, a.State, a.Pincode), ", "))
	lines := nonEmpty(a.Name, a.Line1, a.Line2, cityLine, a.Country)

	if a.Phone != "" {
		lines = append(lines, "Phone: "+a.Phone)
	}

	for i, l := range lines {
		lines[i] = html.EscapeString(l)
	}

	return strings.Join(lines, "<br/>")
}

func nonEmpty(parts ...string) []string {
	var out []string

	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, strings.TrimSpace(p))
		}
	}

	return out
}
