package events

import (
	"ad-auction/internal/auction"
	"ad-auction/internal/storage"
	"context"
	"log"
)

type Event struct {
	Type     string
	Bid      auction.Bid
	WinnerID string
}

func Consume(ctx context.Context, ch <-chan Event, store *storage.Store) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			if event.Type == "bid_placed" {
				if err := store.AddBidStats(ctx, event.Bid); err != nil {
					log.Printf("stats update failed: %v", err)
				}
				if err := store.AddWinStats(ctx, event.WinnerID); err != nil {
					log.Printf("wins update failed: %v", err)
				}
			}
		}
	}
}
