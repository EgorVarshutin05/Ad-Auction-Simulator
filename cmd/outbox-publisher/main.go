package main

import (
	"ad-auction/internal/kafka"
	"ad-auction/internal/outbox"
	"ad-auction/internal/storage"
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// Контекст отменяется при Ctrl+C или SIGTERM.
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://auction:auction_dev_password@127.0.0.1:5433/auction_db?sslmode=disable"
	}

	broker := os.Getenv("KAFKA_BROKER")
	if broker == "" {
		broker = "localhost:9092"
	}

	// Подключаем PostgreSQL.
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	db, err := storage.NewPostgres(dbCtx, databaseURL)
	cancel()

	if err != nil {
		log.Printf("PostgreSQL connection failed: %v", err)
		return
	}
	defer db.Close()

	// Создаём Kafka Producer.
	producer := kafka.NewProducer(broker)
	defer func() {
		if err := producer.Close(); err != nil {
			log.Printf("Kafka producer close failed: %v", err)
		}
	}()

	publisher := &outbox.Publisher{
		DB:       db.Pool,
		Producer: producer,
	}

	// Проверяем Outbox раз в секунду.
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	log.Println("Outbox Publisher started")

	// Выполняем проверку сразу после запуска,
	// не ожидая первого срабатывания таймера.
	publish := func() {
		publishCtx, cancel := context.WithTimeout(
			ctx,
			15*time.Second,
		)
		defer cancel()

		count, err := publisher.PublishPending(publishCtx)

		if err != nil {
			if !errors.Is(err, context.Canceled) {
				log.Printf("Outbox publish error: %v", err)
			}
			return
		}

		// Не засоряем логи, если событий нет.
		if count > 0 {
			log.Printf("Published %d events", count)
		}
	}

	publish()

	for {
		select {
		case <-ctx.Done():
			log.Println("Outbox Publisher stopped")
			return

		case <-ticker.C:
			publish()
		}
	}
}
