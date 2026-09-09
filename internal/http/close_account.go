package http

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// closeAccountPath is the URL of the Close account page. A GET renders the
// confirmation and a POST closes the account.
const closeAccountPath = accountPath + "/close"

// errSoleOwner is returned by closeUserAccount when the account is the only
// owner of a garden. Closing it would leave that garden with nobody who can
// manage it.
var errSoleOwner = errors.New("the account is the only owner of a garden")

type closeAccountPage struct {
	Bar    topbar
	Action string
	// InGarden is false when the account is in no garden. The page then leaves
	// out the tab bar, because every tab needs a garden.
	InGarden bool
	// SoleOwnerNotice is the sentence naming the gardens this account is the
	// only owner of. The page renders no Close account button while it is set.
	SoleOwnerNotice string
}

func (h *more) newCloseAccountPage(ctx context.Context, principal auth.Principal) (closeAccountPage, error) {
	page := closeAccountPage{
		Bar:      topbar{Href: accountPath, Back: "Account", Title: "Close account"},
		Action:   closeAccountPath,
		InGarden: principal.InGarden(),
	}
	if !page.InGarden {
		page.Bar.Href, page.Bar.Back = todayPath, "Back"
	}
	owned, err := h.queries.ListGardensOnlyThisUserOwns(ctx, principal.User.ID, h.now())
	if err != nil {
		return closeAccountPage{}, err
	}
	page.SoleOwnerNotice = soleOwnerNotice(owned)
	return page, nil
}

func soleOwnerNotice(gardens []store.Garden) string {
	if len(gardens) == 0 {
		return ""
	}
	names := make([]string, 0, len(gardens))
	for _, g := range gardens {
		names = append(names, g.Name)
	}
	it := "it"
	if len(names) > 1 {
		it = "them"
	}
	return "You’re the only owner of " + andList(names) + ". Delete " + it + " before closing your account."
}

// andList joins names as "Home", "Home and Upstairs" or "Home, Upstairs and
// Loft".
func andList(names []string) string {
	if len(names) <= 1 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// confirmCloseAccount handles GET /more/account/close.
func (h *more) confirmCloseAccount(w http.ResponseWriter, r *http.Request) {
	page, err := h.newCloseAccountPage(r.Context(), PrincipalFrom(r))
	if err != nil {
		serverError(h.logger, w, r, "open the close account page", err)
		return
	}
	h.templates.render(w, r, view{page: "close-account"}, page)
}

// closeAccount handles POST /more/account/close. The sole-owner check runs in
// the same transaction as the deletes. A refused close writes nothing.
func (h *more) closeAccount(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	err := h.queries.InTx(r.Context(), func(q *store.Queries) error {
		return closeUserAccount(r.Context(), q, principal.User.ID, h.now)
	})
	if errors.Is(err, errSoleOwner) {
		page, err := h.newCloseAccountPage(r.Context(), principal)
		if err != nil {
			serverError(h.logger, w, r, "open the close account page", err)
			return
		}
		h.templates.render(w, r, view{page: "close-account", status: http.StatusUnprocessableEntity}, page)
		return
	}
	if err != nil {
		serverError(h.logger, w, r, "close the account", err)
		return
	}
	// The digest job works out its next send again, because the memberships it
	// was going to send to are gone.
	h.wake.call()
	http.SetCookie(w, h.sessions.ClearedCookie())
	http.Redirect(w, r, signInPath, http.StatusSeeOther)
}

// closeUserAccount deletes the account's credentials, sessions and memberships,
// then marks the row closed. The row is kept so care events and photos still
// show the person's name.
func closeUserAccount(ctx context.Context, q *store.Queries, userID uuid.UUID, now func() time.Time) error {
	owned, err := q.ListGardensOnlyThisUserOwns(ctx, userID, now())
	if err != nil {
		return err
	}
	if len(owned) > 0 {
		return errSoleOwner
	}
	for _, del := range []func(context.Context, uuid.UUID) error{
		q.DeleteUserPasskeys,
		q.DeleteRecoveryCodes,
		q.DeleteUserPushSubscriptions,
		q.DeleteUserSessions,
		q.DeleteUserCeremonies,
		q.DeleteUserReenrolmentInvites,
		q.DeleteUserMemberships,
	} {
		if err := del(ctx, userID); err != nil {
			return err
		}
	}
	closed, err := q.CloseAccount(ctx, now(), userID)
	if err != nil {
		return err
	}
	if closed == 0 {
		return errors.New("the account is already closed")
	}
	return nil
}
