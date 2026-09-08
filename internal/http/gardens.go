package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// gardensPath is the URL of the garden sheet. A GET renders Today with the
// sheet open, and a POST sets the session's garden to the one chosen in it.
const gardensPath = "/gardens"

// gardenField is the name of the hidden input on each garden's form in the
// sheet. Its value is the id of the garden to switch to.
const gardenField = "garden"

// gardenRow is one garden in the sheet.
type gardenRow struct {
	Name string
	// Owner is "Robin's garden", or "Your garden" when the reader owns it.
	// Empty for a garden with no owner left.
	Owner string
	// Role is the reader's role in this garden, capitalised: "Owner", "Member"
	// or "Sitter".
	Role string
	// Until is "until 8 Sep" when the membership has an end date, and empty when
	// it does not. The date is in the reader's timezone.
	Until string
	// Current is true for the garden the session is on. That row shows
	// "Current" and is text rather than a button.
	Current bool
	ID      uuid.UUID
}

// liveGardens returns the account's memberships that have not ended, each with
// its garden. An ended membership is left out because the next request would
// move the session straight off that garden again.
func liveGardens(ctx context.Context, queries *store.Queries, userID uuid.UUID, now time.Time) ([]store.ListMembershipsWithGardensForUserRow, error) {
	all, err := queries.ListMembershipsWithGardensForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("read the memberships: %w", err)
	}
	live := all[:0]
	for _, row := range all {
		if !auth.MembershipEnded(row.Membership, now) {
			live = append(live, row)
		}
	}
	return live, nil
}

// ownerWord returns "Your garden" when the reader owns the garden, "Robin's
// garden" when somebody else owns it, and "" when no owner is left.
func ownerWord(m store.ListMembershipsWithGardensForUserRow) string {
	switch {
	case m.ReaderOwns:
		return "Your garden"
	case m.OwnerName != "":
		return m.OwnerName + "’s garden"
	}
	return ""
}

func gardenRows(principal auth.Principal, memberships []store.ListMembershipsWithGardensForUserRow) []gardenRow {
	location := locationFor(principal.User)
	rows := make([]gardenRow, 0, len(memberships))
	for _, m := range memberships {
		row := gardenRow{
			Name:    m.Garden.Name,
			Owner:   ownerWord(m),
			Role:    roleWord(m.Membership.Role),
			Current: m.Garden.ID == principal.Garden.ID,
			ID:      m.Garden.ID,
		}
		if m.Membership.ExpiresAt != nil {
			row.Until = "until " + dateWord(*m.Membership.ExpiresAt, location)
		}
		rows = append(rows, row)
	}
	return rows
}

// gardenSheet handles GET /gardens. An htmx request targeting the sheet gets
// the open sheet alone. Any other request gets Today with the sheet open, so
// the link works as a page without JavaScript.
func (h *today) gardenSheet(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	g, err := h.load(r.Context(), principal)
	if err != nil {
		serverError(h.logger, w, r, "load the day", err)
		return
	}
	page := newTodayPage(principal, g)
	page.GardenSheet = true
	fragment := ""
	if r.Header.Get("HX-Target") == sheetID {
		fragment = "garden-sheet"
	}
	h.templates.render(w, r, view{page: "today", fragment: fragment}, page)
}

// switchGarden handles POST /gardens. It sets the session's garden to the
// posted one and redirects to Today. A garden the account has no live
// membership in gets a 404, the same as a garden that does not exist. It
// checks no capability, because the next request reads the membership in the
// new garden and takes the role from it.
func (h *today) switchGarden(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	if err := r.ParseForm(); err != nil {
		badRequest(w)
		return
	}
	gardenID, err := uuid.Parse(r.PostForm.Get(gardenField))
	if err != nil {
		notFound(w)
		return
	}

	membership, err := h.queries.GetMembershipWithUserAndGarden(r.Context(), gardenID, principal.User.ID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		notFound(w)
		return
	case err != nil:
		serverError(h.logger, w, r, "read the membership", err)
		return
	case auth.MembershipEnded(membership.Membership, h.now()):
		notFound(w)
		return
	}

	// The garden is recorded on the account as well, so the next session
	// starts there.
	err = h.queries.InTx(r.Context(), func(q *store.Queries) error {
		if _, err := q.SetSessionGarden(r.Context(), &gardenID, principal.Session.TokenHash); err != nil {
			return err
		}
		return q.SetLastGarden(r.Context(), &gardenID, principal.User.ID)
	})
	if err != nil {
		serverError(h.logger, w, r, "set the session's garden", err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
