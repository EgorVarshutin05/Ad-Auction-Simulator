package storage

import (
	"ad-auction/internal/auction"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrAuctionClosed       = errors.New("auction is closed")
	ErrAuctionTypeMismatch = errors.New("auction type mismatch")
)

type Postgres struct {
	Pool *pgxpool.Pool
}

// NewPostgres создаёт пул соединений с PostgreSQL.
func NewPostgres(
	ctx context.Context,
	databaseURL string,
) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}

	return &Postgres{Pool: pool}, nil
}

func (p *Postgres) Close() {
	p.Pool.Close()
}

// CreateAuction создаёт аукцион, если он ещё не существует.
func (p *Postgres) CreateAuction(
	ctx context.Context,
	auctionID string,
	auctionType string,
) error {
	_, err := p.Pool.Exec(ctx, `
		INSERT INTO auctions (id, auction_type)
		VALUES ($1, $2)
		ON CONFLICT (id) DO NOTHING
	`, auctionID, auctionType)

	if err != nil {
		return fmt.Errorf("create auction: %w", err)
	}

	return nil
}

func (p *Postgres) GetAuctionState(
	ctx context.Context,
	auctionID string,
) (auctionType string, status string, exists bool, err error) {

	err = p.Pool.QueryRow(ctx, `
		SELECT auction_type, status
		FROM auctions
		WHERE id = $1
	`, auctionID).Scan(&auctionType, &status)

	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}

	if err != nil {
		return "", "", false, fmt.Errorf(
			"get auction state: %w", err,
		)
	}

	return auctionType, status, true, nil
}

// SaveHistory сохраняет ставку и результат
// в одной транзакции PostgreSQL.
func (p *Postgres) SaveHistory(ctx context.Context, auctionID string, auctionType string, bid auction.Bid, result auction.Result) error {
	tx, err := p.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Создаём аукцион, если он ещё не существует.
	_, err = tx.Exec(ctx, `
		INSERT INTO auctions (id, auction_type)
		VALUES ($1, $2)
		ON CONFLICT (id) DO NOTHING
	`, auctionID, auctionType)
	if err != nil {
		return fmt.Errorf("create auction: %w", err)
	}

	// Проверяем тип и статус существующего аукциона.
	var savedType string
	var status string

	err = tx.QueryRow(ctx, `
		SELECT auction_type, status
		FROM auctions
		WHERE id = $1
		FOR UPDATE
	`, auctionID).Scan(&savedType, &status)
	if err != nil {
		return fmt.Errorf("get auction: %w", err)
	}

	if savedType != auctionType {
		return ErrAuctionTypeMismatch
	}

	if status != "active" {
		return ErrAuctionClosed
	}

	// Сохраняем историю поступившей ставки.
	_, err = tx.Exec(ctx, `
		INSERT INTO bids (
			auction_id,
			advertiser_id,
			amount,
			predicted_ctr
		)
		VALUES ($1, $2, $3, $4)
	`,
		auctionID,
		bid.AdvertiserID,
		bid.Amount,
		bid.PredictedCTR,
	)
	if err != nil {
		return fmt.Errorf("insert bid: %w", err)
	}

	// Сохраняем результат после этой ставки.
	_, err = tx.Exec(ctx, `
		INSERT INTO auction_results (
			auction_id,
			advertiser_id,
			bid,
			predicted_ctr,
			effective_bid,
			price,
			auction_type
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`,
		auctionID,
		result.AdvertiserID,
		result.Bid,
		result.PredictedCTR,
		result.EffectiveBid,
		result.Price,
		result.AuctionType,
	)
	if err != nil {
		return fmt.Errorf("insert auction result: %w", err)
	}

	// Формируем событие о размещённой ставке.
	event := struct {
		AuctionID   string      `json:"auction_id"`
		Bid         auction.Bid `json:"bid"`
		WinnerID    string      `json:"winner_id"`
		AuctionType string      `json:"auction_type"`
	}{
		AuctionID:   auctionID,
		Bid:         bid,
		WinnerID:    result.AdvertiserID,
		AuctionType: auctionType,
	}

	// Преобразуем событие в JSON.
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal outbox event: %w", err)
	}

	// Сохраняем событие в той же SQL-транзакции.
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (
			auction_id,
			event_type,
			payload
		)
		VALUES ($1, $2, $3::jsonb)
	`,
		auctionID,
		"bid_placed",
		string(payload),
	)
	if err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}

	// Подтверждаем все изменения транзакции.
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func (p *Postgres) StatsForAuction(
	ctx context.Context,
	auctionID string,
) (map[string]interface{}, error) {

	var totalBids int64

	// Получаем количество обработанных Kafka-событий.
	err := p.Pool.QueryRow(ctx, `
		SELECT COALESCE(
			(
				SELECT bid_count
				FROM kafka_auction_stats
				WHERE auction_id = $1
			),
			0
		)
	`, auctionID).Scan(&totalBids)

	if err != nil {
		return nil, fmt.Errorf(
			"get auction stats: %w", err,
		)
	}

	// Получаем счётчики текущего лидерства.
	rows, err := p.Pool.Query(ctx, `
		SELECT advertiser_id, wins
		FROM kafka_winner_stats
		WHERE auction_id = $1
	`, auctionID)

	if err != nil {
		return nil, fmt.Errorf(
			"get winner stats: %w", err,
		)
	}
	defer rows.Close()

	wins := make(map[string]int64)

	for rows.Next() {
		var advertiserID string
		var count int64

		if err := rows.Scan(
			&advertiserID,
			&count,
		); err != nil {
			return nil, fmt.Errorf(
				"scan winner stats: %w", err,
			)
		}

		wins[advertiserID] = count
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"iterate winner stats: %w", err,
		)
	}

	return map[string]interface{}{
		"total_bids":              totalBids,
		"times_leading_after_bid": wins,
		"wins_by_advertiser":      wins,
	}, nil
}
