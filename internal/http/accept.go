package http

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// acceptPattern is the mux pattern of the two routes that accept an invite as
// the account already signed in. Both require a session. The invite page they
// are reached from does not, and it links here for somebody who already has an
// account.
const acceptPattern = invitedPattern + acceptSuffix

// acceptSuffix is what follows the token in an accept page's path.
const acceptSuffix = "/accept"

// acceptPath is the URL of the page that accepts the invite for token as the
// account signed in. The form on it posts to the same URL.
func acceptPath(token string) string { return invitedPath(token) + acceptSuffix }

// signInToAcceptPath is the URL of the sign-in page with its next parameter
// set to the accept page for token, so signing in there redirects to it.
func signInToAcceptPath(token string) string {
	return signInPath + "?" + nextField + "=" + url.QueryEscape(acceptPath(token))
}

// acceptPage is the page a signed-in account accepts an invite on. It renders
// one of three things: the sentence for a link that cannot be used, the
// sentence for an account already in the garden, or the Join button.
type acceptPage struct {
	// Unusable is true when the link cannot be redeemed, whatever the reason.
	// The page is then a heading, one sentence and a link back to Today.
	Unusable bool
	// Current is the name of the garden the session is on, and empty when the
	// account is in no garden. The link back to Today reads "Back to" that
	// name when it is set, and "Back" when it is not.
	Current string
	// AlreadyIn is true when the account has a live membership of the invite's
	// garden. The page then offers Open in place of Join. The invite is not
	// marked used.
	AlreadyIn bool
	// Garden is the name of the garden the link joins. By is the display name
	// of the person who created the invite. Name is the display name of the
	// account signed in.
	Garden string
	By     string
	Name   string
	// Joining is the sentence under the heading. It names the role, what the
	// role can do, and the day access ends when the invite sets one.
	Joining string
	// Action is the URL the Join button's form posts to.
	Action string
	// Switch is the URL the Open button's form posts to when AlreadyIn is true.
	// It is POST /gardens, the same post the garden sheet makes to move the
	// session. Field is the name of the hidden input holding GardenID.
	Switch   string
	Field    string
	GardenID string
	// SignIn is the URL the "Sign in as somebody else" link points at. It goes
	// to the sign-in page. Signing in there redirects back to this page.
	SignIn string
}

// showAccept handles GET /invite/{token}/accept.
func (h *invited) showAccept(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	open, usable, err := h.openToAccept(r)
	if err != nil {
		serverError(h.logger, w, r, "open the invite", err)
		return
	}
	if !usable {
		h.renderCannotAccept(w, r, principal)
		return
	}
	_, live, err := existingMembership(r.Context(), h.queries, h.now(), open.row.Invite.GardenID, principal.User.ID)
	if err != nil {
		serverError(h.logger, w, r, "read the membership", err)
		return
	}
	page := newAcceptPage(r.PathValue("token"), principal, open)
	page.AlreadyIn = live
	h.templates.render(w, r, view{page: "accept"}, page)
}

// accept handles POST /invite/{token}/accept. It writes the membership the
// invite describes for the account signed in, marks the invite used, moves
// the session onto that garden and redirects to Today. No account and no
// passkey is written, because the account has both already.
func (h *invited) accept(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	open, usable, err := h.openToAccept(r)
	if err != nil {
		serverError(h.logger, w, r, "accept the invite", err)
		return
	}
	if !usable {
		h.renderCannotAccept(w, r, principal)
		return
	}
	existing, live, err := existingMembership(r.Context(), h.queries, h.now(), open.row.Invite.GardenID, principal.User.ID)
	if err != nil {
		serverError(h.logger, w, r, "read the membership", err)
		return
	}
	if live {
		page := newAcceptPage(r.PathValue("token"), principal, open)
		page.AlreadyIn = true
		h.templates.render(w, r, view{page: "accept", status: http.StatusUnprocessableEntity}, page)
		return
	}

	invite := open.row.Invite
	err = h.queries.InTx(r.Context(), func(q *store.Queries) error {
		ctx := r.Context()
		if err := writeAcceptance(ctx, q, h.now(), invite, existing, principal.User.ID); err != nil {
			return err
		}
		// The session moves onto the new garden, the same write the garden
		// sheet makes. The garden is recorded on the account as well, so the
		// next session starts there.
		if _, err := q.SetSessionGarden(ctx, &invite.GardenID, principal.Session.TokenHash); err != nil {
			return err
		}
		return q.SetLastGarden(ctx, &invite.GardenID, principal.User.ID)
	})
	if errors.Is(err, errInviteUsed) {
		h.renderCannotAccept(w, r, principal)
		return
	}
	if err != nil {
		serverError(h.logger, w, r, "accept the invite", err)
		return
	}
	// The account may already have a subscribed browser, so the new membership
	// can be due a digest at once.
	h.wake.call()
	http.Redirect(w, r, todayPath, http.StatusSeeOther)
}

// openToAccept reads the invite for the request and reports whether it can be
// accepted. It refuses what open refuses, plus a re-enrolment link, because
// that link adds a device to the account it names and grants nothing to the
// account signed in.
func (h *invited) openToAccept(r *http.Request) (openInvite, bool, error) {
	open, usable, err := h.open(r)
	if err != nil || !usable {
		return open, usable, err
	}
	if open.reenrol() {
		return openInvite{}, false, nil
	}
	return open, true, nil
}

// writeAcceptance marks invite used and writes the membership it describes for
// userID, through q. existing is the membership userID already holds on the
// garden, or nil. A membership that ended still has a row, so it is updated
// rather than inserted. It returns errInviteUsed when the invite was redeemed
// or ran out before the row lock was taken.
func writeAcceptance(ctx context.Context, q *store.Queries, now time.Time, invite store.Invite, existing *store.Membership, userID uuid.UUID) error {
	if err := markRedeemed(ctx, q, now, invite); err != nil {
		return err
	}
	write := createMembership
	if existing != nil {
		write = renewMembership
	}
	_, err := write(ctx, q, newMembership{
		GardenID:  invite.GardenID,
		UserID:    userID,
		Role:      invite.Role,
		InvitedBy: &invite.CreatedBy,
		ExpiresAt: invite.MembershipExpiresAt,
	})
	return err
}

// existingMembership returns the membership userID already holds on gardenID,
// or nil, and whether it is live at now.
func existingMembership(ctx context.Context, queries *store.Queries, now time.Time, gardenID, userID uuid.UUID) (*store.Membership, bool, error) {
	row, err := queries.GetMembershipWithUserAndGarden(ctx, gardenID, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &row.Membership, !auth.MembershipEnded(row.Membership, now), nil
}

// newAcceptPage builds the page for open as principal, with AlreadyIn left
// false for the caller to set. The end date in Joining is read in the
// account's own timezone, because the person accepting has one.
func newAcceptPage(token string, principal auth.Principal, open openInvite) acceptPage {
	invite := open.row.Invite
	return acceptPage{
		Current:  principal.Garden.Name,
		Garden:   open.row.Garden.Name,
		By:       open.row.AppUser.DisplayName,
		Name:     principal.User.DisplayName,
		Joining:  joiningSentence(invite.Role, invite.MembershipExpiresAt, locationFor(principal.User)),
		Action:   acceptPath(token),
		Switch:   gardensPath,
		Field:    gardenField,
		GardenID: invite.GardenID.String(),
		SignIn:   signInToAcceptPath(token),
	}
}

// renderCannotAccept renders the page for a link that cannot be accepted, with
// a 404. Every reason gets the same page and the same status, so the response
// cannot tell somebody trying tokens which ones were real.
func (h *invited) renderCannotAccept(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	h.templates.render(w, r, view{page: "accept", status: http.StatusNotFound}, acceptPage{Unusable: true, Current: principal.Garden.Name})
}
