package fault

import "errors"

var (
	Invalid             = errors.New("invalid checkout request")
	NotFound            = errors.New("resource not found")
	OutOfStock          = errors.New("insufficient stock")
	IdempotencyConflict = errors.New("idempotency key already used with different request")
)
