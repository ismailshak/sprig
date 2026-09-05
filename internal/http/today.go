package http

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/schedule"
	"github.com/ismailshak/sprig/internal/store"
)

const todayPath = "/"

type today struct {
	logger    *slog.Logger
	queries   *store.Queries
	templates *Templates
	// now supplies the current time, so a test can fix the day.
	now func() time.Time
}

func (h *today) show(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	g, err := h.load(r.Context(), principal)
	if err != nil {
		serverError(h.logger, w, r, "load the day", err)
		return
	}
	if strings.HasPrefix(r.Header.Get("HX-Target"), careRowPrefix) {
		h.templates.render(w, r, view{page: "today", fragment: "care-settled"}, newCareSettled(principal, g))
		return
	}
	h.templates.render(w, r, view{page: "today"}, newTodayPage(principal, g))
}

// graceWindow is how long a logged row stays on Today with an Undo button
// beside it. It is short because the page runs it as a timer.
const graceWindow = 4 * time.Second

// undoWindow is how long after logging a care the feed still shows an Undo
// button for it. It is longer than graceWindow because without JavaScript the
// page is not refreshed, so the button has to stay valid for as long as someone
// might still be looking at it. The stylesheet hides the feed's Undo when
// JavaScript is running.
//
// The window only decides whether to render the button. A button already on
// the page still works after the window closes, because rejecting a click the
// page offered is worse than undoing a minute late.
const undoWindow = 30 * time.Second

// collapseMS is the duration in milliseconds of the stylesheet's transition on
// a row being removed.
const collapseMS = 320

// gardenDay is the garden's schedules and recent care as of the reader's
// current time. The page and the sheet render from the same one so they agree
// on the schedule.
type gardenDay struct {
	lines  []schedule.Line
	day    schedule.Day
	latest []store.CareEvent
	recent []store.ListRecentCareEventsRow
	plants int64
	// now is in the reader's timezone.
	now time.Time
}

func (h *today) load(ctx context.Context, principal auth.Principal) (gardenDay, error) {
	g := gardenDay{now: h.now().In(locationFor(principal.User))}

	schedules, err := h.queries.ListCareSchedules(ctx, principal.Garden.ID)
	if err != nil {
		return g, fmt.Errorf("list the schedules: %w", err)
	}
	g.latest, err = h.queries.ListLatestCareEvents(ctx, principal.Garden.ID)
	if err != nil {
		return g, fmt.Errorf("list the latest care: %w", err)
	}
	g.recent, err = h.queries.ListRecentCareEvents(ctx, principal.Garden.ID, feedLength)
	if err != nil {
		return g, fmt.Errorf("list the recent care: %w", err)
	}
	g.plants, err = h.queries.CountPlants(ctx, principal.Garden.ID)
	if err != nil {
		return g, fmt.Errorf("count the plants: %w", err)
	}

	g.lines = schedule.Resolve(schedules, g.latest, g.now)
	g.day = schedule.Today(g.lines)
	return g, nil
}

// windows reports whether any care is still inside its grace window.
func (g gardenDay) windows() bool {
	for _, e := range g.latest {
		if g.now.Sub(e.RecordedAt) < graceWindow {
			return true
		}
	}
	return false
}

// plantLines returns one plant's schedules, in Resolve's order. An empty result
// means the garden does not have the plant, since ListCareSchedules leaves out
// archived plants.
func plantLines(lines []schedule.Line, plantID uuid.UUID) []schedule.Line {
	var out []schedule.Line
	for _, line := range lines {
		if line.Plant.ID == plantID {
			out = append(out, line)
		}
	}
	return out
}

type todayPage struct {
	Date     string
	Garden   string
	Sheet    *sheet
	Head     todayHead
	Sections []todaySection
	Feed     todayFeed
	// OOB is true when the body is rendered as an out-of-band swap, so a
	// response to one row can also replace the rest of the page.
	OOB bool
}

type todayHead struct {
	// Exactly one of Summary, Clear and Empty is set. Clear stands in for
	// Empty while logged rows are still inside their grace windows, because
	// the empty state would say they had gone.
	Summary *todaySummary
	Clear   bool
	Empty   *todayEmpty
	OOB     bool
}

type todaySummary struct {
	Outstanding int
	Overdue     int
}

type todaySection struct {
	// ID is the HTML id of the section heading, so the section is a labelled
	// region.
	ID    string
	Title string
	// Alert colours the title. Only Overdue has it.
	Alert bool
	Rows  []careRow
}

type todayEmpty struct {
	Title string
	Line  string
	// Done shows the tick icon instead of the leaf.
	Done   bool
	Action *link
}

type link struct {
	Label string
	Href  string
}

// careSwap is the response to a swap of one row. The heading is included
// because logging the last outstanding care changes what it says. The feed is
// included because every log and undo adds or removes a line.
type careSwap struct {
	Row  careRow
	Head todayHead
	Feed todayFeed
}

// careSettled is the response when a row's grace window has closed. Only the
// heading is sent while other rows are still inside their windows, because
// replacing the body would remove them too. The feed is left out because a
// closing window records nothing.
type careSettled struct {
	Head todayHead
	Body *todayPage
}

func newCareSettled(principal auth.Principal, g gardenDay) careSettled {
	if g.windows() {
		return careSettled{Head: swapHead(principal, g)}
	}
	page := newTodayPage(principal, g)
	page.OOB = true
	return careSettled{Body: &page}
}

// careRow is the data the care-row fragment renders from, on a page load and
// on a swap.
type careRow struct {
	// ID is the id a swap targets. It uses the care type's slug rather than
	// its name, so renaming a type does not change it.
	ID string
	// Href opens the sheet for the row. Path is the URL the care button posts
	// to.
	Href string
	Path string
	Name string
	// Botanical is true when Name is the botanical name, shown in italics.
	Botanical bool
	Location  string
	// Late is the overdue text, empty on a row that is not overdue.
	Late string
	// When is the due text on a row that is coming up, empty otherwise. When
	// it is set the row shows the quiet Log button instead of the care
	// button, since the care is not due yet.
	When string
	Care string
	Slug string
	// Done marks a row whose care was just logged, and Said is its meta line.
	// Grace and Collapse are in milliseconds because the style attribute and
	// the trigger delay are written in them.
	Done     bool
	Said     string
	Undo     string
	Grace    int
	Collapse int
}

// careRowPrefix starts every care row id, so a swap targeting a row can be
// told from any other.
const careRowPrefix = "care-"

func careRowID(plant store.Plant, careType store.CareType) string {
	return fmt.Sprintf("%s%s-%s", careRowPrefix, plant.ID, careType.Slug)
}

func newTodayPage(principal auth.Principal, g gardenDay) todayPage {
	day, latest, plants, now := g.day, g.latest, g.plants, g.now
	page := todayPage{
		Date:   now.Format("Monday 2 January"),
		Garden: principal.Garden.Name,
		Feed:   newTodayFeed(principal, g),
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

	page.Head = newTodayHead(principal, day, latest, plants, now)
	return page
}

// newTodayHead builds the heading for a page load.
func newTodayHead(principal auth.Principal, day schedule.Day, latest []store.CareEvent, plants int64, now time.Time) todayHead {
	if outstanding := len(day.Overdue) + len(day.DueToday); outstanding > 0 {
		return todayHead{Summary: &todaySummary{Outstanding: outstanding, Overdue: len(day.Overdue)}}
	}
	return todayHead{Empty: newTodayEmpty(principal, day, latest, plants, now)}
}

// swapHead builds the heading for a swap response. Clear is only set here,
// because a page load never renders a row inside a grace window.
func swapHead(principal auth.Principal, g gardenDay) todayHead {
	head := newTodayHead(principal, g.day, g.latest, g.plants, g.now)
	if head.Empty != nil && g.windows() {
		head = todayHead{Clear: true}
	}
	head.OOB = true
	return head
}

// newTodayEmpty picks which empty state to show. The tick means something was
// finished, so a day with nothing scheduled gets the leaf. The next-up line and
// the link to the plant list are shown only when Coming up is empty, since
// otherwise that section says the same thing.
func newTodayEmpty(principal auth.Principal, day schedule.Day, latest []store.CareEvent, plants int64, now time.Time) *todayEmpty {
	if plants == 0 {
		// The link is shown only to a reader who may create a plant, since the
		// route refuses anyone else.
		empty := &todayEmpty{
			Title: "No plants yet",
			Line:  "Add a plant and sprig will remind you when to water it.",
		}
		if principal.Can(auth.PlantCreate) {
			empty.Action = &link{Label: "Add a plant", Href: newPlantPath}
		}
		return empty
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
		empty.Line = fmt.Sprintf("%s is next, %s.", next.Plant.DisplayName(), comingWord(next.Care, now))
	}
	empty.Action = &link{Label: "See all plants", Href: "/plants"}
	return empty
}

// caredForOn reports whether any care was performed on now's date in now's
// timezone. A skip counts, since it dealt with the row as much as a watering
// did.
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
		Href:      sheetPath(row.Plant.ID, row.Care.CareType.Slug),
		Path:      logPath(row.Plant.ID),
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
		r.Late = overdueWord(row.Care, now)
	case schedule.Upcoming:
		r.When = comingWord(row.Care, now)
	}
	return r
}

// feedLength is how many events the feed at the bottom of Today shows. A
// household of two or three logs about five cares a day, and the whole log is
// on Activity.
const feedLength = 5

// todayFeed is its own element so a swap response to one row can replace it
// alongside the row.
type todayFeed struct {
	Lines []feedLine
	OOB   bool
}

type feedLine struct {
	Who   string
	Did   string
	Plant string
	When  string
	// Undo is the URL the line's form posts to delete the event. Empty when
	// the reader may not undo it.
	Undo string
}

func newTodayFeed(principal auth.Principal, g gardenDay) todayFeed {
	feed := todayFeed{Lines: make([]feedLine, 0, len(g.recent))}
	for _, e := range g.recent {
		feed.Lines = append(feed.Lines, newFeedLine(principal, e, g.now))
	}
	return feed
}

func newFeedLine(principal auth.Principal, e store.ListRecentCareEventsRow, now time.Time) feedLine {
	line := feedLine{
		Plant: e.Plant.DisplayName(),
		When:  feedWhen(e.CareEvent.PerformedAt, now),
	}
	line.Who, line.Did = whoDid(principal, e.PerformedByName, e.CareEvent, e.CareType)
	if mayUndo(principal, e.CareEvent, now) {
		line.Undo = undoFormPath(e.CareEvent.PlantID, e.CareEvent.ID)
	}
	return line
}

// mayUndo reports whether a feed line shows Undo. The event has to be the
// reader's own and inside undoWindow, since undo is for a care just recorded.
// The delete itself is limited only by capability, so a button rendered inside
// the window still works after it closes.
func mayUndo(principal auth.Principal, e store.CareEvent, now time.Time) bool {
	if e.PerformedBy != principal.User.ID || now.Sub(e.RecordedAt) >= undoWindow {
		return false
	}
	return principal.Can(auth.CareDeleteOwn) || principal.Can(auth.CareDeleteAny)
}

// swapFeed builds the feed for a swap response. It is out of band because the
// swap targets a row.
func swapFeed(principal auth.Principal, g gardenDay) todayFeed {
	feed := newTodayFeed(principal, g)
	feed.OOB = true
	return feed
}
