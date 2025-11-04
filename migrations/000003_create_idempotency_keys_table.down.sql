-- Drop function
DROP FUNCTION IF EXISTS cleanup_expired_idempotency_keys;

-- Drop index
DROP INDEX IF EXISTS idx_idempotency_expires;

-- Drop table
DROP TABLE IF EXISTS idempotency_keys;
