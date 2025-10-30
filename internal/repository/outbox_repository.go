package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cypherlabdev/order-book-service/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// OutboxRepository defines the interface for outbox event operations
type OutboxRepository interface {
	// Create inserts a new outbox event within a transaction
	// MUST be called within a transaction
	Create(ctx context.Context, tx pgx.Tx, event *models.OutboxEvent) error

	// GetUnprocessedEvents retrieves unprocessed events for publishing
	// Returns events that haven't been processed and haven't exceeded max retries
	GetUnprocessedEvents(ctx context.Context, limit int) ([]*models.OutboxEvent, error)

	// MarkProcessed marks an event as successfully published
	// Updates processed_at timestamp
	MarkProcessed(ctx context.Context, eventID uuid.UUID) error

	// IncrementRetryCount increments the retry count for a failed publish
	// Stores the error message for debugging
	IncrementRetryCount(ctx context.Context, eventID uuid.UUID, errorMsg string) error

	// CleanupProcessedEvents deletes old processed events to prevent table bloat
	// Returns the number of deleted events
	CleanupProcessedEvents(ctx context.Context, olderThan time.Duration) (int64, error)
}

// PostgresOutboxRepository implements OutboxRepository using PostgreSQL
type PostgresOutboxRepository struct {
	pool   *pgxpool.Pool
	logger zerolog.Logger
}

// NewPostgresOutboxRepository creates a new PostgreSQL outbox repository
func NewPostgresOutboxRepository(pool *pgxpool.Pool, logger zerolog.Logger) *PostgresOutboxRepository {
	return &PostgresOutboxRepository{
		pool:   pool,
		logger: logger.With().Str("component", "postgres_outbox_repository").Logger(),
	}
}

// Create inserts a new outbox event within a transaction
func (r *PostgresOutboxRepository) Create(ctx context.Context, tx pgx.Tx, event *models.OutboxEvent) error {
	query := `
		INSERT INTO outbox_events (
			id, aggregate_id, aggregate_type, event_type, event_payload,
			saga_id, created_at, retry_count, max_retries
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	// Generate UUID if not provided
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}

	// Set created_at timestamp
	event.CreatedAt = time.Now()

	// Convert event payload to JSON
	payloadJSON, err := json.Marshal(event.EventPayload)
	if err != nil {
		r.logger.Error().Err(err).
			Str("event_type", event.EventType).
			Msg("failed to marshal event payload")
		return fmt.Errorf("marshal event payload: %w", err)
	}

	_, err = tx.Exec(ctx, query,
		event.ID,
		event.AggregateID,
		event.AggregateType,
		event.EventType,
		payloadJSON,
		event.SagaID,
		event.CreatedAt,
		event.RetryCount,
		event.MaxRetries,
	)

	if err != nil {
		r.logger.Error().Err(err).
			Str("event_type", event.EventType).
			Str("aggregate_id", event.AggregateID.String()).
			Msg("failed to create outbox event")
		return fmt.Errorf("create outbox event: %w", err)
	}

	r.logger.Debug().
		Str("event_id", event.ID.String()).
		Str("event_type", event.EventType).
		Str("aggregate_type", event.AggregateType).
		Str("aggregate_id", event.AggregateID.String()).
		Msg("outbox event created")

	return nil
}

// GetUnprocessedEvents retrieves unprocessed events for publishing
func (r *PostgresOutboxRepository) GetUnprocessedEvents(ctx context.Context, limit int) ([]*models.OutboxEvent, error) {
	query := `
		SELECT id, aggregate_id, aggregate_type, event_type, event_payload,
		       saga_id, created_at, processed_at, retry_count, max_retries, last_error
		FROM outbox_events
		WHERE processed_at IS NULL AND retry_count < max_retries
		ORDER BY created_at ASC
		LIMIT $1
	`

	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		r.logger.Error().Err(err).Msg("failed to query unprocessed events")
		return nil, fmt.Errorf("query unprocessed events: %w", err)
	}
	defer rows.Close()

	var events []*models.OutboxEvent
	for rows.Next() {
		var event models.OutboxEvent
		var payloadJSON []byte

		err := rows.Scan(
			&event.ID,
			&event.AggregateID,
			&event.AggregateType,
			&event.EventType,
			&payloadJSON,
			&event.SagaID,
			&event.CreatedAt,
			&event.ProcessedAt,
			&event.RetryCount,
			&event.MaxRetries,
			&event.LastError,
		)

		if err != nil {
			r.logger.Error().Err(err).Msg("failed to scan outbox event")
			return nil, fmt.Errorf("scan outbox event: %w", err)
		}

		// Parse JSON payload
		if err := json.Unmarshal(payloadJSON, &event.EventPayload); err != nil {
			r.logger.Error().Err(err).
				Str("event_id", event.ID.String()).
				Msg("failed to unmarshal event payload")
			return nil, fmt.Errorf("unmarshal event payload: %w", err)
		}

		events = append(events, &event)
	}

	if err := rows.Err(); err != nil {
		r.logger.Error().Err(err).Msg("rows error")
		return nil, fmt.Errorf("rows error: %w", err)
	}

	r.logger.Debug().
		Int("count", len(events)).
		Msg("retrieved unprocessed events")

	return events, nil
}

// MarkProcessed marks an event as successfully published
func (r *PostgresOutboxRepository) MarkProcessed(ctx context.Context, eventID uuid.UUID) error {
	query := `
		UPDATE outbox_events
		SET processed_at = NOW()
		WHERE id = $1
	`

	result, err := r.pool.Exec(ctx, query, eventID)
	if err != nil {
		r.logger.Error().Err(err).
			Str("event_id", eventID.String()).
			Msg("failed to mark event as processed")
		return fmt.Errorf("mark event as processed: %w", err)
	}

	if result.RowsAffected() == 0 {
		r.logger.Warn().
			Str("event_id", eventID.String()).
			Msg("event not found")
		return fmt.Errorf("event not found: %s", eventID.String())
	}

	r.logger.Debug().
		Str("event_id", eventID.String()).
		Msg("event marked as processed")

	return nil
}

// IncrementRetryCount increments the retry count for a failed publish
func (r *PostgresOutboxRepository) IncrementRetryCount(ctx context.Context, eventID uuid.UUID, errorMsg string) error {
	query := `
		UPDATE outbox_events
		SET retry_count = retry_count + 1, last_error = $2
		WHERE id = $1
	`

	result, err := r.pool.Exec(ctx, query, eventID, errorMsg)
	if err != nil {
		r.logger.Error().Err(err).
			Str("event_id", eventID.String()).
			Msg("failed to increment retry count")
		return fmt.Errorf("increment retry count: %w", err)
	}

	if result.RowsAffected() == 0 {
		r.logger.Warn().
			Str("event_id", eventID.String()).
			Msg("event not found")
		return fmt.Errorf("event not found: %s", eventID.String())
	}

	r.logger.Debug().
		Str("event_id", eventID.String()).
		Int("error_length", len(errorMsg)).
		Msg("retry count incremented")

	return nil
}

// CleanupProcessedEvents deletes old processed events to prevent table bloat
func (r *PostgresOutboxRepository) CleanupProcessedEvents(ctx context.Context, olderThan time.Duration) (int64, error) {
	query := `
		DELETE FROM outbox_events
		WHERE processed_at IS NOT NULL AND processed_at < NOW() - $1::interval
	`

	result, err := r.pool.Exec(ctx, query, olderThan.String())
	if err != nil {
		r.logger.Error().Err(err).
			Dur("older_than", olderThan).
			Msg("failed to cleanup processed events")
		return 0, fmt.Errorf("cleanup processed events: %w", err)
	}

	deletedCount := result.RowsAffected()
	if deletedCount > 0 {
		r.logger.Info().
			Int64("deleted_count", deletedCount).
			Dur("older_than", olderThan).
			Msg("cleaned up processed events")
	}

	return deletedCount, nil
}
