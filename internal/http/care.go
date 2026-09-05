package http

import (
	"errors"
	"maps"
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

// sheetFormID is the HTML id of the sheet's form. A What chip swaps this
// element, not the whole dialog.
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

// overPlant is the value of the sheet's "over" field when the sheet was opened
// from a plant's page. Every fetch and post sends it back, so the handler knows
// which page to render. Opened from Today, the field is empty.
const overPlant = "plant"

// clockLayout is the format an <input type="time"> submits, and atLayout the
// format an <input type="datetime-local"> submits.
const (
	clockLayout = "15:04"
	atLayout    = "2006-01-02T15:04"
)

func logPath(plantID uuid.UUID) string {
	return "/plants/" + plantID.String() + "/log"
}

// undoPath is the URL the Undo button on a logged row sends its DELETE to. The
// row parameter names the care type whose row the response renders, since a
// sheet opened from the watering row can log a feed instead.
func undoPath(plantID, eventID uuid.UUID, slug string) string {
	return logPath(plantID) + "/" + eventID.String() + "?row=" + url.QueryEscape(slug)
}

// undoFormPath is the URL the Undo button posts to without JavaScript, since an
// HTML form cannot send DELETE. There is no row parameter because a form post
// gets a redirect to Today rather than a row.
func undoFormPath(plantID, eventID uuid.UUID) string {
	return logPath(plantID) + "/" + eventID.String() + "/undo"
}

func sheetPath(plantID uuid.UUID, slug string) string {
	return logPath(plantID) + "?row=" + url.QueryEscape(slug)
}

// draft holds the sheet's field values, read from the query string on a fetch
// and from the body on a post.
type draft struct {
	// Row is the slug of the care type whose Today row the sheet was opened
	// from. The post replaces that row.
	Row  string
	Over string
	// Care is the slug of the care type selected under What, the one the post
	// records.
	Care    string
	Skipped bool
	When    string
	Clock   string
	At      string
	Again   int32
	Note    string
}

// readDraft fills a draft from a query string or a posted form. It returns
// false for a value the sheet never offers: an unknown "over", an unknown
// outcome, a When not in the chips, or an "again" that is not a number.
func readDraft(values url.Values) (draft, bool) {
	d := draft{
		Row:   values.Get("row"),
		Over:  values.Get("over"),
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
	if d.Over != "" && d.Over != overPlant {
		return d, false
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

// whenRefused is an error whose text is shown under the When chips when the
// time cannot be recorded.
type whenRefused string

func (r whenRefused) Error() string { return string(r) }

// errNotOffered is returned when a post names a skip interval that was not one
// of the chips the sheet rendered.
var errNotOffered = errors.New("the sheet did not offer that")

// performedAt returns the time the draft says the care happened. A time that
// does not parse, or one later than now, returns a whenRefused, because the
// log records care already given.
func (d draft) performedAt(now time.Time) (time.Time, error) {
	loc := now.Location()
	var at time.Time
	switch d.When {
	case whenNow:
		return now, nil
	case whenToday, whenYesterday:
		clock, err := time.Parse(clockLayout, d.Clock)
		if err != nil {
			return at, whenRefused("Give a time.")
		}
		day := now
		if d.When == whenYesterday {
			day = now.AddDate(0, 0, -1)
		}
		at = time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, loc)
	case whenOther:
		parsed, err := time.ParseInLocation(atLayout, d.At, loc)
		if err != nil {
			return at, whenRefused("Give a day and a time.")
		}
		at = parsed
	}
	if at.After(now) {
		return at, whenRefused("That is later than now.")
	}
	return at, nil
}

type chip struct {
	Value string
	Label string
	On    bool
}

// hidden is one hidden input on the sheet's form.
type hidden struct {
	Name  string
	Value string
}

func hiddenValues(values url.Values) []hidden {
	out := make([]hidden, 0, len(values))
	for _, name := range slices.Sorted(maps.Keys(values)) {
		out = append(out, hidden{Name: name, Value: values.Get(name)})
	}
	return out
}

func chipsHave(chips []chip, value string) bool {
	for _, c := range chips {
		if c.Value == value {
			return true
		}
	}
	return false
}

// offer is one care type listed under What, with the plant's schedule for it.
// The skip chips read the usual interval from the schedule. It is the zero
// value for a care type the plant has no schedule for.
type offer struct {
	CareType store.CareType
	Schedule store.CareSchedule
}

func offerOf(line schedule.Line) offer {
	return offer{CareType: line.CareType, Schedule: line.Schedule}
}

// offersOf makes one offer per schedule line. The sheet opened from Today uses
// it, so it lists only the care types the plant is scheduled for.
func offersOf(lines []schedule.Line) []offer {
	out := make([]offer, 0, len(lines))
	for _, line := range lines {
		out = append(out, offerOf(line))
	}
	return out
}

func offerFor(offers []offer, slug string) (offer, bool) {
	for _, o := range offers {
		if o.CareType.Slug == slug {
			return o, true
		}
	}
	return offer{}, false
}

// sheet is the data the log-care sheet renders from, on Today and on a plant's
// page.
type sheet struct {
	// Path is the URL the sheet's form posts to, and the URL a What chip
	// fetches the sheet again from.
	Path string
	// Target is the HTML id of the element the post replaces: the care row on
	// Today, the log body on the activity page. It is empty on a plant's page,
	// which renders the whole page again.
	Target string
	Over   string
	Label  string
	Plant  sheetPlant
	// Cares is one chip per care type the sheet offers. The template renders
	// the What field only when there is more than one.
	Cares   []chip
	Row     string
	Care    string
	Skipped bool
	Whens   []chip
	Clock   string
	At      string
	// WhenError is the message shown under the When chips after a refused post,
	// and is empty otherwise.
	WhenError string
	Snoozes   []chip
	Note      string
	// Noun is the care type as a noun, such as "watering".
	Noun string
	// Correcting is true for the sheet opened over an event already recorded.
	// Its primary button reads Save changes rather than naming the care.
	Correcting bool
	// Recorded is the line above the buttons saying who recorded the event and
	// when. Empty unless Correcting.
	Recorded string
	// Delete is the URL the Delete button posts to. Empty for a reader who may
	// not delete the event, and the button is then not rendered.
	Delete string
	// DeleteTarget is the HTML id of the row the delete replaces.
	DeleteTarget string
	// Query is the activity log's own query string as hidden fields. They are
	// in the form as well as in the URL because a What chip submits the form as
	// a GET, and a GET form replaces the query string of the URL it submits to,
	// so the filter and the paging cursor would be dropped. Empty unless
	// Correcting.
	Query []hidden
	// careTypeID is the id of the care type Care names, so the post does not
	// look it up again.
	careTypeID uuid.UUID
}

type sheetPlant struct {
	Href      string
	Name      string
	Botanical bool
	// Sub is the second name shown under the plant's name: the common name when
	// the plant goes by a nickname, otherwise the botanical name. Italic is
	// true when Sub is the botanical name.
	Sub      string
	Italic   bool
	Location string
}

func newSheet(plant store.Plant, offers []offer, care offer, d draft, now time.Time) *sheet {
	s := &sheet{
		Path:    logPath(plant.ID),
		Target:  careRowID(plant, care.CareType),
		Over:    d.Over,
		Label:   "Log care for " + plant.DisplayName(),
		Plant:   newSheetPlant(plant),
		Row:     d.Row,
		Care:    care.CareType.Slug,
		Skipped: d.Skipped,
		Clock:   d.Clock,
		At:      d.At,
		Note:    d.Note,
		Noun:    careNoun(care.CareType),

		careTypeID: care.CareType.ID,
	}
	for _, o := range offers {
		if o.CareType.Slug == d.Row {
			s.Target = careRowID(plant, o.CareType)
		}
		s.Cares = append(s.Cares, chip{Value: o.CareType.Slug, Label: o.CareType.Name, On: o.CareType.ID == care.CareType.ID})
	}
	for _, w := range whens {
		w.On = w.Value == d.When
		s.Whens = append(s.Whens, w)
	}
	if s.Clock == "" {
		s.Clock = now.Format(clockLayout)
	}
	if s.At == "" {
		s.At = now.Format(atLayout)
	}
	s.Snoozes = snoozes(usualDays(care.Schedule), d.Again)
	return s
}

// forPlant adjusts the sheet for a plant's page. The plant heading is no longer
// a link, since it would point at the page already open. The swap target is
// cleared because that page has no care row to replace, so the post renders the
// whole page again.
func (s *sheet) forPlant() {
	s.Target = ""
	s.Plant.Href = ""
}

// forEvent adjusts the sheet for an event already recorded, opened over its row
// on the activity log. The form posts to that event's own URL and swaps the log
// body, because a correction that moves the time moves the row to the day it
// now belongs under. Delete sits beside Save changes for a reader who may
// delete the event.
func (s *sheet) forEvent(principal auth.Principal, e event, d draft) {
	s.Path = eventPath(e.care().PlantID, e.care().ID, "", e.q)
	s.Target = logBodyID
	// Row names a care row on Today, and this sheet is not open over one.
	s.Row = ""
	s.Query = hiddenValues(e.q.values())
	s.Correcting = true
	s.Recorded = recordedLine(principal, e)
	if mayDelete(principal, e.care()) {
		s.Delete = eventPath(e.care().PlantID, e.care().ID, "/delete", e.q)
		s.DeleteTarget = eventRowID(e.care().ID)
	}
	s.offerSnooze(e.care().OverrideIntervalDays, d.Again)
}

// offerSnooze adds the interval a skip was recorded with, when it is not one of
// the chips already. A schedule whose interval has changed since leaves an
// event holding a number nothing offers, and the sheet would then show a skip
// with no chip selected and refuse to save it again.
func (s *sheet) offerSnooze(days *int32, selected int32) {
	if days == nil {
		return
	}
	value := strconv.Itoa(int(*days))
	if chipsHave(s.Snoozes, value) {
		return
	}
	at := len(s.Snoozes)
	for i, c := range s.Snoozes {
		if n, err := strconv.Atoi(c.Value); err == nil && n > int(*days) {
			at = i
			break
		}
	}
	s.Snoozes = slices.Insert(s.Snoozes, at, chip{Value: value, Label: daysWord(int(*days)), On: *days == selected})
}

// accept returns the time a posted draft is recorded at. A skip whose interval
// was not one of the chips is refused, because a posted zero would make the
// skip due again the moment it is stored. A time that does not parse, or one
// after now, returns a whenRefused for the sheet to show.
func (s *sheet) accept(d draft, now time.Time) (time.Time, error) {
	if d.Skipped && !chipsHave(s.Snoozes, strconv.Itoa(int(d.Again))) {
		return time.Time{}, errNotOffered
	}
	return d.performedAt(now)
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

// usualDays converts a schedule's interval to whole days, since a skip stores
// its override in days. It returns 0 for a schedule with no interval and for a
// monthly or yearly one, which is not a fixed number of days.
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

// snoozes builds the chips for how long a skip puts a care off: one, two and
// three days, plus the plant's usual interval, or seven days when the schedule
// has no interval in days. The usual chip says "The usual" followed by the
// number of days, since nothing else on the sheet says what the interval is.
func snoozes(usual, selected int32) []chip {
	days := []int32{1, 2, 3}
	fourth := usual
	if fourth == 0 {
		fourth = 7
	}
	// A usual of one, two or three days is relabelled below rather than added
	// twice.
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

// sheet handles GET /plants/{plant}/log. A page navigation gets the whole page,
// the swap that opens the sheet gets the dialog, and the swap that switches
// What gets the form alone. The "over" value decides which page the sheet is
// rendered on.
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
	if d.Over == overPlant {
		h.plantSheet(w, r, principal, plantID, d)
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
	if d.Care == "" {
		d.Care = lines[0].CareType.Slug
		d.Row = d.Care
	}
	care, ok := lineFor(lines, d.Care)
	if !ok {
		http.Error(w, "the plant is not scheduled for that care", http.StatusBadRequest)
		return
	}

	page := newTodayPage(principal, g)
	page.Sheet = newSheet(lines[0].Plant, offersOf(lines), offerOf(care), d, g.now)
	h.templates.render(w, r, view{page: "today", fragment: sheetFragment(r)}, page)
}

// plantSheet renders the same sheet on a plant's own page. It offers every care
// type in the garden rather than only the plant's schedules, so a repot the
// plant has no schedule for can still be logged.
func (h *today) plantSheet(w http.ResponseWriter, r *http.Request, principal auth.Principal, plantID uuid.UUID, d draft) {
	detail, err := loadPlant(r.Context(), h.queries, principal, plantID, h.now())
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		http.NotFound(w, r)
		return
	case err != nil:
		serverError(h.logger, w, r, "load the plant", err)
		return
	}
	// Nothing can be logged against an archived plant.
	if detail.plant.ArchivedAt != nil {
		http.NotFound(w, r)
		return
	}

	care, ok := chosenCare(detail, d)
	if !ok {
		http.Error(w, "the garden has no such care", http.StatusBadRequest)
		return
	}
	page := newPlantPage(principal, detail)
	page.Sheet = newSheet(detail.plant, detail.offers(), care, d, detail.now)
	page.Sheet.forPlant()
	h.templates.render(w, r, view{page: "plant", fragment: sheetFragment(r)}, page)
}

// chosenCare picks the care type the sheet on a plant's page opens on. With
// none named in the URL it takes the one due soonest, and failing that the
// garden's first care type.
func chosenCare(detail plantDetail, d draft) (offer, bool) {
	offers := detail.offers()
	if d.Care != "" {
		return offerFor(offers, d.Care)
	}
	if care, ok := schedule.Nearest(detail.lines); ok {
		return offerOf(care), true
	}
	if len(offers) == 0 {
		return offer{}, false
	}
	return offers[0], true
}

// sheetFragment picks which part of the page a swap returns. A swap targeting
// the form gets the form alone, because replacing the whole dialog would replay
// its entrance animation and lose its scroll position.
func sheetFragment(r *http.Request) string {
	if r.Header.Get("HX-Target") == sheetFormID {
		return "sheet-form"
	}
	return "sheet"
}

// log handles POST /plants/{plant}/log and records the care the draft
// describes. An htmx post returns the care row in its logged state, with the
// day's heading and the feed. An ordinary form post gets a redirect to the page
// the sheet was opened on.
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
	// "over" is read before the rest of the form because it decides which page
	// renders the result.
	if r.PostForm.Get("over") == overPlant {
		h.logOnPlant(w, r, principal, plantID)
		return
	}

	// The plant is resolved before the draft is read, because a plant the
	// garden does not have is a 404 whatever the form says.
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
	s := newSheet(plant, offersOf(lines), offerOf(care), d, g.now)

	performedAt, err := s.accept(d, g.now)
	var refused whenRefused
	switch {
	case errors.As(err, &refused):
		s.WhenError = string(refused)
		page := newTodayPage(principal, g)
		page.Sheet = s
		// The form targets the care row, so a refusal retargets the response
		// at the sheet so the message is shown.
		w.Header().Set("HX-Retarget", "#sheet")
		w.Header().Set("HX-Reswap", "outerHTML")
		h.templates.render(w, r, view{page: "today", fragment: "sheet", status: http.StatusUnprocessableEntity}, page)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	logged, err := h.queries.CreateCareEvent(r.Context(), careEventParams(principal, plant.ID, care.CareType.ID, d, performedAt, g.now))
	if err != nil {
		serverError(h.logger, w, r, "record the care", err)
		return
	}

	if !isHTMX(r) {
		http.Redirect(w, r, todayPath, http.StatusSeeOther)
		return
	}
	// Today is loaded again because the new event changes the heading and the
	// feed. The row is built from the first load, the state the sheet was
	// filled in against.
	after, err := h.load(r.Context(), principal)
	if err != nil {
		serverError(h.logger, w, r, "load the day", err)
		return
	}
	swap := careSwap{
		Row:  loggedRow(plant, lines, d, care.CareType, logged, g.now),
		Head: swapHead(principal, after),
		Feed: swapFeed(principal, after),
	}
	h.templates.render(w, r, view{page: "today", fragment: "care-logged"}, swap)
}

// logOnPlant records the care posted from the sheet on a plant's page and
// redirects back to that page. The page has no care row to replace, so the post
// never returns a fragment.
func (h *today) logOnPlant(w http.ResponseWriter, r *http.Request, principal auth.Principal, plantID uuid.UUID) {
	detail, err := loadPlant(r.Context(), h.queries, principal, plantID, h.now())
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		http.NotFound(w, r)
		return
	case err != nil:
		serverError(h.logger, w, r, "load the plant", err)
		return
	}
	if detail.plant.ArchivedAt != nil {
		http.NotFound(w, r)
		return
	}

	d, ok := readDraft(r.PostForm)
	if !ok || d.Care == "" {
		http.Error(w, "the sheet did not send that", http.StatusBadRequest)
		return
	}
	care, ok := offerFor(detail.offers(), d.Care)
	if !ok {
		http.Error(w, "the garden has no such care", http.StatusBadRequest)
		return
	}
	s := newSheet(detail.plant, detail.offers(), care, d, detail.now)
	s.forPlant()

	performedAt, err := s.accept(d, detail.now)
	var refused whenRefused
	switch {
	case errors.As(err, &refused):
		s.WhenError = string(refused)
		page := newPlantPage(principal, detail)
		page.Sheet = s
		h.templates.render(w, r, view{page: "plant", status: http.StatusUnprocessableEntity}, page)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	params := careEventParams(principal, detail.plant.ID, care.CareType.ID, d, performedAt, detail.now)
	if _, err := h.queries.CreateCareEvent(r.Context(), params); err != nil {
		serverError(h.logger, w, r, "record the care", err)
		return
	}
	http.Redirect(w, r, plantPath(detail.plant.ID), http.StatusSeeOther)
}

// careEventParams builds the care event a draft describes. recordedAt is the
// time the page was rendered at, so the stored event and the page agree on when
// now was.
func careEventParams(principal auth.Principal, plantID, careTypeID uuid.UUID, d draft, performedAt, recordedAt time.Time) store.CreateCareEventParams {
	params := store.CreateCareEventParams{
		GardenID:    principal.Garden.ID,
		PlantID:     plantID,
		CareTypeID:  careTypeID,
		PerformedBy: principal.User.ID,
		PerformedAt: performedAt,
		RecordedAt:  recordedAt,
		Done:        !d.Skipped,
	}
	if d.Note != "" {
		params.Note = &d.Note
	}
	if d.Skipped {
		params.OverrideIntervalDays = &d.Again
	}
	return params
}

// undo handles DELETE /plants/{plant}/log/{event} and POST
// /plants/{plant}/log/{event}/undo. It deletes the event the Undo button was
// rendered beside. An htmx request returns the care row in its current state,
// with the day's heading and the feed. A form post gets a redirect to Today.
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
		// Someone else's event is a 404, the same as an event that does not
		// exist.
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
	// Today is loaded after the delete so the row is built against a schedule
	// that no longer counts the deleted event.
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
	swap := careSwap{Row: row, Head: swapHead(principal, g), Feed: swapFeed(principal, g)}
	h.templates.render(w, r, view{page: "today", fragment: "care-undone"}, swap)
}

func lineFor(lines []schedule.Line, slug string) (schedule.Line, bool) {
	for _, line := range lines {
		if line.CareType.Slug == slug {
			return line, true
		}
	}
	return schedule.Line{}, false
}

// loggedRow builds the care row in its logged state for the swap. Its id, name
// and care come from the row the sheet was opened from, because that is the row
// on the page whichever care was logged. The line under it names the care that
// was recorded.
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

// said builds the line under a logged row saying what was done and when.
// justNow comes from the draft rather than from comparing the event's time with
// now, because Postgres stores microseconds and Go nanoseconds, so the two are
// never equal.
func said(careType store.CareType, logged store.CareEvent, justNow bool, loc *time.Location) string {
	if !logged.Done {
		return "Skipped · asking again in " + daysWord(int(*logged.OverrideIntervalDays))
	}
	verb := capitalise(carePast(careType))
	if justNow {
		return verb + " just now"
	}
	return verb + " " + stamp(logged.PerformedAt.In(loc))
}
