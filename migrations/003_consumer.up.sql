
-- Какие события уже обработаны.
CREATE TABLE IF NOT EXISTS processed_events (
    event_id BIGINT PRIMARY KEY,
    auction_id VARCHAR(64) NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Количество обработанных ставок по аукционам.
CREATE TABLE IF NOT EXISTS kafka_auction_stats (
    auction_id VARCHAR(64) PRIMARY KEY,
    bid_count BIGINT NOT NULL DEFAULT 0
        CHECK (bid_count >= 0)
);

-- Количество побед рекламодателей.
CREATE TABLE IF NOT EXISTS kafka_winner_stats (
    auction_id VARCHAR(64) NOT NULL,
    advertiser_id VARCHAR(100) NOT NULL,
    wins BIGINT NOT NULL DEFAULT 0
        CHECK (wins >= 0),

    PRIMARY KEY (auction_id, advertiser_id)
);
