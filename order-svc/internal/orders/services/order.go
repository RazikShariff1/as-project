package services

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/clients/inventory"
	"github.com/zopping/order-svc/common/apperr"
	"github.com/zopping/order-svc/common/httpx"
	"github.com/zopping/order-svc/filters"
	"github.com/zopping/order-svc/models"
	"github.com/zopping/order-svc/stores"
	"github.com/zopping/order-svc/utils"
)

type orderService struct {
	store     stores.Order
	inventory inventory.Client
	ttl       int
}

// New returns the order-change service. ttlSeconds is how long stock reserved for an item edit is held before inventory-svc may
// release it.
func New(store stores.Order, inv inventory.Client, ttlSeconds int) OrderService {
	return &orderService{store: store, inventory: inv, ttl: ttlSeconds}
}

// Patch loads the order, works out the changed order in memory (status, items, details, payment), saves it in one transaction and
// then settles stock. Concurrent patches cannot both win: the store refuses to save over an order changed since it was read here.
func (s *orderService) Patch(ctx *gofr.Context, refNum int64, payload *PatchOrderPayload) (*models.Order, error) {
	if err := validatePatch(payload); err != nil {
		return nil, err
	}

	// A customer's filter only matches their own orders, so someone else's is a 404.
	orders, err := s.store.Index(ctx, &filters.OrderDetails{ReferenceNumbers: []int64{refNum}, CustomerID: payload.CustomerID}, 1, 0)
	if err != nil {
		return nil, httpx.FromStoreID(err, "order", refNum)
	}

	if len(orders) == 0 {
		return nil, httpx.FromStoreID(apperr.ErrNotFound, "order", refNum)
	}

	oldOrder := orders[0]

	// `updatedOrder` is a deep copy of `oldOrder`: the changes below only touch it, and it is saved as it is.
	updatedOrder := oldOrder.DeepCopy()

	if payload.Status != "" && payload.Status != updatedOrder.Status {
		if err = s.processStatusChange(payload, oldOrder, updatedOrder); err != nil {
			return nil, err
		}
	}

	if payload.OrderItems != nil {
		if err = s.updateOrderItems(ctx, payload, oldOrder, updatedOrder); err != nil {
			return nil, err
		}
	}

	if payload.DeliveryAddress != nil || payload.Notes != nil {
		if err = updateAddressAndNotes(payload, oldOrder, updatedOrder); err != nil {
			s.releaseNewReservation(ctx, oldOrder, updatedOrder)

			return nil, err
		}
	}

	if payload.MetaData != nil {
		updateMetaData(payload, updatedOrder)
	}

	updatePayment(payload, oldOrder, updatedOrder)

	if !reflect.DeepEqual(oldOrder, updatedOrder) {
		saved, saveErr := s.store.Update(ctx, oldOrder.ReferenceNumber, updatedOrder)
		if saveErr != nil {
			s.releaseNewReservation(ctx, oldOrder, updatedOrder)

			return nil, httpx.FromStoreID(saveErr, "order", refNum)
		}

		updatedOrder = saved
	}

	return updatedOrder, s.publishPatchEvents(ctx, updatedOrder, payload)
}

// validatePatch checks and normalises the payload in place and applies the customer's narrower rights.
func validatePatch(p *PatchOrderPayload) error {
	p.Status = strings.ToUpper(strings.TrimSpace(p.Status))
	p.PaymentStatus = strings.ToUpper(strings.TrimSpace(p.PaymentStatus))
	p.Note = strings.TrimSpace(p.Note)
	p.CancellationReason = strings.TrimSpace(p.CancellationReason)

	customer := p.CustomerID != 0

	if customer && (p.PaymentStatus != "" || len(p.MetaData) > 0 || len(p.OrderItems) > 0 ||
		(p.Status != "" && p.Status != models.StatusCancelled)) {
		return &apperr.Forbidden{Reason: "only staff may do this"}
	}

	if p.Status == "" && p.PaymentStatus == "" && p.DeliveryAddress == nil && p.Notes == nil && len(p.MetaData) == 0 &&
		len(p.OrderItems) == 0 {
		return httpx.InvalidParam("body")
	}

	switch p.Status {
	case "", models.StatusPacked, models.StatusDispatched, models.StatusCompleted, models.StatusCancelled:
	default:
		return httpx.InvalidParam("status")
	}

	switch p.PaymentStatus {
	case "", models.PaymentPending, models.PaymentPaid, models.PaymentCancelled, models.PaymentRefund:
	default:
		return httpx.InvalidParam("paymentStatus")
	}

	if len(p.Note) > utils.MaxNotes {
		return httpx.InvalidParam("note")
	}

	if len(p.CancellationReason) > utils.MaxNotes || (p.CancellationReason != "" && p.Status != models.StatusCancelled) {
		return httpx.InvalidParam("cancellationReason")
	}

	if p.DeliveryAddress != nil {
		if err := utils.NormaliseAddress(p.DeliveryAddress); err != nil {
			return err
		}
	}

	if p.Notes != nil {
		trimmed := strings.TrimSpace(*p.Notes)
		if len(trimmed) > utils.MaxNotes {
			return httpx.InvalidParam("notes")
		}

		p.Notes = &trimmed
	}

	if err := validateItems(p.OrderItems); err != nil {
		return err
	}

	return utils.CheckMetaData(p.MetaData)
}

// processStatusChange moves updatedOrder to the status in the payload, or answers 409 if that is not a step the order can take.
// PENDING -> PACKED -> DISPATCHED -> COMPLETED, or CANCELLED from PENDING (packing turns the reservation into a sale, so a packed
// order cannot be cancelled).
func (s *orderService) processStatusChange(payload *PatchOrderPayload, oldOrder, updatedOrder *models.Order) error {
	oldStatus, newStatus := oldOrder.Status, payload.Status

	switch {
	case oldStatus == models.StatusPending && newStatus == models.StatusPacked,
		oldStatus == models.StatusPacked && newStatus == models.StatusDispatched,
		oldStatus == models.StatusDispatched && newStatus == models.StatusCompleted:
		return s.moveStatus(payload, updatedOrder)
	case oldStatus == models.StatusPending && newStatus == models.StatusCancelled:
		return s.cancelOrder(payload, updatedOrder)
	default:
		return httpx.Conflict(fmt.Sprintf("the order is %s; it cannot move to %s", oldStatus, newStatus))
	}
}

func (*orderService) moveStatus(payload *PatchOrderPayload, updatedOrder *models.Order) error {
	updatedOrder.OrderLogs = append(updatedOrder.OrderLogs, utils.NewLog(updatedOrder.ReferenceNumber, payload.UserID,
		models.LogStatus, utils.Change(updatedOrder.Status, payload.Status, payload.Note)))
	updatedOrder.Status = payload.Status

	if payload.Status == models.StatusCompleted {
		now := time.Now().UTC()
		updatedOrder.CompletedAt = &now
	}

	return nil
}

// cancelOrder cancels the order and keeps the reason (or else the note) with it.
func (*orderService) cancelOrder(payload *PatchOrderPayload, updatedOrder *models.Order) error {
	note := payload.Note
	if note == "" {
		note = payload.CancellationReason
	}

	updatedOrder.OrderLogs = append(updatedOrder.OrderLogs, utils.NewLog(updatedOrder.ReferenceNumber, payload.UserID,
		models.LogStatus, utils.Change(updatedOrder.Status, models.StatusCancelled, note)))
	updatedOrder.Status = models.StatusCancelled
	updatedOrder.CancelReason = payload.CancellationReason

	return nil
}

// updateAddressAndNotes changes the delivery address and notes, which is possible until the order is dispatched.
func updateAddressAndNotes(payload *PatchOrderPayload, oldOrder, updatedOrder *models.Order) error {
	if oldOrder.Status != models.StatusPending && oldOrder.Status != models.StatusPacked {
		return httpx.Conflict(fmt.Sprintf("the order is %s; its address and notes can only change before it is dispatched", oldOrder.Status))
	}

	if payload.DeliveryAddress != nil && *payload.DeliveryAddress != updatedOrder.DeliveryAddress {
		updatedOrder.DeliveryAddress = *payload.DeliveryAddress
		updatedOrder.OrderLogs = append(updatedOrder.OrderLogs, utils.NewLog(updatedOrder.ReferenceNumber, payload.UserID,
			models.LogUpdate, map[string]any{"field": "deliveryAddress", "note": payload.Note}))
	}

	if payload.Notes != nil && *payload.Notes != updatedOrder.Notes {
		updatedOrder.Notes = *payload.Notes
		updatedOrder.OrderLogs = append(updatedOrder.OrderLogs, utils.NewLog(updatedOrder.ReferenceNumber, payload.UserID,
			models.LogUpdate, map[string]any{"field": "notes", "note": payload.Note}))
	}

	return nil
}

// updateMetaData merges the payload's metadata into the order's.
func updateMetaData(payload *PatchOrderPayload, updatedOrder *models.Order) {
	if updatedOrder.MetaData == nil {
		updatedOrder.MetaData = map[string]any{}
	}

	keys := make([]string, 0, len(payload.MetaData))

	for k, v := range payload.MetaData {
		updatedOrder.MetaData[k] = v

		keys = append(keys, k)
	}

	slicesSort(keys)

	updatedOrder.OrderLogs = append(updatedOrder.OrderLogs, utils.NewLog(updatedOrder.ReferenceNumber, payload.UserID,
		models.LogUpdate, map[string]any{"field": "metaData", "keys": keys}))
}

// updatePayment records the payment status: what the payload says, and PAID for a COD order that has just been completed.
func updatePayment(payload *PatchOrderPayload, oldOrder, updatedOrder *models.Order) {
	payment, note := updatedOrder.PaymentStatus, payload.Note

	if payload.Status == models.StatusCompleted && oldOrder.Status != models.StatusCompleted &&
		updatedOrder.PaymentMethod == models.MethodCOD && payment == models.PaymentPending {
		payment, note = models.PaymentPaid, "cash on delivery"
	}

	if payload.PaymentStatus != "" && payload.PaymentStatus != payment {
		payment, note = payload.PaymentStatus, payload.Note
	}

	if payment == updatedOrder.PaymentStatus {
		return
	}

	updatedOrder.OrderLogs = append(updatedOrder.OrderLogs, utils.NewLog(updatedOrder.ReferenceNumber, payload.UserID,
		models.LogPayment, utils.Change(updatedOrder.PaymentStatus, payment, note)))
	updatedOrder.PaymentStatus = payment
}

// publishPatchEvents does the inventory-svc calls a saved change implies. Replaced reservations are released best effort (they
// expire on their own and are retried by the next patch); packing confirms the stock and cancelling releases it, and those errors
// are returned because the order's stock is then out of step with its status until the call is repeated.
func (s *orderService) publishPatchEvents(ctx *gofr.Context, o *models.Order, payload *PatchOrderPayload) error {
	for _, stale := range o.StaleRefs {
		if err := s.inventory.Release(ctx, stale); err != nil && !errors.Is(err, apperr.ErrNotFound) {
			ctx.Errorf("order %d: replaced stock reservation %s could not be released (retried on the next update): %v",
				o.ReferenceNumber, stale, err)

			continue
		}

		if err := s.store.ClearStaleRef(ctx, o.ReferenceNumber, stale); err != nil {
			ctx.Errorf("order %d: could not clear released reservation %s: %v", o.ReferenceNumber, stale, err)
		}
	}

	switch payload.Status {
	case models.StatusPacked:
		if err := s.inventory.Confirm(ctx, o.OrderRef); err != nil {
			return fmt.Errorf("the order is packed but its stock could not be confirmed: repeat the call to retry: %w", err)
		}
	case models.StatusCancelled:
		if err := s.inventory.Release(ctx, o.OrderRef); err != nil && !errors.Is(err, apperr.ErrNotFound) {
			return fmt.Errorf("the order is cancelled but its stock could not be released: repeat the call to retry: %w", err)
		}
	}

	return nil
}

// releaseNewReservation gives back the stock reserved for an item edit that was not saved.
func (s *orderService) releaseNewReservation(ctx *gofr.Context, oldOrder, updatedOrder *models.Order) {
	if updatedOrder.OrderRef == oldOrder.OrderRef {
		return
	}

	if err := s.inventory.Release(ctx, updatedOrder.OrderRef); err != nil && !errors.Is(err, apperr.ErrNotFound) {
		ctx.Errorf("order %d was not updated and stock reservation %s could not be released (it expires on its own): %v",
			oldOrder.ReferenceNumber, updatedOrder.OrderRef, err)
	}
}
