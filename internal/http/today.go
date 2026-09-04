package http

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ismailshak/sprig/internal/schedule"
	"github.com/ismailshak/sprig/internal/store"
)

type today struct {
	logger    *slog.Logger
	queries   *store.Queries
	templates *Templates
	// now is the clock the day is read from, so a test can pin the day.
	now func() time.Time
}

func (h *today) show(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	principal := PrincipalFrom(r)
	now := h.now().In(locationFor(principal.User))

	schedules, err := h.queries.ListCareSchedules(ctx, principal.Garden.ID)
	if err != nil {
		serverError(h.logger, w, r, "list the schedules", err)
		return
	}
	latest, err := h.queries.ListLatestCareEvents(ctx, principal.Garden.ID)
	if err != nil {
		serverError(h.logger, w, r, "list the latest care", err)
		return
	}
	plants, err := h.queries.CountPlants(ctx, principal.Garden.ID)
	if err != nil {
		serverError(h.logger, w, r, "count the plants", err)
		return
	}

	day := schedule.Today(schedule.Resolve(schedules, latest, now))
	h.templates.render(w, r, view{page: "today"}, newTodayPage(principal.Garden, day, latest, plants, now))
}

type todayPage struct {
	Date   string
	Garden string
	// Summary is nil when nothing is outstanding, and Empty carries the news
	// in its place.
	Summary  *todaySummary
	Empty    *todayEmpty
	Sections []todaySection
}

type todaySummary struct {
	Outstanding int
	Overdue     int
}

type todaySection struct {
	// ID names the heading the section points at, so the section is a region
	// reached by its title.
	ID    string
	Title string
	// Alert colours the title, which Overdue alone gets.
	Alert bool
	Rows  []careRow
}

type todayEmpty struct {
	Title string
	Line  string
	// Done draws the tick rather than the leaf.
	Done   bool
	Action *link
}

type link struct {
	Label string
	Href  string
}

// careRow is what the care-row fragment takes, so a page load and a swap draw
// the same row.
type careRow struct {
	// ID is the id a swap targets. It carries the care type's slug rather
	// than its name, because renaming a type leaves the slug alone.
	ID   string
	Name string
	// Botanical marks a name that is the botanical one, which is set in
	// italic.
	Botanical bool
	Location  string
	// Late is empty on a row that is not overdue.
	Late string
	// When is empty on a row that is not coming up. It also puts the quiet
	// Log button on the row in place of the care button, since logging
	// before the day is not what the screen asks for.
	When string
	Care string
	Slug string
}

func careRowID(plant store.Plant, careType store.CareType) string {
	return fmt.Sprintf("care-%s-%s", plant.ID, careType.Slug)
}

func newTodayPage(garden store.Garden, day schedule.Day, latest []store.CareEvent, plants int64, now time.Time) todayPage {
	page := todayPage{
		Date:   now.Format("Monday 2 January"),
		Garden: garden.Name,
	}
	if rows := day.Overdue; len(rows) > 0 {
		page.Sections = append(page.Sections, todaySection{ID: "overdue", Title: "Overdue", Alert: true, Rows: careRows(rows, now)})
	}
	if rows := day.DueToday; len(rows) > 0 {
		page.Sections = append(page.Sections, todaySection{ID: "due-today", Title: "Due today", Rows: careRows(rows, now)})
	}
	if rows := day.ComingUp; len(rows) > 0 {
		page.Sections = append(page.Sections, todaySection{ID: "coming-up", Title: "Coming up", Rows: careRows(rows, now)})
	}

	if outstanding := len(day.Overdue) + len(day.DueToday); outstanding > 0 {
		page.Summary = &todaySummary{Outstanding: outstanding, Overdue: len(day.Overdue)}
		return page
	}
	page.Empty = newTodayEmpty(day, latest, plants, now)
	return page
}

// newTodayEmpty picks which of three pieces of news an empty list is. A tick
// claims something was finished, so a day where nothing was scheduled gets the
// leaf. The next-up line and the link to the plant list wait on an empty
// Coming up, which otherwise says the same thing below them.
func newTodayEmpty(day schedule.Day, latest []store.CareEvent, plants int64, now time.Time) *todayEmpty {
	if plants == 0 {
		return &todayEmpty{
			Title:  "No plants yet",
			Line:   "Add one and sprig will tell you when it needs water.",
			Action: &link{Label: "Add a plant", Href: "/plants/new"},
		}
	}

	empty := &todayEmpty{Title: "Nothing needs you today", Line: "Nothing is due, and nothing is overdue."}
	if caredForOn(latest, now) {
		empty.Done = true
		empty.Title = "All done for today"
		empty.Line = "Nothing else is due."
	}
	if len(day.ComingUp) > 0 {
		return empty
	}
	if next := day.Next; next != nil {
		empty.Line = fmt.Sprintf("%s is next, %s.", next.Plant.DisplayName(), whenWord(next.Care.Days, now))
	}
	empty.Action = &link{Label: "See all plants", Href: "/plants"}
	return empty
}

// caredForOn reports whether any care was performed on now's date, read in
// now's location. A skip counts, because it dealt with the row as much as a
// watering did.
func caredForOn(latest []store.CareEvent, now time.Time) bool {
	y, m, d := now.Date()
	for _, e := range latest {
		if ey, em, ed := e.PerformedAt.In(now.Location()).Date(); ey == y && em == m && ed == d {
			return true
		}
	}
	return false
}

func careRows(rows []schedule.Row, now time.Time) []careRow {
	out := make([]careRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, newCareRow(row, now))
	}
	return out
}

func newCareRow(row schedule.Row, now time.Time) careRow {
	r := careRow{
		ID:        careRowID(row.Plant, row.Care.CareType),
		Name:      row.Plant.DisplayName(),
		Botanical: row.Plant.BotanicalOnly(),
		Care:      row.Care.CareType.Name,
		Slug:      row.Care.CareType.Slug,
	}
	if row.Plant.Location != nil {
		r.Location = *row.Plant.Location
	}
	switch row.Care.State {
	case schedule.Overdue:
		r.Late = lateWord(-row.Care.Days)
	case schedule.Upcoming:
		r.When = whenWord(row.Care.Days, now)
	}
	return r
}
