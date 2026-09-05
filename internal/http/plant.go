package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/schedule"
	"github.com/ismailshak/sprig/internal/store"
)

// recentLength is how many events the Recent section shows. Four is a month for
// a plant watered weekly, enough to see the pattern.
const recentLength = 4

func plantSheetPath(plantID uuid.UUID) string {
	return logPath(plantID) + "?over=" + overPlant
}

// plantDetail is everything a plant's page and the sheet it opens render from.
type plantDetail struct {
	plant store.Plant
	// lines is every schedule the plant has, resolved against now.
	lines []schedule.Line
	// cares is every care type in the garden, since the sheet on this page
	// offers all of them.
	cares  []store.CareType
	recent []store.ListPlantCareEventsRow
	// now is in the reader's timezone.
	now time.Time
}

// loadPlant reads the plant and everything its page shows. It returns
// pgx.ErrNoRows for a plant the garden does not have, the same as for one that
// does not exist.
func loadPlant(ctx context.Context, queries *store.Queries, principal auth.Principal, plantID uuid.UUID, now time.Time) (plantDetail, error) {
	d := plantDetail{now: now.In(locationFor(principal.User))}

	plant, err := queries.GetPlant(ctx, principal.Garden.ID, plantID)
	if err != nil {
		return d, err
	}
	d.plant = plant

	schedules, err := queries.ListCareSchedules(ctx, principal.Garden.ID)
	if err != nil {
		return d, fmt.Errorf("list the schedules: %w", err)
	}
	latest, err := queries.ListLatestCareEvents(ctx, principal.Garden.ID)
	if err != nil {
		return d, fmt.Errorf("list the latest care: %w", err)
	}
	d.cares, err = queries.ListCareTypes(ctx, principal.Garden.ID)
	if err != nil {
		return d, fmt.Errorf("list the care types: %w", err)
	}
	d.recent, err = queries.ListPlantCareEvents(ctx, store.ListPlantCareEventsParams{
		GardenID: principal.Garden.ID,
		PlantID:  plantID,
		Count:    recentLength,
	})
	if err != nil {
		return d, fmt.Errorf("list the plant's care: %w", err)
	}

	d.lines = plantLines(schedule.Resolve(schedules, latest, d.now), plantID)
	return d, nil
}

// offers returns every care type in the garden, with the plant's schedule where
// it has one.
func (d plantDetail) offers() []offer {
	out := make([]offer, 0, len(d.cares))
	for _, care := range d.cares {
		o := offer{CareType: care}
		if line, ok := lineFor(d.lines, care.Slug); ok {
			o.Schedule = line.Schedule
		}
		out = append(out, o)
	}
	return out
}

type plantPage struct {
	Name string
	// Botanical is true when the heading is the botanical name, which is shown
	// in italics.
	Botanical bool
	// Names is the plant's other names, the common name before the botanical.
	Names    []plantName
	Room     string
	Schedule []scheduleRow
	// Log is the URL the Log care button opens the sheet from. Empty for a
	// reader who may not log care and for an archived plant.
	Log string
	// Foot is nil for an archived plant.
	Foot *plantFoot
	// Reference is nil when the plant has no facts or notes, and the section
	// is not rendered.
	Reference *plantReference
	Recent    []recentLine
	// Activity is the link under Recent to the log filtered to this plant. Nil
	// for a plant with nothing recorded, since that page would only repeat the
	// line above the link.
	Activity *link
	Sheet    *sheet
}

type plantName struct {
	Text   string
	Italic bool
}

type scheduleRow struct {
	// ID is the HTML id of the row's <li>. It is the same whether the row is
	// closed or being edited, so htmx can replace one with the other.
	ID   string
	Care string
	// Slug picks the row's icon.
	Slug string
	// Rule is the schedule as text, such as "Every 7 days". Empty for a care
	// type the plant has no schedule for.
	Rule string
	// Season is the months a seasonal schedule runs in, empty otherwise.
	Season string
	When   string
	// Late is true for an overdue schedule and Off for one out of season. Each
	// changes the colour of the due text.
	Late      bool
	Off       bool
	Scheduled bool
	// Open is the URL that opens the row as an editor. Empty for a reader who
	// may not edit schedules.
	Open string
	// Edit is set while the row is being edited, nil otherwise.
	Edit *scheduleEditor
}

type plantReference struct {
	Facts    []plantFact
	Note     string
	Acquired string
}

type plantFact struct {
	Label string
	Value string
}

type recentLine struct {
	Who  string
	Did  string
	When string
}

func newPlantPage(principal auth.Principal, d plantDetail) plantPage {
	plant := d.plant
	page := plantPage{
		Name:      plant.DisplayName(),
		Botanical: plant.BotanicalOnly(),
		Names:     otherNames(plant),
		Schedule:  scheduleRows(principal, d),
		Reference: newPlantReference(plant),
		Recent:    recentLines(principal, d.recent, d.now),
	}
	if plant.Location != nil {
		page.Room = *plant.Location
	}
	if len(d.recent) > 0 {
		page.Activity = &link{Label: "All activity for " + page.Name, Href: plantActivityPath(plant.ID)}
	}
	if plant.ArchivedAt != nil {
		return page
	}
	if principal.Can(auth.CareLog) {
		page.Log = plantSheetPath(plant.ID)
	}
	page.Foot = newPlantFoot(principal, plant)
	return page
}

// plantFoot is the buttons at the bottom of a plant's page: Edit plant and
// Archive, or the archive confirmation that replaces them.
type plantFoot struct {
	Edit string
	// Archive is the URL for archiving. A GET renders the confirmation and a
	// POST archives.
	Archive string
	// Asking is true while the confirmation replaces the two buttons. Keep is
	// the URL of the Keep link, which goes back to the plant's page.
	Asking bool
	Name   string
	Keep   string
}

// newPlantFoot builds the plantFoot with its buttons showing. It returns nil for a
// reader who may neither edit nor archive.
func newPlantFoot(principal auth.Principal, plant store.Plant) *plantFoot {
	foot := &plantFoot{Name: plant.DisplayName(), Keep: plantPath(plant.ID)}
	if principal.Can(auth.PlantEdit) {
		foot.Edit = editPlantPath(plant.ID)
	}
	if principal.Can(auth.PlantArchive) {
		foot.Archive = archivePlantPath(plant.ID)
	}
	if foot.Edit == "" && foot.Archive == "" {
		return nil
	}
	return foot
}

// asking returns the plantFoot with the archive confirmation showing. Archiving asks
// first because it removes the plant from the Plants list and leaves no row to
// undo from.
func (f *plantFoot) asking() *plantFoot {
	if f == nil {
		return nil
	}
	f.Asking = true
	return f
}

func otherNames(plant store.Plant) []plantName {
	var names []plantName
	display := plant.DisplayName()
	if isSet(plant.CommonName) && *plant.CommonName != display {
		names = append(names, plantName{Text: *plant.CommonName})
	}
	if isSet(plant.BotanicalName) && *plant.BotanicalName != display {
		names = append(names, plantName{Text: *plant.BotanicalName, Italic: true})
	}
	return names
}

// scheduleRows returns the plant's schedules followed by the garden's care
// types it has no schedule for. The unscheduled ones are listed rather than
// hidden behind an Add button, because this section exists to change what the
// plant is scheduled for and they are the only way to add a schedule here. They
// are left out for a reader who cannot edit, since clicking is all they are
// for.
func scheduleRows(principal auth.Principal, d plantDetail) []scheduleRow {
	edit := principal.Can(auth.ScheduleEdit) && d.plant.ArchivedAt == nil

	rows := make([]scheduleRow, 0, len(d.cares))
	scheduled := make(map[string]bool, len(d.lines))
	for _, line := range d.lines {
		// A one-off that has been done no longer counts as a schedule.
		if line.State == schedule.Spent {
			continue
		}
		scheduled[line.CareType.Slug] = true
		rows = append(rows, newScheduleRow(line, d, edit))
	}
	if !edit {
		return rows
	}
	for _, care := range d.cares {
		if !scheduled[care.Slug] {
			rows = append(rows, scheduleRow{
				ID:   scheduleRowID(care),
				Care: care.Name,
				Slug: care.Slug,
				When: "Not scheduled",
				Open: schedulePath(d.plant.ID, care.Slug),
			})
		}
	}
	return rows
}

// rowFor returns the schedule row for a care type, or nil if the page has none
// for it.
func (p *plantPage) rowFor(slug string) *scheduleRow {
	for i := range p.Schedule {
		if p.Schedule[i].Slug == slug {
			return &p.Schedule[i]
		}
	}
	return nil
}

func newScheduleRow(line schedule.Line, d plantDetail, edit bool) scheduleRow {
	row := scheduleRow{
		ID:        scheduleRowID(line.CareType),
		Care:      line.CareType.Name,
		Slug:      line.CareType.Slug,
		Rule:      ruleWord(line.Schedule),
		When:      dueWord(line, d.now),
		Scheduled: true,
	}
	if edit {
		row.Open = schedulePath(d.plant.ID, line.CareType.Slug)
	}
	// The schema sets the two season months together, so the end is non-nil
	// wherever the start is.
	if line.Schedule.SeasonStartMonth != nil {
		row.Season = seasonWord(*line.Schedule.SeasonStartMonth, *line.Schedule.SeasonEndMonth)
	}
	row.Late = line.State == schedule.Overdue
	row.Off = line.State == schedule.Dormant
	return row
}

// ruleWord is the text on the left of a schedule row, such as "Every 7 days"
// or "Just once".
func ruleWord(s store.CareSchedule) string {
	if s.IntervalCount == nil {
		return "Just once"
	}
	return "Every " + everyWord(*s.IntervalCount, *s.IntervalUnit)
}

// dueWord is the text on the right of a schedule row. A date is prefixed with
// "Due" so the row does not read as a bare "Friday" beside "Every 7 days".
func dueWord(line schedule.Line, now time.Time) string {
	switch line.State {
	case schedule.Dormant:
		return "Out of season"
	case schedule.Overdue:
		// Capitalised because the text stands alone here. On the Plants list
		// the care name comes before it.
		return capitalise(overdueWord(line, now))
	case schedule.DueToday:
		// A schedule precise only to a month is due for the whole month, so the
		// month is named instead of "Due today".
		if line.Precision != schedule.PrecisionMonth {
			return "Due today"
		}
	}
	// Beyond the coming week an anchored schedule shows its date rather than a
	// count of days, because "1 May 2027" is what was entered and "in 241
	// days" has to be converted back.
	if line.Schedule.AnchorDate != nil && line.Days > comingWeek {
		return "Due " + anchorWord(line.Due, line.Precision, now)
	}
	return "Due " + comingWord(line, now)
}

// comingWeek is how many days ahead a due date is shown as a count rather than
// a date. It matches the week Today's Coming up covers.
const comingWeek = 7

// newPlantReference builds the facts in a fixed order for every plant. Water
// and Feed repeat the care types above, because a schedule says how often and a
// fact says what this plant needs.
func newPlantReference(plant store.Plant) *plantReference {
	ref := &plantReference{Acquired: acquiredWord(plant.AcquiredYear, plant.AcquiredMonth)}
	for _, f := range []struct {
		label string
		value *string
	}{
		{"Sun", plant.Sun},
		{"Water", plant.WaterNeeds},
		{"Feed", plant.FeedNeeds},
		{"Soil", plant.Soil},
		{"Climate", plant.Climate},
		{"Pot", plant.Pot},
	} {
		if isSet(f.value) {
			ref.Facts = append(ref.Facts, plantFact{Label: f.label, Value: *f.value})
		}
	}
	if isSet(plant.Notes) {
		ref.Note = *plant.Notes
	}
	if len(ref.Facts) == 0 && ref.Note == "" && ref.Acquired == "" {
		return nil
	}
	return ref
}

func recentLines(principal auth.Principal, recent []store.ListPlantCareEventsRow, now time.Time) []recentLine {
	lines := make([]recentLine, 0, len(recent))
	for _, e := range recent {
		line := recentLine{When: agoWord(e.CareEvent.PerformedAt, now)}
		line.Who, line.Did = whoDid(principal, e.PerformedByName, e.CareEvent, e.CareType)
		lines = append(lines, line)
	}
	return lines
}

// plant handles GET /plants/{plant}.
func (h *plants) plant(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	plantID, err := uuid.Parse(r.PathValue("plant"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	detail, err := loadPlant(r.Context(), h.queries, principal, plantID, h.now())
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		http.NotFound(w, r)
		return
	case err != nil:
		serverError(h.logger, w, r, "load the plant", err)
		return
	}
	page := newPlantPage(principal, detail)
	fragment, ok := plantSwap(r, &page)
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.templates.render(w, r, view{page: "plant", fragment: fragment}, page)
}

// plantFootID is both the HTML id of the bottom section and the name of the
// template that renders it, so the swap target and the fragment are one string.
const plantFootID = "plant-foot"

// plantSwap picks the fragment an htmx request gets and narrows the page to
// what that fragment renders. A page navigation, or a swap targeting anything
// else, gets the whole page. It returns false for a schedule row the page does
// not have.
func plantSwap(r *http.Request, page *plantPage) (string, bool) {
	target := r.Header.Get("HX-Target")
	if target == plantFootID {
		return plantFootID, true
	}
	if slug, ok := scheduleRowSlug(target); ok {
		row := page.rowFor(slug)
		if row == nil {
			return "", false
		}
		page.Schedule = []scheduleRow{*row}
		return scheduleRowsFragment, true
	}
	return "", true
}
