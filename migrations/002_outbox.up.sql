
CREATE TABLE IF NOT EXISTS outbox_events (
    id BIGSERIAL PRIMARY KEY,

    auction_id VARCHAR(64) NOT NULL
        REFERENCES auctions(id),

    event_type VARCHAR(100) NOT NULL,

    payload JSONB NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    published_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_outbox_pending
    ON outbox_events (id)
    WHERE published_at IS NULL;
