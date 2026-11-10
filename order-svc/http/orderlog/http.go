// Package orderlog holds the HTTP handlers for /order-log, an order's history.
package orderlog

import (
	"encoding/json"

	"golang.org/x/sync/errgroup"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/common/httpx"
	"github.com/zopping/order-svc/filters"
	"github.com/zopping/order-svc/models"
	"github.com/zopping/order-svc/services"
)

// Handler serves GET and POST /order-log.
type Handler struct {
	orderLogService services.OrderLog
}

// New returns an order-log handler.
func New(orderLogService services.OrderLog) *Handler {
	return &Handler{orderLogService: orderLogService}
}

// Index lists an order's history, newest first. Query: orderReferenceNumber (required), orderId, action. The count and the entries
// are read at the same time.
func (h *Handler) Index(ctx *gofr.Context) (any, error) {
	filter := filters.OrderLog{
		OrderReferenceNumber: ctx.Param("orderReferenceNumber"),
		OrderID:              ctx.Param("orderId"),
		Action:               ctx.Param("action"),
	}

	var (
		count    int
		orderLog []*models.OrderLog
		g        errgroup.Group
	)

	// Each call scopes its own copy of the filter to the caller.
	countFilter, findFilter := filter, filter

	g.Go(func() (err error) {
		count, err = h.orderLogService.Count(ctx, &countFilter)

		return err
	})

	g.Go(func() (err error) {
		orderLog, err = h.orderLogService.Find(ctx, &findFilter)

		return err
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	var externalOrder models.ExternalOrder
	if len(orderLog) > 0 {
		externalOrder.ClientID = orderLog[0].ClientID
	}

	return models.OrderLogResponse{Limit: count, Offset: 0, Count: count, OrderLog: orderLog, Order: &externalOrder}, nil
}

// Create adds a staff entry to an order's history and answers {"id": ...}.
func (h *Handler) Create(ctx *gofr.Context) (any, error) {
	var request models.OrderLogCreateRequest
	if err := ctx.Bind(&request); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	if request.ReferenceNumber < 1 {
		return nil, httpx.MissingParam("referenceNumber")
	}

	if request.Action == "" {
		return nil, httpx.MissingParam("action")
	}

	if len(request.Details) == 0 || !json.Valid(request.Details) {
		return nil, httpx.MissingParam("details")
	}

	orderID, err := h.orderLogService.GetOrderID(ctx, request.ReferenceNumber)
	if err != nil {
		return nil, err
	}

	id, err := h.orderLogService.Create(ctx, &models.OrderLog{OrderID: orderID, Action: request.Action, Details: request.Details})
	if err != nil {
		return nil, err
	}

	return map[string]int64{"id": id}, nil
}
