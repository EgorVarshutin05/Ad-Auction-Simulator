package storage

import (
	"ad-auction/internal/auction"
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

var testKeys = []string{
	"auction:bids",
	"auction:scores",
	"auction:winners",
	"auction:stats",
	"auction:wins",
}

// Создаём Redis-клиент для тестовой базы DB 15
func setupTestRedis(t *testing.T) *Store {
	t.Helper()

	address := os.Getenv("REDIS_ADDR")
	if address == "" {
		address = "localhost:6379"
	}

	client := redis.NewClient(&redis.Options{
		Addr: address,
		DB:   15,
	})

	ctx, cancel := context.WithTimeout(
		context.Background(),
		3*time.Second,
	)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		t.Fatalf("Redis недоступен: %v", err)
	}

	// Не трогаем базу, если тестовые ключи уже существуют
	count, err := client.Exists(ctx, testKeys...).Result()
	if err != nil {
		client.Close()
		t.Fatalf("Ошибка проверки Redis: %v", err)
	}
	if count != 0 {
		client.Close()
		t.Fatal("DB 15 содержит ключи аукциона; тесты не запущены")
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			3*time.Second,
		)
		defer cleanupCancel()

		if err := client.Del(cleanupCtx, testKeys...).Err(); err != nil {
			t.Errorf("Ошибка очистки Redis: %v", err)
		}

		if err := client.Close(); err != nil {
			t.Errorf("Ошибка закрытия Redis: %v", err)
		}
	})

	return &Store{Client: client}
}

func TestRedisIntegration(t *testing.T) {
	store := setupTestRedis(t)
	ctx := context.Background()

	// Проверяем пустой аукцион
	t.Run("EmptyAuction", func(t *testing.T) {
		bids, err := store.GetTopBids(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if len(bids) != 0 {
			t.Fatalf("Ожидали 0 ставок, получили %d", len(bids))
		}
	})

	// Проверяем сохранение ставки
	t.Run("SaveBid", func(t *testing.T) {
		bid := auction.Bid{
			AdvertiserID: "A",
			Amount:       150,
			PredictedCTR: 0.05,
		}

		if err := store.SaveBid(ctx, bid); err != nil {
			t.Fatal(err)
		}

		bids, err := store.GetTopBids(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if len(bids) != 1 || bids[0] != bid {
			t.Fatalf("Ожидали %+v, получили %+v", bid, bids)
		}
	})

	// Проверяем определение двух лидеров
	t.Run("TopTwoBids", func(t *testing.T) {
		otherBids := []auction.Bid{
			{AdvertiserID: "B", Amount: 100, PredictedCTR: 0.10},
			{AdvertiserID: "C", Amount: 200, PredictedCTR: 0.02},
		}

		for _, bid := range otherBids {
			if err := store.SaveBid(ctx, bid); err != nil {
				t.Fatal(err)
			}
		}

		bids, err := store.GetTopBids(ctx)
		if err != nil {
			t.Fatal(err)
		}

		expected := []string{"B", "A"}

		if len(bids) != 2 {
			t.Fatalf("Ожидали 2 ставки, получили %d", len(bids))
		}

		actual := []string{
			bids[0].AdvertiserID,
			bids[1].AdvertiserID,
		}

		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("Ожидали %v, получили %v", expected, actual)
		}
	})

	// Проверяем обновление существующей ставки
	t.Run("UpdateBid", func(t *testing.T) {
		updated := auction.Bid{
			AdvertiserID: "A",
			Amount:       300,
			PredictedCTR: 0.05,
		}

		if err := store.SaveBid(ctx, updated); err != nil {
			t.Fatal(err)
		}

		bids, err := store.GetTopBids(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if len(bids) != 2 || bids[0] != updated {
			t.Fatalf("Обновление ставки не сработало: %+v", bids)
		}

		// Проверяем, что A не дублируется
		count, err := store.Client.ZCard(ctx, "auction:scores").Result()
		if err != nil {
			t.Fatal(err)
		}

		if count != 3 {
			t.Fatalf("Ожидали 3 уникальных участников, получили %d", count)
		}
	})

	// Проверяем равенство эффективных ставок
	t.Run("EqualScores", func(t *testing.T) {
		bid := auction.Bid{
			AdvertiserID: "B",
			Amount:       150,
			PredictedCTR: 0.10,
		}

		if err := store.SaveBid(ctx, bid); err != nil {
			t.Fatal(err)
		}

		bids, err := store.GetTopBids(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if len(bids) != 2 {
			t.Fatalf("Ожидали 2 ставки, получили %d", len(bids))
		}

		// A и B имеют score=15
		// При равенстве первым должен быть A
		if bids[0].AdvertiserID != "A" ||
			bids[1].AdvertiserID != "B" {
			t.Fatalf("Неправильный порядок: %+v", bids)
		}
	})

	// Проверяем сохранение и чтение статистики
	t.Run("Stats", func(t *testing.T) {
		bid := auction.Bid{
			AdvertiserID: "A",
			Amount:       300,
			PredictedCTR: 0.05,
		}

		if err := store.AddBidStats(ctx, bid); err != nil {
			t.Fatal(err)
		}

		if err := store.AddLeaderStats(ctx, "A"); err != nil {
			t.Fatal(err)
		}

		data, err := store.Stats(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if data["total_bids"] != int64(1) {
			t.Fatalf("Неправильное число ставок: %v", data)
		}

		leaders, ok := data["times_leading_after_bid"].(map[string]string)
		if !ok {
			t.Fatalf("Неправильный формат статистики: %v", data)
		}

		if leaders["A"] != "1" {
			t.Fatalf("Неправильная статистика A: %v", leaders)
		}
	})
}
