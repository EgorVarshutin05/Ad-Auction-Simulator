package storage

import (
	"ad-auction/internal/auction"
	"context"
	"fmt"
	"testing"
	"time"
)

func TestAuctionIsolation(t *testing.T) {
	store := setupTestRedis(t)
	ctx := context.Background()

	firstID := fmt.Sprintf("test-first-%d", time.Now().UnixNano())
	secondID := fmt.Sprintf("test-second-%d", time.Now().UnixNano())

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		for _, id := range []string{firstID, secondID} {
			for _, suffix := range []string{
				"bids",
				"scores",
				"winners",
				"stats",
				"wins",
			} {
				if err := store.Client.Del(cleanupCtx, auctionKey(id, suffix)).Err(); err != nil {
					t.Errorf("cleanup %s: %v", id, err)
				}
			}
		}
	})

	a := auction.Bid{
		AdvertiserID: "A",
		Amount:       100,
		PredictedCTR: 0.1,
	}

	b := auction.Bid{
		AdvertiserID: "B",
		Amount:       200,
		PredictedCTR: 0.1,
	}

	if err := store.SaveBidForAuction(ctx, firstID, a); err != nil {
		t.Fatal(err)
	}

	if err := store.SaveBidForAuction(ctx, secondID, b); err != nil {
		t.Fatal(err)
	}

	first, err := store.GetTopBidsForAuction(ctx, firstID)
	if err != nil {
		t.Fatal(err)
	}

	second, err := store.GetTopBidsForAuction(ctx, secondID)
	if err != nil {
		t.Fatal(err)
	}

	if len(first) != 1 || first[0] != a {
		t.Fatalf("first auction: %+v", first)
	}

	if len(second) != 1 || second[0] != b {
		t.Fatalf("second auction: %+v", second)
	}

	if err := store.AddBidStatsForAuction(ctx, firstID); err != nil {
		t.Fatal(err)
	}

	if err := store.AddLeaderStatsForAuction(ctx, firstID, "A"); err != nil {
		t.Fatal(err)
	}

	firstStats, err := store.StatsForAuction(ctx, firstID)
	if err != nil {
		t.Fatal(err)
	}

	secondStats, err := store.StatsForAuction(ctx, secondID)
	if err != nil {
		t.Fatal(err)
	}

	if firstStats["total_bids"] != int64(1) ||
		secondStats["total_bids"] != int64(0) {
		t.Fatalf("statistics are mixed: first=%v second=%v", firstStats, secondStats)
	}
}
