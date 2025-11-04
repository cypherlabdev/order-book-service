-- Create outbox_events table for transactional outbox pattern
CREATE TABLE IF NOT EXISTS outbox_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    aggregate_id    UUID NOT NULL,
    aggregate_type  VARCHAR(50) NOT NULL,
    event_type      VARCHAR(100) NOT NULL,
    event_payload   JSONB NOT NULL,
    saga_id         UUID,
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    processed_at    TIMESTAMP,
    retry_count     INT DEFAULT 0,
    max_retries     INT DEFAULT 3
);

-- Create indexes for performance
CREATE INDEX idx_outbox_unprocessed ON outbox_events(created_at)
WHERE processed_at IS NULL;

CREATE INDEX idx_outbox_aggregate ON outbox_events(aggregate_id, aggregate_type);

CREATE INDEX idx_outbox_saga_id ON outbox_events(saga_id)
WHERE saga_id IS NOT NULL;

-- Add comments
COMMENT ON TABLE outbox_events IS 'Transactional outbox for atomic database writes and Kafka publishing';
COMMENT ON COLUMN outbox_events.aggregate_id IS 'ID of the aggregate that generated this event';
COMMENT ON COLUMN outbox_events.aggregate_type IS 'Type of aggregate (order, bet, etc)';
COMMENT ON COLUMN outbox_events.event_type IS 'Type of event (order.placed, order.settled, etc)';
COMMENT ON COLUMN outbox_events.event_payload IS 'JSON payload of the event';
COMMENT ON COLUMN outbox_events.saga_id IS 'Temporal workflow saga ID if part of distributed transaction';
COMMENT ON COLUMN outbox_events.processed_at IS 'Timestamp when event was published to Kafka';
COMMENT ON COLUMN outbox_events.retry_count IS 'Number of times publishing was retried';
