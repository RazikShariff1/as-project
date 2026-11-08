package sellerproductchange

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"
	"gofr.dev/pkg/gofr/logging"

	"github.com/zopping/catalogue-svc/services"
)

func ctxWith(body string) *gofr.Context {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	return &gofr.Context{
		Request:       gofrhttp.NewRequest(r),
		ContextLogger: logging.ContextLoggerFor(context.Background(), logging.NewMockLogger(logging.DEBUG)),
	}
}

func TestHandle(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	svc := services.NewMockSellerChange(ctrl)
	sub := New(svc)
	boom := errors.New("inventory-svc unreachable")

	ctx := ctxWith(`{"sellerProductRequestIDs": [4, 9]}`)
	svc.EXPECT().Process(gomock.Any(), []int64{4, 9}).Return(nil)
	assert.NoError(t, sub.Handle(ctx))

	ctx = ctxWith(`{"sellerProductRequestIDs": [2]}`)
	svc.EXPECT().Process(gomock.Any(), []int64{2}).Return(boom)
	assert.NoError(t, sub.Handle(ctx), "a failed apply is acknowledged: with GoFr's Google backend an error return would only pin "+
		"the message for an hour. The request stays unsynced and the reconcile endpoint applies it later")

	assert.NoError(t, sub.Handle(ctxWith(`{not json`)), "a malformed message is acknowledged and dropped: retrying cannot fix it")
}
