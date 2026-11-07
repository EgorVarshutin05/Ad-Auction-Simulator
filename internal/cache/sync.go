package cache

import (
	"ad-auction/internal/auction"
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Sync переносит подтвержденное состояние PostgreSQL в Redis.
// HTTP-обработчики не должны напрямую менять эти ключи.
type Sync struct {
	DB    *pgxpool.Pool
	Redis *redis.Client
}

// latestSnapshot получает согласованный снимок одного аукциона.
func (s *Sync) latestSnapshot(ctx context.Context, id string) ([]auction.Bid, auction.Result, int64, error) {

	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return nil, auction.Result{}, 0,
			fmt.Errorf("begin snapshot: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// Последняя ставка каждого рекламодателя.
	rows, err := tx.Query(ctx, `
		SELECT id, advertiser_id, amount, predicted_ctr
		FROM (
			SELECT DISTINCT ON (advertiser_id)
				id, advertiser_id, amount, predicted_ctr
			FROM bids
			WHERE auction_id = $1
			ORDER BY advertiser_id, id DESC
		) AS latest
		ORDER BY advertiser_id
	`, id)

	if err != nil {
		return nil, auction.Result{}, 0,
			fmt.Errorf("read latest bids: %w", err)
	}

	bids := make([]auction.Bid, 0)
	var revision int64

	for rows.Next() {
		var bid auction.Bid
		var bidID int64

		if err := rows.Scan(
			&bidID,
			&bid.AdvertiserID,
			&bid.Amount,
			&bid.PredictedCTR,
		); err != nil {
			rows.Close()
			return nil, auction.Result{}, 0, err
		}

		if bidID > revision {
			revision = bidID
		}

		bids = append(bids, bid)
	}

	err = rows.Err()
	rows.Close()

	if err != nil {
		return nil, auction.Result{}, 0,
			fmt.Errorf("iterate bids: %w", err)
	}

	if revision == 0 {
		return nil, auction.Result{}, 0, nil
	}

	// Последний подтверждённый результат аукциона.
	var result auction.Result

	err = tx.QueryRow(ctx, `
		SELECT
			advertiser_id,
			bid,
			predicted_ctr,
			effective_bid,
			price,
			auction_type
		FROM auction_results
		WHERE auction_id = $1
		ORDER BY id DESC
		LIMIT 1
	`, id).Scan(
		&result.AdvertiserID,
		&result.Bid,
		&result.PredictedCTR,
		&result.EffectiveBid,
		&result.Price,
		&result.AuctionType,
	)

	if err != nil {
		return nil, auction.Result{}, 0,
			fmt.Errorf("read current result: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, auction.Result{}, 0,
			fmt.Errorf("commit snapshot: %w", err)
	}

	return bids, result, revision, nil
}

// Атомарное обновление Redis.Устаревшие версии отклоняются
var applySnapshot = redis.NewScript(`
local previous = redis.call('GET', KEYS[4])
local incoming = ARGV[1]

if previous then
	if #previous > #incoming or
	   (#previous == #incoming and previous > incoming) then
		return 0
	end

	if previous == incoming and
	   redis.call('EXISTS', KEYS[1], KEYS[2], KEYS[3]) == 3 then
		return 0
	end
end

redis.call('DEL', KEYS[1], KEYS[2], KEYS[3])

local n = tonumber(ARGV[3])

for i = 1, n do
	local at = 4 + (i - 1) * 3

	redis.call('HSET', KEYS[1],
		ARGV[at], ARGV[at + 1])

	redis.call('ZADD', KEYS[2],
		ARGV[at + 2], ARGV[at])
end

redis.call('LPUSH', KEYS[3], ARGV[2])
redis.call('SET', KEYS[4], incoming)

return 1
`)

// Используем прежний формат ключей
func key(id, suffix string) string {
	if id == "default" {
		return "auction:" + suffix
	}
	return "auction:" + id + ":" + suffix
}

// Auction синхронизирует один аукцион
func (s *Sync) Auction(ctx context.Context, id string) (bool, error) {

	bids, result, revision, err := s.latestSnapshot(ctx, id)
	if err != nil {
		return false, err
	}

	if revision == 0 {
		return false, nil
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return false, err
	}

	args := make([]interface{}, 0, 3+len(bids)*3)

	args = append(
		args,
		strconv.FormatInt(revision, 10),
		string(resultJSON),
		len(bids),
	)

	for _, bid := range bids {
		encoded, err := json.Marshal(bid)
		if err != nil {
			return false, err
		}

		score := -(bid.Amount * bid.PredictedCTR)

		args = append(
			args,
			bid.AdvertiserID,
			string(encoded),
			strconv.FormatFloat(score, 'g', -1, 64),
		)
	}

	applied, err := applySnapshot.Run(
		ctx,
		s.Redis,
		[]string{
			key(id, "bids"),
			key(id, "scores"),
			key(id, "winners"),
			key(id, "cache_version"),
		},
		args...,
	).Int()

	if err != nil {
		return false, fmt.Errorf("apply Redis snapshot: %w", err)
	}

	return applied == 1, nil
}

// All последовательно обходит все аукционы со ставками.
func (s *Sync) All(ctx context.Context) (int, error) {
	last := ""
	updated := 0

	for {
		rows, err := s.DB.Query(ctx, `
			SELECT a.id
			FROM auctions a
			WHERE a.id > $1
			  AND EXISTS (
				SELECT 1
				FROM bids b
				WHERE b.auction_id = a.id
			  )
			ORDER BY a.id
			LIMIT 100
		`, last)

		if err != nil {
			return updated, err
		}

		ids := make([]string, 0, 100)

		for rows.Next() {
			var id string

			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return updated, err
			}

			ids = append(ids, id)
		}

		err = rows.Err()
		rows.Close()

		if err != nil {
			return updated, err
		}

		if len(ids) == 0 {
			return updated, nil
		}

		for _, id := range ids {
			applied, err := s.Auction(ctx, id)
			if err != nil {
				return updated, fmt.Errorf(
					"sync auction %q: %w", id, err,
				)
			}

			if applied {
				updated++
			}
		}

		last = ids[len(ids)-1]
	}
}
