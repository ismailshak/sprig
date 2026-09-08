package push

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ismailshak/sprig/internal/store"
)

// deliver sends n to each of one person's browsers and returns the number of
// successful sends. A push failure is logged and the next browser tried, so
// the only error returned is a database failure.
func deliver(ctx context.Context, logger *slog.Logger, q *store.Queries, sender *Sender, handle string, subscriptions []store.PushSubscription, n Notification, now time.Time) (int, error) {
	sent := 0
	for _, subscription := range subscriptions {
		var write storeFailure
		switch err := sendOne(ctx, q, sender, subscription, n, now); {
		case errors.As(err, &write):
			return sent, err
		case errors.Is(err, ErrGone):
			logger.Info("browser gone", "user", handle, "user_agent", userAgentOf(subscription))
		case err != nil:
			logger.Error("push failed", "user", handle, "user_agent", userAgentOf(subscription), "err", err)
		default:
			sent++
		}
	}
	return sent, nil
}

// sendOne sends n to one browser and records the send on its row. A browser
// the push service reports gone is deleted and ErrGone returned, because a
// failed send is the only way the app learns the subscription is dead.
func sendOne(ctx context.Context, q *store.Queries, sender *Sender, subscription store.PushSubscription, n Notification, now time.Time) error {
	switch err := sender.Send(ctx, subscription, n); {
	case errors.Is(err, ErrGone):
		if _, err := q.DeletePushSubscription(ctx, subscription.UserID, subscription.ID); err != nil {
			return storeFailure{fmt.Errorf("deleting the browser the push service dropped: %w", err)}
		}
		return ErrGone
	case err != nil:
		return err
	}
	err := q.SetPushSubscriptionSent(ctx, store.SetPushSubscriptionSentParams{
		SentAt:         &now,
		UserID:         subscription.UserID,
		SubscriptionID: subscription.ID,
	})
	if err != nil {
		return storeFailure{fmt.Errorf("recording the send on the browser: %w", err)}
	}
	return nil
}

// storeFailure wraps a database error. deliver stops at the first one instead
// of trying the next browser.
type storeFailure struct{ err error }

func (f storeFailure) Error() string { return f.err.Error() }

func (f storeFailure) Unwrap() error { return f.err }

func userAgentOf(subscription store.PushSubscription) string {
	if subscription.UserAgent == nil {
		return ""
	}
	return *subscription.UserAgent
}
