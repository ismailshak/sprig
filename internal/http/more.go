package http

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/build"
	"github.com/ismailshak/sprig/internal/store"
)

const (
	morePath          = "/more"
	accountPath       = morePath + "/account"
	passkeysPath      = morePath + "/passkeys"
	notificationsPath = morePath + "/notifications"
	gardenPath        = morePath + "/garden"
	peoplePath        = morePath + "/people"
	tokensPath        = morePath + "/tokens"
	installPath       = "/install"
	signOutPath       = "/signout"
)

// more serves the fourth tab: the index of everything that is not about
// plants, and the pages behind it.
type more struct {
	logger    *slog.Logger
	sessions  *auth.Sessions
	queries   *store.Queries
	templates *Templates
	build     build.Info
	// now supplies the current time, so a test can fix the day.
	now func() time.Time
}

type morePage struct {
	Rows []moreRow
	// Version and Revision are the running binary's, shown as the last line of
	// the page. Revision is empty in a binary built outside a git working tree.
	Version  string
	Revision string
}

type moreRow struct {
	Label string
	Href  string
	// Note is the text at the right-hand end of the row. It is empty unless the
	// page behind the row has something outstanding: notifications that are
	// off, or an invite nobody has taken up.
	Note string
}

// moreState is what the index needs beyond the reader's role.
type moreState struct {
	notificationsOn bool
	waitingInvites  int
}

func (h *more) show(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	state, err := h.state(r.Context(), principal)
	if err != nil {
		serverError(h.logger, w, r, "load the More index", err)
		return
	}
	h.templates.render(w, r, view{page: "more"}, newMorePage(principal, state, h.build))
}

func (h *more) state(ctx context.Context, principal auth.Principal) (moreState, error) {
	preferences, err := h.queries.ListNotificationPreferences(ctx, principal.Membership.ID)
	if err != nil {
		return moreState{}, fmt.Errorf("read the notification preferences: %w", err)
	}
	state := moreState{notificationsOn: anyNotificationOn(preferences)}

	// Only the People row reports waiting invites, and only an owner has that
	// row, so a member's index does not pay for the count.
	if principal.Can(auth.MemberManage) {
		waiting, err := h.queries.CountWaitingInvites(ctx, principal.Garden.ID, h.now())
		if err != nil {
			return moreState{}, fmt.Errorf("count the waiting invites: %w", err)
		}
		state.waitingInvites = int(waiting)
	}
	return state, nil
}

// newMorePage builds the index. A row the reader's role cannot use is left out
// rather than shown and refused when pressed, so a sitter gets the first three
// rows and the foot.
func newMorePage(principal auth.Principal, state moreState, info build.Info) morePage {
	rows := []moreRow{
		{Label: "Account", Href: accountPath},
		{Label: "Passkeys", Href: passkeysPath},
		{Label: "Notifications", Href: notificationsPath, Note: offNote(state.notificationsOn)},
	}
	if principal.Can(auth.GardenEdit) {
		rows = append(rows, moreRow{Label: "Garden", Href: gardenPath})
	}
	if principal.Can(auth.MemberManage) {
		rows = append(rows, moreRow{Label: "People", Href: peoplePath, Note: waitingNote(state.waitingInvites)})
	}
	if principal.Can(auth.TokenManage) {
		rows = append(rows, moreRow{Label: "Tokens", Href: tokensPath})
	}
	return morePage{Rows: rows, Version: info.Version, Revision: shortRevision(info.Revision)}
}

// signOut deletes this session's row and clears the cookie. A cookie kept
// after signing out resolves to nothing.
func (h *more) signOut(w http.ResponseWriter, r *http.Request) {
	if err := h.sessions.Delete(r.Context(), h.sessions.TokenFromRequest(r)); err != nil {
		serverError(h.logger, w, r, "end the session", err)
		return
	}
	http.SetCookie(w, h.sessions.ClearedCookie())
	http.Redirect(w, r, signInPath, http.StatusSeeOther)
}
