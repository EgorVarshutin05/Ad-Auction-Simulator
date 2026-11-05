//go:build integration

package storage

import (
	"ad-auction/internal/auction"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Подключаемся к тестируемому PostgreSQL.
func setupTestPostgres(t *testing.T) *Postgres {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://auction:auction_dev_password@127.0.0.1:5433/auction_db?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	db, err := NewPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatalf("PostgreSQL недоступен: %v", err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

// Создаём уникальный ID и регистрируем очистку записей.
func newTestAuctionID(t *testing.T, db *Postgres) string {
	t.Helper()

	id := fmt.Sprintf("test-%d", time.Now().UnixNano())

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()

		// Сначала удаляем дочерние записи.
		for _, query := range []string{
			"DELETE FROM outbox_events WHERE auction_id = $1",
			"DELETE FROM auction_results WHERE auction_id = $1",
			"DELETE FROM bids WHERE auction_id = $1",
			"DELETE FROM auctions WHERE id = $1",
		} {
			if _, err := db.Pool.Exec(ctx, query, id); err != nil {
				t.Errorf("Ошибка очистки %s: %v", id, err)
			}
		}
	})

	return id
}

// Возвращает количество строк для тестового аукциона.
func countRows(t *testing.T, db *Postgres, table string, auctionID string) int {
	t.Helper()

	// Имя таблицы выбираем только из фиксированного списка
	switch table {
	case "auctions":
		query := "SELECT COUNT(*) FROM auctions WHERE id = $1"

		var count int
		if err := db.Pool.QueryRow(
			context.Background(), query, auctionID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count

	case "bids", "auction_results", "outbox_events":
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE auction_id = $1", table)

		var count int
		if err := db.Pool.QueryRow(
			context.Background(), query, auctionID,
		).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count

	default:
		t.Fatalf("Неизвестная таблица: %s", table)
		return 0
	}
}

func testBidAndResult() (auction.Bid, auction.Result) {
	bid := auction.Bid{
		AdvertiserID: "A",
		Amount:       150,
		PredictedCTR: 0.05,
	}

	result := auction.Result{
		AdvertiserID: "A",
		Bid:          150,
		PredictedCTR: 0.05,
		EffectiveBid: 7.5,
		Price:        0,
		AuctionType:  "second-price",
	}

	return bid, result
}

// 1. Проверяем создание аукциона.
func TestPostgresCreateAuction(t *testing.T) {
	db := setupTestPostgres(t)
	id := newTestAuctionID(t, db)
	ctx := context.Background()

	err := db.CreateAuction(ctx, id, "second-price")
	if err != nil {
		t.Fatal(err)
	}

	if got := countRows(t, db, "auctions", id); got != 1 {
		t.Fatalf("Ожидали 1 аукцион, получили %d", got)
	}

	// Повторное создание не должно создавать дубликат.
	err = db.CreateAuction(ctx, id, "second-price")
	if err != nil {
		t.Fatal(err)
	}

	if got := countRows(t, db, "auctions", id); got != 1 {
		t.Fatalf("После повторного создания получили %d строк", got)
	}

}

// 2. Проверяем сохранение ставки и результата.
func TestPostgresSaveHistory(t *testing.T) {
	db := setupTestPostgres(t)
	id := newTestAuctionID(t, db)
	ctx := context.Background()

	bid, result := testBidAndResult()

	err := db.SaveHistory(
		ctx, id, "second-price", bid, result,
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{
		"auctions",
		"bids",
		"auction_results",
		"outbox_events",
	} {
		if got := countRows(t, db, table, id); got != 1 {
			t.Errorf("%s: ожидали 1 строку, получили %d", table, got)
		}
	}
}

// 3. Проверяем несовпадение типа аукциона.
func TestPostgresAuctionTypeMismatch(t *testing.T) {
	db := setupTestPostgres(t)
	id := newTestAuctionID(t, db)
	ctx := context.Background()

	err := db.CreateAuction(ctx, id, "first-price")
	if err != nil {
		t.Fatal(err)
	}

	bid, result := testBidAndResult()

	err = db.SaveHistory(
		ctx, id, "second-price", bid, result,
	)

	if !errors.Is(err, ErrAuctionTypeMismatch) {
		t.Fatalf(
			"Ожидали ErrAuctionTypeMismatch, получили %v",
			err,
		)
	}

	if got := countRows(t, db, "bids", id); got != 0 {
		t.Errorf("Ставка не должна была сохраниться: %d", got)
	}

	if got := countRows(t, db, "auction_results", id); got != 0 {
		t.Errorf("Результат не должен был сохраниться: %d", got)
	}
}

// 4. Проверяем запрет записи в закрытый аукцион.
func TestPostgresClosedAuction(t *testing.T) {
	db := setupTestPostgres(t)
	id := newTestAuctionID(t, db)
	ctx := context.Background()

	if err := db.CreateAuction(
		ctx, id, "second-price",
	); err != nil {
		t.Fatal(err)
	}

	_, err := db.Pool.Exec(ctx, `
		UPDATE auctions
		SET status = 'closed'
		WHERE id = $1
	`, id)
	if err != nil {
		t.Fatal(err)
	}

	bid, result := testBidAndResult()

	err = db.SaveHistory(
		ctx, id, "second-price", bid, result,
	)

	if !errors.Is(err, ErrAuctionClosed) {
		t.Fatalf(
			"Ожидали ErrAuctionClosed, получили %v",
			err,
		)
	}

	if got := countRows(t, db, "bids", id); got != 0 {
		t.Errorf("Закрытый аукцион принял ставку: %d", got)
	}

	if got := countRows(t, db, "auction_results", id); got != 0 {
		t.Errorf("Закрытый аукцион сохранил результат: %d", got)
	}
}

// 5. Проверяем откат при ошибке второго INSERT.
func TestPostgresRollback(t *testing.T) {
	db := setupTestPostgres(t)
	id := newTestAuctionID(t, db)
	ctx := context.Background()

	bid, result := testBidAndResult()

	// В таблице auction_results advertiser_id VARCHAR(100).
	// Значение длиной 101 символ вызовет ошибку SQL.
	result.AdvertiserID = strings.Repeat("X", 101)

	err := db.SaveHistory(
		ctx, id, "second-price", bid, result,
	)

	if err == nil {
		t.Fatal("Ожидали ошибку PostgreSQL, получили nil")
	}

	// INSERT в bids был выполнен до ошибочного INSERT.
	// Но после Rollback ставка не должна сохраниться.
	for _, table := range []string{
		"auctions",
		"bids",
		"auction_results",
		"outbox_events",
	} {
		if got := countRows(t, db, table, id); got != 0 {
			t.Errorf(
				"%s: после Rollback ожидали 0 строк, получили %d",
				table, got,
			)
		}
	}
}
