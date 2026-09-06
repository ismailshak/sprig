package http

import (
	"net/http"
	"slices"
	"time"
)

// defaultInviteRole is the role chosen when the page opens. Sitter is the one
// of the two that grants less.
const defaultInviteRole = "sitter"

// untilUnusable is the message under the Until field when what was posted is
// not a date. A date input cannot produce one, so this is for a browser that
// renders the field as plain text.
const untilUnusable = "Give the last day as a date, or leave it empty."

// invitePage is the Invite someone page. It is two steps in one page: the form
// until the link is created, then the link in place of it. The second step is
// not a page of its own, because only a hash of the link is stored and the link
// itself exists in one response.
type invitePage struct {
	Bar topbar
	// Action is the URL both the chips and Create the link submit to. A chip
	// sends a GET, and the response is the form again with that role chosen.
	Action string
	// Secret is the link just created. The page shows the form instead while it
	// is nil.
	Secret *secretBox
	// Roles are the role chips, exactly one of them pressed. Chosen is that
	// chip's value, and Create the link posts it.
	Roles  []chip
	Chosen string
	// What is the sentence under the chips, saying what the chosen role can do.
	What string
	// Until is the end date field's value, in the format a date input reads. It
	// is empty by default, because a date the app filled in is a date nobody
	// chose.
	Until string
	// UntilError is shown under the end date, empty when the field is fine.
	UntilError string
	// Made is the paragraph under the link, saying what it does and how long it
	// works for.
	Made string
	// Done is the URL the Done link points at, back to People.
	Done string
}

// invite handles GET /more/people/invite. The role and the end date come from
// the query string, because a chip submits the form here as a GET and the date
// has to survive that.
func (h *more) invite(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	h.templates.render(w, r, view{page: "invite"}, newInvitePage(query.Get("role"), query.Get("until"), ""))
}

// createInviteLink handles POST /more/people/invite. It renders the link
// rather than redirecting to it, because the link exists in this response and
// nowhere else.
func (h *more) createInviteLink(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "the form did not parse", http.StatusBadRequest)
		return
	}
	role := r.PostForm.Get("role")
	if !slices.Contains(offeredRoles, role) {
		http.Error(w, "the form did not offer that", http.StatusBadRequest)
		return
	}

	// The date is optional. It is read in the reader's timezone, because nobody
	// has taken the invite up yet and there is no other account to read it in.
	posted := r.PostForm.Get("until")
	var ends *time.Time
	if posted != "" {
		at, err := parseAccessEnd(posted, locationFor(PrincipalFrom(r).User))
		if err != nil {
			page := newInvitePage(role, posted, untilUnusable)
			h.templates.render(w, r, view{page: "invite", status: http.StatusUnprocessableEntity}, page)
			return
		}
		ends = &at
	}

	token, err := h.createInvite(r, role, nil, ends)
	if err != nil {
		serverError(h.logger, w, r, "make the invite link", err)
		return
	}
	h.templates.render(w, r, view{page: "invite"}, madeInvitePage(inviteLink(r, token), ends, locationFor(PrincipalFrom(r).User)))
}

// newInvitePage builds the page with the form on it. role is the chip pressed,
// until the date field's value, and message the error under that field.
func newInvitePage(role, until, message string) invitePage {
	if !slices.Contains(offeredRoles, role) {
		role = defaultInviteRole
	}
	page := invitePage{
		Bar:        peopleBar("Invite someone"),
		Action:     invitePath,
		Chosen:     role,
		What:       roleWhat[role],
		Until:      until,
		UntilError: message,
	}
	for _, offered := range offeredRoles {
		page.Roles = append(page.Roles, chip{Value: offered, Label: roleWord(offered), On: offered == role})
	}
	return page
}

// madeInvitePage builds the page with the link on it: the link, the paragraph
// under it, and Done. ends is the day the membership the link creates stops on,
// and nil when the invite set no date.
func madeInvitePage(link string, ends *time.Time, location *time.Location) invitePage {
	made := "It works once, for one person, and expires in 7 days. Until they open it, it sits on People and can be revoked."
	if ends != nil {
		made += " Their access ends on " + dateWord(*ends, location) + ", whenever they take it up."
	}
	return invitePage{
		Bar:    peopleBar("Invite someone"),
		Action: invitePath,
		Secret: &secretBox{
			Label: "The link",
			Value: link,
			Why:   "This is the only time it is shown — sprig keeps a hash of it and nothing else. Send it however you already message them.",
		},
		Made: made,
		Done: peoplePath,
	}
}

// peopleBar is the top bar for a page reached from People, with its back link
// pointing there.
func peopleBar(title string) topbar {
	return topbar{Href: peoplePath, Back: "People", Title: title}
}
