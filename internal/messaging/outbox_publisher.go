package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/IBM/sarama"
	"github.com/rs/zerolog"
	"github.com/cypherlabdev/order-book-service/internal/models"
	"github.com/cypherlabdev/order-book-service/internal/repository"
)

// OutboxPublisher polls the outbox table and publishes events to Kafka
type OutboxPublisher struct {
	outboxRepo     repository.OutboxRepository
	kafkaProducer  sarama.SyncProducer
	logger         zerolog.Logger
	pollInterval   time.Duration
	batchSize      int
	topicMap       map[string]string // event_type -> Kafka topic
}

// NewOutboxPublisher creates a new outbox publisher
func NewOutboxPublisher(
	outboxRepo repository.OutboxRepository,
	kafkaProducer sarama.SyncProducer,
	logger zerolog.Logger,
) *OutboxPublisher {
	return &OutboxPublisher{
		outboxRepo:    outboxRepo,
		kafkaProducer: kafkaProducer,
		logger:        logger.With().Str("component", "outbox_publisher").Logger(),
		pollInterval:  100 * time.Millisecond,
		batchSize:     100,
		topicMap: map[string]string{
			"order.placed":  "order.events",
			"order.matched": "order.events",
			"order.settled": "order.settlements",
			"order.cancelled": "order.events",
		},
	}
}

// Start begins polling for outbox events
func (p *OutboxPublisher) Start(ctx context.Context) {
	p.logger.Info().Msg("outbox publisher started")
	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.publishPending(ctx)
		case <-ctx.Done():
			p.logger.Info().Msg("outbox publisher stopping")
			return
		}
	}
}

// publishPending retrieves and publishes unprocessed events
func (p *OutboxPublisher) publishPending(ctx context.Context) {
	events, err := p.outboxRepo.GetUnprocessedEvents(ctx, p.batchSize)
	if err != nil {
		p.logger.Error().Err(err).Msg("failed to get unprocessed events")
		return
	}

	if len(events) == 0 {
		return
	}

	for _, event := range events {
		publishErr := p.publishEvent(ctx, event)
		if publishErr != nil {
			p.logger.Error().
				Err(publishErr).
				Str("event_id", event.ID.String()).
				Str("event_type", event.EventType).
				Msg("failed to publish event")

			// Increment retry count
			if err := p.outboxRepo.IncrementRetryCount(ctx, event.ID, publishErr.Error()); err != nil {
				p.logger.Error().Err(err).Msg("failed to increment retry count")
			}
		} else {
			// Mark as processed
			if err := p.outboxRepo.MarkProcessed(ctx, event.ID); err != nil {
				p.logger.Error().Err(err).Msg("failed to mark event as processed")
			}
		}
	}
}

// publishEvent publishes a single event to Kafka
func (p *OutboxPublisher) publishEvent(ctx context.Context, event *models.OutboxEvent) error {
	// Get Kafka topic for this event type
	topic, ok := p.topicMap[event.EventType]
	if !ok {
		topic = "order.events" // Default topic
	}

	// Create Kafka message
	payload, err := json.Marshal(event.EventPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal event payload: %w", err)
	}

	msg := &sarama.ProducerMessage{
		Topic: topic,
		Key:   sarama.StringEncoder(event.AggregateID.String()),
		Value: sarama.ByteEncoder(payload),
		Headers: []sarama.RecordHeader{
			{Key: []byte("event_type"), Value: []byte(event.EventType)},
			{Key: []byte("aggregate_type"), Value: []byte(event.AggregateType)},
		},
	}

	if event.SagaID != nil {
		msg.Headers = append(msg.Headers, sarama.RecordHeader{
			Key:   []byte("saga_id"),
			Value: []byte(event.SagaID.String()),
		})
	}

	// Send to Kafka
	partition, offset, err := p.kafkaProducer.SendMessage(msg)
	if err != nil {
		return fmt.Errorf("failed to send to Kafka: %w", err)
	}

	p.logger.Debug().
		Str("event_type", event.EventType).
		Str("topic", topic).
		Int32("partition", partition).
		Int64("offset", offset).
		Msg("published event to Kafka")

	return nil
}
