package kafka

import (
	"context"

	kafkago "github.com/segmentio/kafka-go"
)

const TopicBidPlaced = "auction.bid-placed"

// Producer отправляет события в Kafka
type Producer struct {
	writer *kafkago.Writer
}

// NewProducer создаёт Kafka Producer
func NewProducer(broker string) *Producer {
	writer := &kafkago.Writer{
		Addr:         kafkago.TCP(broker),
		Topic:        TopicBidPlaced,
		Balancer:     &kafkago.Hash{},
		RequiredAcks: kafkago.RequireAll,
	}

	return &Producer{writer: writer}
}

// Publish отправляет одно сообщение
func (p *Producer) Publish(
	ctx context.Context,
	auctionID string,
	data []byte,
) error {
	return p.writer.WriteMessages(ctx, kafkago.Message{
		Key:   []byte(auctionID),
		Value: data,
	})
}

// Close закрывает Producer
func (p *Producer) Close() error {
	return p.writer.Close()
}
