package api

import (
	"ad-auction/internal/auction"
	"ad-auction/internal/events"
	"ad-auction/internal/metrics"
	"ad-auction/internal/storage"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
)

type Server struct {
	Store       *storage.Store
	AuctionType string
	Events      chan<- events.Event
	mu          sync.Mutex
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) Bid(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, 405, "method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var b auction.Bid

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&b); err != nil {
		writeError(w, 400, "invalid JSON: "+err.Error())
		return
	}

	var extra interface{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeError(w, 400, "expected one JSON object")
		return
	}

	if err := auction.Validate(b); err != nil {
		writeError(w, 400, err.Error())
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := s.Store.SaveBid(ctx, b); err != nil {
		writeError(w, 503, "Redis unavailable")
		return
	}

	bids, err := s.Store.GetBids(ctx)
	if err != nil {
		writeError(w, 503, "Redis unavailable")
		return
	}

	result, err := auction.Calculate(bids, s.AuctionType)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	if err := s.Store.SaveWinner(ctx, result); err != nil {
		writeError(w, 503, "Redis unavailable")
		return
	}

	select {
	case s.Events <- events.Event{Type: "bid_placed", Bid: b, WinnerID: result.AdvertiserID}:
	case <-ctx.Done():
		writeError(w, 503, "event queue timeout")
		return
	}

	metrics.BidAmount.Observe(b.Amount)
	writeJSON(w, 201, result)
}
func (s *Server) Winner(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, 405, "method not allowed")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	bids, err := s.Store.GetBids(ctx)
	if err != nil {
		writeError(w, 503, "Redis unavailable")
		return
	}

	if len(bids) == 0 {
		writeError(w, 404, "no bids yet")
		return
	}

	result, err := auction.Calculate(bids, s.AuctionType)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	writeJSON(w, 200, result)
}
func (s *Server) Stats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, 405, "method not allowed")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	data, err := s.Store.Stats(ctx)
	if err != nil {
		writeError(w, 503, "Redis unavailable")
		return
	}

	writeJSON(w, 200, data)
}
