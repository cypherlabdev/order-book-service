-- Drop indexes
DROP INDEX IF EXISTS idx_orders_version;
DROP INDEX IF EXISTS idx_orders_status;
DROP INDEX IF EXISTS idx_orders_saga_id;
DROP INDEX IF EXISTS idx_orders_event_id;
DROP INDEX IF EXISTS idx_orders_user_id;

-- Drop table
DROP TABLE IF EXISTS orders;
