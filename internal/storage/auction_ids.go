package storage

import (
	"ad-auction/internal/auction"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// auctionKey формирует Redis-ключ для конкретного аукциона
// Для default сохраняем совместимость со старыми ключами
func auctionKey(auctionID, suffix string) string {
	if auctionID == "default" {
		return "auction:" + suffix
	}

	return "auction:" + auctionID + ":" + suffix
}

// SaveBidForAuction сохраняет ставку и обновляет Sorted Set
func (s *Store) SaveBidForAuction(ctx context.Context, auctionID string, b auction.Bid) error {
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}

	score := -(b.Amount * b.PredictedCTR)

	_, err = s.Client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ctx, auctionKey(auctionID, "bids"), b.AdvertiserID, data)

		pipe.ZAdd(ctx, auctionKey(auctionID, "scores"), redis.Z{
			Score:  score,
			Member: b.AdvertiserID,
		})

		return nil
	})

	return err
}

// GetTopBidsForAuction возвращает двух лучших участников
func (s *Store) GetTopBidsForAuction(ctx context.Context, auctionID string) ([]auction.Bid, error) {

	ids, err := s.Client.ZRange(ctx, auctionKey(auctionID, "scores"), 0, 1).Result()

	if err != nil {
		return nil, err
	}

	if len(ids) == 0 {
		return []auction.Bid{}, nil
	}

	values, err := s.Client.HMGet(ctx, auctionKey(auctionID, "bids"), ids...).Result()

	if err != nil {
		return nil, err
	}

	bids := make([]auction.Bid, 0, len(values))

	for i, value := range values {
		if value == nil {
			return nil, fmt.Errorf("missing bid for advertiser %q", ids[i])
		}

		raw, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("unexpected Redis value type %T", value)
		}

		var bid auction.Bid

		if err := json.Unmarshal([]byte(raw), &bid); err != nil {
			return nil, err
		}

		bids = append(bids, bid)
	}

	return bids, nil
}

// SaveWinnerForAuction сохраняет результат аукциона
func (s *Store) SaveWinnerForAuction(ctx context.Context, auctionID string, result auction.Result) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}

	return s.Client.LPush(ctx, auctionKey(auctionID, "winners"), data).Err()
}

// StatsForAuction возвращает статистику выбранного аукциона
func (s *Store) StatsForAuction(ctx context.Context, auctionID string) (map[string]interface{}, error) {
	total, err := s.Client.HGet(ctx, auctionKey(auctionID, "stats"), "total_bids").Int64()

	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}

	wins, err := s.Client.HGetAll(ctx, auctionKey(auctionID, "wins")).Result()

	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"total_bids":              total,
		"times_leading_after_bid": wins,
	}, nil
}

// AddBidStatsForAuction увеличивает счетчик ставок
func (s *Store) AddBidStatsForAuction(ctx context.Context, auctionID string) error {
	return s.Client.HIncrBy(ctx, auctionKey(auctionID, "stats"), "total_bids", 1).Err()
}

// AddLeaderStatsForAuction увеличивает счётчик лидерства
func (s *Store) AddLeaderStatsForAuction(ctx context.Context, auctionID string, advertiserID string) error {
	return s.Client.HIncrBy(ctx, auctionKey(auctionID, "wins"), advertiserID, 1).Err()
}
