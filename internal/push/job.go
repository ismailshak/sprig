package push

import (
	"context"
	"log/slog"
	"time"
)

// retryAfter is how long a job waits before looking again after a database
// error.
const retryAfter = time.Minute

// runJob calls look, sleeps until the instant it returns, and calls it again,
// until ctx is done. A wake on the channel ends the sleep early. With a zero
// instant nothing is coming and the job sleeps until a wake. An error from
// look is logged under name and look is called again after retryAfter.
//
// The timer runs to the next send rather than on a fixed interval, so a send
// is never up to an interval late.
func runJob(ctx context.Context, logger *slog.Logger, name string, now func() time.Time, wake <-chan struct{}, look func(context.Context) (time.Time, error)) {
	for {
		next, err := look(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Error(name+" job failed", "err", err)
			next = now().Add(retryAfter)
		}

		// fire stays nil for a zero next, so the select waits for a wake or
		// for ctx to end.
		var timer *time.Timer
		var fire <-chan time.Time
		if !next.IsZero() {
			timer = time.NewTimer(next.Sub(now()))
			fire = timer.C
		}
		select {
		case <-ctx.Done():
		case <-wake:
		case <-fire:
		}
		if timer != nil {
			timer.Stop()
		}
		if ctx.Err() != nil {
			return
		}
	}
}

// wakeJob sends on wake without blocking. The channel is buffered to one,
// because two changes before the job next looks need only one look.
func wakeJob(wake chan<- struct{}) {
	select {
	case wake <- struct{}{}:
	default:
	}
}

// locationOf loads the timezone on a member's account row. The Account page
// only offers names the zone database knows, so an unknown one means the row
// was written some other way. It is logged and read as UTC, the fallback the
// pages use for the same column.
func locationOf(logger *slog.Logger, handle, timezone string) *time.Location {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		logger.Warn("timezone unknown", "user", handle, "timezone", timezone)
		return time.UTC
	}
	return loc
}
