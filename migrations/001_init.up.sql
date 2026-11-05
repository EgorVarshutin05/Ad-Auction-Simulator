
-- Аукционы
CREATE TABLE IF NOT EXISTS auctions (
    id VARCHAR(64) PRIMARY KEY,
    auction_type VARCHAR(20) NOT NULL CHECK (auction_type IN ('first-price', 'second-price')),
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'closed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- История ставок
CREATE TABLE IF NOT EXISTS bids (
    id BIGSERIAL PRIMARY KEY,
    auction_id VARCHAR(64) NOT NULL REFERENCES auctions(id),
    advertiser_id VARCHAR(100) NOT NULL,
    amount NUMERIC NOT NULL CHECK (amount > 0),
    predicted_ctr DOUBLE PRECISION NOT NULL CHECK (predicted_ctr > 0 AND predicted_ctr <= 1),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- История результатов аукциона
CREATE TABLE IF NOT EXISTS auction_results (
    id BIGSERIAL PRIMARY KEY,
    auction_id VARCHAR(64) NOT NULL REFERENCES auctions(id),
    advertiser_id VARCHAR(100) NOT NULL,
    bid NUMERIC NOT NULL,
    predicted_ctr DOUBLE PRECISION NOT NULL,
    effective_bid DOUBLE PRECISION NOT NULL,
    price NUMERIC NOT NULL,
    auction_type VARCHAR(20) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Индексы для быстрого поиска истории
CREATE INDEX IF NOT EXISTS idx_bids_auction_created ON bids (auction_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_results_auction_created ON auction_results (auction_id, created_at DESC);
