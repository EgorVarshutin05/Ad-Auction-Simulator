package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Event — сообщение, опубликованное Outbox Publisher.
type Event struct {
	EventID   int64           `json:"event_id"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
}

// BidPlacedPayload — данные события о ставке.
type BidPlacedPayload struct {
	AuctionID string `json:"auction_id"`
	WinnerID  string `json:"winner_id"`
}

// Handler обрабатывает события и обновляет PostgreSQL.
type Handler struct {
	DB *pgxpool.Pool
}

// Handle возвращает true, если событие обработано впервые.
// Если событие уже было обработано, возвращает false, nil.
func (h *Handler) Handle(
	ctx context.Context,
	data []byte,
) (bool, error) {
	var event Event

	if err := json.Unmarshal(data, &event); err != nil {
		return false, fmt.Errorf("decode event: %w", err)
	}

	if event.EventID <= 0 {
		return false, errors.New("invalid event_id")
	}

	if event.EventType != "bid_placed" {
		return false, fmt.Errorf(
			"unsupported event type: %s",
			event.EventType,
		)
	}

	var payload BidPlacedPayload

	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return false, fmt.Errorf("decode payload: %w", err)
	}

	if payload.AuctionID == "" || payload.WinnerID == "" {
		return false, errors.New("invalid bid_placed payload")
	}

	// Все изменения выполняем в одной SQL-транзакции.
	tx, err := h.DB.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Регистрируем event_id.
	// Если он уже существует, повторно ничего не считаем.
	var insertedID int64

	err = tx.QueryRow(ctx, `
		INSERT INTO processed_events (
			event_id,
			auction_id
		)
		VALUES ($1, $2)
		ON CONFLICT (event_id) DO NOTHING
		RETURNING event_id
	`, event.EventID, payload.AuctionID).Scan(&insertedID)

	if errors.Is(err, pgx.ErrNoRows) {
		// Такое событие уже обработано.
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("register event: %w", err)
	}

	// Увеличиваем количество обработанных ставок.
	_, err = tx.Exec(ctx, `
		INSERT INTO kafka_auction_stats (
			auction_id,
			bid_count
		)
		VALUES ($1, 1)
		ON CONFLICT (auction_id)
		DO UPDATE SET
			bid_count = kafka_auction_stats.bid_count + 1
	`, payload.AuctionID)

	if err != nil {
		return false, fmt.Errorf("update auction stats: %w", err)
	}

	// Увеличиваем счётчик побед текущего лидера.
	_, err = tx.Exec(ctx, `
		INSERT INTO kafka_winner_stats (
			auction_id,
			advertiser_id,
			wins
		)
		VALUES ($1, $2, 1)
		ON CONFLICT (auction_id, advertiser_id)
		DO UPDATE SET
			wins = kafka_winner_stats.wins + 1
	`, payload.AuctionID, payload.WinnerID)

	if err != nil {
		return false, fmt.Errorf("update winner stats: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit transaction: %w", err)
	}

	return true, nil
}
