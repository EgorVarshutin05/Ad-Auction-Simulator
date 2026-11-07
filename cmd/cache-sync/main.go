package main

import (
	"ad-auction/internal/cache"
	"ad-auction/internal/storage"
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
)

func env(key, fallback string) string {
	if s := os.Getenv(key); s != "" {
		return s
	}
	return fallback
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pg, err := storage.NewPostgres(
		ctx,
		env(
			"DATABASE_URL",
			"postgres://auction:auction_dev_password@127.0.0.1:5433/auction_db?sslmode=disable",
		),
	)
	if err != nil {
		log.Fatalf("PostgreSQL: %v", err)
	}
	defer pg.Close()

	redisClient := redis.NewClient(&redis.Options{
		Addr: env("REDIS_ADDR", "localhost:6379"),
	})
	defer func() {
		if err := redisClient.Close(); err != nil {
			log.Printf("close Redis client: %v", err)
		}
	}()

	syncer := cache.Sync{
		DB:    pg.Pool,
		Redis: redisClient,
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	log.Println("Redis cache synchronizer started")

	for {
		cycleCtx, cancel := context.WithTimeout(ctx, 30*time.Second)

		updated, err := syncer.All(cycleCtx)
		cancel()

		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("cache synchronization error: %v", err)
		} else if updated > 0 {
			log.Printf("cache snapshots applied: %d", updated)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
