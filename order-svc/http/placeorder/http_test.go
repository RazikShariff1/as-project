package placeorder

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/zopping/order-svc/common/testutil"
	"github.com/zopping/order-svc/models"
)

func TestACustomerAlwaysOrdersForThemselves(t *testing.T) {
	req := &models.OrderPlaceCreateHTTP{CustomerID: 99, ClientID: "c1"}

	got := getOrderPlaceCreateSvc(testutil.CtxAs(t, testutil.Customer), req)

	assert.Equal(t, int64(5), got.CustomerID, "the customerId in the body is ignored for a customer")
	assert.Equal(t, int64(5), *got.UserID)
	assert.Equal(t, "c1", got.ClientID)
}

func TestStaffNameTheCustomer(t *testing.T) {
	got := getOrderPlaceCreateSvc(testutil.CtxAs(t, testutil.Staff), &models.OrderPlaceCreateHTTP{CustomerID: 99})

	assert.Equal(t, int64(99), got.CustomerID)
	assert.Equal(t, int64(1), *got.UserID)
}
