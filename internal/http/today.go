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
	// now is the clock the day is read from, so a test can pin the day.
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

// graceWindow is how long a logged row keeps its place on Today with an Undo
// beside it. It is short because it runs as a timer in the page.
const graceWindow = 4 * time.Second

// undoWindow is how long after an event was recorded the feed offers it back.
// It is longer than graceWindow because the feed's Undo sits on a page that
// stays as it was drawn, reached by a redirect with no script running. The
// stylesheet hides that button whenever a script is running.
//
// It decides what a render draws and nothing else. A button already on the
// page goes on working however long it has been there because refusing a
// press the page invited is worse than deleting an event a minute late.
const undoWindow = 30 * time.Second

// collapseMS is the duration of the stylesheet's transition on a leaving row.
const collapseMS = 320

// gardenDay is one read of the garden against the reader's clock. The page
// and the sheet over it draw from the same one because the two have to agree
// about the schedule.
type gardenDay struct {
	lines  []schedule.Line
	day    schedule.Day
	latest []store.CareEvent
	recent []store.ListRecentCareEventsRow
	plants int64
	// now is in the reader's location.
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

// plantLines is every schedule the plant has, in Resolve's order. An empty
// result is a plant the garden does not have because ListCareSchedules leaves
// out an archived plant.
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
	// OOB marks the body as an out-of-band swap, which is how an answer aimed
	// at one row reaches the rest of the day.
	OOB bool
}

type todayHead struct {
	// Exactly one of the three stands. Clear holds Empty's place while logged
	// rows are still inside their windows because the empty screen would claim
	// they had gone.
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

// careSwap is what a swap of one row answers with. The head comes back beside
// the row because logging the last thing outstanding changes what it says. The
// feed comes back because every log and undo adds or removes one of its lines.
type careSwap struct {
	Row  careRow
	Head todayHead
	Feed todayFeed
}

// careSettled is what a row whose grace window has closed answers with. The
// head comes back alone while other rows are still inside their windows
// because replacing the body under them would take them with it. The feed is
// not in it because a closing window records and reverses nothing.
type careSettled struct {
	Head todayHead
	Body *todayPage
}

func newCareSettled(principal auth.Principal, g gardenDay) careSettled {
	if g.windows() {
		return careSettled{Head: swapHead(g)}
	}
	page := newTodayPage(principal, g)
	page.OOB = true
	return careSettled{Body: &page}
}

// careRow is what the care-row fragment takes, so a page load and a swap draw
// the same row.
type careRow struct {
	// ID is the id a swap targets. It carries the care type's slug rather
	// than its name, because renaming a type leaves the slug alone.
	ID string
	// Href opens the sheet for the row. Path is where the care button posts.
	Href string
	Path string
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
	// Done marks a row whose care was just logged. Said is what its meta line
	// says instead. Grace and Collapse are in milliseconds because the style
	// attribute and the trigger delay are written in them.
	Done     bool
	Said     string
	Undo     string
	Grace    int
	Collapse int
}

// careRowPrefix opens every row id, which is how a swap aimed at a row is
// told from one aimed at anything else.
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

	page.Head = newTodayHead(day, latest, plants, now)
	return page
}

// newTodayHead is the head as a page load draws it.
func newTodayHead(day schedule.Day, latest []store.CareEvent, plants int64, now time.Time) todayHead {
	if outstanding := len(day.Overdue) + len(day.DueToday); outstanding > 0 {
		return todayHead{Summary: &todaySummary{Outstanding: outstanding, Overdue: len(day.Overdue)}}
	}
	return todayHead{Empty: newTodayEmpty(day, latest, plants, now)}
}

// swapHead is the head as an answer to a swap draws it. Clear is reachable
// only here because a navigation draws no row inside a grace window.
func swapHead(g gardenDay) todayHead {
	head := newTodayHead(g.day, g.latest, g.plants, g.now)
	if head.Empty != nil && g.windows() {
		head = todayHead{Clear: true}
	}
	head.OOB = true
	return head
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
		r.Late = lateWord(-row.Care.Days)
	case schedule.Upcoming:
		r.When = whenWord(row.Care.Days, now)
	}
	return r
}

// feedLength is how many events the feed at the foot of Today carries. A
// household of two or three logs about five cares in a day, and the whole log
// is on Activity.
const feedLength = 5

// todayFeed is the feed as its own element because an answer aimed at one row
// swaps it in beside the row.
type todayFeed struct {
	Lines []feedLine
	OOB   bool
}

type feedLine struct {
	Who   string
	Did   string
	Plant string
	When  string
	// Undo is where the line's form posts to delete the event. It is empty on
	// a line the reader may not take back.
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
		Who:   e.PerformedByName,
		Did:   carePast(e.CareType),
		Plant: e.Plant.DisplayName(),
		When:  feedWhen(e.CareEvent.PerformedAt, now),
	}
	if e.CareEvent.PerformedBy == principal.User.ID {
		line.Who = "You"
	}
	if !e.CareEvent.Done {
		line.Did = "skipped"
	}
	if mayUndo(principal, e.CareEvent, now) {
		line.Undo = undoFormPath(e.CareEvent.PlantID, e.CareEvent.ID)
	}
	return line
}

// mayUndo reports whether the feed draws a line's Undo. The line has to be
// the reader's own and inside undoWindow because an undo is the way back from
// a care just recorded. The delete behind the button is bounded by the
// capability alone. A button drawn inside the window still works after it.
func mayUndo(principal auth.Principal, e store.CareEvent, now time.Time) bool {
	if e.PerformedBy != principal.User.ID || now.Sub(e.RecordedAt) >= undoWindow {
		return false
	}
	return principal.Can(auth.CareDeleteOwn) || principal.Can(auth.CareDeleteAny)
}

// swapFeed is the feed as an answer to a swap draws it, out of band because
// the swap that asked for it is aimed at a row.
func swapFeed(principal auth.Principal, g gardenDay) todayFeed {
	feed := newTodayFeed(principal, g)
	feed.OOB = true
	return feed
}
