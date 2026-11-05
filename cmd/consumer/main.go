package main

import (
	"ad-auction/internal/consumer"
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
	// Контекст отменяется при Ctrl+C.
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

	// Подключаемся к PostgreSQL.
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	db, err := storage.NewPostgres(dbCtx, databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Обработчик отвечает за PostgreSQL.
	handler := &consumer.Handler{
		DB: db.Pool,
	}

	// Consumer отвечает за Kafka.
	c := consumer.NewConsumer(broker, handler)
	defer c.Close()

	// Запускаем обработку сообщений.
	if err := c.Run(ctx); err != nil &&
		!errors.Is(err, context.Canceled) {
		log.Fatalf("consumer stopped: %v", err)
	}

	log.Println("consumer stopped")
}
