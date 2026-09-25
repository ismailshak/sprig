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
	"uuid"

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

// calendarGridID is the HTML id of the month's grid. Adding, saving and
// deleting a note swap it, because a note changes the marks on the grid and
// nothing else in #calendar.
const calendarGridID = "calendar-grid"

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

// bandColours is how many colours the stylesheet has for the bands that show
// each member's days on the grid. Bands take them in turn.
const bandColours = 3

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
	members, err := h.queries.ListMembers(ctx, principal.Garden.ID)
	if err != nil {
		return calendarPage{}, fmt.Errorf("list the members: %w", err)
	}
	notes, err := h.queries.ListCalendarNotesBetween(ctx, store.ListCalendarNotesBetweenParams{
		GardenID: principal.Garden.ID,
		Since:    q.month,
		Until:    q.month.AddDate(0, 1, -1),
	})
	if err != nil {
		return calendarPage{}, fmt.Errorf("list the month's notes: %w", err)
	}
	return newCalendarPage(principal, q, schedule.Resolve(schedules, latest, now), events, sittings(principal, members, now), calendarNotes(principal, q, notes, now), now), nil
}

// calendarNote is one note on the calendar, a line of text over a run of
// days.
type calendarNote struct {
	ID   uuid.UUID
	Text string
	// First and Last are midnight in the reader's timezone on the note's
	// first and last days.
	First, Last time.Time
	// Span is the second line of the note's row in a day's sheet, "3–17 Oct".
	// It is empty for a note on one day.
	Span string
	// Href is the URL of the Edit note sheet. It is empty for a reader who may
	// not edit notes. Their row is plain text.
	Href string
}

// covers reports whether date, midnight in the reader's timezone, is one of
// the note's days.
func (n calendarNote) covers(date time.Time) bool {
	return !date.Before(n.First) && !date.After(n.Last)
}

// calendarNotes returns the month's notes as the calendar shows them. A
// note's days are calendar dates with no timezone, so they are read as the
// same dates in the reader's timezone.
func calendarNotes(principal auth.Principal, q calendarQuery, notes []store.CalendarNote, now time.Time) []calendarNote {
	loc := now.Location()
	out := make([]calendarNote, 0, len(notes))
	for _, n := range notes {
		note := calendarNote{
			ID:    n.ID,
			Text:  n.Text,
			First: midnightOn(n.StartsOn, loc),
			Last:  midnightOn(n.EndsOn, loc),
		}
		note.Span = dayRange(note.First, note.Last, now)
		if principal.Can(auth.CalendarNoteManage) {
			note.Href = notePath(n.ID, q.month)
		}
		out = append(out, note)
	}
	return out
}

// dayRange is the days a note covers, "3–17 Oct" or "28 Sep–3 Oct". A year
// is added to a date that is not in now's year, once for both dates when they
// share it.
func dayRange(first, last, now time.Time) string {
	if first.Equal(last) {
		return ""
	}
	end := last.Format("2 Jan")
	if last.Year() != now.Year() {
		end = last.Format("2 Jan 2006")
	}
	switch {
	case first.Year() != last.Year() && first.Year() != now.Year():
		return first.Format("2 Jan 2006") + "–" + end
	case first.Year() != last.Year() || first.Month() != last.Month():
		return first.Format("2 Jan") + "–" + end
	default:
		return first.Format("2") + "–" + end
	}
}

// sitting is a membership with an end date, as the calendar shows it. It
// covers the days from the one the membership was made on to the last one with
// any access.
type sitting struct {
	// Name is the member's display name, or "You" for the reader's own
	// membership.
	Name string
	You  bool
	// First and Last are midnight in the reader's timezone on the first and
	// last covered days.
	First, Last time.Time
	// Until is the second line of the member's row in a day's sheet, "until 14
	// Sep" or "Access ended 14 Sep".
	Until string
}

// covers reports whether date, midnight in the reader's timezone, is one of
// the sitting's days.
func (s sitting) covers(date time.Time) bool {
	return !date.Before(s.First) && !date.After(s.Last)
}

// sittings returns the memberships with an end date the reader may see, in
// the order they were made. A reader with the sitting.view capability sees
// every one. Any other reader sees their own membership alone. A membership
// whose end date is on or before the day it was made covers no day and is
// left out.
//
// The dates are read in the member's timezone, because People shows and sets
// the end date there. The calendar marks the same dates in the reader's
// timezone.
func sittings(principal auth.Principal, members []store.ListMembersRow, now time.Time) []sitting {
	loc := now.Location()
	var out []sitting
	for _, m := range members {
		if m.Membership.ExpiresAt == nil {
			continue
		}
		you := m.Membership.ID == principal.Membership.ID
		if !you && !principal.Can(auth.SittingView) {
			continue
		}
		memberLoc := locationFor(m.AppUser)
		// An end date set on People or an invite is midnight. Access is gone
		// for the whole of that day. Any other instant still covers the day it
		// falls in.
		ends := m.Membership.ExpiresAt.In(memberLoc)
		last := midnightOn(ends, memberLoc)
		if last.Equal(ends) {
			last = last.AddDate(0, 0, -1)
		}
		s := sitting{
			Name:  m.AppUser.DisplayName,
			You:   you,
			First: midnightOn(m.Membership.CreatedAt.In(memberLoc), loc),
			Last:  midnightOn(last, loc),
			Until: accessUntilWord(m, now),
		}
		if s.Last.Before(s.First) {
			continue
		}
		if you {
			s.Name = "You"
		}
		out = append(out, s)
	}
	return out
}

// midnightOn is midnight in loc on the date t has in its own location.
func midnightOn(t time.Time, loc *time.Location) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
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
	// NoteSheet is the Add note or Edit note sheet, open in place of the day's
	// sheet, or nil.
	NoteSheet *noteSheet
}

// calendarDay is one cell of the grid.
type calendarDay struct {
	// Number is the day of the month. It is zero for a cell before the 1st or
	// after the month's last day.
	Number int
	// ID is the HTML id of the cell, "day-2026-09-03". After a saved or
	// deleted note swaps the grid, the sheet's script puts focus back on the
	// link inside the cell with the id the opener's cell had.
	ID string
	// Href is the URL that opens the day's sheet. Every day is a link for a
	// reader who may add a note. For anyone else, a day with nothing logged or
	// due, no note and no sitting they can see has no Href and is not a link.
	Href string
	// Label is the link's accessible name, "Thursday 3 September, Ellie away,
	// 1 logged, 2 due".
	Label string
	Today bool
	// Shown is the first shownCares cares on the day, the logged ones first.
	// More counts the rest.
	Shown []calendarRow
	More  int
	// Sitting is true on a day the reader's own access covers, for a reader
	// who sees no other member's days. The stylesheet tints the whole cell.
	Sitting bool
	// Bands is one band per member with an end date whose days fall in the
	// month, in the order the memberships were made. Bands is empty for a
	// reader who sees only their own days.
	Bands []calendarBand
	Notes []calendarNote
}

// calendarBand is one member's band on a day in the grid. A day the member
// does not cover still renders the band, empty, so the bands below it stay at
// the same height.
type calendarBand struct {
	Covered bool
	// Colour is 1 to bandColours and picks the band's colour in the
	// stylesheet.
	Colour int
	// Starts and Ends are true on the member's first and last covered day.
	// The stylesheet rounds those ends of the band.
	Starts, Ends bool
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
	Notes  []calendarNote
	Logged []calendarRow
	Due    []calendarRow
	// Sitting is the members with an end date whose days include this one.
	Sitting []sitting
	// AddNote is the URL of the Add note link. It is empty for a reader who
	// may not add a note.
	AddNote string
}

// dueCare is a schedule and one date in the month it falls due on.
type dueCare struct {
	line schedule.Line
	date schedule.Projected
}

func newCalendarPage(principal auth.Principal, q calendarQuery, lines []schedule.Line, events []store.ListCareEventsBetweenRow, sittings []sitting, notes []calendarNote, now time.Time) calendarPage {
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

	// inMonth is the sittings with a day in the month, one band each.
	lastDay := q.month.AddDate(0, 1, -1)
	var inMonth []sitting
	for _, s := range sittings {
		if !s.Last.Before(q.month) && !s.First.After(lastDay) {
			inMonth = append(inMonth, s)
		}
	}
	showBands := principal.Can(auth.SittingView)
	mayManageNotes := principal.Can(auth.CalendarNoteManage)

	today := midnightOn(now, loc)
	week := make([]calendarDay, mondayIndex(q.month))
	for d := 1; d <= days; d++ {
		date := time.Date(q.month.Year(), q.month.Month(), d, 0, 0, 0, 0, loc)
		day := calendarDay{ID: dayID(date), Number: d, Today: date.Equal(today)}
		for _, n := range notes {
			if n.covers(date) {
				day.Notes = append(day.Notes, n)
			}
		}
		var covering []sitting
		for i, s := range inMonth {
			band := calendarBand{Covered: s.covers(date)}
			if band.Covered {
				band.Colour = i%bandColours + 1
				band.Starts, band.Ends = date.Equal(s.First), date.Equal(s.Last)
				covering = append(covering, s)
			}
			if showBands {
				day.Bands = append(day.Bands, band)
			}
		}
		day.Sitting = !showBands && len(covering) > 0
		dueOnDay := dueRows(due[d], now)
		cares := slices.Concat(logged[d], dueOnDay)
		if len(cares) > 0 || len(covering) > 0 || len(day.Notes) > 0 || mayManageNotes {
			day.Href = calendarHref(q.month, &date)
			day.Label = dayLabel(date, day.Notes, len(logged[d]), len(dueOnDay), covering)
			day.Shown = cares[:min(len(cares), shownCares)]
			day.More = len(cares) - len(day.Shown)
			if q.day != nil && q.day.Equal(date) {
				page.Sheet = &daySheet{Title: dayTitle(date, now), Notes: day.Notes, Logged: logged[d], Due: dueOnDay, Sitting: covering}
				if mayManageNotes {
					page.Sheet.AddNote = newNotePath(q.month, date)
				}
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

func dayID(date time.Time) string {
	return "day-" + date.Format(time.DateOnly)
}

func dayLabel(date time.Time, notes []calendarNote, logged, due int, covering []sitting) string {
	parts := []string{date.Format("Monday 2 January")}
	for _, n := range notes {
		parts = append(parts, n.Text)
	}
	if logged > 0 {
		parts = append(parts, strconv.Itoa(logged)+" logged")
	}
	if due > 0 {
		parts = append(parts, strconv.Itoa(due)+" due")
	}
	if len(covering) > 0 {
		parts = append(parts, sittingWords(covering))
	}
	return strings.Join(parts, ", ")
}

// sittingWords names the members covering a day, "Jo sitting" or "Jo and
// Clare sitting". It is "you’re sitting" when the reader's own membership is
// the only one.
func sittingWords(covering []sitting) string {
	if len(covering) == 1 && covering[0].You {
		return "you’re sitting"
	}
	names := make([]string, len(covering))
	for i, s := range covering {
		names[i] = s.Name
		if s.You {
			names[i] = "you"
		}
	}
	return andList(names) + " sitting"
}

func dayTitle(date, now time.Time) string {
	if date.Year() != now.Year() {
		return date.Format("Monday 2 January 2006")
	}
	return date.Format("Monday 2 January")
}
