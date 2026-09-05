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
	"github.com/ismailshak/sprig/internal/schedule"
	"github.com/ismailshak/sprig/internal/store"
)

const plantsPath = "/plants"

func plantPath(plantID uuid.UUID) string {
	return plantsPath + "/" + plantID.String()
}

// noRoom is the heading for the plants whose location was never filled in. It
// names the absence rather than a place, because a plant nobody placed is not
// somewhere other than the rooms above it. A room actually called No room
// merges into the group.
const noRoom = "No room"

type plants struct {
	logger    *slog.Logger
	queries   *store.Queries
	templates *Templates
	// now is the clock the day is read from, so a test can pin the day.
	now func() time.Time
}

func (h *plants) show(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	page, err := h.roster(r.Context(), principal)
	if err != nil {
		serverError(h.logger, w, r, "load the roster", err)
		return
	}
	h.templates.render(w, r, view{page: "plants"}, page)
}

func (h *plants) roster(ctx context.Context, principal auth.Principal) (rosterPage, error) {
	now := h.now().In(locationFor(principal.User))

	list, err := h.queries.ListPlants(ctx, principal.Garden.ID)
	if err != nil {
		return rosterPage{}, fmt.Errorf("list the plants: %w", err)
	}
	schedules, err := h.queries.ListCareSchedules(ctx, principal.Garden.ID)
	if err != nil {
		return rosterPage{}, fmt.Errorf("list the schedules: %w", err)
	}
	latest, err := h.queries.ListLatestCareEvents(ctx, principal.Garden.ID)
	if err != nil {
		return rosterPage{}, fmt.Errorf("list the latest care: %w", err)
	}

	return newRosterPage(principal, list, schedule.Resolve(schedules, latest, now), now), nil
}

type rosterPage struct {
	// Add draws the button in the bar and the one on the empty screen. A
	// sitter gets neither rather than a disabled pair.
	Add   bool
	Rooms []roomSection
}

type roomSection struct {
	// ID is the room's position rather than its name, because two rooms can
	// differ only in punctuation an id would drop.
	ID     string
	Title  string
	Plants []rosterRow
}

type rosterRow struct {
	Href      string
	Name      string
	Botanical bool
	// Sub is the name the first line did not use, empty on a plant down to a
	// single name.
	Sub          string
	SubBotanical bool
	// Standing is empty unless a care is overdue. A care due today is left out
	// because Today already says so.
	Standing string
}

func newRosterPage(principal auth.Principal, list []store.Plant, lines []schedule.Line, now time.Time) rosterPage {
	page := rosterPage{Add: principal.Can(auth.PlantCreate)}

	rooms := map[string][]rosterRow{}
	for _, plant := range list {
		room := noRoom
		if plant.Location != nil && *plant.Location != "" {
			room = *plant.Location
		}
		rooms[room] = append(rooms[room], newRosterRow(plant, plantLines(lines, plant.ID), now))
	}

	for _, room := range sortedRooms(rooms) {
		roomRows := rooms[room]
		slices.SortStableFunc(roomRows, func(a, b rosterRow) int { return compareNames(a.Name, b.Name) })
		page.Rooms = append(page.Rooms, roomSection{
			ID:     fmt.Sprintf("room-%d", len(page.Rooms)),
			Title:  room,
			Plants: roomRows,
		})
	}
	return page
}

// sortedRooms orders the rooms here rather than in the query because No room
// is a heading no column holds.
func sortedRooms(rooms map[string][]rosterRow) []string {
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

// compareNames ignores case, so Anthurium and anthurium sit together rather
// than in separate halves of the alphabet. Comparing the names themselves
// breaks a tie between two that differ only in case.
func compareNames(a, b string) int {
	return cmp.Or(cmp.Compare(strings.ToLower(a), strings.ToLower(b)), cmp.Compare(a, b))
}

func newRosterRow(plant store.Plant, lines []schedule.Line, now time.Time) rosterRow {
	row := rosterRow{
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

// mostOverdue is the line furthest past its due date, and nil where nothing is
// late. A plant three weeks without water and two days without feeding is
// three weeks overdue.
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
