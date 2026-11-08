package models

import "time"

// Reservation statuses.
const (
	ReservationReserved  = "RESERVED"
	ReservationConfirmed = "CONFIRMED"
	ReservationReleased  = "RELEASED"
)

// DefaultReservationTTLSeconds is how long stock is held when the request does not say.
const DefaultReservationTTLSeconds = 900

// ReservationItem is a quantity of one seller's offer for a variant in a reservation.
type ReservationItem struct {
	VariantID int64 `json:"variantId"`
	SellerID  int64 `json:"sellerId"`
	Quantity  int64 `json:"quantity"`
}

// Reservation is a hold on stock for one order. Status is the common status of its items.
type Reservation struct {
	OrderRef  string            `json:"orderRef"`
	Status    string            `json:"status"`
	Items     []ReservationItem `json:"items"`
	ExpiresAt time.Time         `json:"expiresAt"`
	CreatedAt time.Time         `json:"createdAt"`
}

// ReservationRequest reserves every item for an order, all or nothing. Repeating a request with the same
// orderRef and items is a no-op that returns the existing reservation.
type ReservationRequest struct {
	OrderRef   string            `json:"orderRef"`
	Items      []ReservationItem `json:"items"`
	TTLSeconds int               `json:"ttlSeconds"`
}
