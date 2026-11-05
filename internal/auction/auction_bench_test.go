package auction

import (
	"fmt"
	"sort"
	"testing"
)

func calculateOld(bids []Bid, auctionType string) (Result, error) {
	if auctionType != "first-price" && auctionType != "second-price" {
		return Result{}, fmt.Errorf("unknown auction type")
	}
	if len(bids) == 0 {
		return Result{}, fmt.Errorf("no bids")
	}
	ordered := append([]Bid(nil), bids...)
	for _, bid := range ordered {
		if err := Validate(bid); err != nil {
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
			price = ordered[1].Amount * ordered[1].PredictedCTR / winner.PredictedCTR
		}
	}
	return Result{winner.AdvertiserID, winner.Amount, winner.PredictedCTR,
		winner.Amount * winner.PredictedCTR, price, auctionType}, nil
}

// makeBenchmarkBids creates input once, outside the timed benchmark loop.
func makeBenchmarkBids(size int) []Bid {
	bids := make([]Bid, size)
	for i := range bids {
		bids[i] = Bid{
			AdvertiserID: fmt.Sprintf("advertiser-%05d", i),
			Amount:       float64(50 + (i*73)%500),
			PredictedCTR: float64(1+(i*37)%100) / 1000,
		}
	}
	return bids
}

var benchmarkResult Result

func BenchmarkCalculate(b *testing.B) {
	for _, size := range []int{10, 100, 1000} {
		bids := makeBenchmarkBids(size)
		b.Run(fmt.Sprintf("Old/%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				result, err := calculateOld(bids, "second-price")
				if err != nil {
					b.Fatal(err)
				}
				benchmarkResult = result
			}
		})
		b.Run(fmt.Sprintf("New/%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				result, err := Calculate(bids, "second-price")
				if err != nil {
					b.Fatal(err)
				}
				benchmarkResult = result
			}
		})
	}
}

func TestCalculateMatchesOld(t *testing.T) {
	for _, size := range []int{1, 2, 10, 100, 1000} {
		bids := makeBenchmarkBids(size)
		for _, auctionType := range []string{"first-price", "second-price"} {
			oldResult, oldErr := calculateOld(bids, auctionType)
			newResult, newErr := Calculate(bids, auctionType)
			if oldErr != nil || newErr != nil || oldResult != newResult {
				t.Fatalf("size=%d type=%s old=%+v (%v) new=%+v (%v)", size, auctionType, oldResult, oldErr, newResult, newErr)
			}
		}
	}
}
