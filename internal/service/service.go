package service

import (
	"ad-auction/internal/auction"
	"ad-auction/internal/storage"
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidBid       = errors.New("invalid bid")
	ErrInvalidAuctionID = errors.New("invalid auction_id")
	ErrNoBids           = errors.New("no bids yet")
	ErrStorage          = errors.New("storage unavailable")
	ErrHistory          = errors.New("history storage unavailable")

	ErrAuctionClosed       = storage.ErrAuctionClosed
	ErrAuctionTypeMismatch = storage.ErrAuctionTypeMismatch
)

const DefaultAuctionID = "default"

var auctionIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func NormalizeAuctionID(id string) (string, error) {
	if id == "" {
		return DefaultAuctionID, nil
	}
	if !auctionIDPattern.MatchString(id) {
		return "", ErrInvalidAuctionID
	}
	return id, nil
}

type HistoryRepository interface {
	PlaceBidTransaction(context.Context, string, string, auction.Bid) (auction.Result, error)

	CurrentWinnerForAuction(context.Context, string) (auction.Result, error)
}

type StatsRepository interface {
	StatsForAuction(context.Context, string) (map[string]interface{}, error)
}

type Service struct {
	History     HistoryRepository
	StatsStore  StatsRepository
	AuctionType string
}

// PostgreSQL принимает решение о ставке.Redis обновляет отдельный процесс cache-sync.
func (s *Service) PlaceBid(ctx context.Context, auctionID string, bid auction.Bid) (auction.Result, error) {

	id, err := NormalizeAuctionID(auctionID)
	if err != nil {
		return auction.Result{}, err
	}

	if err := auction.Validate(bid); err != nil {
		return auction.Result{}, fmt.Errorf("%w: %v", ErrInvalidBid, err)
	}

	result, err := s.History.PlaceBidTransaction(ctx, id, s.AuctionType, bid)

	if err != nil {
		if errors.Is(err, ErrAuctionClosed) ||
			errors.Is(err, ErrAuctionTypeMismatch) {
			return auction.Result{}, err
		}

		return auction.Result{}, fmt.Errorf("%w: %w", ErrHistory, err)
	}

	return result, nil
}

func (s *Service) CurrentWinner(ctx context.Context, auctionID string) (auction.Result, error) {

	id, err := NormalizeAuctionID(auctionID)
	if err != nil {
		return auction.Result{}, err
	}

	result, err := s.History.CurrentWinnerForAuction(ctx, id)

	if errors.Is(err, pgx.ErrNoRows) {
		return auction.Result{}, ErrNoBids
	}

	if err != nil {
		return auction.Result{}, fmt.Errorf("%w: %w", ErrHistory, err)
	}

	return result, nil
}

func (s *Service) Stats(ctx context.Context, auctionID string) (map[string]interface{}, error) {

	id, err := NormalizeAuctionID(auctionID)
	if err != nil {
		return nil, err
	}

	if s.StatsStore == nil {
		return nil, fmt.Errorf("%w: statistics repository not configured", ErrStorage)
	}

	data, err := s.StatsStore.StatsForAuction(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStorage, err)
	}

	return data, nil
}
