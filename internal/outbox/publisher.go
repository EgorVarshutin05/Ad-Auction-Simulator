package outbox

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Producer — интерфейс отправки событий.
type Producer interface {
	Publish(ctx context.Context, auctionID string, data []byte) error
}

type Publisher struct {
	DB       *pgxpool.Pool
	Producer Producer
}

// OutboxEvent — запись из PostgreSQL.
type OutboxEvent struct {
	ID        int64
	AuctionID string
	EventType string
	Payload   []byte
}

// PublishPending отправляет до 20 неопубликованных событий.
func (p *Publisher) PublishPending(ctx context.Context) (int, error) {
	rows, err := p.DB.Query(ctx, `
		SELECT id, auction_id, event_type, payload
		FROM outbox_events
		WHERE published_at IS NULL
		ORDER BY id
		LIMIT 20
	`)
	if err != nil {
		return 0, fmt.Errorf("read outbox: %w", err)
	}

	// Сначала читаем записи в память.
	events := make([]OutboxEvent, 0, 20)

	for rows.Next() {
		var event OutboxEvent

		if err := rows.Scan(
			&event.ID,
			&event.AuctionID,
			&event.EventType,
			&event.Payload,
		); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan outbox: %w", err)
		}

		events = append(events, event)
	}

	err = rows.Err()
	rows.Close()

	if err != nil {
		return 0, fmt.Errorf("iterate outbox: %w", err)
	}

	published := 0

	for _, event := range events {
		// Оборачиваем событие в сообщение с уникальным ID.
		message := struct {
			EventID   int64           `json:"event_id"`
			EventType string          `json:"event_type"`
			Payload   json.RawMessage `json:"payload"`
		}{
			EventID:   event.ID,
			EventType: event.EventType,
			Payload:   event.Payload,
		}

		data, err := json.Marshal(message)
		if err != nil {
			return published, fmt.Errorf("marshal event: %w", err)
		}

		// Сначала отправляем сообщение в Kafka.
		if err := p.Producer.Publish(
			ctx,
			event.AuctionID,
			data,
		); err != nil {
			return published, fmt.Errorf("publish event %d: %w", event.ID, err)
		}

		// Только после успешной отправки отмечаем событие.
		_, err = p.DB.Exec(ctx, `
			UPDATE outbox_events
			SET published_at = NOW()
			WHERE id = $1 AND published_at IS NULL
		`, event.ID)
		if err != nil {
			return published, fmt.Errorf("mark event %d: %w", event.ID, err)
		}

		published++
	}

	return published, nil
}
