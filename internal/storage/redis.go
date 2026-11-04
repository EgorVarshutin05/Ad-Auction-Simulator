package storage

import (
	"ad-auction/internal/auction"
	"context"
	"encoding/json"
	"errors"

	"github.com/redis/go-redis/v9"
)

type Store struct{ Client *redis.Client }

func New(address string) *Store {
	return &Store{Client: redis.NewClient(&redis.Options{Addr: address})}
}

func (s *Store) SaveBid(ctx context.Context, b auction.Bid) error {
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return s.Client.HSet(ctx, "auction:bids", b.AdvertiserID, data).Err()
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

func (s *Store) AddWinStats(ctx context.Context, advertiserID string) error {
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
	return map[string]interface{}{"total_bids": total, "wins_by_advertiser": wins}, nil
}
