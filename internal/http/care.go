package http

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/schedule"
	"github.com/ismailshak/sprig/internal/store"
)

// sheetFormID is the id a What chip names as the target of its swap.
const sheetFormID = "sheet-form"

const (
	whenNow       = "now"
	whenToday     = "today"
	whenYesterday = "yesterday"
	whenOther     = "other"
)

var whens = []chip{
	{Value: whenNow, Label: "Just now"},
	{Value: whenToday, Label: "Earlier today"},
	{Value: whenYesterday, Label: "Yesterday"},
	{Value: whenOther, Label: "Another day"},
}

// The browser's time and datetime-local inputs send these two formats.
const (
	clockLayout = "15:04"
	atLayout    = "2006-01-02T15:04"
)

func logPath(plantID uuid.UUID) string {
	return "/plants/" + plantID.String() + "/log"
}

// undoPath is where a logged row's Undo deletes the event. The slug names the
// row to give back because a sheet opened over a watering can log a feed.
func undoPath(plantID, eventID uuid.UUID, slug string) string {
	return logPath(plantID) + "/" + eventID.String() + "?row=" + url.QueryEscape(slug)
}

func sheetPath(plantID uuid.UUID, slug string) string {
	return logPath(plantID) + "?row=" + url.QueryEscape(slug)
}

// draft is the sheet as far as it has been filled in.
type draft struct {
	// Row is the slug of the care the row was opened for and names the row the
	// post replaces. Care is the slug selected under What.
	Row     string
	Care    string
	Skipped bool
	When    string
	Clock   string
	At      string
	Again   int32
	Note    string
}

// readDraft reads a draft from a query or a form body. It returns false for a
// value the sheet's chips do not offer.
func readDraft(values url.Values) (draft, bool) {
	d := draft{
		Row:   values.Get("row"),
		Care:  values.Get("care"),
		When:  values.Get("when"),
		Clock: values.Get("time"),
		At:    values.Get("at"),
		Again: 2,
		Note:  strings.TrimSpace(values.Get("note")),
	}
	if d.Care == "" {
		d.Care = d.Row
	}
	if d.Row == "" {
		d.Row = d.Care
	}
	if d.When == "" {
		d.When = whenNow
	}

	switch values.Get("outcome") {
	case "", "done":
	case "skipped":
		d.Skipped = true
	default:
		return d, false
	}
	if !chipsHave(whens, d.When) {
		return d, false
	}
	if again := values.Get("again"); again != "" {
		n, err := strconv.ParseInt(again, 10, 32)
		if err != nil {
			return d, false
		}
		d.Again = int32(n)
	}
	return d, true
}

// performedAt is the instant the draft says the care happened, or the
// sentence the sheet shows under When. A time after now is refused because
// the log records what has happened.
func (d draft) performedAt(now time.Time) (time.Time, string) {
	loc := now.Location()
	var at time.Time
	switch d.When {
	case whenNow:
		return now, ""
	case whenToday, whenYesterday:
		clock, err := time.Parse(clockLayout, d.Clock)
		if err != nil {
			return at, "Give a time."
		}
		day := now
		if d.When == whenYesterday {
			day = now.AddDate(0, 0, -1)
		}
		at = time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, loc)
	case whenOther:
		parsed, err := time.ParseInLocation(atLayout, d.At, loc)
		if err != nil {
			return at, "Give a day and a time."
		}
		at = parsed
	}
	if at.After(now) {
		return at, "That is later than now."
	}
	return at, ""
}

type chip struct {
	Value string
	Label string
	On    bool
}

func chipsHave(chips []chip, value string) bool {
	for _, c := range chips {
		if c.Value == value {
			return true
		}
	}
	return false
}

// sheet is the logging sheet drawn over a Today row.
type sheet struct {
	// Path is where the sheet posts and where switching What fetches it
	// from. Target is the id of the row the post replaces.
	Path   string
	Target string
	Label  string
	Plant  sheetPlant
	// Cares is the What chips. The sheet draws them only when the plant has
	// more than one care.
	Cares   []chip
	Row     string
	Care    string
	Skipped bool
	Whens   []chip
	Clock   string
	At      string
	// WhenError is the sentence under When after a refused post.
	WhenError string
	Snoozes   []chip
	Note      string
	// Noun is the care worded as a noun, "watering".
	Noun string
}

type sheetPlant struct {
	Href      string
	Name      string
	Botanical bool
	// Sub is the plant's next name after the one it goes by. Italic marks the
	// botanical one.
	Sub      string
	Italic   bool
	Location string
}

func newSheet(plant store.Plant, lines []schedule.Line, care schedule.Line, d draft, now time.Time) *sheet {
	s := &sheet{
		Path:    logPath(plant.ID),
		Target:  careRowID(plant, care.CareType),
		Label:   "Log care for " + plant.DisplayName(),
		Plant:   newSheetPlant(plant),
		Row:     d.Row,
		Care:    care.CareType.Slug,
		Skipped: d.Skipped,
		Clock:   d.Clock,
		At:      d.At,
		Note:    d.Note,
		Noun:    careNoun(care.CareType),
	}
	for _, line := range lines {
		if line.CareType.Slug == d.Row {
			s.Target = careRowID(plant, line.CareType)
		}
		s.Cares = append(s.Cares, chip{Value: line.CareType.Slug, Label: line.CareType.Name, On: line.CareType.ID == care.CareType.ID})
	}
	for _, w := range whens {
		w.On = w.Value == d.When
		s.Whens = append(s.Whens, w)
	}
	// The pickers start at now. performedAt refuses anything later.
	if s.Clock == "" {
		s.Clock = now.Format(clockLayout)
	}
	if s.At == "" {
		s.At = now.Format(atLayout)
	}
	s.Snoozes = snoozes(usualDays(care.Schedule), d.Again)
	return s
}

func newSheetPlant(plant store.Plant) sheetPlant {
	p := sheetPlant{
		Href:      "/plants/" + plant.ID.String(),
		Name:      plant.DisplayName(),
		Botanical: plant.BotanicalOnly(),
	}
	if plant.Location != nil {
		p.Location = *plant.Location
	}
	switch {
	case isSet(plant.Nickname) && isSet(plant.CommonName):
		p.Sub = *plant.CommonName
	case !plant.BotanicalOnly() && isSet(plant.BotanicalName):
		p.Sub = *plant.BotanicalName
		p.Italic = true
	}
	return p
}

func isSet(s *string) bool {
	return s != nil && *s != ""
}

// usualDays is the schedule's interval in days, which is what a skip's
// override is stored as. A monthly or yearly cadence has no usual because a
// month is not a number of days.
func usualDays(s store.CareSchedule) int32 {
	if s.IntervalCount == nil {
		return 0
	}
	switch *s.IntervalUnit {
	case schedule.UnitDay:
		return *s.IntervalCount
	case schedule.UnitWeek:
		return 7 * *s.IntervalCount
	}
	return 0
}

// snoozes is the skip durations, three short re-checks and the usual
// interval. The usual chip carries the number as well as the word because a
// reader does not otherwise know the interval.
func snoozes(usual, selected int32) []chip {
	days := []int32{1, 2, 3}
	fourth := usual
	if fourth == 0 {
		fourth = 7
	}
	// A usual of one, two or three days is already a chip and takes the
	// wording.
	if !slices.Contains(days, fourth) {
		days = append(days, fourth)
	}

	out := make([]chip, 0, len(days))
	for _, n := range days {
		c := chip{Value: strconv.Itoa(int(n)), Label: daysWord(int(n)), On: n == selected}
		if n == usual {
			c.Label = "The usual " + daysWord(int(n))
		}
		out = append(out, c)
	}
	return out
}

// sheet answers GET /plants/{plant}/log with the sheet over Today, as the
// page for a navigation, the dialog for a swap that opens it, and the form
// for a swap that switches What.
func (h *today) sheet(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	plantID, err := uuid.Parse(r.PathValue("plant"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d, ok := readDraft(r.URL.Query())
	if !ok {
		http.Error(w, "the sheet did not send that", http.StatusBadRequest)
		return
	}

	g, err := h.load(r.Context(), principal)
	if err != nil {
		serverError(h.logger, w, r, "load the day", err)
		return
	}
	lines := plantLines(g.lines, plantID)
	if len(lines) == 0 {
		http.NotFound(w, r)
		return
	}
	// A bare path opens the sheet on the plant's first care.
	if d.Care == "" {
		d.Care = lines[0].CareType.Slug
		d.Row = d.Care
	}
	care, ok := lineFor(lines, d.Care)
	if !ok {
		http.Error(w, "the plant is not scheduled for that care", http.StatusBadRequest)
		return
	}

	page := newTodayPage(principal.Garden, g)
	page.Sheet = newSheet(lines[0].Plant, lines, care, d, g.now)
	// Answering the form alone leaves the dialog in place because replacing it
	// replays the entrance animation and drops the panel's scroll.
	fragment := "sheet"
	if r.Header.Get("HX-Target") == sheetFormID {
		fragment = "sheet-form"
	}
	h.templates.render(w, r, view{page: "today", fragment: fragment}, page)
}

// log answers POST /plants/{plant}/log by recording the event the draft
// describes. A swap gets the row in its logged state. A form post is sent back
// to Today.
func (h *today) log(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	plantID, err := uuid.Parse(r.PathValue("plant"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "the form did not parse", http.StatusBadRequest)
		return
	}

	// The plant resolves before the draft because a plant the garden does not
	// have is 404 whatever was posted at it.
	g, err := h.load(r.Context(), principal)
	if err != nil {
		serverError(h.logger, w, r, "load the day", err)
		return
	}
	lines := plantLines(g.lines, plantID)
	if len(lines) == 0 {
		http.NotFound(w, r)
		return
	}
	d, ok := readDraft(r.PostForm)
	if !ok || d.Care == "" {
		http.Error(w, "the sheet did not send that", http.StatusBadRequest)
		return
	}
	care, ok := lineFor(lines, d.Care)
	if !ok {
		http.Error(w, "the plant is not scheduled for that care", http.StatusBadRequest)
		return
	}
	plant := lines[0].Plant
	s := newSheet(plant, lines, care, d, g.now)

	// The snooze chips offer no zero. A zero would make a skip due the instant
	// it is recorded.
	if d.Skipped && !chipsHave(s.Snoozes, strconv.Itoa(int(d.Again))) {
		http.Error(w, "the sheet did not offer that", http.StatusBadRequest)
		return
	}
	performedAt, problem := d.performedAt(g.now)
	if problem != "" {
		s.WhenError = problem
		page := newTodayPage(principal.Garden, g)
		page.Sheet = s
		// The refusal goes back to the sheet rather than the row the form targeted.
		w.Header().Set("HX-Retarget", "#sheet")
		w.Header().Set("HX-Reswap", "outerHTML")
		h.templates.render(w, r, view{page: "today", fragment: "sheet", status: http.StatusUnprocessableEntity}, page)
		return
	}

	params := store.CreateCareEventParams{
		GardenID:    principal.Garden.ID,
		PlantID:     plant.ID,
		CareTypeID:  care.CareType.ID,
		PerformedBy: principal.User.ID,
		PerformedAt: performedAt,
		RecordedAt:  g.now,
		Done:        !d.Skipped,
	}
	if d.Note != "" {
		params.Note = &d.Note
	}
	if d.Skipped {
		params.OverrideIntervalDays = &d.Again
	}
	event, err := h.queries.CreateCareEvent(r.Context(), params)
	if err != nil {
		serverError(h.logger, w, r, "record the care", err)
		return
	}

	if !isHTMX(r) {
		http.Redirect(w, r, todayPath, http.StatusSeeOther)
		return
	}
	// The day is read again because the event has just changed what the head
	// says. The row is drawn from the first read, which is the day the sheet
	// was filled in against.
	after, err := h.load(r.Context(), principal)
	if err != nil {
		serverError(h.logger, w, r, "load the day", err)
		return
	}
	swap := careSwap{Row: loggedRow(plant, lines, d, care.CareType, event, g.now), Head: swapHead(after)}
	h.templates.render(w, r, view{page: "today", fragment: "care-logged"}, swap)
}

// undo answers DELETE /plants/{plant}/log/{event} by deleting the event the
// button was drawn beside. What comes back is the row as the day now has it,
// which is the state it was in before the event.
func (h *today) undo(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	plantID, err := uuid.Parse(r.PathValue("plant"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	eventID, err := uuid.Parse(r.PathValue("event"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	_, err = h.queries.DeleteCareEvent(r.Context(), store.DeleteCareEventParams{
		ID:           eventID,
		GardenID:     principal.Garden.ID,
		PlantID:      plantID,
		MayDeleteAny: principal.Can(auth.CareDeleteAny),
		PerformedBy:  principal.User.ID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// An event of somebody else's is as hidden as one that is not there.
		http.NotFound(w, r)
		return
	case err != nil:
		serverError(h.logger, w, r, "delete the care", err)
		return
	}

	if !isHTMX(r) {
		http.Redirect(w, r, todayPath, http.StatusSeeOther)
		return
	}
	// The day is read after the delete because the row is drawn against a
	// schedule that no longer counts the event.
	g, err := h.load(r.Context(), principal)
	if err != nil {
		serverError(h.logger, w, r, "load the day", err)
		return
	}
	lines := plantLines(g.lines, plantID)
	if len(lines) == 0 {
		http.NotFound(w, r)
		return
	}
	line := lines[0]
	if named, ok := lineFor(lines, r.URL.Query().Get("row")); ok {
		line = named
	}
	row := newCareRow(schedule.Row{Plant: line.Plant, Care: line, Lines: lines}, g.now)
	h.templates.render(w, r, view{page: "today", fragment: "care-undone"}, careSwap{Row: row, Head: swapHead(g)})
}

func lineFor(lines []schedule.Line, slug string) (schedule.Line, bool) {
	for _, line := range lines {
		if line.CareType.Slug == slug {
			return line, true
		}
	}
	return schedule.Line{}, false
}

// loggedRow is the row the swap replaces, in its logged state. It keeps the
// id of the row the sheet was opened from because that row is the element on
// the page, whichever care was logged.
func loggedRow(plant store.Plant, lines []schedule.Line, d draft, careType store.CareType, event store.CareEvent, now time.Time) careRow {
	rowType := careType
	if line, ok := lineFor(lines, d.Row); ok {
		rowType = line.CareType
	}
	r := careRow{
		ID:        careRowID(plant, rowType),
		Href:      sheetPath(plant.ID, rowType.Slug),
		Path:      logPath(plant.ID),
		Name:      plant.DisplayName(),
		Botanical: plant.BotanicalOnly(),
		Care:      rowType.Name,
		Slug:      rowType.Slug,
		Done:      true,
		Said:      said(careType, event, d.When == whenNow, now.Location()),
		Undo:      undoPath(plant.ID, event.ID, rowType.Slug),
		Grace:     int(graceWindow.Milliseconds()),
		Collapse:  collapseMS,
	}
	if plant.Location != nil {
		r.Location = *plant.Location
	}
	return r
}

// said takes justNow from the draft rather than comparing the row's instant
// with the clock because Postgres keeps microseconds and the clock nanoseconds.
func said(careType store.CareType, event store.CareEvent, justNow bool, loc *time.Location) string {
	if !event.Done {
		return "Skipped · asking again in " + daysWord(int(*event.OverrideIntervalDays))
	}
	verb := capitalise(carePast(careType))
	if justNow {
		return verb + " just now"
	}
	return verb + " " + stamp(event.PerformedAt.In(loc))
}
