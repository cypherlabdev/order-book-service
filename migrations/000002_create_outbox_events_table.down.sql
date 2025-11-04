-- Drop indexes
DROP INDEX IF EXISTS idx_outbox_saga_id;
DROP INDEX IF EXISTS idx_outbox_aggregate;
DROP INDEX IF EXISTS idx_outbox_unprocessed;

-- Drop table
DROP TABLE IF EXISTS outbox_events;
