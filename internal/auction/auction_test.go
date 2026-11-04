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
	r, err := Calculate([]Bid{{"B", 100, 0.1}, {"A", 200, 0.05}}, "first-price")
	if err != nil || r.AdvertiserID != "A" {
		t.Fatalf("result=%+v, err=%v", r, err)
	}
}
func TestInvalidBids(t *testing.T) {
	for _, b := range []Bid{{"", 100, .1}, {"A", -1, .1}, {"A", 100, 1.1}, {"A", math.NaN(), .1}} {
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
