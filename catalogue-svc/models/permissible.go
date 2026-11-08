package models

import "time"

// Permissible seller statuses.
const (
	PermissibleEnabled  = "ENABLED"
	PermissibleDisabled = "DISABLED"
)

// PermissibleSeller says whether a seller may sell a product.
type PermissibleSeller struct {
	SellerID  int64     `json:"sellerId"`
	ProductID int64     `json:"productId"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// PermissibleSellersRequest replaces the set of sellers allowed to sell a product: the listed sellers become ENABLED
// and every other seller previously listed becomes DISABLED.
type PermissibleSellersRequest struct {
	SellerIDs []int64 `json:"sellerIds"`
}

// PermissibleSellerList is a page of permissible sellers with the total match count.
type PermissibleSellerList struct {
	PermissibleSellers []PermissibleSeller `json:"permissibleSellers"`
	Count              int                 `json:"count"`
	Offset             int                 `json:"offset"`
	Limit              int                 `json:"limit"`
}
