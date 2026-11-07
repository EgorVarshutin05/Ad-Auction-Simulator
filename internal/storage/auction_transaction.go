package storage

import (
	"ad-auction/internal/auction"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// PlaceBidTransaction принимает ставку, рассчитывает победителя
// и сохраняет результат вместе с Outbox в одной транзакции
func (p *Postgres) PlaceBidTransaction(ctx context.Context, auctionID string, auctionType string, bid auction.Bid) (auction.Result, error) {

	tx, err := p.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return auction.Result{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// Создаем аукцион, если его еще нет
	_, err = tx.Exec(ctx, `
		INSERT INTO auctions (id, auction_type)
		VALUES ($1, $2)
		ON CONFLICT (id) DO NOTHING
	`, auctionID, auctionType)
	if err != nil {
		return auction.Result{}, fmt.Errorf("create auction: %w", err)
	}

	// Блокируем строку аукциона до завершения транзакции
	var savedType, status string

	err = tx.QueryRow(ctx, `
		SELECT auction_type, status
		FROM auctions
		WHERE id = $1
		FOR UPDATE
	`, auctionID).Scan(&savedType, &status)

	if err != nil {
		return auction.Result{}, fmt.Errorf("lock auction: %w", err)
	}

	if savedType != auctionType {
		return auction.Result{}, ErrAuctionTypeMismatch
	}

	if status != "active" {
		return auction.Result{}, ErrAuctionClosed
	}

	// Записываем новую ставку
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
		return auction.Result{}, fmt.Errorf("insert bid: %w", err)
	}

	// Получаем последние ставки каждого рекламодателя. Если A ставил несколько раз, участвует только его
	// последняя ставка — так же, как раньше в Redis Hash
	rows, err := tx.Query(ctx, `
		SELECT advertiser_id, amount, predicted_ctr
		FROM (
			SELECT DISTINCT ON (advertiser_id)
				advertiser_id,
				amount,
				predicted_ctr
			FROM bids
			WHERE auction_id = $1
			ORDER BY advertiser_id, id DESC
		) AS latest_bids
	`, auctionID)

	if err != nil {
		return auction.Result{}, fmt.Errorf("get latest bids: %w", err)
	}

	bids := make([]auction.Bid, 0)

	for rows.Next() {
		var current auction.Bid

		if err := rows.Scan(
			&current.AdvertiserID,
			&current.Amount,
			&current.PredictedCTR,
		); err != nil {
			rows.Close()
			return auction.Result{}, fmt.Errorf("scan bid: %w", err)
		}

		bids = append(bids, current)
	}

	err = rows.Err()
	rows.Close()

	if err != nil {
		return auction.Result{}, fmt.Errorf("iterate bids: %w", err)
	}

	// Используем существующий алгоритм аукциона
	result, err := auction.Calculate(bids, auctionType)
	if err != nil {
		return auction.Result{}, fmt.Errorf("calculate auction: %w", err)
	}

	// Сохраняем результат аукциона
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
		return auction.Result{}, fmt.Errorf("insert result: %w", err)
	}

	// Формируем событие для кафки
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

	payload, err := json.Marshal(event)
	if err != nil {
		return auction.Result{}, fmt.Errorf("marshal event: %w", err)
	}

	// Сохраняем Outbox-событие в той же транзакции
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
		return auction.Result{}, fmt.Errorf("insert outbox event: %w", err)
	}

	// Подтверждаем все изменения одновременно
	if err := tx.Commit(ctx); err != nil {
		return auction.Result{}, fmt.Errorf("commit transaction: %w", err)
	}

	return result, nil
}

// CurrentWinnerForAuction возвращает последний сохраненный результат из PostgreSQL
func (p *Postgres) CurrentWinnerForAuction(ctx context.Context, auctionID string) (auction.Result, error) {

	var result auction.Result

	err := p.Pool.QueryRow(ctx, `
		SELECT
			advertiser_id,
			bid,
			predicted_ctr,
			effective_bid,
			price,
			auction_type
		FROM auction_results
		WHERE auction_id = $1
		ORDER BY id DESC
		LIMIT 1
	`, auctionID).Scan(
		&result.AdvertiserID,
		&result.Bid,
		&result.PredictedCTR,
		&result.EffectiveBid,
		&result.Price,
		&result.AuctionType,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return auction.Result{}, pgx.ErrNoRows
	}

	if err != nil {
		return auction.Result{}, fmt.Errorf("get current winner: %w", err)
	}

	return result, nil
}
