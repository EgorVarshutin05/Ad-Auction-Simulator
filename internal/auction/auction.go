package auction

import (
	"errors"
	"math"
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

	bestIdx, secondIdx := -1, -1
	var bestScore, secondScore float64

	for i, bid := range bids {
		if err := Validate(bid); err != nil {
			return Result{}, err
		}

		score := bid.Amount * bid.PredictedCTR

		if bestIdx == -1 ||
			score > bestScore ||
			(score == bestScore && bid.AdvertiserID < bids[bestIdx].AdvertiserID) {

			secondIdx, secondScore = bestIdx, bestScore
			bestIdx, bestScore = i, score

		} else if secondIdx == -1 ||
			score > secondScore ||
			(score == secondScore && bid.AdvertiserID < bids[secondIdx].AdvertiserID) {

			secondIdx, secondScore = i, score
		}
	}

	winner := bids[bestIdx]
	price := winner.Amount

	if auctionType == "second-price" {
		price = 0

		if secondIdx != -1 {
			price = secondScore / winner.PredictedCTR
		}
	}

	return Result{
		AdvertiserID: winner.AdvertiserID,
		Bid:          winner.Amount,
		PredictedCTR: winner.PredictedCTR,
		EffectiveBid: bestScore,
		Price:        price,
		AuctionType:  auctionType,
	}, nil
}
