package communication

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zopping/order-svc/clients/lookup"
	"github.com/zopping/order-svc/models"
)

func order() *models.Order {
	return &models.Order{
		ReferenceNumber: 42, OrderRef: "ORD-1", CustomerID: 7, PaymentMethod: "COD", Subtotal: 250, DiscountTotal: 20, Total: 230,
		DeliveryAddress: models.Address{Name: "Asha <b>", Phone: "99999", Line1: "1 Main St", City: "Pune", Pincode: "411001"},
		Items: []models.OrderItem{
			{VariantID: 1, Quantity: 2, UnitPrice: 100, LineTotal: 200},
			{VariantID: 2, Quantity: 1, UnitPrice: 50, LineTotal: 50},
		},
	}
}

func TestInvoice(t *testing.T) {
	got := Invoice(order(), map[int64]string{1: "Rice <script>"}, "₹")

	assert.Contains(t, got, "Rice &lt;script&gt;", "names are escaped: the template inserts the invoice as is")
	assert.Contains(t, got, "Item #2", "a variant with no name still shows")
	assert.Contains(t, got, "₹200.00")
	assert.Contains(t, got, "-₹20.00")
	assert.Contains(t, got, "Asha &lt;b&gt;<br/>1 Main St<br/>Pune, 411001<br/>Phone: 99999")
	assert.Contains(t, got, "<b>Payment:</b> COD")
	assert.NotContains(t, got, "<script>")

	o := order()
	o.DiscountTotal = 0
	assert.NotContains(t, Invoice(o, nil, "₹"), "Discount")
}

func TestEmails(t *testing.T) {
	h := &HTTP{Owner: "admin@example.com", StoreName: "Zopping", SupportEmail: "help@example.com", Currency: "₹"}

	emails := h.Emails(order(), lookup.Contact{Name: "Asha", Email: "asha@example.com"}, nil)
	require.Len(t, emails, 2)

	customer, owner := emails[0], emails[1]
	assert.Equal(t, "customer/placeOrder", customer.Type)
	assert.Equal(t, "asha@example.com", customer.To)
	assert.Equal(t, "Asha", customer.Data["customerName"])
	assert.Equal(t, "42", customer.Data["orderNumber"])
	assert.Equal(t, "Zopping", customer.Data["organizationName"])
	assert.Equal(t, "help@example.com", customer.Data["supportEmail"])
	assert.True(t, strings.HasPrefix(customer.Data["invoice"].(string), "<table"))

	assert.Equal(t, "owner/placeOrder", owner.Type)
	assert.Equal(t, "admin@example.com", owner.To)
	assert.Equal(t, "Admin", owner.Data["ownerName"])
	assert.NotContains(t, owner.Data, "customerName")
}

func TestEmails_OnlyWhoCanBeReached(t *testing.T) {
	// No shopper address (guest, or the lookup failed): only the owner hears about it.
	h := &HTTP{Owner: "admin@example.com"}
	emails := h.Emails(order(), lookup.Contact{}, nil)
	require.Len(t, emails, 1)
	assert.Equal(t, "owner/placeOrder", emails[0].Type)

	// No owner configured: only the shopper, and a missing support address leaves that field out.
	h = &HTTP{}
	emails = h.Emails(order(), lookup.Contact{Email: "asha@example.com"}, nil)
	require.Len(t, emails, 1)
	assert.Equal(t, "Customer", emails[0].Data["customerName"])
	assert.NotContains(t, emails[0].Data, "supportEmail")
}
