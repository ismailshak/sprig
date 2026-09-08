package http

import (
	"cmp"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/schedule"
	"github.com/ismailshak/sprig/internal/store"
)

const choresPath = "/api/chores"

// chores handles GET /api/chores. It returns the garden's overdue, due-today
// and upcoming cares as JSON to a caller holding an API token.
type chores struct {
	logger  *slog.Logger
	queries *store.Queries
	// now supplies the current time, so a test can fix the day.
	now func() time.Time
}

// choresResponse is the body of GET /api/chores.
type choresResponse struct {
	Garden string `json:"garden"`
	// Date is the day the chores are for, as YYYY-MM-DD.
	Date string `json:"date"`
	// Chores is every care that is overdue or due today, most overdue first.
	// It is an empty array rather than null when there is nothing to do.
	Chores []chore `json:"chores"`
	// Upcoming is every care not yet due, soonest first. It is an empty array
	// rather than null when there is none.
	Upcoming []upcomingCare `json:"upcoming"`
}

// chore is one care on one plant.
type chore struct {
	// Plant is the plant's display name: its nickname, or the common or
	// botanical name where it has no nickname.
	Plant string `json:"plant"`
	// Location is the plant's location. It is empty when the plant has none.
	Location string `json:"location"`
	// Care is the care type's name, such as "Water".
	Care string `json:"care"`
	// Due is the day the care fell due, as YYYY-MM-DD. For a schedule precise
	// only to a month it is the first of that month.
	Due string `json:"due"`
	// Late is how far past due the care is, such as "2 days late". It is
	// empty for a care due today.
	Late string `json:"late"`
}

// upcomingCare is one care on one plant that is not yet due.
type upcomingCare struct {
	Plant    string `json:"plant"`
	Location string `json:"location"`
	Care     string `json:"care"`
	// Due is the day the care falls due, as YYYY-MM-DD. For a schedule
	// precise only to a month it is the first of that month.
	Due string `json:"due"`
	// When is the due day in the words the Today page uses: "tomorrow", a
	// weekday name, "in 12 days" or "in March".
	When string `json:"when"`
}

func (h *chores) show(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)

	// A token is not tied to a signed-in account, so today's date is worked
	// out in the timezone of the account that created the token.
	creator, err := h.queries.GetUser(r.Context(), principal.APIToken.CreatedBy)
	if err != nil {
		serverError(h.logger, w, r, "read the token's creator", err)
		return
	}
	now := h.now().In(locationFor(creator))

	schedules, err := h.queries.ListCareSchedules(r.Context(), principal.Garden.ID)
	if err != nil {
		serverError(h.logger, w, r, "list the schedules", err)
		return
	}
	latest, err := h.queries.ListLatestCareEvents(r.Context(), principal.Garden.ID)
	if err != nil {
		serverError(h.logger, w, r, "list the latest care", err)
		return
	}
	lines := schedule.Resolve(schedules, latest, now)

	body, err := json.Marshal(newChoresResponse(principal, lines, now))
	if err != nil {
		serverError(h.logger, w, r, "encode the chores", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	// The list changes as care is logged, so no proxy may keep a copy.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

// newChoresResponse lists the overdue plants first and then those due today,
// in the same order as the Today page. A plant with two cares due gets a chore
// for each, the overdue one first. Upcoming holds the cares that are not yet
// due, soonest first.
func newChoresResponse(principal auth.Principal, lines []schedule.Line, now time.Time) choresResponse {
	response := choresResponse{
		Garden:   principal.Garden.Name,
		Date:     now.Format(time.DateOnly),
		Chores:   []chore{},
		Upcoming: []upcomingCare{},
	}
	day := schedule.Today(lines)
	for _, rows := range [][]schedule.Row{day.Overdue, day.DueToday} {
		for _, row := range rows {
			for _, state := range []schedule.State{schedule.Overdue, schedule.DueToday} {
				for _, line := range row.Lines {
					if line.State == state {
						response.Chores = append(response.Chores, newChore(line, now))
					}
				}
			}
		}
	}
	for _, line := range upcomingLines(lines) {
		response.Upcoming = append(response.Upcoming, newUpcomingCare(line, now))
	}
	return response
}

func newChore(line schedule.Line, now time.Time) chore {
	c := chore{
		Plant: line.Plant.DisplayName(),
		Care:  line.CareType.Name,
		Due:   line.Due.Format(time.DateOnly),
	}
	if line.Plant.Location != nil {
		c.Location = *line.Plant.Location
	}
	if line.State == schedule.Overdue {
		c.Late = overdueWord(line, now)
	}
	return c
}

func newUpcomingCare(line schedule.Line, now time.Time) upcomingCare {
	c := upcomingCare{
		Plant: line.Plant.DisplayName(),
		Care:  line.CareType.Name,
		Due:   line.Due.Format(time.DateOnly),
		When:  comingWord(line, now),
	}
	if line.Plant.Location != nil {
		c.Location = *line.Plant.Location
	}
	return c
}

// upcomingLines returns the lines not yet due, soonest first. Two cares due on
// the same day are ordered by plant name, then plant id, then care name, so the
// order does not change between polls.
func upcomingLines(lines []schedule.Line) []schedule.Line {
	var out []schedule.Line
	for _, line := range lines {
		if line.State == schedule.Upcoming {
			out = append(out, line)
		}
	}
	slices.SortStableFunc(out, func(a, b schedule.Line) int {
		return cmp.Or(
			cmp.Compare(a.Days, b.Days),
			strings.Compare(strings.ToLower(a.Plant.DisplayName()), strings.ToLower(b.Plant.DisplayName())),
			cmp.Compare(a.Plant.ID.String(), b.Plant.ID.String()),
			strings.Compare(a.CareType.Name, b.CareType.Name),
		)
	})
	return out
}

// choresLimits returns the rate limiter wrapped around GET /api/chores. It is
// keyed on the token and not the address, because two devices behind one
// tunnel arrive from the same address and would share a budget. Six a minute
// is well above a device polling every fifteen minutes, and catches one
// misconfigured to poll every second. The limiter runs outside the token
// lookup, so a refused request costs no query.
func choresLimits() []middleware {
	return []middleware{Limit(NewLimiter(rate.Every(time.Minute/6), 6), ByToken, http.HandlerFunc(tooManyChoreRequests))}
}

// tooManyChoreRequests writes the 429 for a request past the limit. The body
// is plain text rather than an HTML page, because the caller is a device.
func tooManyChoreRequests(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
}
