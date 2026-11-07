package api

import (
	"ad-auction/internal/auction"
	"ad-auction/internal/metrics"
	"ad-auction/internal/service"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

type Server struct {
	Service *service.Service
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// bidRequest представляет JSON-запрос на размещение ставки
type bidRequest struct {
	auction.Bid
	AuctionID string `json:"auction_id"`
}

func (s *Server) Bid(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req bidRequest

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	var extra interface{}

	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "expected one JSON object")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	result, err := s.Service.PlaceBid(ctx, req.AuctionID, req.Bid)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidBid),
			errors.Is(err, service.ErrInvalidAuctionID):
			writeError(w, http.StatusBadRequest, err.Error())

		case errors.Is(err, service.ErrAuctionClosed),
			errors.Is(err, service.ErrAuctionTypeMismatch):
			writeError(w, http.StatusConflict, err.Error())

		case errors.Is(err, service.ErrStorage),
			errors.Is(err, service.ErrHistory):
			writeError(w, http.StatusServiceUnavailable, "service unavailable")

		default:
			writeError(w, http.StatusInternalServerError, "auction calculation failed")
		}

		return
	}

	metrics.BidAmount.Observe(req.Amount)
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) Winner(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	auctionID := r.URL.Query().Get("auction_id")

	result, err := s.Service.CurrentWinner(ctx, auctionID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidAuctionID):
			writeError(w, http.StatusBadRequest, err.Error())

		case errors.Is(err, service.ErrNoBids):
			writeError(w, http.StatusNotFound, "no bids yet")

		case errors.Is(err, service.ErrStorage),
			errors.Is(err, service.ErrHistory):
			writeError(w, http.StatusServiceUnavailable, "service unavailable")

		default:
			writeError(w, http.StatusInternalServerError, "auction calculation failed")
		}

		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) Stats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	auctionID := r.URL.Query().Get("auction_id")

	data, err := s.Service.Stats(ctx, auctionID)
	if err != nil {
		if errors.Is(err, service.ErrInvalidAuctionID) {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			writeError(w, http.StatusServiceUnavailable, "Redis unavailable")
		}
		return
	}

	writeJSON(w, http.StatusOK, data)
}
