package push

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// The kind column of the ledger rows the Deadlines job writes.
const (
	tokenExpiringKind = "token_expiring" //nolint:gosec // a notification kind, not a credential
	tokenExpiredKind  = "token_expired"  //nolint:gosec // a notification kind, not a credential
	sittingEndedKind  = "sitting_ended"
)

// warnBefore is how long before a token expires the warning is sent.
const warnBefore = 7 * 24 * time.Hour

// maxDeadlineLate is how far past a deadline the job still sends for it.
// Anything older is never sent, so the first start with this job in the binary
// does not send for every token that has ever expired and every sitting that
// has ever ended.
const maxDeadlineLate = 7 * 24 * time.Hour

// Deadlines is the job that sends the notifications due at a time stored on a
// row: a token a week from expiry and a token expired, to everyone who can
// manage the garden's tokens, and a sitting ended, to the sitter and the
// person who invited them.
//
// It sleeps on one timer until the earliest of those instants, sends
// everything due, then works out the next one. Each send claims a ledger row
// in the transaction it sends in, so a restart cannot send twice.
type Deadlines struct {
	logger  *slog.Logger
	queries *store.Queries
	sender  *Sender
	// tokensURL and peopleURL are the absolute URLs a token notification and
	// the inviter's sitting notification open.
	tokensURL string
	peopleURL string
	// now supplies the current time, so a test can fix the day.
	now func() time.Time
	// wake is the channel Wake sends on.
	wake chan struct{}
}

// NewDeadlines returns a Deadlines that sends through sender. tokensPath and
// peoplePath are the paths of the Tokens and People pages, made absolute
// under baseURL. Run starts it.
func NewDeadlines(logger *slog.Logger, queries *store.Queries, sender *Sender, baseURL, tokensPath, peoplePath string) *Deadlines {
	base := strings.TrimSuffix(baseURL, "/")
	return &Deadlines{
		logger:    logger,
		queries:   queries,
		sender:    sender,
		tokensURL: absolute(base, tokensPath),
		peopleURL: absolute(base, peoplePath),
		now:       time.Now,
		wake:      make(chan struct{}, 1),
	}
}

// Wake has the job work out its next send again. A handler calls it after
// committing a token created or revoked, or a membership's end date set or
// moved. It never blocks. A second call before the job looks again does
// nothing.
func (d *Deadlines) Wake() {
	wakeJob(d.wake)
}

// Run sends until ctx is done. It returns no error: a failed look is logged
// and tried again after retryAfter.
func (d *Deadlines) Run(ctx context.Context) {
	runJob(ctx, d.logger, "deadlines", d.now, d.wake, d.sendDue)
}

// sendDue sends every notification whose instant has come and returns the
// instant of the next one, or the zero time when none is coming. An instant
// more than maxDeadlineLate ago is not sent for.
func (d *Deadlines) sendDue(ctx context.Context) (time.Time, error) {
	now := d.now()
	since := now.Add(-maxDeadlineLate)
	var next time.Time
	soonest := func(at time.Time) {
		if next.IsZero() || at.Before(next) {
			next = at
		}
	}
	// failed notes a send that failed on the database and has the job look
	// again after retryAfter. It reports whether the job should stop.
	failed := func(err error, what string, args ...any) bool {
		if ctx.Err() != nil {
			return true
		}
		d.logger.Error(what, append(args, "err", err)...)
		soonest(now.Add(retryAfter))
		return false
	}

	tokens, err := d.queries.ListTokenDeadlines(ctx, store.ListTokenDeadlinesParams{
		Now:        now,
		Capability: string(auth.TokenManage),
		Since:      since,
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("listing the tokens: %w", err)
	}
	for _, row := range tokens {
		token := row.APIToken
		to := recipient{membershipID: row.MembershipID, userID: row.UserID, handle: row.Handle, garden: row.GardenName}
		warnAt := token.ExpiresAt.Add(-warnBefore)
		// A token made with a week or less to live gets no warning, because it
		// would arrive the moment the token was made. A warning missed while
		// the process was down is sent late only while the token still works.
		// Once it has expired, the expired notification says everything the
		// warning would have.
		if warnAt.After(token.CreatedAt) {
			switch {
			case warnAt.After(now):
				soonest(warnAt)
			case now.Before(token.ExpiresAt):
				loc := locationOf(d.logger, row.Handle, row.Timezone)
				n := tokenExpiringNotification(GardenNamed(row.GardenName, row.OwnerName, row.RecipientOwns), token, loc, d.tokensURL)
				err := d.send(ctx, to, tokenExpiringKind, token.ID.String(), n, now)
				if err != nil && failed(err, "token expiring not sent", "user", row.Handle, "garden", row.GardenName, "token", token.Prefix) {
					return time.Time{}, err
				}
			}
		}
		if token.ExpiresAt.After(now) {
			soonest(token.ExpiresAt)
			continue
		}
		n := tokenExpiredNotification(GardenNamed(row.GardenName, row.OwnerName, row.RecipientOwns), token, d.tokensURL)
		err := d.send(ctx, to, tokenExpiredKind, token.ID.String(), n, now)
		if err != nil && failed(err, "token expired not sent", "user", row.Handle, "garden", row.GardenName, "token", token.Prefix) {
			return time.Time{}, err
		}
	}

	sittings, err := d.queries.ListSittingDeadlines(ctx, now, since)
	if err != nil {
		return time.Time{}, fmt.Errorf("listing the sittings: %w", err)
	}
	for _, row := range sittings {
		if row.EndsAt.After(now) {
			soonest(row.EndsAt)
			continue
		}
		to := recipient{membershipID: row.MembershipID, userID: row.UserID, handle: row.Handle, garden: row.GardenName}
		n := sittingEndedNotification(row, d.peopleURL)
		err := d.send(ctx, to, sittingEndedKind, sittingKey(row.SittingID, row.EndsAt), n, now)
		if err != nil && failed(err, "sitting ended not sent", "user", row.Handle, "garden", row.GardenName, "sitter", row.SitterName) {
			return time.Time{}, err
		}
	}
	return next, nil
}

// tokenNamed is the token's name followed by its prefix in brackets, the two
// things the Tokens list shows for it.
func tokenNamed(token store.APIToken) string {
	return token.Name + " (" + token.Prefix + "…)"
}

// tokenExpiringNotification reads "The kitchen display (sprg_7c1f…) in
// Ellie’s Rosewood expires on 10 Sep." with the date in the recipient's
// timezone, and opens Tokens.
func tokenExpiringNotification(garden string, token store.APIToken, loc *time.Location, tokensURL string) Notification {
	return Notification{Title: "Token expires soon", Body: tokenNamed(token) + " in " + garden + " expires on " + token.ExpiresAt.In(loc).Format("2 Jan") + ".", URL: tokensURL}
}

// tokenExpiredNotification reads "The kitchen display (sprg_7c1f…) in Ellie’s
// Rosewood has expired." and opens Tokens.
func tokenExpiredNotification(garden string, token store.APIToken, tokensURL string) Notification {
	return Notification{Title: "Token expired", Body: tokenNamed(token) + " in " + garden + " has expired.", URL: tokensURL}
}

// sittingEndedNotification is what one recipient is told when a sitter's
// access to a garden ends. The sitter's reads "Your access to Ellie’s Rosewood
// has ended." and opens nothing, because every page would send them to sign
// in. The inviter's reads "Sam no longer has access to Rosewood." and opens
// People. That line avoids two possessives in a row.
func sittingEndedNotification(row store.ListSittingDeadlinesRow, peopleURL string) Notification {
	garden := GardenNamed(row.GardenName, row.OwnerName, row.RecipientOwns)
	if row.IsSitter {
		return Notification{Title: "Access ended", Body: "Your access to " + garden + " has ended."}
	}
	return Notification{Title: "Access ended", Body: row.SitterName + " no longer has access to " + garden + ".", URL: peopleURL}
}

// sittingKey is the send key of a sitting's notification: the membership id
// and its end date. A membership renewed past a date already sent for gets a
// new key, so its next end is sent for too.
func sittingKey(membershipID uuid.UUID, endsAt time.Time) string {
	return membershipID.String() + " " + endsAt.UTC().Format(time.RFC3339)
}

// recipient is the member one notification goes to. handle and garden appear
// in the log line. gardenID is set only for a digest or a reminder. Both list
// what is due in that garden.
type recipient struct {
	membershipID uuid.UUID
	gardenID     uuid.UUID
	userID       uuid.UUID
	handle       string
	garden       string
}

// send claims the recipient's ledger row for kind and key and sends n to each
// of their browsers, in one transaction. The claim comes first, so a row an
// earlier look wrote stops this send. A process that dies part-way commits
// nothing and sends again on restart.
//
// An error from the push service is logged and the ledger row stays written:
// nothing retries, so a second send would only fail the same way.
func (d *Deadlines) send(ctx context.Context, to recipient, kind, key string, n Notification, now time.Time) error {
	return d.queries.InTx(ctx, func(q *store.Queries) error {
		claimed, err := q.ClaimNotificationSend(ctx, store.ClaimNotificationSendParams{
			MembershipID: to.membershipID,
			Kind:         kind,
			SendKey:      key,
		})
		if err != nil {
			return fmt.Errorf("claiming the send: %w", err)
		}
		if claimed == 0 {
			return nil
		}
		subscriptions, err := q.ListPushSubscriptions(ctx, to.userID)
		if err != nil {
			return fmt.Errorf("listing the browsers: %w", err)
		}
		sent, err := deliver(ctx, d.logger, q, d.sender, to.handle, subscriptions, n, now)
		if err != nil {
			return err
		}
		d.logger.Info("deadline notification sent", "kind", kind, "user", to.handle, "garden", to.garden, "key", key, "browsers", sent)
		return nil
	})
}
