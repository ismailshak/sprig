package push

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/store"
)

// Activity sends the other members of a garden a notification when one of
// them logs care. It writes no notification_send row, because the send
// happens once in the request that recorded the care and is never retried.
type Activity struct {
	logger  *slog.Logger
	queries *store.Queries
	sender  *Sender
	// baseURL is the origin the app is served at, with no trailing slash.
	baseURL string
	// now supplies the current time. It is the cutoff for an ended membership
	// and the value written to last_sent_at.
	now   func() time.Time
	sends sync.WaitGroup
}

// NewActivity returns an Activity that sends through sender and links pages
// under baseURL.
func NewActivity(logger *slog.Logger, queries *store.Queries, sender *Sender, baseURL string) *Activity {
	return &Activity{
		logger:  logger,
		queries: queries,
		sender:  sender,
		baseURL: strings.TrimSuffix(baseURL, "/"),
		now:     time.Now,
	}
}

// Send delivers n to every browser of every other current member of the
// garden who has the activity notification on. actor is the person who logged
// the care and is never sent it. n.URL and n.Icon are paths, made absolute
// under the base URL.
//
// Send starts the send in a goroutine and returns. The goroutine ignores a
// cancelled ctx, so the request that logged the care neither waits for the
// push service nor stops the send when it returns.
func (a *Activity) Send(ctx context.Context, gardenID, actor uuid.UUID, n Notification) {
	n.URL = a.absolute(n.URL)
	n.Icon = a.absolute(n.Icon)
	a.sends.Add(1)
	go func() {
		defer a.sends.Done()
		a.send(context.WithoutCancel(ctx), gardenID, actor, n)
	}()
}

// Wait blocks until every send started by Send has finished. It is called at
// shutdown, before the database pool is closed.
func (a *Activity) Wait() {
	a.sends.Wait()
}

// absolute puts the base URL in front of a path. An empty path stays empty.
func (a *Activity) absolute(path string) string {
	if path == "" {
		return ""
	}
	return a.baseURL + path
}

func (a *Activity) send(ctx context.Context, gardenID, actor uuid.UUID, n Notification) {
	now := a.now()
	rows, err := a.queries.ListActivitySubscriptions(ctx, store.ListActivitySubscriptionsParams{
		GardenID: gardenID,
		ActorID:  actor,
		Now:      now,
	})
	if err != nil {
		a.logger.Error("activity not sent", "garden_id", gardenID, "err", err)
		return
	}

	// The rows come ordered by member, so one person's browsers are
	// consecutive.
	for i := 0; i < len(rows); {
		member := rows[i]
		var subscriptions []store.PushSubscription
		for i < len(rows) && rows[i].PushSubscription.UserID == member.PushSubscription.UserID {
			subscriptions = append(subscriptions, rows[i].PushSubscription)
			i++
		}
		sent, err := deliver(ctx, a.logger, a.queries, a.sender, member.PushSubscription.UserID, member.Handle, subscriptions, n, now)
		if err != nil {
			a.logger.Error("activity not sent", "user", member.Handle, "garden", member.GardenName, "err", err)
			continue
		}
		a.logger.Info("activity sent", "user", member.Handle, "garden", member.GardenName, "browsers", sent)
	}
}
