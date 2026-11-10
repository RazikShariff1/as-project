package orderlog

import "github.com/zopping/order-svc/common/httpx"

// Errors for an entry that is missing a part. Each answers 400 naming the part.
var (
	ErrOrderIDMissing = httpx.MissingParam("referenceNumber")
	ErrActionMissing  = httpx.InvalidParam("action")
	ErrDetailsMissing = httpx.MissingParam("details")
)
