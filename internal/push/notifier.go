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

// Notifier sends the notifications a request handler sends: to the other
// members of a garden when one of them logs care, and to one person when
// something happens to their access or their garden. It writes no
// notification_send row, because each send happens once in the request that
// made the change and is never retried.
type Notifier struct {
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

// NewNotifier returns a Notifier that sends through sender and links pages
// under baseURL.
func NewNotifier(logger *slog.Logger, queries *store.Queries, sender *Sender, baseURL string) *Notifier {
	return &Notifier{
		logger:  logger,
		queries: queries,
		sender:  sender,
		baseURL: strings.TrimSuffix(baseURL, "/"),
		now:     time.Now,
	}
}

// SendActivity delivers n to every browser of every other current member of
// the garden who has the activity notification on. actor is the person who
// logged the care and is never sent it. n.URL and n.Icon are paths, made
// absolute under the base URL.
//
// The send runs in a goroutine and SendActivity returns at once. The
// goroutine ignores a cancelled ctx, so the request that logged the care
// neither waits for the push service nor stops the send when it returns.
func (p *Notifier) SendActivity(ctx context.Context, gardenID, actor uuid.UUID, n Notification) {
	p.start(ctx, n, func(ctx context.Context, n Notification) {
		p.sendActivity(ctx, gardenID, actor, n)
	})
}

// SendToUser delivers n to every browser user has subscribed, whatever
// gardens they are in. n.URL is a path, made absolute under the base URL, or
// empty for a notification that opens nothing. The send runs in a goroutine
// the way SendActivity's does.
func (p *Notifier) SendToUser(ctx context.Context, user store.AppUser, n Notification) {
	p.start(ctx, n, func(ctx context.Context, n Notification) {
		p.sendToUser(ctx, user, n)
	})
}

// Wait blocks until every send started so far has finished. It is called at
// shutdown, before the database pool is closed.
func (p *Notifier) Wait() {
	p.sends.Wait()
}

// start makes n's paths absolute and runs send in a goroutine counted by
// Wait.
func (p *Notifier) start(ctx context.Context, n Notification, send func(context.Context, Notification)) {
	n.URL = p.absolute(n.URL)
	n.Icon = p.absolute(n.Icon)
	p.sends.Add(1)
	go func() {
		defer p.sends.Done()
		send(context.WithoutCancel(ctx), n)
	}()
}

func (p *Notifier) absolute(path string) string {
	return absolute(p.baseURL, path)
}

// GardenNamed is how a notification names a garden: "Ellie’s Rosewood" to
// anyone but the owner, and "Rosewood" to the owner, who would otherwise read
// their own name. It is the garden's name alone when no owner is left.
func GardenNamed(garden, ownerName string, toOwner bool) string {
	if toOwner || ownerName == "" {
		return garden
	}
	return ownerName + "’s " + garden
}

// absolute puts baseURL in front of path. An empty path stays empty.
func absolute(baseURL, path string) string {
	if path == "" {
		return ""
	}
	return baseURL + path
}

func (p *Notifier) sendActivity(ctx context.Context, gardenID, actor uuid.UUID, n Notification) {
	now := p.now()
	rows, err := p.queries.ListActivitySubscriptions(ctx, store.ListActivitySubscriptionsParams{
		GardenID: gardenID,
		ActorID:  actor,
		Now:      now,
	})
	if err != nil {
		p.logger.Error("activity not sent", "garden_id", gardenID, "err", err)
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
		sent, err := deliver(ctx, p.logger, p.queries, p.sender, member.Handle, subscriptions, n, now)
		if err != nil {
			p.logger.Error("activity not sent", "user", member.Handle, "garden", member.GardenName, "err", err)
			continue
		}
		p.logger.Info("activity sent", "user", member.Handle, "garden", member.GardenName, "browsers", sent)
	}
}

func (p *Notifier) sendToUser(ctx context.Context, user store.AppUser, n Notification) {
	now := p.now()
	subscriptions, err := p.queries.ListPushSubscriptions(ctx, user.ID)
	if err != nil {
		p.logger.Error("notification not sent", "user", user.Handle, "title", n.Title, "err", err)
		return
	}
	sent, err := deliver(ctx, p.logger, p.queries, p.sender, user.Handle, subscriptions, n, now)
	if err != nil {
		p.logger.Error("notification not sent", "user", user.Handle, "title", n.Title, "err", err)
		return
	}
	p.logger.Info("notification sent", "user", user.Handle, "title", n.Title, "browsers", sent)
}
