package push

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
	"uuid"

	"github.com/ismailshak/sprig/internal/schedule"
	"github.com/ismailshak/sprig/internal/store"
)

// digestKind is the value of the kind column on the digest's notification_send
// rows.
const digestKind = "digest"

// Digest is the job that sends each member what is due in their garden, once a
// day at the hour they chose in their timezone.
//
// It sleeps on one timer until the earliest of those hours, sends everything
// due, then works out the next one. Waking on a fixed interval instead would
// make every digest up to that interval late.
type Digest struct {
	logger  *slog.Logger
	queries *store.Queries
	sender  *Sender
	// todayURL is the URL the notification opens when it is tapped: the Today
	// page.
	todayURL string
	// now supplies the current time, so a test can fix the day.
	now func() time.Time
	// wake is the channel Wake sends on.
	wake chan struct{}
	// skipped holds, per membership, the last digest instant dropped for being
	// more than maxLate late. Every look drops the same one again, so the map
	// keeps the warning to one log line.
	skipped map[uuid.UUID]time.Time
}

// NewDigest returns a Digest that sends through sender and links the Today
// page under baseURL. Run starts it.
func NewDigest(logger *slog.Logger, queries *store.Queries, sender *Sender, baseURL string) *Digest {
	return &Digest{
		logger:   logger,
		queries:  queries,
		sender:   sender,
		todayURL: strings.TrimSuffix(baseURL, "/") + "/",
		now:      time.Now,
		wake:     make(chan struct{}, 1),
		skipped:  map[uuid.UUID]time.Time{},
	}
}

// Wake has the job work out its next send again. A handler calls it after
// committing a change to who gets a digest and when. It never blocks. A second
// call before the job looks again does nothing.
func (d *Digest) Wake() {
	wakeJob(d.wake)
}

// Run sends digests until ctx is done. It returns no error: a failed look is
// logged and tried again after retryAfter.
func (d *Digest) Run(ctx context.Context) {
	runJob(ctx, d.logger, "digest", d.now, d.wake, d.sendDue)
}

// sendDue sends every digest whose hour has come and returns the instant of
// the next one, or the zero time when nobody is waiting for a digest.
func (d *Digest) sendDue(ctx context.Context) (time.Time, error) {
	now := d.now()
	members, err := d.queries.ListDigestMembers(ctx, now)
	if err != nil {
		return time.Time{}, fmt.Errorf("listing who gets a digest: %w", err)
	}

	var next time.Time
	soonest := func(at time.Time) {
		if next.IsZero() || at.Before(next) {
			next = at
		}
	}
	for _, member := range members {
		loc := locationOf(d.logger, member.Handle, member.Timezone)
		at, key, skipped := nextDigest(int(member.DigestHour), loc, member.SentThrough, now)
		if !skipped.IsZero() && !d.skipped[member.MembershipID].Equal(skipped) {
			d.skipped[member.MembershipID] = skipped
			d.logger.Warn("digest skipped", "user", member.Handle, "garden", member.GardenName,
				"date", skipped.In(loc).Format(dateKey), "late", now.Sub(skipped).Truncate(time.Second))
		}
		if at.After(now) {
			soonest(at)
			continue
		}
		if err := d.send(ctx, member, loc, key, now); err != nil {
			if ctx.Err() != nil {
				return time.Time{}, err
			}
			d.logger.Error("digest not sent", "user", member.Handle, "garden", member.GardenName, "date", key, "err", err)
			soonest(now.Add(retryAfter))
			continue
		}
		// The next one for this member is the day after the one just sent.
		at, _, _ = nextDigest(int(member.DigestHour), loc, key, now)
		soonest(at)
	}
	return next, nil
}

// send claims the member's ledger row and sends the digest to each of their
// browsers, in one transaction. The claim comes first, so a row an earlier look
// wrote stops this send. A process that dies part-way commits nothing and sends
// the digest again on restart.
//
// An error from the push service is logged and the ledger row stays written:
// nothing retries, so a second send would only fail the same way.
func (d *Digest) send(ctx context.Context, member store.ListDigestMembersRow, loc *time.Location, key string, now time.Time) error {
	return d.queries.InTx(ctx, func(q *store.Queries) error {
		claimed, err := q.ClaimNotificationSend(ctx, store.ClaimNotificationSendParams{
			MembershipID: member.MembershipID,
			Kind:         digestKind,
			SendKey:      key,
		})
		if err != nil {
			return fmt.Errorf("claiming the send: %w", err)
		}
		if claimed == 0 {
			return nil
		}

		schedules, err := q.ListCareSchedules(ctx, member.GardenID)
		if err != nil {
			return fmt.Errorf("listing the schedules: %w", err)
		}
		latest, err := q.ListLatestCareEvents(ctx, member.GardenID)
		if err != nil {
			return fmt.Errorf("listing the latest care: %w", err)
		}
		notification, items := digestOf(member.GardenName, schedule.Resolve(schedules, latest, now.In(loc)), d.todayURL)
		// The ledger row stays written on a day with nothing due, so the job
		// does not look at that day again.
		if items == 0 {
			d.logger.Info("digest empty", "user", member.Handle, "garden", member.GardenName, "date", key)
			return nil
		}

		subscriptions, err := q.ListPushSubscriptions(ctx, member.UserID)
		if err != nil {
			return fmt.Errorf("listing the browsers: %w", err)
		}
		sent, err := deliver(ctx, d.logger, q, d.sender, member.Handle, subscriptions, notification, now)
		if err != nil {
			return err
		}
		d.logger.Info("digest sent", "user", member.Handle, "garden", member.GardenName, "date", key, "items", items, "browsers", sent)
		return nil
	})
}

// digestOf builds the notification for a garden: every care overdue or due
// today, grouped by care type in the order the plants come in, as "Water Big
// Fella, Doris and Nigel. Feed Sprout." The title is the garden's name, so
// somebody in two gardens can tell which one it is for. The int is how many
// cares it lists; zero means nothing is due and the caller sends nothing.
func digestOf(garden string, lines []schedule.Line, url string) (Notification, int) {
	type group struct {
		care   string
		plants []string
	}
	var groups []group
	items := 0
	for _, line := range lines {
		if line.State > schedule.DueToday {
			continue
		}
		items++
		i := 0
		for i < len(groups) && groups[i].care != line.CareType.Name {
			i++
		}
		if i == len(groups) {
			groups = append(groups, group{care: line.CareType.Name})
		}
		groups[i].plants = append(groups[i].plants, line.Plant.DisplayName())
	}

	sentences := make([]string, 0, len(groups))
	for _, g := range groups {
		sentences = append(sentences, capitalise(g.care)+" "+listOf(g.plants)+".")
	}
	return Notification{Title: garden, Body: strings.Join(sentences, " "), URL: url}, items
}

// capitalise upper-cases the first rune. A care type is named by the person
// who added it, so "dust" would otherwise start a sentence in lower case.
func capitalise(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

// listOf joins names as a sentence lists them: "Doris", "Doris and Nigel",
// "Big Fella, Doris and Nigel".
func listOf(names []string) string {
	if len(names) <= 1 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
