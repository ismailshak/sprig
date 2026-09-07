package http

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/schedule"
	"github.com/ismailshak/sprig/internal/store"
)

const activityPath = "/activity"

// plantActivityPath is the URL of the activity log filtered to one plant. It is
// the same page as the whole garden's log with a query parameter on it.
func plantActivityPath(plantID uuid.UUID) string {
	return logQuery{plant: &plantID}.href()
}

// logPageSize is how many events one page of the activity log shows. Twenty is
// a few days for a garden that logs about five cares a day.
const logPageSize = 20

// gardenSilence is the shortest gap between days that gets a "nothing for N
// days" marker. A minimum of one would put a marker between most days, since
// something is logged most days.
const gardenSilence = 3

// shortestPlantSilence is the smallest gap the filtered log will label, in days.
//
// Filtered to one plant, every row is separated from the next by the plant's
// watering interval, so a fixed floor of three days would label every row. The
// floor there is twice the plant's own median interval instead, and never less
// than this, so a plant watered daily does not collect a label every other row.
const shortestPlantSilence = 14

type activity struct {
	logger    *slog.Logger
	queries   *store.Queries
	templates *Templates
	now       func() time.Time
}

// logQuery is the query string of a request for the log. plant is nil for the
// whole garden, before is nil for the newest page.
type logQuery struct {
	plant  *uuid.UUID
	before *cursor
}

const (
	plantParam  = "plant"
	beforeParam = "before"
)

// parseLogQuery reads the query string. It returns false when either
// parameter is present and unparseable. The handler then returns 404, because
// such a URL names no page.
func parseLogQuery(r *http.Request) (logQuery, bool) {
	var q logQuery
	values := r.URL.Query()
	if s := values.Get(plantParam); s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			return q, false
		}
		q.plant = &id
	}
	if s := values.Get(beforeParam); s != "" {
		c, ok := parseCursor(s)
		if !ok {
			return q, false
		}
		q.before = &c
	}
	return q, true
}

// values is the query string as parameters. The correcting sheet's URLs carry
// the same ones, so that a save or a delete can render the page the sheet was
// opened over.
func (q logQuery) values() url.Values {
	values := url.Values{}
	if q.plant != nil {
		values.Set(plantParam, q.plant.String())
	}
	if q.before != nil {
		values.Set(beforeParam, q.before.String())
	}
	return values
}

func (q logQuery) href() string {
	values := q.values()
	if len(values) == 0 {
		return activityPath
	}
	return activityPath + "?" + values.Encode()
}

// cursor identifies the last event on a page. The next page holds the events
// ordering after it, so the page a reader is looking at does not shift when care
// is logged above it, as it would under an offset.
//
// It holds the id as well as the time because the log is ordered by
// (performed_at, id) and performed_at is only minute precise when somebody
// backdates the care. Several plants logged as "yesterday at 9am" share one
// timestamp, and a cursor holding the timestamp alone would either repeat those
// events on the next page or skip them.
type cursor struct {
	at time.Time
	id uuid.UUID
}

// String formats the cursor for the query string, as an RFC 3339 timestamp and a
// uuid separated by a comma. The timestamp is in UTC so that one instant has one
// spelling in the URL, and the same page has one address whatever zone the
// reader keeps.
func (c cursor) String() string {
	return c.at.UTC().Format(time.RFC3339Nano) + "," + c.id.String()
}

func parseCursor(s string) (cursor, bool) {
	stamp, id, found := strings.Cut(s, ",")
	if !found {
		return cursor{}, false
	}
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return cursor{}, false
	}
	event, err := uuid.Parse(id)
	if err != nil {
		return cursor{}, false
	}
	return cursor{at: at, id: event}, true
}

func (h *activity) show(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	q, ok := parseLogQuery(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	page, err := h.page(r.Context(), principal, q)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		http.NotFound(w, r)
		return
	case err != nil:
		serverError(h.logger, w, r, "load the log", err)
		return
	}
	// The events a cursor points past can be deleted while somebody is looking
	// at the link to them. There is nothing to render and no Older link to put
	// under it, so send the reader to the top of the log rather than to a page
	// holding one sentence and no way on.
	if page.Empty != nil && q.before != nil {
		http.Redirect(w, r, logQuery{plant: q.plant}.href(), http.StatusSeeOther)
		return
	}
	h.templates.render(w, r, view{page: "activity", fragment: logFragment(r)}, page)
}

// logBodyID is the HTML id of the list, the pager and the empty state together.
// A save and the end of a delete's undo window both swap it, because either can
// change a day's count, the gap labels around a row, and whether there is
// anything on the page at all.
const logBodyID = "log-body"

// logFragment picks the part of the page a swap returns. A swap aimed at the
// log body gets that alone, and everything else, a page navigation included,
// gets the whole page.
func logFragment(r *http.Request) string {
	if r.Header.Get("HX-Target") == logBodyID {
		return logBodyID
	}
	return ""
}

func (h *activity) page(ctx context.Context, principal auth.Principal, q logQuery) (activityPage, error) {
	now := h.now().In(locationFor(principal.User))

	// Read the plant first so that an id from another garden returns 404. Passing
	// the id straight to the query would instead return no rows and render an
	// empty log for a plant the reader may not see.
	var plant *store.Plant
	if q.plant != nil {
		p, err := h.queries.GetPlant(ctx, principal.Garden.ID, *q.plant)
		if err != nil {
			return activityPage{}, err
		}
		plant = &p
	}

	params := store.ListCareEventLogParams{
		GardenID: principal.Garden.ID,
		PlantID:  q.plant,
		// One row more than the page holds. If it is returned there is another
		// page. That is cheaper than counting the whole log.
		Count: logPageSize + 1,
	}
	if q.before != nil {
		params.BeforeAt = &q.before.at
		params.BeforeID = &q.before.id
	}
	events, err := h.queries.ListCareEventLog(ctx, params)
	if err != nil {
		return activityPage{}, fmt.Errorf("list the log: %w", err)
	}
	plants, err := h.queries.CountPlants(ctx, principal.Garden.ID)
	if err != nil {
		return activityPage{}, fmt.Errorf("count the plants: %w", err)
	}

	return newActivityPage(principal, q, plant, events, plants, now), nil
}

type activityPage struct {
	// Back links to the plant the log is filtered to. It is nil for the whole
	// garden's log, which is reached from the tab bar and needs no way back.
	Back  *link
	Items []logItem
	// Pager is nil when the log fits on one page.
	Pager *pager
	// Empty is set only when Items is empty.
	Empty *activityEmpty
	// Sheet is the correcting sheet, open over one of the rows. It is nil when
	// the page is rendered with no sheet on it.
	Sheet *sheet
}

// pager holds the URLs of the links under the list. Older is empty on the last
// page and Latest on the first.
//
// There is no link to the previous page. It would have to be anchored at its
// bottom edge, so its contents would change whenever care was logged. Latest
// returns to the top of the log instead, and is the same URL from every page.
type pager struct {
	Older  string
	Latest string
}

// logItem is one item in the activity log. Exactly one field is set.
type logItem struct {
	Day     *dayMarker
	Silence *silence
	Event   *eventRow
}

type dayMarker struct {
	Title string
	// Count is how many events were logged that day.
	Count int
}

type silence struct {
	Label string
}

type eventRow struct {
	// ID is the HTML id of the row. A delete swaps it.
	ID string
	// Href is the URL of the sheet that corrects this event. It is empty when
	// the reader may not correct it, and the row is then not pressable.
	Href string
	// Act is the care as a headline, "Watered" or "Skipped". It is set only on
	// the log filtered to one plant, where the plant's name would be the same on
	// every row and the care is what differs. The template renders the plant's
	// name instead when Act is empty.
	Act string
	// Slug picks the icon shown in place of the plant's photo on a filtered row.
	Slug string
	// Skipped is true for a skip. The icon is then grey instead of green.
	Skipped bool
	Name    string
	// Botanical is true when Name is the plant's botanical name.
	Botanical bool
	// Picture is the URL of the plant's profile picture as a square, empty for
	// a plant with no picture. The row shows it only when Act is empty, in
	// place of the care icon.
	Picture string
	// Who and Did make up "Sam watered". Did is empty on a filtered row, where
	// Act has already said what was done.
	Who string
	Did string
	// When is the time of day on the whole garden's log, where a heading above
	// the row gives the date. A filtered log has no headings, so When there is
	// the date and the time.
	When string
	// Extra is the note and, on a skip, when it will be asked again. Empty on
	// most rows.
	Extra string

	// Deleted is true for the row a delete leaves in place of the event while
	// its undo window runs. The row keeps its lead and says "Deleted", with an
	// Undo button where the event's own line was.
	Deleted bool
	// Restore is the URL the Undo button posts to, set only on a deleted row.
	Restore string
	// Fields is the deleted event, as hidden inputs on the Undo form.
	Fields *restoreFields
	// Settled is the log's own URL. The row fetches it when its window closes.
	// The response replaces the whole log body, since a row leaving
	// changes the count on its day and the gaps around it.
	Settled string
	// Grace is the length of the undo window in milliseconds.
	Grace int
}

type activityEmpty struct {
	// NoPlants shows the plants icon rather than the activity one, since a
	// garden with no plants is a first run rather than an empty history.
	NoPlants bool
	Title    string
	Line     string
	Action   *link
}

func newActivityPage(principal auth.Principal, q logQuery, plant *store.Plant, events []store.ListCareEventLogRow, plants int64, now time.Time) activityPage {
	var page activityPage
	if plant != nil {
		page.Back = &link{Label: plant.DisplayName(), Href: plantPath(plant.ID)}
	}

	more := len(events) > logPageSize
	if more {
		events = events[:logPageSize]
	}
	if len(events) == 0 {
		page.Empty = newActivityEmpty(principal, plants)
		return page
	}

	if plant != nil {
		page.Items = plantLogItems(principal, q, events, now)
	} else {
		page.Items = logItems(principal, q, events, now)
	}
	page.Pager = newPager(q, events[len(events)-1].CareEvent, more)
	return page
}

// newPager returns nil when neither link applies, which is a log of one page.
func newPager(q logQuery, last store.CareEvent, more bool) *pager {
	var p pager
	if more {
		next := logQuery{plant: q.plant, before: &cursor{at: last.PerformedAt, id: last.ID}}
		p.Older = next.href()
	}
	if q.before != nil {
		p.Latest = logQuery{plant: q.plant}.href()
	}
	if p.Older == "" && p.Latest == "" {
		return nil
	}
	return &p
}

// newActivityEmpty builds the empty state. Only a garden with no plants gets a
// link, since care is logged on Today or a plant's page rather than here. A
// reader who may not create a plant gets no link.
func newActivityEmpty(principal auth.Principal, plants int64) *activityEmpty {
	if plants == 0 {
		empty := &activityEmpty{
			NoPlants: true,
			Title:    "No plants yet",
			Line:     "Add a plant and sprig will remind you when to water it.",
		}
		if principal.Can(auth.PlantCreate) {
			empty.Action = &link{Label: "Add a plant", Href: newPlantPath}
		}
		return empty
	}
	return &activityEmpty{
		Title: "Nothing recorded yet",
		Line:  "Water or feed something and it will show up here, with who did it and when.",
	}
}

// logItems builds the log items from events ordered newest first. Each day
// marker counts the run of events that follows it.
func logItems(principal auth.Principal, q logQuery, events []store.ListCareEventLogRow, now time.Time) []logItem {
	items := make([]logItem, 0, len(events))
	day := func(i int) int { return schedule.DaysBetween(events[i].CareEvent.PerformedAt, now) }

	for i := 0; i < len(events); {
		run := 1
		for i+run < len(events) && day(i+run) == day(i) {
			run++
		}
		if i > 0 {
			if quiet := day(i) - day(i-1) - 1; quiet >= gardenSilence {
				items = append(items, logItem{Silence: &silence{Label: silenceLabel(quiet)}})
			}
		}
		items = append(items, logItem{Day: &dayMarker{Title: dayHeading(events[i].CareEvent.PerformedAt, now), Count: run}})
		for _, e := range events[i : i+run] {
			items = append(items, logItem{Event: newEventRow(principal, q, e, now)})
		}
		i += run
	}
	return items
}

// plantLogItems builds the list for the log filtered to one plant, newest event
// first.
//
// It writes no day headings. A heading exists to give a date once for the
// several rows under it, and a plant is nearly always cared for at most once a
// day, so there would be a heading over every row. The date goes on the row
// instead.
//
// A gap here is the number of days between two cares, where the whole garden's
// log counts the days that held nothing at all.
func plantLogItems(principal auth.Principal, q logQuery, events []store.ListCareEventLogRow, now time.Time) []logItem {
	floor := plantSilenceFloor(events, now)

	items := make([]logItem, 0, len(events))
	for i, e := range events {
		if i > 0 {
			if gap := daysApart(events[i-1], e, now); gap >= floor {
				items = append(items, logItem{Silence: &silence{Label: silenceLabel(gap)}})
			}
		}
		items = append(items, logItem{Event: newPlantEventRow(principal, q, e, now)})
	}
	return items
}

func silenceLabel(days int) string {
	return "nothing for " + daysWord(days)
}

// plantSilenceFloor is the smallest gap this page will label, in days: twice the
// median interval between the events on it, and at least shortestPlantSilence.
func plantSilenceFloor(events []store.ListCareEventLogRow, now time.Time) int {
	gaps := make([]int, 0, len(events))
	for i := 1; i < len(events); i++ {
		gaps = append(gaps, daysApart(events[i-1], events[i], now))
	}
	if len(gaps) == 0 {
		return shortestPlantSilence
	}
	slices.Sort(gaps)
	return max(shortestPlantSilence, gaps[len(gaps)/2]*2)
}

// daysApart is the number of days between two events. newer is the more recent
// of the two.
func daysApart(newer, older store.ListCareEventLogRow, now time.Time) int {
	return schedule.DaysBetween(older.CareEvent.PerformedAt, now) - schedule.DaysBetween(newer.CareEvent.PerformedAt, now)
}

func newEventRow(principal auth.Principal, q logQuery, e store.ListCareEventLogRow, now time.Time) *eventRow {
	row := &eventRow{
		ID:        eventRowID(e.CareEvent.ID),
		Href:      correctHref(principal, q, e.CareEvent),
		Name:      e.Plant.DisplayName(),
		Botanical: e.Plant.BotanicalOnly(),
		Picture:   squarePicturePath(e.Plant),
		When:      clockWord(e.CareEvent.PerformedAt, now),
		Extra:     eventExtra(e.CareEvent),
	}
	row.Who, row.Did = whoDid(principal, e.PerformedByName, e.CareEvent, e.CareType)
	return row
}

// newPlantEventRow builds a row for the log filtered to one plant: the care
// instead of the plant's name, the care's icon instead of its photo, and the
// date on the row because the filtered log has no day headings.
func newPlantEventRow(principal auth.Principal, q logQuery, e store.ListCareEventLogRow, now time.Time) *eventRow {
	who, did := whoDid(principal, e.PerformedByName, e.CareEvent, e.CareType)
	return &eventRow{
		ID:      eventRowID(e.CareEvent.ID),
		Href:    correctHref(principal, q, e.CareEvent),
		Act:     capitalise(did),
		Slug:    e.CareType.Slug,
		Skipped: !e.CareEvent.Done,
		Who:     who,
		When:    agoWord(e.CareEvent.PerformedAt, now) + ", " + clockWord(e.CareEvent.PerformedAt, now),
		Extra:   eventExtra(e.CareEvent),
	}
}

func eventExtra(e store.CareEvent) string {
	var parts []string
	if !e.Done && e.OverrideIntervalDays != nil {
		parts = append(parts, "Asking again in "+daysWord(int(*e.OverrideIntervalDays)))
	}
	if e.Note != nil && *e.Note != "" {
		parts = append(parts, "“"+*e.Note+"”")
	}
	return strings.Join(parts, " · ")
}
