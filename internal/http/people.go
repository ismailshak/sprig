package http

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// invitePath is the URL of the Invite someone page, linked at the bottom of
// People.
const invitePath = peoplePath + "/invite"

// memberPath is the URL prefix for one member's controls. The member is named
// by handle, because two members can have the same display name.
func memberPath(handle string) string {
	return peoplePath + "/" + url.PathEscape(handle)
}

// removeMemberPath is the URL behind a row's Remove link. A GET renders People
// with that row replaced by "Remove Ellie?" and its two buttons. A POST deletes
// the membership.
func removeMemberPath(handle string) string {
	return memberPath(handle) + "/remove"
}

// reenrolMemberPath is the URL the Re-enrol button posts to. It creates an
// invite link that adds a device to the account this person already has.
func reenrolMemberPath(handle string) string {
	return memberPath(handle) + "/reenrol"
}

// revokeInvitePath is the URL an invite row's Revoke button posts to.
func revokeInvitePath(inviteID uuid.UUID) string {
	return peoplePath + "/invites/" + inviteID.String() + "/revoke"
}

// roleWhat is the one-sentence description of each role, shown on a member's
// row and under the chips on Invite someone. The sentences describe what
// role_capability grants, so a change to that table has to be made here too.
var roleWhat = map[string]string{
	"owner":  "Everything, including inviting people and changing what they can do.",
	"member": "Logs care, adds and edits plants, and adds photos.",
	"sitter": "Logs care and sees everything. Adds no photos and no plants.",
}

// offeredRoles are the roles the select on a member's row and the chips on
// Invite someone offer. Owner is not one of them, because a garden has one
// owner and they are the only person who reaches these pages.
var offeredRoles = []string{"member", "sitter"}

// peoplePage is the People page: every membership in the garden, then the
// invites that have been sent and not opened.
type peoplePage struct {
	Bar topbar
	// Action is the URL the members form posts to.
	Action string
	// Secret is the re-enrolment link the Re-enrol button just created. It is
	// nil on every other request.
	Secret *secretBox
	// SecretWhy is the paragraph under that link, saying what it does to the
	// account it names.
	SecretWhy string
	Members   []memberRow
	// Save is false when the reader is the only member. Their own row has no
	// controls, so there is nothing to save.
	Save bool
	// Invites are the rows in the Invited section. The section is left off the
	// page when there are none rather than rendered empty.
	Invites []inviteRow
	// InviteSomeone is the URL of the Invite someone link at the bottom. It is
	// empty for a reader who cannot invite, and the link is then not rendered.
	InviteSomeone string
}

// memberRow is one person in the Members list.
type memberRow struct {
	Name string
	// Handle is shown beside the name only where another member has the same
	// display name. It is empty otherwise.
	Handle string
	// You is true on the reader's own row. That row says "You" after the name
	// and has no controls.
	You bool
	// Role is the role's display word, "Owner" or "Sitter". It is set on the
	// reader's own row and on a row with an end date, and empty on the rest.
	Role string
	// What is the sentence under the name saying what this person can do. A row
	// with an end date shows the date there instead and leaves this empty.
	What string
	// Until is the date on the second line of a row with an end date: "until 14
	// Sep" before that day, "Access ended 14 Sep" after it.
	Until string
	// Ended is true once the end date has passed. The row is greyed and has no
	// role select, because there is no role to choose for access that has
	// ended.
	Ended bool
	// Roles is the role select's options. It is nil on the reader's own row and
	// on a row whose access has ended, neither of which has a select.
	Roles []option
	// RoleField and UntilField are the names of this row's two controls. Every
	// row is inside one form, so each name includes the handle.
	RoleField  string
	UntilField string
	// UntilValue is the date input's value, in the format a date input reads.
	// It is empty on a membership with no end date, and that row has no date
	// input: an end date is set when the invite is created, and this row can
	// only move one that exists.
	UntilValue string
	// RoleLabel and UntilLabel are the accessible names of the two controls.
	// Neither has a visible label, because the row's own name is above them.
	RoleLabel  string
	UntilLabel string
	// Who is what to call this person in a sentence: their display name, with
	// the handle in brackets after it where another member has the same name.
	Who string
	// Reenrol is the URL the Re-enrol button posts to, and ReenrolForm the id
	// of the form it belongs to. Forms cannot nest, so that form is rendered
	// after the members form and the button names it in its form attribute.
	Reenrol     string
	ReenrolForm string
	// Remove is the URL the Remove button links to.
	Remove string
	// Asking is set on at most one row. That row shows "Remove Ellie?" and two
	// buttons in place of the name and the controls.
	Asking *removeQuestion
}

// removeQuestion is what a row shows once Remove is pressed. Remove asks first
// because the row is gone once it acts, leaving nothing to undo from.
type removeQuestion struct {
	// Ask reads "Remove Ellie?".
	Ask string
	// Action is the URL the Remove button posts to, and Form the id of the form
	// it belongs to. That form is rendered outside the members form, because
	// forms cannot nest.
	Action string
	Form   string
	// Keep is the URL the Keep them link points at: People with no row asking.
	Keep string
}

// inviteRow is one invite in the Invited section: a link that has been sent and
// not opened.
type inviteRow struct {
	// Role is the role the invite grants, "Sitter". The person has no name yet.
	Role string
	// Sent reads "Sent 2 Sep".
	Sent string
	// Expires reads "expires in 5 days", or "expired".
	Expires string
	// Revoke is the URL the Revoke button posts to.
	Revoke string
}

// secretBox is a value shown on one response and never again: an invite link, a
// re-enrolment link or a new API token. Only a hash of it is stored, so it
// cannot be shown again once the page is left.
type secretBox struct {
	// Label is the line above the value, "The link" or "The token".
	Label string
	Value string
	// Why is the paragraph under the value. It says the value is shown once,
	// and for a token it names the date it stops working.
	Why string
}

// peopleState is what one request adds to the People page: the row asking
// "Remove Ellie?", and a re-enrolment link that was just created.
type peopleState struct {
	// asking is the handle of the member whose row shows "Remove Ellie?".
	asking string
	// reenrolled is the member the link was created for, and link the link
	// itself. Both are empty on every request that created no link.
	reenrolled store.AppUser
	link       string
}

func (h *more) people(w http.ResponseWriter, r *http.Request) {
	h.renderPeople(w, r, peopleState{})
}

// saveMembers handles POST /more/people. It writes the role and the end date
// of every row that changed one.
func (h *more) saveMembers(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "the form did not parse", http.StatusBadRequest)
		return
	}
	members, err := h.queries.ListMembers(r.Context(), principal.Garden.ID)
	if err != nil {
		serverError(h.logger, w, r, "list the members", err)
		return
	}
	changes, ok := postedChanges(members, principal, r.PostForm)
	if !ok {
		http.Error(w, "the form did not offer that", http.StatusBadRequest)
		return
	}

	// One transaction, so a write that fails partway leaves no row changed.
	err = h.queries.InTx(r.Context(), func(q *store.Queries) error {
		for _, change := range changes {
			if change.role != "" {
				params := store.SetMemberRoleParams{Role: change.role, GardenID: principal.Garden.ID, UserID: change.userID}
				if _, err := q.SetMemberRole(r.Context(), params); err != nil {
					return err
				}
			}
			if change.ends != nil {
				params := store.SetMembershipEndParams{ExpiresAt: change.ends, GardenID: principal.Garden.ID, UserID: change.userID}
				if _, err := q.SetMembershipEnd(r.Context(), params); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		serverError(h.logger, w, r, "save the members", err)
		return
	}
	http.Redirect(w, r, peoplePath, http.StatusSeeOther)
}

// memberChange is one row's post, after the values have been checked. role is
// empty and ends is nil where that control was left alone.
type memberChange struct {
	userID uuid.UUID
	role   string
	ends   *time.Time
}

// postedChanges reads the role and the end date each member's row posted. It
// returns false, before anything is written, for a role that is not one the
// select offers and for a date that will not parse. It walks the members read
// from the database, so a field name matching no row is ignored.
func postedChanges(members []store.ListMembersRow, principal auth.Principal, posted url.Values) ([]memberChange, bool) {
	var changes []memberChange
	for _, member := range members {
		// The reader's own row has no select and no date field, so anything
		// posted under their handle did not come from this page.
		if member.AppUser.ID == principal.User.ID {
			continue
		}
		change := memberChange{userID: member.AppUser.ID}
		if role := posted.Get(roleField(member.AppUser.Handle)); role != "" && role != member.Membership.Role {
			if !slices.Contains(offeredRoles, role) {
				return nil, false
			}
			change.role = role
		}
		// An empty field leaves the date alone. A membership with no end date
		// has no date field, so a date posted for one is ignored.
		if until := posted.Get(untilField(member.AppUser.Handle)); until != "" && member.Membership.ExpiresAt != nil {
			ends, err := parseAccessEnd(until, locationFor(member.AppUser))
			if err != nil {
				return nil, false
			}
			change.ends = &ends
		}
		if change.role != "" || change.ends != nil {
			changes = append(changes, change)
		}
	}
	return changes, true
}

// confirmRemoveMember handles GET /more/people/{member}/remove. It renders
// People with that member's row replaced by "Remove Ellie?" and its two
// buttons.
func (h *more) confirmRemoveMember(w http.ResponseWriter, r *http.Request) {
	member, ok := h.memberFromPath(w, r)
	if !ok {
		return
	}
	h.renderPeople(w, r, peopleState{asking: member.AppUser.Handle})
}

// removeMember handles POST /more/people/{member}/remove. It deletes the
// membership. The person's sessions on this garden go with it through a
// foreign key, and the care events they logged keep their name.
func (h *more) removeMember(w http.ResponseWriter, r *http.Request) {
	member, ok := h.memberFromPath(w, r)
	if !ok {
		return
	}
	removed, err := h.queries.DeleteMembership(r.Context(), member.Membership.GardenID, member.AppUser.ID)
	if err != nil {
		serverError(h.logger, w, r, "remove the member", err)
		return
	}
	if removed == 0 {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, peoplePath, http.StatusSeeOther)
}

// reenrolMember handles POST /more/people/{member}/reenrol. It creates an
// invite against the account this person already has, so opening the link adds
// a device to that account rather than making a second one with the same name.
//
// It renders the link rather than redirecting to it, because only a hash is
// stored and this response is the one place the link itself exists.
func (h *more) reenrolMember(w http.ResponseWriter, r *http.Request) {
	member, ok := h.memberFromPath(w, r)
	if !ok {
		return
	}
	token, err := h.createInvite(r, member.Membership.Role, &member.AppUser.ID, nil)
	if err != nil {
		serverError(h.logger, w, r, "make the re-enrolment link", err)
		return
	}
	h.renderPeople(w, r, peopleState{reenrolled: member.AppUser, link: inviteLink(r, token)})
}

// revokeInvite handles POST /more/people/invites/{invite}/revoke. Deleting the
// row is what makes the link stop working.
func (h *more) revokeInvite(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	inviteID, err := uuid.Parse(r.PathValue("invite"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	revoked, err := h.queries.DeleteWaitingInvite(r.Context(), principal.Garden.ID, inviteID)
	if err != nil {
		serverError(h.logger, w, r, "revoke the invite", err)
		return
	}
	if revoked == 0 {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, peoplePath, http.StatusSeeOther)
}

// memberFromPath reads the member the URL names. It writes a 404 and returns
// false for a handle nobody in this garden holds, and for the reader's own
// handle, because their row has no Remove and no Re-enrol button.
func (h *more) memberFromPath(w http.ResponseWriter, r *http.Request) (store.GetMemberByHandleRow, bool) {
	principal := PrincipalFrom(r)
	member, err := h.queries.GetMemberByHandle(r.Context(), principal.Garden.ID, r.PathValue("member"))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		http.NotFound(w, r)
		return store.GetMemberByHandleRow{}, false
	case err != nil:
		serverError(h.logger, w, r, "read the member", err)
		return store.GetMemberByHandleRow{}, false
	}
	if member.AppUser.ID == principal.User.ID {
		http.NotFound(w, r)
		return store.GetMemberByHandleRow{}, false
	}
	return member, true
}

// createInvite writes an invite row and returns the token that opens it.
// userID is set for a re-enrolment and nil for somebody new to the garden.
// ends is the date the membership the invite creates stops on, and nil for one
// that does not end.
//
// A re-enrolment deletes any unredeemed link for the same person first. Those
// rows are not in the Invited section, so a second press would otherwise leave
// a working link that no page can revoke.
func (h *more) createInvite(r *http.Request, role string, userID *uuid.UUID, ends *time.Time) (string, error) {
	principal := PrincipalFrom(r)
	token := auth.NewInviteToken()
	params := store.CreateInviteParams{
		GardenID:            principal.Garden.ID,
		TokenHash:           auth.HashToken(token),
		Role:                role,
		UserID:              userID,
		CreatedBy:           principal.User.ID,
		ExpiresAt:           h.now().Add(auth.InviteLifetime),
		MembershipExpiresAt: ends,
	}
	err := h.queries.InTx(r.Context(), func(q *store.Queries) error {
		if userID != nil {
			if _, err := q.DeleteReenrolmentInvites(r.Context(), principal.Garden.ID, userID); err != nil {
				return err
			}
		}
		_, err := q.CreateInvite(r.Context(), params)
		return err
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

// inviteLink is the link shown on the page: the host this request arrived at,
// then the path that redeems the token. It has no scheme, because no
// hostname is configured and the request is the only place the host is known.
func inviteLink(r *http.Request, token string) string {
	return r.Host + invitedPath(token)
}

func (h *more) renderPeople(w http.ResponseWriter, r *http.Request, state peopleState) {
	principal := PrincipalFrom(r)
	members, err := h.queries.ListMembers(r.Context(), principal.Garden.ID)
	if err != nil {
		serverError(h.logger, w, r, "list the members", err)
		return
	}
	invites, err := h.queries.ListWaitingInvites(r.Context(), principal.Garden.ID)
	if err != nil {
		serverError(h.logger, w, r, "list the invites", err)
		return
	}

	collides := collidingNames(members)
	page := peoplePage{
		Bar:     moreBar("People"),
		Action:  peoplePath,
		Members: memberRows(members, principal, collides, state.asking, h.now()),
		Save:    len(members) > 1,
		Invites: inviteRows(invites, h.now().In(locationFor(principal.User))),
	}
	if state.link != "" {
		page.Secret, page.SecretWhy = reenrolBox(state.reenrolled, collides, state.link)
	}
	if principal.Can(auth.MemberInvite) {
		page.InviteSomeone = invitePath
	}
	h.templates.render(w, r, view{page: "people"}, page)
}

// reenrolBox builds the box holding a new re-enrolment link and the paragraph
// under it. The link is shown above the members list rather than on a page of
// its own, because a page can be navigated back to and the link exists only in
// this response.
func reenrolBox(user store.AppUser, collides map[string]bool, link string) (*secretBox, string) {
	who := whoWord(user, collides)
	box := &secretBox{
		Label: "The link",
		Value: link,
		Why: "This is the only time it is shown — sprig keeps a hash of it and nothing else. " +
			"Send it to " + who + " however you already message them.",
	}
	return box, "It adds a device to the account " + who + " already has, rather than making a second " +
		user.DisplayName + " — so everything they have logged stays theirs. It works once and expires in 7 days."
}

// memberRows builds the Members list. asking is the handle of the one row that
// shows "Remove Ellie?" in place of its controls.
func memberRows(members []store.ListMembersRow, principal auth.Principal, collides map[string]bool, asking string, now time.Time) []memberRow {
	rows := make([]memberRow, 0, len(members))
	for _, member := range members {
		rows = append(rows, newMemberRow(member, principal, collides, asking, now))
	}
	return rows
}

func newMemberRow(member store.ListMembersRow, principal auth.Principal, collides map[string]bool, asking string, now time.Time) memberRow {
	handle := member.AppUser.Handle
	row := memberRow{Name: member.AppUser.DisplayName, You: member.AppUser.ID == principal.User.ID}
	if collides[member.AppUser.DisplayName] {
		row.Handle = handle
	}

	// The reader's own row has no controls, because an owner who demoted or
	// removed themselves would leave a garden nobody can administer.
	if row.You {
		row.Role = roleWord(member.Membership.Role)
		return row
	}

	row.Who = whoWord(member.AppUser, collides)
	row.Reenrol, row.ReenrolForm = reenrolMemberPath(handle), "reenrol-"+handle
	row.Remove = removeMemberPath(handle)
	row.RoleField, row.UntilField = roleField(handle), untilField(handle)
	row.RoleLabel = "What " + row.Who + " can do"
	row.UntilLabel = "When " + row.Who + "'s access ends"

	// A membership with an end date shows the date on its second line in place
	// of the sentence about what the person can do, because nothing else on the
	// page gives the date.
	if member.Membership.ExpiresAt != nil {
		location := locationFor(member.AppUser)
		at := member.Membership.ExpiresAt.In(location)
		row.Ended = auth.MembershipEnded(member.Membership, now)
		row.Role = roleWord(member.Membership.Role)
		if row.Ended {
			row.Until = "Access ended " + agoWord(at, now.In(location))
		} else {
			row.Until = "until " + dateWord(at, location)
		}
		row.UntilValue = at.Format(accessEndLayout)
	} else {
		row.What = roleWhat[member.Membership.Role]
	}
	if !row.Ended {
		row.Roles = roleOptions(member.Membership.Role)
	}

	if handle == asking {
		row.Asking = &removeQuestion{
			Ask:    "Remove " + row.Who + "?",
			Action: removeMemberPath(handle),
			Form:   "remove-" + handle,
			Keep:   peoplePath,
		}
	}
	return row
}

func inviteRows(invites []store.Invite, now time.Time) []inviteRow {
	rows := make([]inviteRow, 0, len(invites))
	for _, invite := range invites {
		rows = append(rows, inviteRow{
			Role:    roleWord(invite.Role),
			Sent:    "Sent " + agoWord(invite.CreatedAt, now),
			Expires: inviteExpiryWord(invite.ExpiresAt, now),
			Revoke:  revokeInvitePath(invite.ID),
		})
	}
	return rows
}

// roleOptions builds the role select's options, with the member's current role
// chosen.
func roleOptions(role string) []option {
	options := make([]option, 0, len(offeredRoles))
	for _, offered := range offeredRoles {
		options = append(options, option{Value: offered, Label: roleWord(offered), On: offered == role})
	}
	return options
}

// collidingNames returns the display names that more than one member has.
func collidingNames(members []store.ListMembersRow) map[string]bool {
	seen := make(map[string]int, len(members))
	for _, member := range members {
		seen[member.AppUser.DisplayName]++
	}
	collides := map[string]bool{}
	for name, count := range seen {
		if count > 1 {
			collides[name] = true
		}
	}
	return collides
}

func roleField(handle string) string  { return "role." + handle }
func untilField(handle string) string { return "until." + handle }

// accessEndLayout is how a date input writes and reads a date.
const accessEndLayout = "2006-01-02"

// parseAccessEnd turns a posted date into the instant access stops: the start
// of that day in the given zone. A row reading "until 14 Sep" has ended once
// the 14th begins there.
func parseAccessEnd(posted string, location *time.Location) (time.Time, error) {
	return time.ParseInLocation(accessEndLayout, strings.TrimSpace(posted), location)
}

// whoWord is what to call somebody in a sentence: their display name, or the
// display name with the handle in brackets where another member has the same
// name.
func whoWord(user store.AppUser, collides map[string]bool) string {
	if collides[user.DisplayName] {
		return user.DisplayName + " (" + user.Handle + ")"
	}
	return user.DisplayName
}
