//go:build integration

package cache

import (
	"ad-auction/internal/auction"
	"ad-auction/internal/storage"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRestoreSnapshotAndRejectStaleVersion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://auction:auction_dev_password@127.0.0.1:5433/auction_db?sslmode=disable"
	}

	pg, err := storage.NewPostgres(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pg.Close)

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	// Отдельная тестовая база Redis
	client := redis.NewClient(&redis.Options{
		Addr: redisAddr,
		DB:   15,
	})
	t.Cleanup(func() { _ = client.Close() })

	id := fmt.Sprintf("cache-test-%d", time.Now().UnixNano())

	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()

		for _, suffix := range []string{
			"bids", "scores", "winners", "cache_version",
		} {
			if err := client.Del(cleanup, key(id, suffix)).Err(); err != nil {
				t.Errorf("redis cleanup: %v", err)
			}
		}

		for _, table := range []string{
			"outbox_events",
			"auction_results",
			"bids",
		} {
			_, err := pg.Pool.Exec(cleanup, "DELETE FROM "+table+" WHERE auction_id=$1", id)
			if err != nil {
				t.Errorf("postgres cleanup: %v", err)
			}
		}

		_, err := pg.Pool.Exec(cleanup, "DELETE FROM auctions WHERE id=$1", id)
		if err != nil {
			t.Errorf("auction cleanup: %v", err)
		}
	})

	first := auction.Bid{
		AdvertiserID: "A",
		Amount:       100,
		PredictedCTR: 0.1,
	}

	firstResult := auction.Result{
		AdvertiserID: "A",
		Bid:          100,
		PredictedCTR: 0.1,
		EffectiveBid: 10,
		Price:        0,
		AuctionType:  "second-price",
	}

	if err := pg.SaveHistory(ctx, id, "second-price", first, firstResult); err != nil {
		t.Fatal(err)
	}

	second := auction.Bid{
		AdvertiserID: "B",
		Amount:       200,
		PredictedCTR: 0.1,
	}

	secondResult := auction.Result{
		AdvertiserID: "B",
		Bid:          200,
		PredictedCTR: 0.1,
		EffectiveBid: 20,
		Price:        100,
		AuctionType:  "second-price",
	}

	if err := pg.SaveHistory(ctx, id, "second-price", second, secondResult); err != nil {
		t.Fatal(err)
	}

	syncer := &Sync{
		DB:    pg.Pool,
		Redis: client,
	}

	updated, err := syncer.Auction(ctx, id)
	if err != nil || !updated {
		t.Fatalf("sync: updated=%v err=%v", updated, err)
	}

	currentVersion, err := client.Get(ctx, key(id, "cache_version")).Int64()

	if err != nil {
		t.Fatal(err)
	}

	// Проверяем, что обе ставки восстановлены
	n, err := client.HLen(ctx, key(id, "bids")).Result()

	if err != nil || n != 2 {
		t.Fatalf("bids count=%d err=%v", n, err)
	}

	// Проверяем актуального победителя
	latest, err := client.LIndex(ctx, key(id, "winners"), 0).Result()

	if err != nil || !strings.Contains(latest, `"advertiser_id":"B"`) {
		t.Fatalf("winner: %v", err)
	}

	// Синхронизатор сохраняет текущий результат, а не всю историю победителей
	length, err := client.LLen(ctx, key(id, "winners")).Result()

	if err != nil || length != 1 {
		t.Fatalf("winner length=%d err=%v", length, err)
	}

	// Пытаемся применить более старую версию
	oldVersion := strconv.FormatInt(currentVersion-1, 10)

	applied, err := applySnapshot.Run(ctx, client,
		[]string{
			key(id, "bids"),
			key(id, "scores"),
			key(id, "winners"),
			key(id, "cache_version"),
		}, oldVersion, `{}`, 0).Int()

	if err != nil || applied != 0 {
		t.Fatalf("stale snapshot: applied=%d err=%v", applied, err)
	}

	// Данные не должны измениться
	n, err = client.HLen(ctx, key(id, "bids")).Result()

	if err != nil || n != 2 {
		t.Fatalf(
			"stale changed cache: %d, %v",
			n, err,
		)
	}
}
