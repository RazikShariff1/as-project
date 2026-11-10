package services

import (
	"fmt"
	"slices"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/common/httpx"
	"github.com/zopping/order-svc/models"
	"github.com/zopping/order-svc/utils"
)

func slicesSort(s []string) { slices.Sort(s) }

// validateItems rejects item edits that are malformed whatever the order looks like.
func validateItems(edits []*ItemPayload) error {
	seen := make(map[int64]bool, len(edits))

	for _, e := range edits {
		if e == nil || e.ID < 1 || seen[e.ID] || e.Quantity == nil || *e.Quantity < 0 || len(strings.TrimSpace(e.Reason)) > utils.MaxNotes {
			return httpx.InvalidParam("orderItems")
		}

		seen[e.ID] = true
	}

	return nil
}

// updateOrderItems changes the quantities of a PENDING order's lines. Quantity 0 removes a line (it stays, as CANCELLED, with its
// price and original quantity); setting a line to the quantity it already has changes nothing.
//
// Stock cannot shrink a reservation in place, so the active lines are reserved again under a new reference and the order is
// switched to it; the old reference is kept in StaleRefs and released after the order is saved.
func (s *orderService) updateOrderItems(ctx *gofr.Context, payload *PatchOrderPayload, oldOrder, updatedOrder *models.Order) error {
	if oldOrder.Status != models.StatusPending {
		return httpx.Conflict(fmt.Sprintf("the order is %s; items can only be changed while it is %s", oldOrder.Status, models.StatusPending))
	}

	logs, err := applyEdits(payload, updatedOrder)
	if err != nil || len(logs) == 0 {
		return err
	}

	reserve := make([]models.ReservedItem, 0, len(updatedOrder.Items))

	for _, l := range updatedOrder.Items {
		if l.Status != models.ItemCancelled {
			reserve = append(reserve, models.ReservedItem{VariantID: l.VariantID, SellerID: l.SellerID, Quantity: l.Quantity * int64(l.Unit)})
		}
	}

	if len(reserve) == 0 {
		return httpx.Conflict("an order must keep at least one item: cancel the order instead")
	}

	newRef, err := utils.NewOrderRef()
	if err != nil {
		return err
	}

	if err = s.inventory.Reserve(ctx, newRef, reserve, s.ttl); err != nil {
		return httpx.FromStore(err, "order", newRef)
	}

	updatedOrder.StaleRefs = append(updatedOrder.StaleRefs, oldOrder.OrderRef)
	updatedOrder.OrderRef = newRef
	updatedOrder.Subtotal, updatedOrder.DiscountTotal, updatedOrder.Total = utils.Totals(updatedOrder.Items)
	updatedOrder.OrderLogs = append(updatedOrder.OrderLogs, logs...)

	return nil
}

// applyEdits changes updatedOrder's lines in place and returns a history row for each real change.
func applyEdits(payload *PatchOrderPayload, updatedOrder *models.Order) ([]*models.OrderLog, error) {
	byID := make(map[int64]*models.OrderItem, len(updatedOrder.Items))
	for i := range updatedOrder.Items {
		byID[updatedOrder.Items[i].ID] = &updatedOrder.Items[i]
	}

	var logs []*models.OrderLog

	for _, e := range payload.OrderItems {
		line, ok := byID[e.ID]
		if !ok {
			return nil, httpx.InvalidParam("orderItems")
		}

		if line.Status == models.ItemCancelled {
			return nil, httpx.Conflict(fmt.Sprintf("item %d was already removed from the order", line.ID))
		}

		if *e.Quantity == line.Quantity {
			continue
		}

		logs = append(logs, utils.NewLog(updatedOrder.ReferenceNumber, payload.UserID, models.LogItem, map[string]any{
			"orderItemId": line.ID, "variantId": line.VariantID, "sellerId": line.SellerID,
			"from": line.Quantity, "to": *e.Quantity, "reason": strings.TrimSpace(e.Reason),
		}))

		if *e.Quantity == 0 {
			line.Status = models.ItemCancelled

			continue
		}

		line.Quantity = *e.Quantity
		line.LineTotal = utils.Round2(line.UnitPrice * float64(line.Quantity))
	}

	return logs, nil
}
