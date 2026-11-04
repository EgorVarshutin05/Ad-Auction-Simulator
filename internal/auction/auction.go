package auction

import (
	"errors"
	"math"
	"sort"
)

type Bid struct {
	AdvertiserID string  `json:"advertiser_id"`
	Amount       float64 `json:"bid"`
	PredictedCTR float64 `json:"predicted_ctr"`
}

type Result struct {
	AdvertiserID string  `json:"advertiser_id"`
	Bid          float64 `json:"bid"`
	PredictedCTR float64 `json:"predicted_ctr"`
	EffectiveBid float64 `json:"effective_bid"`
	Price        float64 `json:"price"`
	AuctionType  string  `json:"auction_type"`
}

func Validate(b Bid) error {
	if b.AdvertiserID == "" {
		return errors.New("advertiser_id is required")
	}

	if math.IsNaN(b.Amount) || math.IsInf(b.Amount, 0) || b.Amount <= 0 {
		return errors.New("bid must be positive and finite")
	}

	if math.IsNaN(b.PredictedCTR) || math.IsInf(b.PredictedCTR, 0) || b.PredictedCTR <= 0 || b.PredictedCTR > 1 {
		return errors.New("predicted_ctr must be in (0, 1]")
	}

	if math.IsInf(b.Amount*b.PredictedCTR, 0) {
		return errors.New("effective bid is too large")
	}

	return nil
}

func Calculate(bids []Bid, auctionType string) (Result, error) {
	if auctionType != "first-price" && auctionType != "second-price" {
		return Result{}, errors.New("unknown auction type")
	}

	if len(bids) == 0 {
		return Result{}, errors.New("no bids")
	}

	ordered := append([]Bid(nil), bids...)
	for _, b := range ordered {
		if err := Validate(b); err != nil {
			return Result{}, err
		}
	}

	sort.Slice(ordered, func(i, j int) bool {
		left := ordered[i].Amount * ordered[i].PredictedCTR
		right := ordered[j].Amount * ordered[j].PredictedCTR
		if left == right {
			return ordered[i].AdvertiserID < ordered[j].AdvertiserID
		}
		return left > right
	})

	winner := ordered[0]
	price := winner.Amount

	if auctionType == "second-price" {
		price = 0
		if len(ordered) > 1 {
			price = (ordered[1].Amount * ordered[1].PredictedCTR) / winner.PredictedCTR
		}
	}

	return Result{winner.AdvertiserID, winner.Amount, winner.PredictedCTR, winner.Amount * winner.PredictedCTR, price, auctionType}, nil
}
