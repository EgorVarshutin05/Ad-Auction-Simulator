package auction

import (
	"math"
	"testing"
)

func TestCalculate(t *testing.T) {
	bids := []Bid{{"A", 150, 0.05}, {"B", 100, 0.10}, {"C", 200, 0.02}}
	tests := []struct {
		name, kind, winner string
		price              float64
	}{
		{"first price", "first-price", "B", 100},
		{"second price", "second-price", "B", 75},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Calculate(bids, tt.kind)
			if err != nil {
				t.Fatal(err)
			}
			if result.AdvertiserID != tt.winner || math.Abs(result.Price-tt.price) > 1e-9 {
				t.Fatalf("unexpected result: %+v", result)
			}
		})
	}
}

func TestSingleBid(t *testing.T) {
	r, err := Calculate([]Bid{{"A", 100, 0.5}}, "second-price")
	if err != nil || r.Price != 0 {
		t.Fatalf("result=%+v, err=%v", r, err)
	}
}

func TestTieBreak(t *testing.T) {
	tests := []struct {
		name       string
		bids       []Bid
		wantWinner string
		wantPrice  float64
	}{
		{"two tie", []Bid{{"B", 100, 0.1}, {"A", 200, 0.05}}, "A", 200},
		{"three tie and lower bid", []Bid{{"C", 100, 0.1}, {"B", 100, 0.1}, {"A", 200, 0.05}, {"D", 1, 0.1}}, "A", 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := Calculate(tt.bids, "second-price")
			if err != nil || r.AdvertiserID != tt.wantWinner || math.Abs(r.Price-tt.wantPrice) > 1e-9 {
				t.Fatalf("result=%+v, err=%v", r, err)
			}
		})
	}
}

func TestOrderDoesNotMatter(t *testing.T) {
	variants := [][]Bid{
		{{"C", 200, 0.02}, {"B", 100, 0.1}, {"A", 150, 0.05}},
		{{"B", 100, 0.1}, {"A", 150, 0.05}, {"C", 200, 0.02}},
		{{"A", 150, 0.05}, {"C", 200, 0.02}, {"B", 100, 0.1}},
	}
	for _, bids := range variants {
		r, err := Calculate(bids, "second-price")
		if err != nil || r.AdvertiserID != "B" || math.Abs(r.Price-75) > 1e-9 {
			t.Fatalf("result=%+v, err=%v", r, err)
		}
	}
}

func TestInputNotModified(t *testing.T) {
	bids := []Bid{{"B", 100, 0.1}, {"A", 150, 0.05}}
	_, err := Calculate(bids, "second-price")
	if err != nil || bids[0].AdvertiserID != "B" || bids[1].AdvertiserID != "A" {
		t.Fatalf("input was modified: %+v, err=%v", bids, err)
	}
}

func TestInvalidBids(t *testing.T) {
	for _, b := range []Bid{{"", 100, .1}, {"A", -1, .1}, {"A", 100, 1.1}, {"A", math.NaN(), .1}, {"A", math.Inf(1), .1}} {
		if Validate(b) == nil {
			t.Errorf("expected error for %+v", b)
		}
	}
	if _, err := Calculate(nil, "first-price"); err == nil {
		t.Fatal("expected empty auction error")
	}
	if _, err := Calculate([]Bid{{"A", 1, .1}}, "bad"); err == nil {
		t.Fatal("expected type error")
	}
}
