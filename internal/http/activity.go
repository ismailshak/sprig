package http

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/schedule"
	"github.com/ismailshak/sprig/internal/store"
)

const activityPath = "/activity"

// strandPage is how many events one page of the strand holds. Twenty is a few
// days of a garden that logs about five cares a day.
const strandPage = 20

// gardenSilence is the shortest run of empty days the strand marks. A floor of
// one would put "nothing for 1 day" between most of the markers because
// something is logged most days.
const gardenSilence = 3

type activity struct {
	logger    *slog.Logger
	queries   *store.Queries
	templates *Templates
	now       func() time.Time
}

func (h *activity) show(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	page, err := h.page(r.Context(), principal)
	if err != nil {
		serverError(h.logger, w, r, "load the log", err)
		return
	}
	h.templates.render(w, r, view{page: "activity"}, page)
}

func (h *activity) page(ctx context.Context, principal auth.Principal) (activityPage, error) {
	now := h.now().In(locationFor(principal.User))

	events, err := h.queries.ListCareEventLog(ctx, principal.Garden.ID, strandPage)
	if err != nil {
		return activityPage{}, fmt.Errorf("list the log: %w", err)
	}
	plants, err := h.queries.CountPlants(ctx, principal.Garden.ID)
	if err != nil {
		return activityPage{}, fmt.Errorf("count the plants: %w", err)
	}

	return newActivityPage(principal, events, plants, now), nil
}

type activityPage struct {
	Items []strandItem
	// Empty is set only when Items is empty.
	Empty *activityEmpty
}

// strandItem is one item on the strand, and exactly one of the three fields is
// set.
type strandItem struct {
	Day     *dayMarker
	Silence *silence
	Event   *eventRow
}

type dayMarker struct {
	Title string
	// Count is how many events the day holds.
	Count int
}

type silence struct {
	Label string
}

type eventRow struct {
	Name string
	// Botanical marks Name as the plant's botanical name.
	Botanical bool
	// Who and Did are the two halves of "Sam watered".
	Who string
	Did string
	// At is the clock time alone because the marker above the row names the day.
	At string
	// Extra is the skip's re-check and the note.
	Extra string
}

type activityEmpty struct {
	// NoPlants picks the plants icon over the activity one because a garden
	// with no plants is a first run rather than an empty history.
	NoPlants bool
	Title    string
	Line     string
	Action   *link
}

func newActivityPage(principal auth.Principal, events []store.ListCareEventLogRow, plants int64, now time.Time) activityPage {
	if len(events) == 0 {
		return activityPage{Empty: newActivityEmpty(principal, plants)}
	}
	return activityPage{Items: strand(principal, events, now)}
}

// newActivityEmpty offers an action only to a garden with no plants because
// care is logged on Today or on a plant rather than here. A reader who may not
// create a plant is offered none.
func newActivityEmpty(principal auth.Principal, plants int64) *activityEmpty {
	if plants == 0 {
		empty := &activityEmpty{
			NoPlants: true,
			Title:    "No plants yet",
			Line:     "Add one and sprig will tell you when it needs water.",
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

// strand takes events newest first because a day's marker counts the run of
// events that follows it.
func strand(principal auth.Principal, events []store.ListCareEventLogRow, now time.Time) []strandItem {
	items := make([]strandItem, 0, len(events))
	day := func(i int) int { return schedule.DaysBetween(events[i].CareEvent.PerformedAt, now) }

	for i := 0; i < len(events); {
		run := 1
		for i+run < len(events) && day(i+run) == day(i) {
			run++
		}
		if i > 0 {
			if quiet := day(i) - day(i-1) - 1; quiet >= gardenSilence {
				items = append(items, strandItem{Silence: &silence{Label: "nothing for " + daysWord(quiet)}})
			}
		}
		items = append(items, strandItem{Day: &dayMarker{Title: dayHeading(events[i].CareEvent.PerformedAt, now), Count: run}})
		for _, e := range events[i : i+run] {
			items = append(items, strandItem{Event: newEventRow(principal, e, now)})
		}
		i += run
	}
	return items
}

func newEventRow(principal auth.Principal, e store.ListCareEventLogRow, now time.Time) *eventRow {
	row := &eventRow{
		Name:      e.Plant.DisplayName(),
		Botanical: e.Plant.BotanicalOnly(),
		At:        e.CareEvent.PerformedAt.In(now.Location()).Format("3:04pm"),
	}
	row.Who, row.Did = whoDid(principal, e.PerformedByName, e.CareEvent, e.CareType)
	row.Extra = eventExtra(e.CareEvent)
	return row
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
