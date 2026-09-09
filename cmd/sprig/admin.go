package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ismailshak/sprig/internal/auth"
	sprighttp "github.com/ismailshak/sprig/internal/http"
	"github.com/ismailshak/sprig/internal/store"
)

// admin runs sprig admin invite, the one command it has. There is no HTTP
// route for it, because anyone who can run the binary already controls the
// server.
func admin(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "invite" {
		return fmt.Errorf("unknown admin command %q: the one command is invite", strings.Join(args, " "))
	}
	flags := flag.NewFlagSet("sprig admin invite", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	handle := flags.String("user", "", "the handle of the account the link adds a passkey to")
	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("sprig admin invite: %w", err)
	}
	if *handle == "" || flags.NArg() > 0 {
		return errors.New("sprig admin invite takes --user <handle> and nothing else")
	}
	return adminInvite(ctx, getenv, stdout, *handle)
}

// adminInvite issues a re-enrolment link for the account with handle and
// prints it with the date it expires.
func adminInvite(ctx context.Context, getenv func(string) string, stdout io.Writer, handle string) error {
	cfg, err := loadConfig(getenv)
	if err != nil {
		return err
	}
	pool, err := store.Open(ctx, cfg.databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	made, err := auth.IssueReenrolment(ctx, store.New(pool), time.Now(), handle)
	switch {
	case errors.Is(err, auth.ErrUnknownHandle):
		return fmt.Errorf("no open account has the handle %q", handle)
	case errors.Is(err, auth.ErrNoLiveMembership):
		return fmt.Errorf("%s is not a member of any garden, so there is no account to sign in to: invite them to a garden from its People page instead", handle)
	case err != nil:
		return err
	}
	expires := made.Invite.ExpiresAt.In(locationOf(made.User.Timezone)).Format("2 January 2006")
	_, err = fmt.Fprintf(stdout, "%s\nAdds a passkey to %s's account. Works once, until %s.\n",
		cfg.baseURL.JoinPath(sprighttp.InvitedPath(made.Token)), made.User.DisplayName, expires)
	return err
}

// locationOf returns the time zone named zone, or UTC when no zone has that
// name, so a printed date is never missing.
func locationOf(zone string) *time.Location {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.UTC
	}
	return loc
}
