package repository

import (
	"context"

	"github.com/cypherlabdev/order-book-service/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// OrderRepository defines the interface for order data access
type OrderRepository interface {
	// Create creates a new order in a transaction
	// MUST be called within a transaction
	Create(ctx context.Context, tx pgx.Tx, order *models.Order) error

	// GetByID retrieves an order by ID
	// Returns ErrOrderNotFound if order doesn't exist
	GetByID(ctx context.Context, id uuid.UUID) (*models.Order, error)

	// GetByIDForUpdate retrieves an order with FOR UPDATE lock (pessimistic lock)
	// MUST be called within a transaction
	// Returns ErrOrderNotFound if order doesn't exist
	GetByIDForUpdate(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*models.Order, error)

	// Update updates an order with optimistic locking
	// MUST be called within a transaction
	// Returns ErrOptimisticLock if version mismatch
	Update(ctx context.Context, tx pgx.Tx, order *models.Order) error

	// Cancel cancels an order
	// MUST be called within a transaction
	// Returns ErrOptimisticLock if version mismatch
	// Returns ErrInvalidOrderStatus if order cannot be cancelled
	Cancel(ctx context.Context, tx pgx.Tx, id uuid.UUID, sagaID string) error

	// Settle settles an order with result
	// MUST be called within a transaction
	// Returns ErrOptimisticLock if version mismatch
	Settle(ctx context.Context, tx pgx.Tx, id uuid.UUID, result string, actualPayout decimal.Decimal) error

	// UpdateMatched updates the matched amounts for an order
	// MUST be called within a transaction
	// Returns ErrOptimisticLock if version mismatch
	UpdateMatched(ctx context.Context, tx pgx.Tx, id uuid.UUID, sizeMatched, sizeRemaining decimal.Decimal, status models.OrderStatus, version int64) error

	// GetByUserID gets orders for a user with pagination
	// Returns empty slice if no orders found
	GetByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*models.Order, error)

	// GetBySagaID retrieves order by saga ID
	// Returns ErrOrderNotFound if order doesn't exist
	GetBySagaID(ctx context.Context, sagaID string) (*models.Order, error)

	// GetByMarketAndSelection gets orders for a specific market and selection
	// Used for order book retrieval and matching
	GetByMarketAndSelection(ctx context.Context, marketID, selectionID string, side models.OrderSide, status models.OrderStatus, limit int) ([]*models.Order, error)

	// GetPendingOrders gets all pending orders for a market
	// Used for matching engine
	GetPendingOrders(ctx context.Context, marketID string) ([]*models.Order, error)
}
