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

// recentLength is how many events a plant's Recent carries. Four is a month of
// a plant watered weekly, which is enough to see its rhythm.
const recentLength = 4

func plantSheetPath(plantID uuid.UUID) string {
	return logPath(plantID) + "?over=" + overPlant
}

// plantDetail is one read of a plant, serving both the page and the sheet the
// page opens.
type plantDetail struct {
	plant store.Plant
	// lines is every schedule the plant has, resolved against now.
	lines []schedule.Line
	// cares is every care type the garden has, because the sheet opened here
	// offers all of them.
	cares  []store.CareType
	recent []store.ListPlantCareEventsRow
	// now is in the reader's location.
	now time.Time
}

// loadPlant reads the plant and everything its page draws. It returns
// pgx.ErrNoRows for a plant the garden does not have, the same answer as for
// one that does not exist.
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

// offers is every care type the garden has, carrying the plant's schedule
// where it has one.
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
	// Botanical marks the heading as the botanical name, which is set in
	// italic wherever it appears.
	Botanical bool
	// Names is the names the heading did not use, the common one ahead of the
	// botanical one.
	Names    []plantName
	Room     string
	Schedule []scheduleRow
	// Log is where the Log care button opens the sheet. It is empty for a
	// reader who may not record care and for an archived plant.
	Log string
	// Foot is nil for an archived plant, and the page then ends at its
	// history.
	Foot *plantFoot
	// Reference is nil for a plant with nothing written down. The page draws
	// no section rather than an empty one.
	Reference *plantReference
	Recent    []recentLine
	// Activity is the whole log filtered to this plant, at the foot of Recent.
	// It is nil for a plant with nothing recorded, where the link would lead to
	// a page saying what the line above it already says.
	Activity *link
	Sheet    *sheet
}

type plantName struct {
	Text   string
	Italic bool
}

type scheduleRow struct {
	// ID is the row's id in every state, which is the element a swap replaces.
	ID   string
	Care string
	// Slug picks the row's icon, and survives a care type being renamed.
	Slug string
	// Rule is empty for a care type the plant is not scheduled for.
	Rule string
	// Season is the window a seasonal cadence runs in, and empty on the rest.
	Season string
	When   string
	// Late colours the right-hand end for an overdue schedule. Off dims it for
	// one whose season is shut.
	Late bool
	Off  bool
	// Scheduled is false for a care type the plant is not on.
	Scheduled bool
	// Open is where the row opens as the editor, and is empty for a reader who
	// may not change a schedule.
	Open string
	// Edit is the editor the row is open as, and nil for a row at rest.
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

// plantFoot is the end of a plant's page: Edit plant and Archive, or the
// question Archive swaps them for.
type plantFoot struct {
	// One URL answers both halves of archiving, a GET asking the question and a
	// POST doing it.
	Edit    string
	Archive string
	// Asking draws the question in place of the two controls. Keep is the way
	// back to them.
	Asking bool
	Name   string
	Keep   string
}

// newPlantFoot is the foot at rest, and nil for a reader who may neither edit
// nor archive.
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

// asking is the foot with the question up. Archiving asks first because the
// press takes the plant off the Plants list and leaves no row to undo from.
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

// scheduleRows is the plant's schedules and then the care types in the garden
// it is not on. The absent ones are in the list rather than behind an Add
// button because the reason to read this section is to change what the plant is
// scheduled for, and they are the only way it acquires a schedule from here.
// They are left out for a reader who cannot open one, since pressing is all
// they are for.
func scheduleRows(principal auth.Principal, d plantDetail) []scheduleRow {
	edit := principal.Can(auth.ScheduleEdit) && d.plant.ArchivedAt == nil

	rows := make([]scheduleRow, 0, len(d.cares))
	scheduled := make(map[string]bool, len(d.lines))
	for _, line := range d.lines {
		// A one-off that has been done is over, so the plant is no longer
		// scheduled for that care.
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

// rowFor is the schedule row a care type has on the page, and nil for a care
// type the page does not draw one for.
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

// ruleWord is the left of a schedule row. It leaves the date to the
// right-hand end.
func ruleWord(s store.CareSchedule) string {
	if s.IntervalCount == nil {
		return "Just once"
	}
	return "Every " + everyWord(*s.IntervalCount, *s.IntervalUnit)
}

// dueWord is the right of a schedule row. A date carries "Due" in front of it
// so the row does not read as a bare "Friday" beside "Every 7 days".
func dueWord(line schedule.Line, now time.Time) string {
	switch line.State {
	case schedule.Dormant:
		return "Out of season"
	case schedule.Overdue:
		// The row starts its own phrase, unlike the roster's "Repot overdue
		// since March".
		return capitalise(overdueWord(line, now))
	case schedule.DueToday:
		// A month-precise occurrence is due for the whole of its month, so it
		// is named rather than being reported as today.
		if line.Precision != schedule.PrecisionMonth {
			return "Due today"
		}
	}
	// Past the coming week an anchored schedule names its date rather than
	// counting to it, because "1 May 2027" is what was written down and "in
	// 241 days" has to be converted.
	if line.Schedule.AnchorDate != nil && line.Days > comingWeek {
		return "Due " + anchorWord(line.Due, line.Precision, now)
	}
	return "Due " + comingWord(line, now)
}

// comingWeek is how far ahead a schedule is counted towards rather than
// named. It matches the week Today's Coming up runs to.
const comingWeek = 7

// newPlantReference draws the facts in one fixed order for every plant. Water
// and Feed repeat the care types above them, because a schedule says how often
// and a fact says what the care means for this plant.
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

// plant answers GET /plants/{plant}.
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

// plantFootID is the id the foot keeps in both states, and it is the name of
// the template that draws it, so the element a swap aims at and the fragment
// answering it are one string.
const plantFootID = "plant-foot"

// plantSwap is the fragment a swap aimed at the page is answered with, and it
// narrows the page to what that fragment draws. A navigation and a swap aimed
// at anything else get the whole page. It reports false for a schedule row the
// page does not draw, because the swap asked for an element that is not on it.
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
