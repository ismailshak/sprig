package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// invitedPattern is the mux pattern the three invite routes are registered
// under. The token in the URL is the plaintext of the link. The invite row
// holds only its hash.
const invitedPattern = "/invite/{token}"

// InvitedPath is the URL of the page for one token. The form on it posts to
// the same URL. It is exported because sprig admin invite prints the link.
func InvitedPath(token string) string { return "/invite/" + token }

// invitedChallengePath is the URL the page's script posts the form's fields
// to for a registration challenge.
func invitedChallengePath(token string) string { return InvitedPath(token) + "/challenge" }

const (
	// tooManyInviteAttempts is the message shown when either rate limiter refuses
	// a request.
	tooManyInviteAttempts = "Too many attempts. Wait a few minutes and try again."
	addDeviceLabel        = "Add this device"
)

// errInviteUsed is returned inside the transaction that redeems an invite when
// the row had already been redeemed or had expired by the time the post
// arrived.
var errInviteUsed = errors.New("the invite has been used or has run out")

// openInvite is an invite that can still be redeemed. row holds the invite,
// its garden and the account that created it.
type openInvite struct {
	row store.GetInviteByTokenHashRow
	// member is the account a re-enrolment link adds a passkey to. It is the
	// zero value for a join invite, whose account is created by the post.
	member store.AppUser
}

func (o openInvite) reenrol() bool { return o.row.Invite.UserID != nil }

type joinForm struct {
	name string
	// handle is the Handle field in its stored form. It is empty when the field
	// was left blank, and the account then gets the handle made from its
	// display name.
	handle string
	zone   string
}

func readJoinForm(r *http.Request) joinForm {
	return joinForm{
		name:   strings.TrimSpace(r.PostForm.Get("name")),
		handle: store.NormaliseHandle(r.PostForm.Get("handle")),
		zone:   r.PostForm.Get("timezone"),
	}
}

// handleOrDefault is the handle the account is created with: the one typed,
// or the display name's when the field was left blank.
func (f joinForm) handleOrDefault() string {
	if f.handle != "" {
		return f.handle
	}
	return store.HandleFor(f.name)
}

// errors returns the message to show under each empty field, and "" for a
// field that was filled in.
func (f joinForm) errors() (name, zone string) {
	if f.name == "" {
		name = setupNameMissing
	}
	if f.zone == "" {
		zone = zoneMissing
	}
	return name, zone
}

func (f joinForm) valid() bool {
	name, zone := f.errors()
	return name == "" && zone == ""
}

// invitedPage is the page an invite link opens, in one of three shapes: the
// join form, the re-enrolment form, or the sentence for a link that cannot be
// used.
type invitedPage struct {
	// Unusable is true when the link cannot be redeemed, whatever the reason.
	// The page is then a heading and one sentence, with no form.
	Unusable bool
	// SignIn is the URL the sign in link in that sentence points at.
	SignIn string
	// Reenrol is true for a link that adds a device to an account already in
	// the garden. The form then has no fields.
	Reenrol bool
	// By is the display name of the person who created the invite, or empty
	// for a re-enrolment link the operator made with sprig admin invite.
	// Garden is the name of the garden the link joins.
	By        string
	Garden    string
	Name      string
	NameError string
	Handle    string
	// HandleError is shown under Handle when another account already holds
	// the one typed. A blank handle is not an error, because the display
	// name's handle is used instead.
	HandleError string
	// Suggest is the URL the page's script GETs a free handle from.
	Suggest string
	Zone    timezoneField
	// Refusal is the message above the form: why the passkey was not created,
	// or that too many posts have been made. It is empty until a post is
	// refused.
	Refusal string
	// Action is the URL the form posts to. Challenge is the URL the page's
	// script posts the form to for a registration challenge.
	Action    string
	Challenge string
	// Field is the name of the hidden input the browser's credential goes in.
	Field string
	// Joining is the sentence under the join form. It names the role, what the
	// role can do, and the day access ends when the invite sets one.
	Joining string
	// SignInToJoin is the URL the "Sign in to join as yourself" link points at.
	// It goes to the sign-in page. Signing in there redirects to the page that
	// accepts this invite as the account signed in.
	SignInToJoin string
}

func unusableInvitePage() invitedPage {
	return invitedPage{Unusable: true, SignIn: signInPath}
}

// newInvitedPage fills the page for open from form. propose is true on a form
// nobody has posted yet, so the page's script selects the browser's own zone.
// It is false when re-rendering a refused post, so the zone the person chose
// stays.
func newInvitedPage(token string, open openInvite, form joinForm, propose bool) invitedPage {
	invite := open.row.Invite
	page := invitedPage{
		Reenrol:   open.reenrol(),
		By:        open.row.AppUser.DisplayName,
		Garden:    open.row.Garden.Name,
		Name:      form.name,
		Handle:    form.handle,
		Suggest:   handlePath,
		Zone:      timezoneField{Zones: zoneOptions(form.zone), Propose: propose},
		Action:    InvitedPath(token),
		Challenge: invitedChallengePath(token),
		Field:     credentialField,
	}
	// sprig admin invite sets created_by to the account the link is for.
	// Emptying By makes the page say the link signs you in, instead of
	// naming the person as the sender of their own link.
	if page.Reenrol && invite.CreatedBy == open.member.ID {
		page.By = ""
	}
	if !page.Reenrol {
		page.SignInToJoin = signInToAcceptPath(token)
		// The date is read in the inviter's zone, because that is the zone it
		// was chosen in and the person joining has no account to read it in
		// yet.
		page.Joining = joiningSentence(invite.Role, invite.MembershipExpiresAt, locationFor(open.row.AppUser))
	}
	return page
}

// invited serves the page an invite link opens: the page, the registration
// challenge its script asks for, and the post that redeems the link. A join
// invite creates the account, the membership and the passkey in one
// transaction and redirects to Install sprig. A re-enrolment invite adds a
// passkey to the account it names and redirects to Today. Both sign the device
// in.
type invited struct {
	logger   *slog.Logger
	passkeys *auth.Passkeys
	sessions *auth.Sessions
	// resolver turns the session cookie a browser opens a join link with into
	// the account behind it, so the page can send that account to accept the
	// invite as itself.
	resolver  *auth.Resolver
	queries   *store.Queries
	templates *Templates
	// now supplies the current time, so a test can fix the day.
	now  func() time.Time
	wake wakeJobs
	// notify tells the person who created an invite that it was accepted.
	notify notifyUser
}

// open looks up the token in the path and reports whether the link can be
// redeemed.
func (h *invited) open(r *http.Request) (openInvite, bool, error) {
	return openInviteToken(r.Context(), h.queries, h.now(), r.PathValue("token"))
}

// openInviteToken looks up the invite for token and reports whether the link
// can be redeemed. It cannot be when it was never issued or has been revoked,
// has expired, has been redeemed, is a join invite whose membership would
// already have ended, or is a re-enrolment for somebody who is no longer a
// member of the garden. The caller renders the same page for all of them.
func openInviteToken(ctx context.Context, queries *store.Queries, now time.Time, token string) (openInvite, bool, error) {
	row, err := queries.GetInviteByTokenHash(ctx, auth.HashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return openInvite{}, false, nil
	}
	if err != nil {
		return openInvite{}, false, err
	}
	invite := row.Invite
	if invite.RedeemedAt != nil || !now.Before(invite.ExpiresAt) {
		return openInvite{}, false, nil
	}
	if invite.UserID == nil {
		if invite.MembershipExpiresAt != nil && !now.Before(*invite.MembershipExpiresAt) {
			return openInvite{}, false, nil
		}
		return openInvite{row: row}, true, nil
	}

	// A re-enrolment link is issued from a member's row. That membership may
	// have been deleted or ended since, and the link is then unusable.
	member, err := queries.GetMembershipWithUserAndGarden(ctx, invite.GardenID, *invite.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return openInvite{}, false, nil
	}
	if err != nil {
		return openInvite{}, false, err
	}
	if auth.MembershipEnded(member.Membership, now) {
		return openInvite{}, false, nil
	}
	return openInvite{row: row, member: member.AppUser}, true, nil
}

// show handles GET /invite/{token}. A browser that opens a join link while
// signed in is redirected to the page that accepts the invite as that
// account, because the form here would make a second account for a person
// who has one. The route is public and the session is only read when the
// browser sent a cookie.
func (h *invited) show(w http.ResponseWriter, r *http.Request) {
	open, usable, err := h.open(r)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "open the invite", err)
		return
	}
	if !usable {
		h.renderUnusable(w, r)
		return
	}
	token := r.PathValue("token")
	if !open.reenrol() && hasSession(r, h.sessions, h.resolver, h.now()) {
		http.Redirect(w, r, acceptPath(token), http.StatusSeeOther)
		return
	}
	h.templates.render(w, r, view{page: "invited"}, newInvitedPage(token, open, freshJoinForm(open), true))
}

// freshJoinForm is the join form before anybody has posted it. The timezone is
// the inviter's, because the person joining is usually in the same place. The
// page's script replaces it with the browser's own zone where it knows one.
func freshJoinForm(open openInvite) joinForm {
	return joinForm{zone: open.row.AppUser.Timezone}
}

// renderUnusable renders the page for a link that cannot be redeemed, with a
// 404. Every reason gets the same page and the same status, so the response
// cannot tell somebody trying tokens which ones were real.
func (h *invited) renderUnusable(w http.ResponseWriter, r *http.Request) {
	h.templates.render(w, r, view{page: "invited", status: http.StatusNotFound}, unusableInvitePage())
}

// challenge handles POST /invite/{token}/challenge and returns the options for
// navigator.credentials.create as JSON. A form the post would refuse is
// refused here with a 422 and no body, so no passkey is made for it. The
// script then posts the form as it is, and the post renders the reason. A link
// that cannot be redeemed gets the same 422, because the post is what renders
// that page too.
func (h *invited) challenge(w http.ResponseWriter, r *http.Request) {
	open, usable, err := h.open(r)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "start the registration", err)
		return
	}
	if !usable {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.templates.badRequest(w, r)
		return
	}

	if open.reenrol() {
		held, err := h.queries.ListPasskeys(r.Context(), open.member.ID)
		if err != nil {
			h.templates.serverError(h.logger, w, r, "list the passkeys", err)
			return
		}
		creation, cookie, err := h.passkeys.BeginRegistration(r.Context(), h.now(), open.member, held)
		if err != nil {
			h.templates.serverError(h.logger, w, r, "start the registration", err)
			return
		}
		http.SetCookie(w, cookie)
		writeJSON(h.templates, h.logger, w, r, creation)
		return
	}

	form := readJoinForm(r)
	if !form.valid() {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	if !slices.Contains(zones, form.zone) {
		h.templates.badRequest(w, r)
		return
	}
	// The handle is checked here as well as on the post, so the browser is not
	// asked to make a passkey the post will then refuse.
	if taken, err := handleTaken(r.Context(), h.queries, form.handle); err != nil {
		h.templates.serverError(h.logger, w, r, "start the registration", err)
		return
	} else if taken {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	// The account has no row yet. Its id is chosen here and stored with the
	// ceremony, so the row the post writes has the id the passkey was
	// registered under.
	account := store.AppUser{ID: uuid.NewV7(), DisplayName: form.name, Handle: form.handleOrDefault(), Timezone: form.zone}
	creation, cookie, err := h.passkeys.BeginSetup(r.Context(), h.now(), account)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "start the registration", err)
		return
	}
	http.SetCookie(w, cookie)
	writeJSON(h.templates, h.logger, w, r, creation)
}

// redeem handles POST /invite/{token}. Everything the post writes is in one
// transaction with the update that marks the invite used, so a refused
// registration writes nothing.
func (h *invited) redeem(w http.ResponseWriter, r *http.Request) {
	open, usable, err := h.open(r)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "redeem the invite", err)
		return
	}
	if !usable {
		h.renderUnusable(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.templates.badRequest(w, r)
		return
	}
	token := r.PathValue("token")
	form := readJoinForm(r)
	page := newInvitedPage(token, open, form, false)
	// The ceremony cookie is cleared here, so the challenge cannot be
	// answered twice, whatever the post's outcome.
	http.SetCookie(w, h.passkeys.ClearedCeremonyCookie())

	if open.reenrol() {
		user, passkey, err := h.addDevice(r, open)
		h.finish(w, r, page, "add the device", user, open.row.Invite.GardenID, passkey, todayPath, err)
		return
	}

	if form.zone != "" && !slices.Contains(zones, form.zone) {
		h.templates.badRequest(w, r)
		return
	}
	page.NameError, page.Zone.Error = form.errors()
	if !form.valid() {
		h.templates.render(w, r, view{page: "invited", status: http.StatusUnprocessableEntity}, page)
		return
	}
	if taken, err := handleTaken(r.Context(), h.queries, form.handle); err != nil {
		h.templates.serverError(h.logger, w, r, "join the garden", err)
		return
	} else if taken {
		page.HandleError = handleTakenMessage(form.handle)
		h.templates.render(w, r, view{page: "invited", status: http.StatusUnprocessableEntity}, page)
		return
	}
	gardenNamed, err := gardenNamer(r.Context(), h.queries, open.row.Garden)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "read the garden's owner", err)
		return
	}
	user, passkey, err := h.join(r, open, form)
	if err == nil {
		h.notify.call(r.Context(), open.row.AppUser, inviteAcceptedNotification(gardenNamed(open.row.AppUser.ID), user, open.row.Invite.Role))
		// The deadlines job sends when the new membership's end date passes.
		h.wake.call()
	}
	h.finish(w, r, page, "join the garden", user, open.row.Invite.GardenID, passkey, remindersPath, err)
}

// join redeems a join invite: the account, the membership and the passkey are
// written together. The membership's inviter and end date come from the
// invite row.
func (h *invited) join(r *http.Request, open openInvite, form joinForm) (store.AppUser, store.PasskeyCredential, error) {
	ceremony, err := h.passkeys.TakeSetup(r.Context(), h.now(), r)
	if err != nil {
		return store.AppUser{}, store.PasskeyCredential{}, err
	}
	invite := open.row.Invite
	body := strings.NewReader(r.PostForm.Get(credentialField))
	name := browserName(userAgentOf(r))
	var (
		user    store.AppUser
		passkey store.PasskeyCredential
	)
	err = h.queries.InTx(r.Context(), func(q *store.Queries) error {
		ctx := r.Context()
		if err := markRedeemed(ctx, q, h.now(), invite); err != nil {
			return err
		}
		var err error
		if form.handle != "" {
			user, err = q.CreateAccountWithHandle(ctx, ceremony.AccountID, form.name, form.handle, form.zone)
		} else {
			user, err = q.CreateAccount(ctx, ceremony.AccountID, form.name, form.zone)
		}
		if err != nil {
			return err
		}
		_, err = createMembership(ctx, q, newMembership{
			GardenID:  invite.GardenID,
			UserID:    user.ID,
			Role:      invite.Role,
			InvitedBy: &invite.CreatedBy,
			ExpiresAt: invite.MembershipExpiresAt,
		})
		if err != nil {
			return err
		}
		passkey, err = h.passkeys.WithQueries(q).FinishSetup(ctx, ceremony, user, body, name)
		return err
	})
	return user, passkey, err
}

// addDevice redeems a re-enrolment invite: one passkey is added to the account
// the invite names, and nothing else is written.
func (h *invited) addDevice(r *http.Request, open openInvite) (store.AppUser, store.PasskeyCredential, error) {
	body := strings.NewReader(r.PostForm.Get(credentialField))
	name := browserName(userAgentOf(r))
	var passkey store.PasskeyCredential
	err := h.queries.InTx(r.Context(), func(q *store.Queries) error {
		ctx := r.Context()
		if err := markRedeemed(ctx, q, h.now(), open.row.Invite); err != nil {
			return err
		}
		var err error
		passkey, err = h.passkeys.WithQueries(q).FinishRegistration(ctx, h.now(), r, open.member, body, name)
		return err
	})
	return open.member, passkey, err
}

// markRedeemed sets the invite's redeemed_at, and returns errInviteUsed when
// the row was already redeemed or has run out. It runs first in the
// transaction, so the second of two posts waits on the row lock and then
// writes nothing.
func markRedeemed(ctx context.Context, q *store.Queries, now time.Time, invite store.Invite) error {
	n, err := q.RedeemInvite(ctx, store.RedeemInviteParams{Now: now, GardenID: invite.GardenID, InviteID: invite.ID})
	if err != nil {
		return err
	}
	if n == 0 {
		return errInviteUsed
	}
	return nil
}

// finish writes the response to a redeem post. On success it starts a session
// for user on the garden and redirects to next. The session replaces any the
// browser arrived with. A refusal renders the page again with the reason above
// the form. what names the step in a 500's log line.
func (h *invited) finish(w http.ResponseWriter, r *http.Request, page invitedPage, what string, user store.AppUser, gardenID uuid.UUID, passkey store.PasskeyCredential, next string, err error) {
	if errors.Is(err, errInviteUsed) {
		h.renderUnusable(w, r)
		return
	}
	// The handle was free when the challenge and the post checked it, and
	// another account took it between the check and the write.
	if errors.Is(err, store.ErrHandleTaken) {
		page.HandleError = handleTakenMessage(page.Handle)
		h.templates.render(w, r, view{page: "invited", status: http.StatusUnprocessableEntity}, page)
		return
	}
	if err != nil {
		page.Refusal = registrationRefusal(h.templates, h.logger, w, r, err, what)
		if page.Refusal == "" {
			return
		}
		h.templates.render(w, r, view{page: "invited", status: http.StatusUnprocessableEntity}, page)
		return
	}

	if err := h.sessions.DeleteFromRequest(r.Context(), r); err != nil {
		h.templates.serverError(h.logger, w, r, "end the previous session", err)
		return
	}
	token, _, err := h.sessions.Create(r.Context(), h.now(), user.ID, &gardenID, &passkey.ID, r.UserAgent())
	if err != nil {
		h.templates.serverError(h.logger, w, r, "start the session", err)
		return
	}
	http.SetCookie(w, h.sessions.Cookie(token))
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// tooManyAnswers is the response to a POST /invite/{token} past the budget. It
// renders the page with the rate-limit message above the form, because a
// person is reading the response.
func (h *invited) tooManyAnswers(w http.ResponseWriter, r *http.Request) {
	open, usable, err := h.open(r)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "open the invite", err)
		return
	}
	if !usable {
		h.renderUnusable(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.templates.badRequest(w, r)
		return
	}
	page := newInvitedPage(r.PathValue("token"), open, readJoinForm(r), false)
	page.Refusal = tooManyInviteAttempts
	h.templates.render(w, r, view{page: "invited", status: http.StatusTooManyRequests}, page)
}

// tooManyChallenges is the response to a POST /invite/{token}/challenge past
// the budget. The line is plain text, because the page's script fetches this
// URL and puts the body above the form.
func tooManyChallenges(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, tooManyInviteAttempts, http.StatusTooManyRequests)
}
