package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
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
	Sheet     *sheet
}

type plantName struct {
	Text   string
	Italic bool
}

type scheduleRow struct {
	Care string
	// Slug picks the row's icon, and survives a care type being renamed.
	Slug string
	Rule string
	// Season is the window a seasonal cadence runs in, and empty on the rest.
	Season string
	When   string
	// Late colours the right-hand end for an overdue schedule. Off dims it for
	// one whose season is shut.
	Late bool
	Off  bool
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
		Schedule:  scheduleRows(d.lines, d.now),
		Reference: newPlantReference(plant),
		Recent:    recentLines(principal, d.recent, d.now),
	}
	if plant.Location != nil {
		page.Room = *plant.Location
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

func scheduleRows(lines []schedule.Line, now time.Time) []scheduleRow {
	rows := make([]scheduleRow, 0, len(lines))
	for _, line := range lines {
		// A one-off that has been done is over, so the plant is no longer
		// scheduled for that care.
		if line.State == schedule.Spent {
			continue
		}
		rows = append(rows, newScheduleRow(line, now))
	}
	return rows
}

func newScheduleRow(line schedule.Line, now time.Time) scheduleRow {
	row := scheduleRow{
		Care: line.CareType.Name,
		Slug: line.CareType.Slug,
		Rule: ruleWord(line.Schedule),
		When: dueWord(line, now),
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
		// A month-precise anchor names the month it has passed, because it was
		// never precise to a day.
		if line.Precision == schedule.PrecisionMonth {
			return "Overdue since " + strings.TrimPrefix(anchorWord(line.Due, line.Precision, now), "in ")
		}
		return lateWord(-line.Days)
	}
	// Past the coming week an anchored schedule names its date rather than
	// counting to it, because "1 May 2027" is what was written down and "in
	// 241 days" has to be converted. A month-precise occurrence is named in
	// both directions, since it is due for the whole of its month.
	anchored := line.Schedule.AnchorDate != nil
	if line.Precision == schedule.PrecisionMonth || (anchored && line.Days > comingWeek) {
		return "Due " + anchorWord(line.Due, line.Precision, now)
	}
	if line.State == schedule.DueToday {
		return "Due today"
	}
	return "Due " + whenWord(line.Days, now)
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
	h.templates.render(w, r, view{page: "plant", fragment: plantFootFragment(r)}, newPlantPage(principal, detail))
}

// plantFootID is the id the foot keeps in both states, and it is the name of
// the template that draws it, so the element a swap aims at and the fragment
// answering it are one string.
const plantFootID = "plant-foot"

// plantFootFragment answers the foot alone to a swap aimed at it. A navigation
// and every other swap get the whole page.
func plantFootFragment(r *http.Request) string {
	if r.Header.Get("HX-Target") == plantFootID {
		return plantFootID
	}
	return ""
}
