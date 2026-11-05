package storage

import (
	"ad-auction/internal/auction"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"
)

type Store struct {
	Client *redis.Client
}

func New(address string) *Store {
	return &Store{Client: redis.NewClient(&redis.Options{Addr: address})}
}

func (s *Store) SaveBid(ctx context.Context, b auction.Bid) error {
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}

	score := -(b.Amount * b.PredictedCTR)

	_, err = s.Client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ctx, "auction:bids", b.AdvertiserID, data)

		pipe.ZAdd(ctx, "auction:scores", redis.Z{
			Score:  score,
			Member: b.AdvertiserID,
		})

		return nil
	})

	return err
}

func (s *Store) GetBids(ctx context.Context) ([]auction.Bid, error) {
	values, err := s.Client.HVals(ctx, "auction:bids").Result()
	if err != nil {
		return nil, err
	}
	bids := make([]auction.Bid, 0, len(values))
	for _, v := range values {
		var b auction.Bid
		if err := json.Unmarshal([]byte(v), &b); err != nil {
			return nil, err
		}
		bids = append(bids, b)
	}
	return bids, nil
}

func (s *Store) SaveWinner(ctx context.Context, r auction.Result) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return s.Client.LPush(ctx, "auction:winners", data).Err()
}

func (s *Store) AddBidStats(ctx context.Context, b auction.Bid) error {
	return s.Client.HIncrBy(ctx, "auction:stats", "total_bids", 1).Err()
}

func (s *Store) AddLeaderStats(ctx context.Context, advertiserID string) error {
	return s.Client.HIncrBy(ctx, "auction:wins", advertiserID, 1).Err()
}

func (s *Store) Stats(ctx context.Context) (map[string]interface{}, error) {
	total, err := s.Client.HGet(ctx, "auction:stats", "total_bids").Int64()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	wins, err := s.Client.HGetAll(ctx, "auction:wins").Result()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"total_bids":              total,
		"times_leading_after_bid": wins,
	}, nil
}

func (s *Store) GetTopBids(ctx context.Context) ([]auction.Bid, error) {
	// Получаем ID двух лучших рекламодателей.
	ids, err := s.Client.ZRange(ctx, "auction:scores", 0, 1).Result()
	if err != nil {
		return nil, err
	}

	if len(ids) == 0 {
		return []auction.Bid{}, nil
	}

	// Получаем полные данные только этих участников.
	values, err := s.Client.HMGet(ctx, "auction:bids", ids...).Result()
	if err != nil {
		return nil, err
	}

	bids := make([]auction.Bid, 0, len(values))

	for i, value := range values {
		if value == nil {
			return nil, fmt.Errorf("bid not found: %s", ids[i])
		}

		data, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("unexpected bid type: %T", value)
		}

		var bid auction.Bid

		if err := json.Unmarshal([]byte(data), &bid); err != nil {
			return nil, err
		}

		bids = append(bids, bid)
	}

	return bids, nil
}
