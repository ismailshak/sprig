package http

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/photo"
	"github.com/ismailshak/sprig/internal/schedule"
	"github.com/ismailshak/sprig/internal/store"
)

const plantsPath = "/plants"

func plantPath(plantID uuid.UUID) string {
	return plantsPath + "/" + plantID.String()
}

// noRoom is the heading for plants with no location. A room actually named "No
// room" is grouped with them.
const noRoom = "No room"

type plants struct {
	logger    *slog.Logger
	queries   *store.Queries
	photos    *photo.Store
	templates *Templates
	// now supplies the current time, so a test can fix the day.
	now func() time.Time
}

func (h *plants) show(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	page, err := h.load(r.Context(), principal)
	if err != nil {
		serverError(h.logger, w, r, "load the plant list", err)
		return
	}
	h.templates.render(w, r, view{page: "plants"}, page)
}

func (h *plants) load(ctx context.Context, principal auth.Principal) (plantsPage, error) {
	now := h.now().In(locationFor(principal.User))

	list, err := h.queries.ListPlants(ctx, principal.Garden.ID)
	if err != nil {
		return plantsPage{}, fmt.Errorf("list the plants: %w", err)
	}
	schedules, err := h.queries.ListCareSchedules(ctx, principal.Garden.ID)
	if err != nil {
		return plantsPage{}, fmt.Errorf("list the schedules: %w", err)
	}
	latest, err := h.queries.ListLatestCareEvents(ctx, principal.Garden.ID)
	if err != nil {
		return plantsPage{}, fmt.Errorf("list the latest care: %w", err)
	}

	return newPlantsPage(principal, list, schedule.Resolve(schedules, latest, now), now), nil
}

type plantsPage struct {
	// Add shows the Add button in the bar and on the empty state. A reader who
	// may not create a plant gets neither, rather than disabled buttons.
	Add   bool
	Rooms []roomSection
}

type roomSection struct {
	// ID is based on the room's position rather than its name, since two rooms
	// can differ only in punctuation an id would drop.
	ID     string
	Title  string
	Plants []plantRow
}

type plantRow struct {
	Href      string
	Name      string
	Botanical bool
	// Sub is the plant's other name, empty for a plant with only one.
	Sub          string
	SubBotanical bool
	// Standing is the overdue text, empty unless a care is overdue. A care due
	// today is not mentioned because Today already says so.
	Standing string
}

func newPlantsPage(principal auth.Principal, list []store.Plant, lines []schedule.Line, now time.Time) plantsPage {
	page := plantsPage{Add: principal.Can(auth.PlantCreate)}

	rooms := map[string][]plantRow{}
	for _, plant := range list {
		room := noRoom
		if plant.Location != nil && *plant.Location != "" {
			room = *plant.Location
		}
		rooms[room] = append(rooms[room], newPlantRow(plant, plantLines(lines, plant.ID), now))
	}

	for _, room := range sortedRooms(rooms) {
		roomRows := rooms[room]
		slices.SortStableFunc(roomRows, func(a, b plantRow) int { return compareNames(a.Name, b.Name) })
		page.Rooms = append(page.Rooms, roomSection{
			ID:     fmt.Sprintf("room-%d", len(page.Rooms)),
			Title:  room,
			Plants: roomRows,
		})
	}
	return page
}

// sortedRooms sorts room names case-insensitively with noRoom last. This is
// done here rather than in the query because noRoom is not a column value.
func sortedRooms(rooms map[string][]plantRow) []string {
	names := slices.Collect(maps.Keys(rooms))
	slices.SortFunc(names, func(a, b string) int {
		if unplaced := (a == noRoom); unplaced != (b == noRoom) {
			if unplaced {
				return 1
			}
			return -1
		}
		return compareNames(a, b)
	})
	return names
}

// compareNames compares case-insensitively, so Anthurium and anthurium sort
// together. Names that differ only in case are then compared as written.
func compareNames(a, b string) int {
	return cmp.Or(cmp.Compare(strings.ToLower(a), strings.ToLower(b)), cmp.Compare(a, b))
}

func newPlantRow(plant store.Plant, lines []schedule.Line, now time.Time) plantRow {
	row := plantRow{
		Href:      plantPath(plant.ID),
		Name:      plant.DisplayName(),
		Botanical: plant.BotanicalOnly(),
	}
	row.Sub, row.SubBotanical = plant.OtherName()
	if late := mostOverdue(lines); late != nil {
		row.Standing = late.CareType.Name + " " + overdueWord(*late, now)
	}
	return row
}

// mostOverdue returns the schedule furthest past its due date, or nil if none
// is overdue.
func mostOverdue(lines []schedule.Line) *schedule.Line {
	var worst *schedule.Line
	for i, line := range lines {
		if line.State != schedule.Overdue {
			continue
		}
		if worst == nil || line.Days < worst.Days {
			worst = &lines[i]
		}
	}
	return worst
}
