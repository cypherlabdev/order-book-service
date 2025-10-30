package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cypherlabdev/order-book-service/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/shopspring/decimal"
)

// PostgresOrderRepository implements OrderRepository using PostgreSQL
type PostgresOrderRepository struct {
	pool   *pgxpool.Pool
	logger zerolog.Logger
}

// NewPostgresOrderRepository creates a new PostgreSQL order repository
func NewPostgresOrderRepository(pool *pgxpool.Pool, logger zerolog.Logger) *PostgresOrderRepository {
	return &PostgresOrderRepository{
		pool:   pool,
		logger: logger.With().Str("component", "postgres_order_repository").Logger(),
	}
}

// Create creates a new order
func (r *PostgresOrderRepository) Create(ctx context.Context, tx pgx.Tx, order *models.Order) error {
	query := `
		INSERT INTO orders (
			id, user_id, market_id, selection_id, side, price, size,
			size_matched, size_remaining, status, reservation_id, saga_id,
			idempotency_key, placed_at, version
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`

	// Generate UUID if not provided
	if order.ID == uuid.Nil {
		order.ID = uuid.New()
	}

	// Initialize timestamps
	if order.PlacedAt.IsZero() {
		order.PlacedAt = time.Now()
	}

	// Initialize version
	order.Version = 1

	// Initialize matched amounts if zero
	if order.SizeMatched.IsZero() {
		order.SizeMatched = decimal.Zero
	}
	if order.SizeRemaining.IsZero() {
		order.SizeRemaining = order.Size
	}

	// Validate status
	if order.Status == "" {
		order.Status = models.OrderStatusPending
	}

	_, err := tx.Exec(ctx, query,
		order.ID,
		order.UserID,
		order.MarketID,
		order.SelectionID,
		order.Side,
		order.Price.String(),
		order.Size.String(),
		order.SizeMatched.String(),
		order.SizeRemaining.String(),
		order.Status,
		order.ReservationID,
		order.SagaID,
		order.IdempotencyKey,
		order.PlacedAt,
		order.Version,
	)

	if err != nil {
		// Check for unique constraint violation on idempotency key
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23505" { // unique_violation
				r.logger.Debug().
					Str("idempotency_key", order.IdempotencyKey).
					Msg("order with idempotency key already exists")
				return fmt.Errorf("duplicate idempotency key: %w", err)
			}
		}
		r.logger.Error().Err(err).
			Str("order_id", order.ID.String()).
			Str("user_id", order.UserID.String()).
			Msg("failed to create order")
		return fmt.Errorf("create order: %w", err)
	}

	r.logger.Info().
		Str("order_id", order.ID.String()).
		Str("user_id", order.UserID.String()).
		Str("market_id", order.MarketID).
		Str("selection_id", order.SelectionID).
		Str("side", string(order.Side)).
		Str("price", order.Price.String()).
		Str("size", order.Size.String()).
		Msg("order created")

	return nil
}

// GetByID retrieves an order by ID
func (r *PostgresOrderRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Order, error) {
	query := `
		SELECT id, user_id, market_id, selection_id, side, price, size,
			   size_matched, size_remaining, status, reservation_id, saga_id,
			   idempotency_key, placed_at, matched_at, cancelled_at, version
		FROM orders
		WHERE id = $1
	`

	return r.scanOrder(ctx, r.pool.QueryRow(ctx, query, id))
}

// GetByIDForUpdate retrieves an order with FOR UPDATE lock
func (r *PostgresOrderRepository) GetByIDForUpdate(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*models.Order, error) {
	query := `
		SELECT id, user_id, market_id, selection_id, side, price, size,
			   size_matched, size_remaining, status, reservation_id, saga_id,
			   idempotency_key, placed_at, matched_at, cancelled_at, version
		FROM orders
		WHERE id = $1
		FOR UPDATE  -- Pessimistic lock for serialization
	`

	return r.scanOrder(ctx, tx.QueryRow(ctx, query, id))
}

// Update updates an order with optimistic locking
func (r *PostgresOrderRepository) Update(ctx context.Context, tx pgx.Tx, order *models.Order) error {
	query := `
		UPDATE orders
		SET size_matched = $1, size_remaining = $2, status = $3,
		    matched_at = $4, cancelled_at = $5, version = version + 1
		WHERE id = $6 AND version = $7
	`

	result, err := tx.Exec(ctx, query,
		order.SizeMatched.String(),
		order.SizeRemaining.String(),
		order.Status,
		order.MatchedAt,
		order.CancelledAt,
		order.ID,
		order.Version,
	)

	if err != nil {
		r.logger.Error().Err(err).
			Str("order_id", order.ID.String()).
			Msg("failed to update order")
		return fmt.Errorf("update order: %w", err)
	}

	if result.RowsAffected() == 0 {
		// Version mismatch or order not found
		r.logger.Warn().
			Str("order_id", order.ID.String()).
			Int64("version", order.Version).
			Msg("optimistic lock failure")
		return models.ErrOptimisticLock
	}

	// Increment version in memory
	order.Version++

	r.logger.Debug().
		Str("order_id", order.ID.String()).
		Str("status", string(order.Status)).
		Str("size_matched", order.SizeMatched.String()).
		Str("size_remaining", order.SizeRemaining.String()).
		Int64("version", order.Version).
		Msg("order updated")

	return nil
}

// Cancel cancels an order
func (r *PostgresOrderRepository) Cancel(ctx context.Context, tx pgx.Tx, id uuid.UUID, sagaID string) error {
	query := `
		UPDATE orders
		SET status = $1, cancelled_at = $2, version = version + 1
		WHERE id = $3 AND saga_id = $4 AND status IN ($5, $6)
	`

	now := time.Now()
	result, err := tx.Exec(ctx, query,
		models.OrderStatusCancelled,
		now,
		id,
		sagaID,
		models.OrderStatusPending,
		models.OrderStatusPartially,
	)

	if err != nil {
		r.logger.Error().Err(err).
			Str("order_id", id.String()).
			Str("saga_id", sagaID).
			Msg("failed to cancel order")
		return fmt.Errorf("cancel order: %w", err)
	}

	if result.RowsAffected() == 0 {
		r.logger.Warn().
			Str("order_id", id.String()).
			Str("saga_id", sagaID).
			Msg("order not found or cannot be cancelled")
		return models.ErrInvalidOrderStatus
	}

	r.logger.Info().
		Str("order_id", id.String()).
		Str("saga_id", sagaID).
		Msg("order cancelled")

	return nil
}

// Settle settles an order with result
func (r *PostgresOrderRepository) Settle(ctx context.Context, tx pgx.Tx, id uuid.UUID, result string, actualPayout decimal.Decimal) error {
	query := `
		UPDATE orders
		SET status = $1, version = version + 1
		WHERE id = $2 AND status = $3
	`

	// For settlement, we just update status - actual payout is tracked elsewhere
	status := models.OrderStatusMatched // Settled orders remain matched
	execResult, err := tx.Exec(ctx, query,
		status,
		id,
		models.OrderStatusMatched,
	)

	if err != nil {
		r.logger.Error().Err(err).
			Str("order_id", id.String()).
			Str("result", result).
			Msg("failed to settle order")
		return fmt.Errorf("settle order: %w", err)
	}

	if execResult.RowsAffected() == 0 {
		r.logger.Warn().
			Str("order_id", id.String()).
			Msg("order not found or not in matched status")
		return models.ErrInvalidOrderStatus
	}

	r.logger.Info().
		Str("order_id", id.String()).
		Str("result", result).
		Str("payout", actualPayout.String()).
		Msg("order settled")

	return nil
}

// UpdateMatched updates the matched amounts for an order
func (r *PostgresOrderRepository) UpdateMatched(ctx context.Context, tx pgx.Tx, id uuid.UUID, sizeMatched, sizeRemaining decimal.Decimal, status models.OrderStatus, version int64) error {
	query := `
		UPDATE orders
		SET size_matched = $1, size_remaining = $2, status = $3,
		    matched_at = CASE WHEN $3 IN ($4, $5) THEN NOW() ELSE matched_at END,
		    version = version + 1
		WHERE id = $6 AND version = $7
	`

	result, err := tx.Exec(ctx, query,
		sizeMatched.String(),
		sizeRemaining.String(),
		status,
		models.OrderStatusMatched,
		models.OrderStatusPartially,
		id,
		version,
	)

	if err != nil {
		r.logger.Error().Err(err).
			Str("order_id", id.String()).
			Msg("failed to update matched amounts")
		return fmt.Errorf("update matched amounts: %w", err)
	}

	if result.RowsAffected() == 0 {
		r.logger.Warn().
			Str("order_id", id.String()).
			Int64("version", version).
			Msg("optimistic lock failure on update matched")
		return models.ErrOptimisticLock
	}

	r.logger.Debug().
		Str("order_id", id.String()).
		Str("size_matched", sizeMatched.String()).
		Str("size_remaining", sizeRemaining.String()).
		Str("status", string(status)).
		Msg("order matched amounts updated")

	return nil
}

// GetByUserID gets orders for a user with pagination
func (r *PostgresOrderRepository) GetByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*models.Order, error) {
	query := `
		SELECT id, user_id, market_id, selection_id, side, price, size,
			   size_matched, size_remaining, status, reservation_id, saga_id,
			   idempotency_key, placed_at, matched_at, cancelled_at, version
		FROM orders
		WHERE user_id = $1
		ORDER BY placed_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.pool.Query(ctx, query, userID, limit, offset)
	if err != nil {
		r.logger.Error().Err(err).
			Str("user_id", userID.String()).
			Msg("failed to query orders by user")
		return nil, fmt.Errorf("query orders by user: %w", err)
	}
	defer rows.Close()

	return r.scanOrders(rows)
}

// GetBySagaID retrieves order by saga ID
func (r *PostgresOrderRepository) GetBySagaID(ctx context.Context, sagaID string) (*models.Order, error) {
	query := `
		SELECT id, user_id, market_id, selection_id, side, price, size,
			   size_matched, size_remaining, status, reservation_id, saga_id,
			   idempotency_key, placed_at, matched_at, cancelled_at, version
		FROM orders
		WHERE saga_id = $1
	`

	return r.scanOrder(ctx, r.pool.QueryRow(ctx, query, sagaID))
}

// GetByMarketAndSelection gets orders for a specific market and selection
func (r *PostgresOrderRepository) GetByMarketAndSelection(ctx context.Context, marketID, selectionID string, side models.OrderSide, status models.OrderStatus, limit int) ([]*models.Order, error) {
	query := `
		SELECT id, user_id, market_id, selection_id, side, price, size,
			   size_matched, size_remaining, status, reservation_id, saga_id,
			   idempotency_key, placed_at, matched_at, cancelled_at, version
		FROM orders
		WHERE market_id = $1 AND selection_id = $2 AND side = $3 AND status = $4
		ORDER BY
			CASE WHEN side = $5 THEN price END DESC,  -- BACK orders: highest price first
			CASE WHEN side = $6 THEN price END ASC,   -- LAY orders: lowest price first
			placed_at ASC  -- Time priority
		LIMIT $7
	`

	rows, err := r.pool.Query(ctx, query,
		marketID,
		selectionID,
		side,
		status,
		models.OrderSideBack,
		models.OrderSideLay,
		limit,
	)
	if err != nil {
		r.logger.Error().Err(err).
			Str("market_id", marketID).
			Str("selection_id", selectionID).
			Str("side", string(side)).
			Msg("failed to query orders by market and selection")
		return nil, fmt.Errorf("query orders by market and selection: %w", err)
	}
	defer rows.Close()

	return r.scanOrders(rows)
}

// GetPendingOrders gets all pending orders for a market
func (r *PostgresOrderRepository) GetPendingOrders(ctx context.Context, marketID string) ([]*models.Order, error) {
	query := `
		SELECT id, user_id, market_id, selection_id, side, price, size,
			   size_matched, size_remaining, status, reservation_id, saga_id,
			   idempotency_key, placed_at, matched_at, cancelled_at, version
		FROM orders
		WHERE market_id = $1 AND status IN ($2, $3)
		ORDER BY placed_at ASC
	`

	rows, err := r.pool.Query(ctx, query,
		marketID,
		models.OrderStatusPending,
		models.OrderStatusPartially,
	)
	if err != nil {
		r.logger.Error().Err(err).
			Str("market_id", marketID).
			Msg("failed to query pending orders")
		return nil, fmt.Errorf("query pending orders: %w", err)
	}
	defer rows.Close()

	return r.scanOrders(rows)
}

// scanOrder scans a single order from a row
func (r *PostgresOrderRepository) scanOrder(ctx context.Context, row pgx.Row) (*models.Order, error) {
	var order models.Order
	var priceStr, sizeStr, sizeMatchedStr, sizeRemainingStr string

	err := row.Scan(
		&order.ID,
		&order.UserID,
		&order.MarketID,
		&order.SelectionID,
		&order.Side,
		&priceStr,
		&sizeStr,
		&sizeMatchedStr,
		&sizeRemainingStr,
		&order.Status,
		&order.ReservationID,
		&order.SagaID,
		&order.IdempotencyKey,
		&order.PlacedAt,
		&order.MatchedAt,
		&order.CancelledAt,
		&order.Version,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, models.ErrOrderNotFound
		}
		r.logger.Error().Err(err).Msg("failed to scan order")
		return nil, fmt.Errorf("scan order: %w", err)
	}

	// Parse decimal amounts
	order.Price, err = decimal.NewFromString(priceStr)
	if err != nil {
		return nil, fmt.Errorf("parse price: %w", err)
	}

	order.Size, err = decimal.NewFromString(sizeStr)
	if err != nil {
		return nil, fmt.Errorf("parse size: %w", err)
	}

	order.SizeMatched, err = decimal.NewFromString(sizeMatchedStr)
	if err != nil {
		return nil, fmt.Errorf("parse size_matched: %w", err)
	}

	order.SizeRemaining, err = decimal.NewFromString(sizeRemainingStr)
	if err != nil {
		return nil, fmt.Errorf("parse size_remaining: %w", err)
	}

	return &order, nil
}

// scanOrders scans multiple orders from rows
func (r *PostgresOrderRepository) scanOrders(rows pgx.Rows) ([]*models.Order, error) {
	var orders []*models.Order

	for rows.Next() {
		var order models.Order
		var priceStr, sizeStr, sizeMatchedStr, sizeRemainingStr string

		err := rows.Scan(
			&order.ID,
			&order.UserID,
			&order.MarketID,
			&order.SelectionID,
			&order.Side,
			&priceStr,
			&sizeStr,
			&sizeMatchedStr,
			&sizeRemainingStr,
			&order.Status,
			&order.ReservationID,
			&order.SagaID,
			&order.IdempotencyKey,
			&order.PlacedAt,
			&order.MatchedAt,
			&order.CancelledAt,
			&order.Version,
		)

		if err != nil {
			r.logger.Error().Err(err).Msg("failed to scan order")
			return nil, fmt.Errorf("scan order: %w", err)
		}

		// Parse decimal amounts
		order.Price, err = decimal.NewFromString(priceStr)
		if err != nil {
			return nil, fmt.Errorf("parse price: %w", err)
		}

		order.Size, err = decimal.NewFromString(sizeStr)
		if err != nil {
			return nil, fmt.Errorf("parse size: %w", err)
		}

		order.SizeMatched, err = decimal.NewFromString(sizeMatchedStr)
		if err != nil {
			return nil, fmt.Errorf("parse size_matched: %w", err)
		}

		order.SizeRemaining, err = decimal.NewFromString(sizeRemainingStr)
		if err != nil {
			return nil, fmt.Errorf("parse size_remaining: %w", err)
		}

		orders = append(orders, &order)
	}

	if err := rows.Err(); err != nil {
		r.logger.Error().Err(err).Msg("rows error")
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return orders, nil
}
