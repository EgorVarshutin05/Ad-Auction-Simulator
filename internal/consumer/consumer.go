package consumer

import (
	"context"
	"fmt"
	"log"

	kafkago "github.com/segmentio/kafka-go"
)

const (
	TopicBidPlaced = "auction.bid-placed"
	ConsumerGroup  = "auction-statistics-v1"
)

// Consumer читает сообщения из Kafka.
type Consumer struct {
	reader  *kafkago.Reader
	handler *Handler
}

// NewConsumer создаёт Kafka Reader.
func NewConsumer(
	broker string,
	handler *Handler,
) *Consumer {
	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers: []string{broker},
		Topic:   TopicBidPlaced,
		GroupID: ConsumerGroup,

		MinBytes: 1,
		MaxBytes: 10e6,

		// Подтверждаем offset синхронно.
		CommitInterval: 0,

		// Для новой группы начинаем с новых сообщений.
		StartOffset: kafkago.FirstOffset,
	})

	return &Consumer{
		reader:  reader,
		handler: handler,
	}
}

// Run непрерывно читает и обрабатывает сообщения.
func (c *Consumer) Run(ctx context.Context) error {
	log.Printf(
		"Kafka consumer started: topic=%s group=%s",
		TopicBidPlaced,
		ConsumerGroup,
	)

	for {
		// Получаем сообщение, пока не подтверждая offset.
		message, err := c.reader.FetchMessage(ctx)
		if err != nil {
			return fmt.Errorf("fetch message: %w", err)
		}

		// Обновляем статистику в PostgreSQL.
		processed, err := c.handler.Handle(ctx, message.Value)
		if err != nil {
			return fmt.Errorf(
				"process message partition=%d offset=%d: %w",
				message.Partition,
				message.Offset,
				err,
			)
		}

		// Только после успешной обработки подтверждаем offset.
		if err := c.reader.CommitMessages(ctx, message); err != nil {
			return fmt.Errorf("commit offset: %w", err)
		}

		if processed {
			log.Printf(
				"event processed: partition=%d offset=%d",
				message.Partition,
				message.Offset,
			)
		} else {
			log.Printf(
				"duplicate event skipped: partition=%d offset=%d",
				message.Partition,
				message.Offset,
			)
		}
	}
}

// Close освобождает ресурсы Kafka Reader.
func (c *Consumer) Close() error {
	return c.reader.Close()
}
