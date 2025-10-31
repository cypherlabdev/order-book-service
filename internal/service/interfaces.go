package service

import (
	"context"

	"github.com/cypherlabdev/order-book-service/internal/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// OrderService defines the business logic interface for order management
type OrderService interface {
	// PlaceOrder creates a new bet order with idempotency protection
	// Returns the created order or an existing order if idempotency key matches
	PlaceOrder(ctx context.Context, req *PlaceOrderRequest) (*models.Order, error)

	// CancelOrder cancels an active order
	// Only active orders can be cancelled
	CancelOrder(ctx context.Context, req *CancelOrderRequest) error

	// SettleOrder settles a completed order (win or loss)
	// Updates order status and records actual payout
	SettleOrder(ctx context.Context, req *SettleOrderRequest) error

	// GetOrderByID retrieves a single order by ID
	GetOrderByID(ctx context.Context, orderID uuid.UUID) (*models.Order, error)

	// GetUserOrders retrieves orders for a specific user with pagination
	GetUserOrders(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*models.Order, error)

	// GetOrdersByEvent retrieves all orders for a specific event
	GetOrdersByEvent(ctx context.Context, eventID string, limit, offset int) ([]*models.Order, error)

	// GetActiveOrders retrieves all active orders with pagination
	GetActiveOrders(ctx context.Context, limit, offset int) ([]*models.Order, error)
}

// PlaceOrderRequest represents the request to place a new order
type PlaceOrderRequest struct {
	UserID          uuid.UUID       `validate:"required"`
	EventID         string          `validate:"required"`
	BetType         string          `validate:"required"`
	Selection       string          `validate:"required"`
	Amount          decimal.Decimal `validate:"required"`
	Odds            decimal.Decimal `validate:"required"`
	ReservationID   *uuid.UUID
	SagaID          *uuid.UUID
	IdempotencyKey  string          `validate:"required"`
}

// CancelOrderRequest represents the request to cancel an order
type CancelOrderRequest struct {
	OrderID        uuid.UUID  `validate:"required"`
	SagaID         *uuid.UUID
	IdempotencyKey string     `validate:"required"`
}

// SettleOrderRequest represents the request to settle an order
type SettleOrderRequest struct {
	OrderID        uuid.UUID       `validate:"required"`
	Result         string          `validate:"required,oneof=win loss"`
	ActualPayout   decimal.Decimal `validate:"required"`
	SagaID         *uuid.UUID
	IdempotencyKey string          `validate:"required"`
}
