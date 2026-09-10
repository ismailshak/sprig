package http

import (
	"context"
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
	"github.com/ismailshak/sprig/internal/push"
	"github.com/ismailshak/sprig/internal/schedule"
	"github.com/ismailshak/sprig/internal/store"
)

// sheetID is the HTML id of the sheet, on the open dialog and on the empty
// placeholder it swaps with. A request targeting it asks for the sheet alone.
const sheetID = "sheet"

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
	// Care is the slug of the care type selected under Care, the one the post
	// logs.
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
// outcome, a When not in the chips, or a reminder that is not a number. The
// reminder is read from the field named after the care, "again-water", because
// every care type's Remind me in chips are in the form and only the chosen
// type's are shown.
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
	if again := values.Get(againField(d.Care)); again != "" {
		n, err := strconv.ParseInt(again, 10, 32)
		if err != nil {
			return d, false
		}
		d.Again = int32(n)
	}
	return d, true
}

// whenRefused is an error whose text is shown under the When chips when the
// time cannot be logged.
type whenRefused string

func (r whenRefused) Error() string { return string(r) }

// errNotOffered is returned when a post names a skip interval that was not one
// of the chips the sheet rendered.
var errNotOffered = errors.New("the sheet did not offer that")

// performedAt returns the time the draft says the care happened. A time that
// does not parse, or one later than now, returns a whenRefused, because the
// sheet logs care that has already been given.
func (d draft) performedAt(now time.Time) (time.Time, error) {
	loc := now.Location()
	var at time.Time
	switch d.When {
	case whenNow:
		return now, nil
	case whenToday, whenYesterday:
		clock, err := time.Parse(clockLayout, d.Clock)
		if err != nil {
			return at, whenRefused("Enter a time.")
		}
		day := now
		if d.When == whenYesterday {
			day = now.AddDate(0, 0, -1)
		}
		at = time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, loc)
	case whenOther:
		parsed, err := time.ParseInLocation(atLayout, d.At, loc)
		if err != nil {
			return at, whenRefused("Enter a day and time.")
		}
		at = parsed
	}
	if at.After(now) {
		return at, whenRefused("That time is in the future.")
	}
	return at, nil
}

type chip struct {
	Value string
	Label string
	On    bool
}

// careOption is one care type the sheet offers: its chip under Care and the
// two parts of the form that depend on it. The template renders every option's
// parts and the stylesheet shows the chosen one's.
type careOption struct {
	chip
	// Noun is the care type as a noun, such as "watering", for the Log button.
	Noun string
	// Reminders is the Remind me in chips for this care type, under the field
	// name againField gives.
	Reminders []chip
}

// againField is the name of the Remind me in field for one care type. Each
// type's chips are their own radio group, so choosing another care type does
// not clear the choice made under this one.
func againField(care string) string {
	return "again-" + care
}

func chipsHave(chips []chip, value string) bool {
	for _, c := range chips {
		if c.Value == value {
			return true
		}
	}
	return false
}

// offer is one care type listed under Care, with the plant's schedule for it.
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
	// Path is the URL the sheet's form posts to.
	Path string
	// Target is the HTML id of the element the post replaces: the care row on
	// Today, the log body on the Activity page. It is empty on a plant's page,
	// which renders the whole page again.
	Target string
	Over   string
	Label  string
	Plant  sheetPlant
	// Cares is one option per care type the sheet offers. The template renders
	// the Care field only when there is more than one.
	Cares   []careOption
	Row     string
	Care    string
	Skipped bool
	Whens   []chip
	Clock   string
	At      string
	// WhenError is the message shown under the When chips after a refused post,
	// and is empty otherwise.
	WhenError string
	// Reminders is the Remind me in chips of the chosen care type, the ones a
	// post is checked against.
	Reminders []chip
	Note      string
	// Noun is the chosen care type as a noun, such as "watering".
	Noun string
	// Correcting is true for the sheet opened over an event already logged.
	// Its primary button reads Save changes rather than naming the care.
	Correcting bool
	// Logged is the line above the buttons saying who logged the event and
	// when. Empty unless Correcting.
	Logged string
	// Delete is the URL the Delete button posts to. Empty for a reader who may
	// not delete the event, and the button is then not rendered.
	Delete string
	// DeleteTarget is the HTML id of the row the delete replaces.
	DeleteTarget string
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
	Sub    string
	Italic bool
	Room   string
	// Picture is the URL of the plant's profile picture as a square, empty for
	// a plant with no picture.
	Picture string
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
		s.Cares = append(s.Cares, careOption{
			chip:      chip{Value: o.CareType.Slug, Label: o.CareType.Name, On: o.CareType.ID == care.CareType.ID},
			Noun:      careNoun(o.CareType),
			Reminders: reminderChips(usualDays(o.Schedule), d.Again),
		})
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
	s.Reminders = reminderChips(usualDays(care.Schedule), d.Again)
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

// forEvent adjusts the sheet for an event already logged, opened over its row
// on the Activity page. The form posts to that event's own URL and swaps the
// log body, because a correction that moves the time moves the row to the day
// it now belongs under. Delete is rendered beside Save changes for a reader who
// may delete the event.
func (s *sheet) forEvent(principal auth.Principal, e event, d draft) {
	s.Path = eventPath(e.care().PlantID, e.care().ID, "", e.q)
	s.Target = logBodyID
	// Row names a care row on Today, and this sheet is not open over one.
	s.Row = ""
	s.Correcting = true
	s.Logged = loggedLine(principal, e)
	if mayDelete(principal, e.care()) {
		s.Delete = eventPath(e.care().PlantID, e.care().ID, "/delete", e.q)
		s.DeleteTarget = eventRowID(e.care().ID)
	}
	s.offerReminder(e.care().OverrideIntervalDays, d.Again)
}

// offerReminder adds the interval a skip was logged with when no chip offers
// it. That happens when the schedule's interval changed after the skip. Without
// the extra chip the sheet would show the skip with nothing selected and refuse
// to save it again.
func (s *sheet) offerReminder(days *int32, selected int32) {
	if days == nil {
		return
	}
	value := strconv.Itoa(int(*days))
	if chipsHave(s.Reminders, value) {
		return
	}
	at := len(s.Reminders)
	for i, c := range s.Reminders {
		if n, err := strconv.Atoi(c.Value); err == nil && n > int(*days) {
			at = i
			break
		}
	}
	s.Reminders = slices.Insert(s.Reminders, at, chip{Value: value, Label: daysWord(int(*days)), On: *days == selected})
	for i := range s.Cares {
		if s.Cares[i].On {
			s.Cares[i].Reminders = s.Reminders
		}
	}
}

// accept returns the time a posted draft is logged at. A skip whose interval
// was not one of the chips is refused, because a posted zero would make the
// skip due again the moment it is stored. A time that does not parse, or one
// after now, returns a whenRefused for the sheet to show.
func (s *sheet) accept(d draft, now time.Time) (time.Time, error) {
	if d.Skipped && !chipsHave(s.Reminders, strconv.Itoa(int(d.Again))) {
		return time.Time{}, errNotOffered
	}
	return d.performedAt(now)
}

func newSheetPlant(plant store.Plant) sheetPlant {
	p := sheetPlant{
		Href:      "/plants/" + plant.ID.String(),
		Name:      plant.DisplayName(),
		Botanical: plant.BotanicalOnly(),
		Picture:   squarePicturePath(plant),
	}
	if plant.Location != nil {
		p.Room = *plant.Location
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

// fallbackReminderDays is the usual reminder for a schedule with no interval
// in days, such as a one-off repot.
const fallbackReminderDays = 7

// reminderChips builds the Remind me in chips: one, two and three days, plus
// the plant's usual interval, or seven days when the schedule has no interval
// in days. The usual chip is marked "(usual)", since nothing else on the sheet
// says what the interval is.
func reminderChips(usual, selected int32) []chip {
	days := []int32{1, 2, 3}
	fourth := usual
	if fourth == 0 {
		fourth = fallbackReminderDays
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
			c.Label = daysWord(int(n)) + " (usual)"
		}
		out = append(out, c)
	}
	return out
}

// sheet handles GET /plants/{plant}/log. A page navigation gets the whole page
// and the swap that opens the sheet gets the dialog. The "over" value decides
// which page the sheet is rendered on.
func (h *today) sheet(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	plantID, err := uuid.Parse(r.PathValue("plant"))
	if err != nil {
		h.templates.notFound(w, r)
		return
	}
	d, ok := readDraft(r.URL.Query())
	if !ok {
		h.templates.badRequest(w, r)
		return
	}
	if d.Over == overPlant {
		h.plantSheet(w, r, principal, plantID, d)
		return
	}

	g, err := h.load(r.Context(), principal)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "load the day", err)
		return
	}
	lines := plantLines(g.lines, plantID)
	if len(lines) == 0 {
		h.templates.notFound(w, r)
		return
	}
	if d.Care == "" {
		d.Care = lines[0].CareType.Slug
		d.Row = d.Care
	}
	care, ok := lineFor(lines, d.Care)
	if !ok {
		h.templates.badRequest(w, r)
		return
	}

	page := newTodayPage(principal, g)
	page.Sheet = newSheet(lines[0].Plant, offersOf(lines), offerOf(care), d, g.now)
	h.templates.render(w, r, view{page: "today", fragment: "sheet"}, page)
}

// plantSheet renders the same sheet on a plant's own page. It offers every care
// type in the garden rather than only the plant's schedules, so a repot the
// plant has no schedule for can still be logged.
func (h *today) plantSheet(w http.ResponseWriter, r *http.Request, principal auth.Principal, plantID uuid.UUID, d draft) {
	detail, err := loadPlant(r.Context(), h.queries, principal, plantID, h.now())
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		h.templates.notFound(w, r)
		return
	case err != nil:
		h.templates.serverError(h.logger, w, r, "load the plant", err)
		return
	}
	// Nothing can be logged against an archived plant.
	if detail.plant.ArchivedAt != nil {
		h.templates.notFound(w, r)
		return
	}

	care, ok := chosenCare(detail, d)
	if !ok {
		h.templates.badRequest(w, r)
		return
	}
	page := newPlantPage(principal, detail)
	page.Sheet = newSheet(detail.plant, detail.offers(), care, d, detail.now)
	page.Sheet.forPlant()
	h.templates.render(w, r, view{page: "plant", fragment: "sheet"}, page)
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

// log handles POST /plants/{plant}/log and logs the care the draft describes.
// An htmx post returns the care row in its logged state, with the day's
// heading and the feed. An ordinary form post gets a redirect to the page the
// sheet was opened on.
func (h *today) log(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	plantID, err := uuid.Parse(r.PathValue("plant"))
	if err != nil {
		h.templates.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.templates.badRequest(w, r)
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
		h.templates.serverError(h.logger, w, r, "load the day", err)
		return
	}
	lines := plantLines(g.lines, plantID)
	if len(lines) == 0 {
		h.templates.notFound(w, r)
		return
	}
	d, ok := readDraft(r.PostForm)
	if !ok || d.Care == "" {
		h.templates.badRequest(w, r)
		return
	}
	care, ok := lineFor(lines, d.Care)
	if !ok {
		h.templates.badRequest(w, r)
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
		h.templates.render(w, r, view{page: "today", fragment: "sheet", status: http.StatusUnprocessableEntity, announce: s.WhenError}, page)
		return
	case err != nil:
		h.templates.badRequest(w, r)
		return
	}

	logged, err := h.queries.CreateCareEvent(r.Context(), careEventParams(principal, plant.ID, care.CareType.ID, d, performedAt, g.now))
	if err != nil {
		h.templates.serverError(h.logger, w, r, "record the care", err)
		return
	}
	h.notify.call(r.Context(), principal, activityNotification(principal, plant, care.CareType, logged.Done))

	if !isHTMX(r) {
		http.Redirect(w, r, todayPath, http.StatusSeeOther)
		return
	}
	// Today is loaded again because the new event changes the heading and the
	// feed. The row is built from the first load, the state the sheet was
	// filled in against.
	after, err := h.load(r.Context(), principal)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "load the day", err)
		return
	}
	swap := careSwap{
		Row:  loggedRow(plant, lines, d, care.CareType, logged, g.now),
		Head: swapHead(principal, after),
		Feed: swapFeed(principal, after),
	}
	v := view{page: "today", fragment: "care-logged", announce: h.loggedAnnouncement(principal, plant, care.CareType, logged, swap.Head)}
	h.templates.render(w, r, v, swap)
}

// logOnPlant logs the care posted from the sheet on a plant's page and
// redirects back to that page. The page has no care row to replace, so the post
// never returns a fragment.
func (h *today) logOnPlant(w http.ResponseWriter, r *http.Request, principal auth.Principal, plantID uuid.UUID) {
	detail, err := loadPlant(r.Context(), h.queries, principal, plantID, h.now())
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		h.templates.notFound(w, r)
		return
	case err != nil:
		h.templates.serverError(h.logger, w, r, "load the plant", err)
		return
	}
	if detail.plant.ArchivedAt != nil {
		h.templates.notFound(w, r)
		return
	}

	d, ok := readDraft(r.PostForm)
	if !ok || d.Care == "" {
		h.templates.badRequest(w, r)
		return
	}
	care, ok := offerFor(detail.offers(), d.Care)
	if !ok {
		h.templates.badRequest(w, r)
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
		h.templates.badRequest(w, r)
		return
	}

	params := careEventParams(principal, detail.plant.ID, care.CareType.ID, d, performedAt, detail.now)
	logged, err := h.queries.CreateCareEvent(r.Context(), params)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "record the care", err)
		return
	}
	h.notify.call(r.Context(), principal, activityNotification(principal, detail.plant, care.CareType, logged.Done))
	http.Redirect(w, r, plantPath(detail.plant.ID), http.StatusSeeOther)
}

// notifyActivity sends the other members of the garden a push notification
// about care the principal has just logged. It is nil when push is off.
type notifyActivity func(ctx context.Context, gardenID, actorID uuid.UUID, n push.Notification)

func (f notifyActivity) call(ctx context.Context, principal auth.Principal, n push.Notification) {
	if f != nil {
		f(ctx, principal.Garden.ID, principal.User.ID, n)
	}
}

// activityNotification returns the notification the garden's other members
// get when the principal logs care. The body reads "Ellie watered Doris." or
// "Ellie skipped watering Doris."
func activityNotification(principal auth.Principal, plant store.Plant, ct store.CareType, done bool) push.Notification {
	did := carePast(ct)
	if !done {
		did = "skipped " + careNoun(ct)
	}
	return push.Notification{
		Title: principal.Garden.Name,
		Body:  principal.User.DisplayName + " " + did + " " + plant.DisplayName() + ".",
		URL:   plantPath(plant.ID),
		// The recipient's browser fetches the icon itself, with their
		// session cookie. The photo route reads the photo in the session's
		// garden, so a recipient signed in to another garden sees no picture.
		Icon: squarePicturePath(plant),
	}
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
		h.templates.notFound(w, r)
		return
	}
	eventID, err := uuid.Parse(r.PathValue("event"))
	if err != nil {
		h.templates.notFound(w, r)
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
		h.templates.notFound(w, r)
		return
	case err != nil:
		h.templates.serverError(h.logger, w, r, "delete the care", err)
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
		h.templates.serverError(h.logger, w, r, "load the day", err)
		return
	}
	lines := plantLines(g.lines, plantID)
	if len(lines) == 0 {
		h.templates.notFound(w, r)
		return
	}
	line := lines[0]
	if named, ok := lineFor(lines, r.URL.Query().Get("row")); ok {
		line = named
	}
	row := newCareRow(schedule.Row{Plant: line.Plant, Care: line, Lines: lines}, g.now)
	swap := careSwap{Row: row, Head: swapHead(principal, g), Feed: swapFeed(principal, g)}
	v := view{page: "today", fragment: "care-undone", announce: "Undone. " + h.daySentence(swap.Head)}
	h.templates.render(w, r, v, swap)
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
// was logged.
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
		Picture:   squarePicturePath(plant),
		Care:      rowType.Name,
		Slug:      rowType.Slug,
		Done:      true,
		Said:      said(careType, event, d.When == whenNow, now.Location()),
		Undo:      undoPath(plant.ID, event.ID, rowType.Slug),
		Grace:     int(graceWindow.Milliseconds()),
		Collapse:  collapseMS,
	}
	if plant.Location != nil {
		r.Room = *plant.Location
	}
	return r
}

// said builds the line under a logged row saying what was done and when.
// justNow comes from the draft rather than from comparing the event's time with
// now, because Postgres stores microseconds and Go nanoseconds, so the two are
// never equal.
func said(careType store.CareType, logged store.CareEvent, justNow bool, loc *time.Location) string {
	if !logged.Done {
		return "Skipped · reminder in " + daysWord(int(*logged.OverrideIntervalDays))
	}
	verb := capitalise(carePast(careType))
	if justNow {
		return verb + " just now"
	}
	return verb + " " + stamp(logged.PerformedAt.In(loc))
}
