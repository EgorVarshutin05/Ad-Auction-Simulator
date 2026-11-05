package service

import (
	"ad-auction/internal/auction"
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"
)

var (
	ErrInvalidBid          = errors.New("invalid bid")
	ErrInvalidAuctionID    = errors.New("invalid auction_id")
	ErrNoBids              = errors.New("no bids yet")
	ErrStorage             = errors.New("storage unavailable")
	ErrHistory             = errors.New("history storage unavailable")
	ErrAuctionClosed       = errors.New("auction is closed")
	ErrAuctionTypeMismatch = errors.New("auction type mismatch")
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

// Repository описывает операции с Redis
type Repository interface {
	SaveBidForAuction(context.Context, string, auction.Bid) error
	GetTopBidsForAuction(context.Context, string) ([]auction.Bid, error)
	SaveWinnerForAuction(context.Context, string, auction.Result) error
	StatsForAuction(context.Context, string) (map[string]interface{}, error)
}

// StatsRepository предоставляет статистику,рассчитанную Kafka Consumer.
type StatsRepository interface {
	StatsForAuction(ctx context.Context, auctionID string) (map[string]interface{}, error)
}

// HistoryRepository описывает запись истории в PostgreSQL

type HistoryRepository interface {
	GetAuctionState(ctx context.Context, auctionID string) (auctionType string, status string, exists bool, err error)

	SaveHistory(context.Context, string, string, auction.Bid, auction.Result) error
}

type Service struct {
	Store       Repository
	History     HistoryRepository
	AuctionType string
	StatsStore  StatsRepository

	mu sync.Mutex
}

func (s *Service) PlaceBid(ctx context.Context, auctionID string, bid auction.Bid) (auction.Result, error) {
	id, err := NormalizeAuctionID(auctionID)
	if err != nil {
		return auction.Result{}, err
	}

	if err := auction.Validate(bid); err != nil {
		return auction.Result{}, fmt.Errorf("%w: %v", ErrInvalidBid, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Проверяем состояние аукциона в PostgreSQL
	savedType, status, exists, err := s.History.GetAuctionState(ctx, id)
	if err != nil {
		return auction.Result{}, fmt.Errorf("%w: %w", ErrHistory, err)
	}

	if exists {
		if savedType != s.AuctionType {
			return auction.Result{}, ErrAuctionTypeMismatch
		}

		if status != "active" {
			return auction.Result{}, ErrAuctionClosed
		}
	}

	// Сохраняем ставку в Redis
	if err := s.Store.SaveBidForAuction(ctx, id, bid); err != nil {
		return auction.Result{}, fmt.Errorf("%w: %w", ErrStorage, err)
	}

	// Получаем двух лучших участников
	bids, err := s.Store.GetTopBidsForAuction(ctx, id)
	if err != nil {
		return auction.Result{}, fmt.Errorf("%w: %w", ErrStorage, err)
	}

	// Рассчитываем победителя
	result, err := auction.Calculate(bids, s.AuctionType)
	if err != nil {
		return auction.Result{}, err
	}

	// Сохраняем результат в Redis
	if err := s.Store.SaveWinnerForAuction(ctx, id, result); err != nil {
		return auction.Result{}, fmt.Errorf("%w: %w", ErrStorage, err)
	}

	// Записываем историю в PostgreSQL
	if err := s.History.SaveHistory(ctx, id, s.AuctionType, bid, result); err != nil {
		return auction.Result{}, fmt.Errorf("%w: %w", ErrHistory, err)
	}

	return result, nil
}

func (s *Service) CurrentWinner(ctx context.Context, auctionID string) (auction.Result, error) {
	id, err := NormalizeAuctionID(auctionID)
	if err != nil {
		return auction.Result{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	bids, err := s.Store.GetTopBidsForAuction(ctx, id)
	if err != nil {
		return auction.Result{}, fmt.Errorf("%w: %w", ErrStorage, err)
	}

	if len(bids) == 0 {
		return auction.Result{}, ErrNoBids
	}

	return auction.Calculate(bids, s.AuctionType)
}

func (s *Service) Stats(ctx context.Context, auctionID string) (map[string]interface{}, error) {

	id, err := NormalizeAuctionID(auctionID)
	if err != nil {
		return nil, err
	}

	if s.StatsStore == nil {
		return nil, fmt.Errorf("%w: statistics repository is not configured", ErrStorage)
	}

	// Читаем статистику из PostgreSQL.
	data, err := s.StatsStore.StatsForAuction(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStorage, err)
	}

	return data, nil
}
