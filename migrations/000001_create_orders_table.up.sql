-- Create orders table for bet management
CREATE TABLE IF NOT EXISTS orders (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL,
    event_id            VARCHAR(255) NOT NULL,
    bet_type            VARCHAR(50) NOT NULL,
    selection           VARCHAR(255) NOT NULL,
    amount              DECIMAL(20,8) NOT NULL,
    odds                DECIMAL(20,8) NOT NULL,
    reservation_id      UUID,
    saga_id             UUID,
    status              VARCHAR(50) NOT NULL,  -- 'active', 'settled_win', 'settled_loss', 'cancelled'
    potential_payout    DECIMAL(20,8),
    actual_payout       DECIMAL(20,8),
    created_at          TIMESTAMP NOT NULL DEFAULT NOW(),
    settled_at          TIMESTAMP,
    cancelled_at        TIMESTAMP,
    version             BIGINT NOT NULL DEFAULT 1
);

-- Create indexes for performance
CREATE INDEX idx_orders_user_id ON orders(user_id, created_at DESC);
CREATE INDEX idx_orders_event_id ON orders(event_id);
CREATE INDEX idx_orders_saga_id ON orders(saga_id) WHERE saga_id IS NOT NULL;
CREATE INDEX idx_orders_status ON orders(status);
CREATE INDEX idx_orders_version ON orders(id, version);

-- Add comments
COMMENT ON TABLE orders IS 'Stores all bet orders placed by users';
COMMENT ON COLUMN orders.id IS 'Unique identifier for the order';
COMMENT ON COLUMN orders.user_id IS 'Reference to user who placed the bet';
COMMENT ON COLUMN orders.event_id IS 'Reference to the sporting event';
COMMENT ON COLUMN orders.bet_type IS 'Type of bet (match_odds, over_under, handicap, etc)';
COMMENT ON COLUMN orders.selection IS 'Bet selection (home_win, over_2.5, etc)';
COMMENT ON COLUMN orders.amount IS 'Bet amount in decimal';
COMMENT ON COLUMN orders.odds IS 'Decimal odds at time of bet placement';
COMMENT ON COLUMN orders.reservation_id IS 'Reference to wallet reservation';
COMMENT ON COLUMN orders.saga_id IS 'Temporal workflow saga ID for distributed transaction';
COMMENT ON COLUMN orders.status IS 'Current order status';
COMMENT ON COLUMN orders.potential_payout IS 'Maximum payout if bet wins';
COMMENT ON COLUMN orders.actual_payout IS 'Actual payout after settlement';
COMMENT ON COLUMN orders.version IS 'Version number for optimistic locking';
