-- Create idempotency_keys table for idempotent operations
CREATE TABLE IF NOT EXISTS idempotency_keys (
    idempotency_key VARCHAR(255) PRIMARY KEY,
    request_hash    VARCHAR(64) NOT NULL,
    response_data   JSONB,
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMP NOT NULL
);

-- Create index for cleanup
CREATE INDEX idx_idempotency_expires ON idempotency_keys(expires_at);

-- Create function to cleanup expired keys
CREATE OR REPLACE FUNCTION cleanup_expired_idempotency_keys()
RETURNS void AS $$
BEGIN
    DELETE FROM idempotency_keys WHERE expires_at < NOW();
END;
$$ LANGUAGE plpgsql;

-- Add comments
COMMENT ON TABLE idempotency_keys IS 'Stores idempotency keys for ensuring operations are executed exactly once';
COMMENT ON COLUMN idempotency_keys.idempotency_key IS 'Client-provided unique key for the operation';
COMMENT ON COLUMN idempotency_keys.request_hash IS 'SHA256 hash of request parameters to detect conflicts';
COMMENT ON COLUMN idempotency_keys.response_data IS 'Cached response data';
COMMENT ON COLUMN idempotency_keys.expires_at IS 'When this idempotency key expires (default 24 hours)';
