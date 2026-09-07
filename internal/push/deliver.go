package push

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/store"
)

// deliver sends n to each of one person's browsers and returns how many took
// it. A browser the push service reports gone is deleted, because a failed
// send is the only way the app learns of it. Any other push failure is logged
// and the next browser tried, so the only error returned is a database error.
func deliver(ctx context.Context, logger *slog.Logger, q *store.Queries, sender *Sender, userID uuid.UUID, handle string, subscriptions []store.PushSubscription, n Notification, now time.Time) (int, error) {
	sent := 0
	for _, subscription := range subscriptions {
		switch err := sender.Send(ctx, subscription, n); {
		case errors.Is(err, ErrGone):
			if _, err := q.DeletePushSubscription(ctx, userID, subscription.ID); err != nil {
				return sent, fmt.Errorf("deleting the browser the push service dropped: %w", err)
			}
			logger.Info("browser gone", "user", handle, "user_agent", userAgentOf(subscription))
		case err != nil:
			logger.Error("push failed", "user", handle, "user_agent", userAgentOf(subscription), "err", err)
		default:
			err := q.SetPushSubscriptionSent(ctx, store.SetPushSubscriptionSentParams{
				SentAt:         &now,
				UserID:         userID,
				SubscriptionID: subscription.ID,
			})
			if err != nil {
				return sent, fmt.Errorf("recording the send on the browser: %w", err)
			}
			sent++
		}
	}
	return sent, nil
}

func userAgentOf(subscription store.PushSubscription) string {
	if subscription.UserAgent == nil {
		return ""
	}
	return *subscription.UserAgent
}
