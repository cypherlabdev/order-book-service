package models

import (
	"time"

	"github.com/google/uuid"
)

// OutboxEvent represents an event to be published to Kafka via the transactional outbox pattern
type OutboxEvent struct {
	ID            uuid.UUID              `json:"id" db:"id"`
	AggregateID   uuid.UUID              `json:"aggregate_id" db:"aggregate_id"`
	AggregateType string                 `json:"aggregate_type" db:"aggregate_type"`
	EventType     string                 `json:"event_type" db:"event_type"`
	EventPayload  map[string]interface{} `json:"event_payload" db:"event_payload"`
	SagaID        *uuid.UUID             `json:"saga_id,omitempty" db:"saga_id"`
	CreatedAt     time.Time              `json:"created_at" db:"created_at"`
	ProcessedAt   *time.Time             `json:"processed_at,omitempty" db:"processed_at"`
	RetryCount    int                    `json:"retry_count" db:"retry_count"`
	MaxRetries    int                    `json:"max_retries" db:"max_retries"`
	LastError     *string                `json:"last_error,omitempty" db:"last_error"`
}

// IsProcessed returns true if the event has been successfully published
func (e *OutboxEvent) IsProcessed() bool {
	return e.ProcessedAt != nil
}

// CanRetry returns true if the event can be retried
func (e *OutboxEvent) CanRetry() bool {
	return e.RetryCount < e.MaxRetries
}

// AggregateType constants
const (
	AggregateTypeOrder = "order"
	AggregateTypeMatch = "match"
)

// EventType constants for order book service
const (
	EventTypeOrderPlaced     = "order.placed"
	EventTypeOrderMatched    = "order.matched"
	EventTypeOrderPartial    = "order.partially_matched"
	EventTypeOrderCancelled  = "order.cancelled"
	EventTypeOrderExpired    = "order.expired"
	EventTypeMatchCreated    = "match.created"
	EventTypeMatchSettled    = "match.settled"
	EventTypeOrderSettled    = "order.settled"
)
