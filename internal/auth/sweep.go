package auth

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ismailshak/sprig/internal/store"
)

// inviteGrace is how long a redeemed or expired invite is kept before the
// sweep deletes it. It is a month so that a link somebody reports as not
// working can still be found in the table.
const inviteGrace = 30 * 24 * time.Hour

// SweepInterval is how long the server waits between sweeps.
const SweepInterval = 24 * time.Hour

// SweepReport counts the rows one sweep deleted.
type SweepReport struct {
	Sessions      int64
	Invites       int64
	RecoveryCodes int64
}

// Sweep deletes the rows nothing else deletes and no page shows: sessions
// last used a sessionTTL or more ago, redeemed invites and expired sign-in
// links older than inviteGrace, and recovery codes from a batch a newer batch
// replaced. Each holds the hash of a credential that no longer works.
//
// Anything a page still lists is left: an expired API token on Tokens, an
// ended membership on People, an expired join invite under Pending invites,
// each with the button that removes it. A push subscription is left too,
// because nothing tells a dead subscription from a live one until a send is
// tried.
func Sweep(ctx context.Context, q *store.Queries, now time.Time, sessionTTL time.Duration) (SweepReport, error) {
	var report SweepReport
	var err error
	report.Sessions, err = q.DeleteExpiredSessions(ctx, now.Add(-sessionTTL))
	if err != nil {
		return report, fmt.Errorf("delete the expired sessions: %w", err)
	}
	report.Invites, err = q.DeleteRedeemedAndExpiredInvites(ctx, now.Add(-inviteGrace))
	if err != nil {
		return report, fmt.Errorf("delete the spent invites: %w", err)
	}
	report.RecoveryCodes, err = q.DeleteSupersededRecoveryCodes(ctx)
	if err != nil {
		return report, fmt.Errorf("delete the superseded recovery codes: %w", err)
	}
	return report, nil
}

// Sweeper runs Sweep in the server process, once at startup and then every
// SweepInterval. It runs there rather than from a scheduled command, because
// nothing on the host schedules anything.
type Sweeper struct {
	logger     *slog.Logger
	queries    *store.Queries
	sessionTTL time.Duration
	interval   time.Duration
	now        func() time.Time
}

// NewSweeper returns a Sweeper that sweeps every SweepInterval. sessionTTL is
// the server's session lifetime, so the sweep and Sessions.Lookup treat the
// same sessions as expired.
func NewSweeper(logger *slog.Logger, queries *store.Queries, sessionTTL time.Duration) *Sweeper {
	return &Sweeper{
		logger:     logger,
		queries:    queries,
		sessionTTL: sessionTTL,
		interval:   SweepInterval,
		now:        time.Now,
	}
}

// Run sweeps now and then every interval until ctx is done. A failed sweep is
// logged and the next one runs on time, because the rows will still be there.
func (s *Sweeper) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		s.sweep(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// sweep runs one Sweep and logs what it deleted. A sweep that deleted nothing
// logs nothing.
func (s *Sweeper) sweep(ctx context.Context) {
	report, err := Sweep(ctx, s.queries, s.now(), s.sessionTTL)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		s.logger.Error("sweep failed", "err", err)
		return
	}
	if report != (SweepReport{}) {
		s.logger.Info("swept", "sessions", report.Sessions, "invites", report.Invites, "recovery_codes", report.RecoveryCodes)
	}
}
