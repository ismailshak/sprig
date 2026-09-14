package http

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/schedule"
	"github.com/ismailshak/sprig/internal/store"
)

// calendarPath is the URL of the calendar. The query string names the month,
// "?month=2026-09", and the day whose sheet is open, "&day=2026-09-19".
const calendarPath = activityPath + "/calendar"

const (
	monthParam  = "month"
	dayParam    = "day"
	monthFormat = "2006-01"
)

// calendarMonths is how many months after the current one the calendar goes
// to. A cadence's dates are computed one interval at a time from today, so a
// later month takes longer to render.
const calendarMonths = 12

// calendarID is the HTML id of the element holding the month's heading, the
// cares due in the month and the grid. Previous and Next swap it.
const calendarID = "calendar"

// calendarContents is the template that renders the inside of #calendar. It is
// the response to a Back or Forward that misses htmx's history cache, because
// htmx puts that response inside #calendar.
const calendarContents = "calendar-contents"

// daySheetTemplate is the template rendering a day's sheet, or the empty
// placeholder when no day is open.
const daySheetTemplate = "day-sheet"

// shownCares is how many cares a day in the grid lists. The rest are counted
// as "+2".
const shownCares = 3

// calendarQuery is the query string of a request for the calendar.
type calendarQuery struct {
	// month is midnight on the first of the month in the reader's timezone.
	month time.Time
	// day is midnight on the day whose sheet is open, or nil.
	day *time.Time
}

// parseCalendarQuery reads the query string. now is the current time in the
// reader's timezone. A month that is missing, malformed or after the last
// month the calendar goes to is the current month. A day that is malformed or
// outside the month opens no sheet.
func parseCalendarQuery(values url.Values, now time.Time) calendarQuery {
	loc := now.Location()
	q := calendarQuery{month: startOfMonth(now)}
	if month, err := time.ParseInLocation(monthFormat, values.Get(monthParam), loc); err == nil && !month.After(lastCalendarMonth(now)) {
		q.month = month
	}
	if day, err := time.ParseInLocation(time.DateOnly, values.Get(dayParam), loc); err == nil && startOfMonth(day).Equal(q.month) {
		q.day = &day
	}
	return q
}

// lastCalendarMonth is the first of the last month the calendar goes to.
func lastCalendarMonth(now time.Time) time.Time {
	return startOfMonth(now).AddDate(0, calendarMonths, 0)
}

func startOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

// calendarHref is the URL of the calendar on month, with day's sheet open when
// day is not nil.
func calendarHref(month time.Time, day *time.Time) string {
	values := url.Values{monthParam: {month.Format(monthFormat)}}
	if day != nil {
		values.Set(dayParam, day.Format(time.DateOnly))
	}
	return calendarPath + "?" + values.Encode()
}

// calendar handles GET /activity/calendar.
func (h *activity) calendar(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	now := h.now().In(locationFor(principal.User))
	q := parseCalendarQuery(r.URL.Query(), now)
	page, err := h.calendarPage(r.Context(), principal, q, now)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "load the calendar", err)
		return
	}
	v := view{page: "calendar", fragment: calendarFragment(r)}
	if v.fragment == calendarID {
		v.announce = page.Title + "."
	}
	h.templates.render(w, r, v, page)
}

// calendarFragment picks the part of the page a request gets: the inside of
// #calendar for a history restore, #calendar for Previous, Next and Today, the
// sheet for a day's link, and the whole page for anything else.
func calendarFragment(r *http.Request) string {
	if r.Header.Get("HX-History-Restore-Request") == "true" {
		return calendarContents
	}
	switch r.Header.Get("HX-Target") {
	case calendarID:
		return calendarID
	case sheetID:
		return daySheetTemplate
	}
	return ""
}

func (h *activity) calendarPage(ctx context.Context, principal auth.Principal, q calendarQuery, now time.Time) (calendarPage, error) {
	schedules, err := h.queries.ListCareSchedules(ctx, principal.Garden.ID)
	if err != nil {
		return calendarPage{}, fmt.Errorf("list the schedules: %w", err)
	}
	latest, err := h.queries.ListLatestCareEvents(ctx, principal.Garden.ID)
	if err != nil {
		return calendarPage{}, fmt.Errorf("list the latest care: %w", err)
	}
	events, err := h.queries.ListCareEventsBetween(ctx, store.ListCareEventsBetweenParams{
		GardenID: principal.Garden.ID,
		Since:    q.month,
		Until:    q.month.AddDate(0, 1, 0),
	})
	if err != nil {
		return calendarPage{}, fmt.Errorf("list the month's care: %w", err)
	}
	return newCalendarPage(principal, q, schedule.Resolve(schedules, latest, now), events, now), nil
}

type calendarPage struct {
	// Back is the URL of the link back to Activity.
	Back string
	// Today is the URL of the Today link in the top bar. It points at the
	// current month.
	Today string
	// Title is the month and year, "September 2026".
	Title string
	// Previous and Next are the URLs of the months either side. Next is empty
	// on the last month the calendar goes to.
	Previous string
	Next     string
	// MonthDue is the cares due some time in the month with no day given. The
	// page lists them above the grid under MonthDueTitle, "Due in September".
	MonthDue      []calendarRow
	MonthDueTitle string
	// Weekdays is the grid's column headings, Monday first.
	Weekdays []string
	// Weeks is the grid's rows, seven days each, Monday first.
	Weeks [][]calendarDay
	// Sheet is the open day's sheet, or nil.
	Sheet *daySheet
}

// calendarDay is one cell of the grid.
type calendarDay struct {
	// Number is the day of the month. It is zero for a cell before the 1st or
	// after the month's last day.
	Number int
	// Href is the URL that opens the day's sheet. A day with nothing logged or
	// due has no Href and is not a link.
	Href string
	// Label is the link's accessible name, "Thursday 3 September, 1 logged, 2
	// due".
	Label string
	Today bool
	// Shown is the first shownCares cares on the day, the logged ones first.
	// More counts the rest.
	Shown []calendarRow
	More  int
}

// calendarRow is one care on the calendar, in a day's sheet or in the cares
// due in the month. A day in the grid shows only its Name and its Slug's icon.
type calendarRow struct {
	// Href is the URL of the plant's page.
	Href string
	Name string
	// Botanical is true when Name is the botanical name, shown in italics.
	Botanical bool
	// Picture is the URL of the plant's profile picture as a square, empty for
	// a plant with no picture.
	Picture string
	// Slug picks the care's icon.
	Slug string
	// Logged is true for care that was logged and false for care that is due.
	// Skipped is true for a logged skip.
	Logged  bool
	Skipped bool
	// Care is "Sam watered" on a logged row and the care type's name on a due
	// row.
	Care string
	// Note is the time of day on a logged row. On a due row it is how late an
	// overdue care is, "3 days late" or "overdue since August", "Estimated" for
	// a cadence date after the next one, and empty otherwise.
	Note string
	// Late colours Note as overdue.
	Late bool
}

// daySheet is the sheet a day in the grid opens.
type daySheet struct {
	// Title is the day, "Thursday 3 September", with the year added when it is
	// not the current year.
	Title  string
	Logged []calendarRow
	Due    []calendarRow
}

// dueCare is a schedule and one date in the month it falls due on.
type dueCare struct {
	line schedule.Line
	date schedule.Projected
}

func newCalendarPage(principal auth.Principal, q calendarQuery, lines []schedule.Line, events []store.ListCareEventsBetweenRow, now time.Time) calendarPage {
	loc := now.Location()
	page := calendarPage{
		Back:          activityPath,
		Today:         calendarPath,
		Title:         q.month.Format("January 2006"),
		Previous:      calendarHref(q.month.AddDate(0, -1, 0), nil),
		MonthDueTitle: "Due in " + q.month.Month().String(),
		Weekdays:      weekdays(),
	}
	if q.month.Before(lastCalendarMonth(now)) {
		page.Next = calendarHref(q.month.AddDate(0, 1, 0), nil)
	}

	// logged and due hold each day's care, indexed by the day of the month.
	// Index 0 is unused.
	//
	// q.month has to be midnight on the first in now's location. InMonth reads
	// the month in that location. A date from another month would be indexed
	// on the wrong day or past the end of due.
	days := q.month.AddDate(0, 1, -1).Day()
	logged := make([][]calendarRow, days+1)
	due := make([][]dueCare, days+1)
	for _, e := range events {
		d := e.CareEvent.PerformedAt.In(loc).Day()
		logged[d] = append(logged[d], newLoggedRow(principal, e, now))
	}
	var monthDue []dueCare
	for _, line := range lines {
		for _, date := range schedule.InMonth(line.Schedule, line.Last, q.month, now) {
			if date.Precision == schedule.PrecisionMonth {
				monthDue = append(monthDue, dueCare{line, date})
				continue
			}
			d := date.At.Day()
			due[d] = append(due[d], dueCare{line, date})
		}
	}
	page.MonthDue = dueRows(monthDue, now)

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	week := make([]calendarDay, mondayIndex(q.month))
	for d := 1; d <= days; d++ {
		date := time.Date(q.month.Year(), q.month.Month(), d, 0, 0, 0, 0, loc)
		day := calendarDay{Number: d, Today: date.Equal(today)}
		dueOnDay := dueRows(due[d], now)
		if cares := slices.Concat(logged[d], dueOnDay); len(cares) > 0 {
			day.Href = calendarHref(q.month, &date)
			day.Label = dayLabel(date, len(logged[d]), len(dueOnDay))
			day.Shown = cares[:min(len(cares), shownCares)]
			day.More = len(cares) - len(day.Shown)
			if q.day != nil && q.day.Equal(date) {
				page.Sheet = &daySheet{Title: dayTitle(date, now), Logged: logged[d], Due: dueOnDay}
			}
		}
		week = append(week, day)
		if len(week) == 7 {
			page.Weeks = append(page.Weeks, week)
			week = nil
		}
	}
	if len(week) > 0 {
		page.Weeks = append(page.Weeks, append(week, make([]calendarDay, 7-len(week))...))
	}
	return page
}

// weekdays is the grid's column headings, "Mon" to "Sun".
func weekdays() []string {
	names := make([]string, 7)
	for i := range names {
		names[i] = time.Weekday((i + 1) % 7).String()[:3]
	}
	return names
}

// mondayIndex is the column t's day falls in, counting Monday as 0.
func mondayIndex(t time.Time) int {
	return (int(t.Weekday()) + 6) % 7
}

func newLoggedRow(principal auth.Principal, e store.ListCareEventsBetweenRow, now time.Time) calendarRow {
	who, did := whoDid(principal, e.PerformedByName, e.CareEvent, e.CareType)
	return calendarRow{
		Href:      plantPath(e.Plant.ID),
		Name:      e.Plant.DisplayName(),
		Botanical: e.Plant.BotanicalOnly(),
		Picture:   squarePicturePath(e.Plant),
		Slug:      e.CareType.Slug,
		Logged:    true,
		Skipped:   !e.CareEvent.Done,
		Care:      who + " " + did,
		Note:      clockWord(e.CareEvent.PerformedAt, now),
	}
}

// dueRows returns the rows for due cares, overdue first and then by plant name.
// A plant's own cares keep the order of its schedules.
func dueRows(cares []dueCare, now time.Time) []calendarRow {
	slices.SortStableFunc(cares, func(a, b dueCare) int {
		return cmp.Or(
			overdueFirst(a, b),
			strings.Compare(strings.ToLower(a.line.Plant.DisplayName()), strings.ToLower(b.line.Plant.DisplayName())),
			strings.Compare(a.line.Plant.ID.String(), b.line.Plant.ID.String()),
		)
	})
	rows := make([]calendarRow, 0, len(cares))
	for _, c := range cares {
		row := calendarRow{
			Href:      plantPath(c.line.Plant.ID),
			Name:      c.line.Plant.DisplayName(),
			Botanical: c.line.Plant.BotanicalOnly(),
			Picture:   squarePicturePath(c.line.Plant),
			Slug:      c.line.CareType.Slug,
			Care:      c.line.CareType.Name,
		}
		switch {
		case c.date.Overdue:
			row.Note, row.Late = overdueWord(c.line, now), true
		case c.date.Estimated:
			row.Note = "Estimated"
		}
		rows = append(rows, row)
	}
	return rows
}

func overdueFirst(a, b dueCare) int {
	switch {
	case a.date.Overdue == b.date.Overdue:
		return 0
	case a.date.Overdue:
		return -1
	default:
		return 1
	}
}

func dayLabel(date time.Time, logged, due int) string {
	parts := []string{date.Format("Monday 2 January")}
	if logged > 0 {
		parts = append(parts, strconv.Itoa(logged)+" logged")
	}
	if due > 0 {
		parts = append(parts, strconv.Itoa(due)+" due")
	}
	return strings.Join(parts, ", ")
}

func dayTitle(date, now time.Time) string {
	if date.Year() != now.Year() {
		return date.Format("Monday 2 January 2006")
	}
	return date.Format("Monday 2 January")
}
