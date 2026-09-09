package http

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/build"
	"github.com/ismailshak/sprig/internal/photo"
	"github.com/ismailshak/sprig/internal/store"
)

const (
	morePath          = "/more"
	accountPath       = morePath + "/account"
	recoveryPath      = accountPath + "/recovery"
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
	logger   *slog.Logger
	sessions *auth.Sessions
	queries  *store.Queries
	// photos supplies the Garden page's photo storage figure, and deletes a
	// garden's photo files when the garden is deleted.
	photos    *photo.Store
	templates *Templates
	build     build.Info
	// now supplies the current time, so a test can fix the day.
	now func() time.Time
	// pushKey is the VAPID public key the Notifications page gives the browser
	// to subscribe with. It is empty when push is off, and the page then says
	// notifications are not enabled.
	pushKey string
	wake    wakeDigest
	// test sends the Notifications page's test message to one browser. It is
	// nil when push is off.
	test sendTest
}

type morePage struct {
	Rows []linkRow
	// Version and Revision are the running binary's, shown as the last line of
	// the page. Revision is empty in a binary built outside a git working tree.
	Version  string
	Revision string
}

// topbar is the bar at the top of every page under More: a link back above the
// page's title. The title is in the bar because a word like "Passkeys" appears
// nowhere else on the page.
type topbar struct {
	// Href is the URL the back link points at, and Back the word on it.
	Href  string
	Back  string
	Title string
}

// moreBar is the top bar for a page reached from More's index, with its back
// link pointing there.
func moreBar(title string) topbar {
	return topbar{Href: morePath, Back: "More", Title: title}
}

// linkRow is a row in a settings list, rendered as a label, a chevron and
// sometimes a note. More's index is a list of them. Account has one, linking
// to Recovery codes.
type linkRow struct {
	Label string
	Href  string
	// Note is the text at the right-hand end of the row. It is empty unless the
	// page behind the row has something outstanding: notifications that are
	// off, an invite nobody has taken up, or no recovery codes.
	Note string
}

// moreState is what the index needs beyond the reader's role.
type moreState struct {
	notificationsOn bool
	pendingInvites  int
	// noCodesLeft is true when the signed-in person manages the garden's people
	// and holds no unused recovery code. Only someone who manages people is
	// prompted for codes, because there is nobody above them to send a new
	// invite.
	noCodesLeft bool
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

	// The People row and the Account row's note are both only rendered for
	// someone who can manage people, so a member's index does not pay for
	// either query.
	if principal.Can(auth.MemberManage) {
		pending, err := h.queries.CountPendingInvites(ctx, principal.Garden.ID, h.now())
		if err != nil {
			return moreState{}, fmt.Errorf("count the pending invites: %w", err)
		}
		state.pendingInvites = int(pending)

		batch, live, err := h.recoveryBatch(ctx, principal.User.ID)
		if err != nil {
			return moreState{}, err
		}
		state.noCodesLeft = !live || batch.Unused == 0
	}
	return state, nil
}

// newMorePage builds the index. A row the reader's role cannot use is left out
// rather than shown and refused when pressed, so a sitter gets the first three
// rows and the links below them.
func newMorePage(principal auth.Principal, state moreState, info build.Info) morePage {
	rows := []linkRow{
		{Label: "Account", Href: accountPath, Note: codesNote(state.noCodesLeft)},
		{Label: "Passkeys", Href: passkeysPath},
		{Label: "Notifications", Href: notificationsPath, Note: offNote(state.notificationsOn)},
	}
	if principal.Can(auth.GardenEdit) {
		rows = append(rows, linkRow{Label: "Garden", Href: gardenPath})
	}
	if principal.Can(auth.MemberManage) {
		rows = append(rows, linkRow{Label: "People", Href: peoplePath, Note: pendingNote(state.pendingInvites)})
	}
	if principal.Can(auth.TokenManage) {
		rows = append(rows, linkRow{Label: "Tokens", Href: tokensPath})
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
