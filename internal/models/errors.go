package models

import "errors"

// Repository errors
var (
	ErrOrderNotFound       = errors.New("order not found")
	ErrOptimisticLock      = errors.New("optimistic lock failure: version mismatch")
	ErrIdempotencyMismatch = errors.New("idempotency key exists with different request hash")
	ErrInvalidOrderStatus  = errors.New("invalid order status for operation")
)
