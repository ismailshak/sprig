package http

import (
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/store"
)

var (
	pruneID      = uuid.MustParse("00000000-0000-7000-8000-000000000d10")
	pruneEventID = uuid.MustParse("00000000-0000-7000-8000-000000000d11")
	wipeID       = uuid.MustParse("00000000-0000-7000-8000-000000000d12")
)

// pruneBigFella gives Rosewood a Prune care type with the shears icon, prunes
// Big Fella on 29 August and schedules pruning him every day. On 3 September,
// the date the tests read the garden on, his pruning is more overdue than his
// watering. Today shows the pruning as his row.
func pruneBigFella(t *testing.T, tx pgx.Tx) store.CareType {
	t.Helper()

	prune := store.CareType{ID: pruneID, Name: "Prune", Slug: "prune", Icon: "prune"}
	insertCareTypes(t, tx, rosewoodID, prune)
	mustExec(t, tx, "INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit, set_at) VALUES ($1, $2, $3, 1, 'day', $4)",
		rosewoodID, bigFellaID, pruneID, day(time.August, 28))
	mustExec(t, tx, "INSERT INTO care_event (id, garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done) VALUES ($1, $2, $3, $4, $5, $6, $6, true)",
		pruneEventID, rosewoodID, bigFellaID, pruneID, readerID, day(time.August, 29))
	return prune
}

// iconsIn returns the name of each care icon inside e, in page order.
func iconsIn(e *element) []string {
	if e == nil {
		return nil
	}
	var names []string
	for _, icon := range e.all(hasAttr("data-icon")) {
		names = append(names, icon.attr("data-icon"))
	}
	return names
}

func TestToday_TheCareButtonShowsTheIconChosenForACareTypeTheGardenAdded(t *testing.T) {
	f := rosewood(t)
	prune := pruneBigFella(t, f.tx)

	row := readHTML(f.show(t)).byID(careRowID(store.Plant{ID: bigFellaID}, prune))

	if got := iconsIn(row); !slices.Equal(got, []string{"prune"}) {
		t.Errorf("Big Fella's pruning row shows the icons %v, want [prune]", got)
	}
}

func TestToday_ACareTypeWithNoIconChosenShowsTheWaterDrop(t *testing.T) {
	f := rosewood(t)
	wipe := store.CareType{ID: wipeID, Name: "Wipe", Slug: "wipe"}
	f.exec(t, "INSERT INTO care_type (id, garden_id, name, slug) VALUES ($1, $2, 'Wipe', 'wipe')", wipeID, rosewoodID)
	f.exec(t, "INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit, set_at) VALUES ($1, $2, $3, 1, 'day', $4)",
		rosewoodID, bigFellaID, wipeID, day(time.August, 30))

	row := readHTML(f.show(t)).byID(careRowID(store.Plant{ID: bigFellaID}, wipe))

	if got := iconsIn(row); !slices.Equal(got, []string{"water"}) {
		t.Errorf("Big Fella's wiping row shows the icons %v, want [water]", got)
	}
}

func TestPlant_AScheduleRowShowsTheIconChosenForItsCareType(t *testing.T) {
	f := rosewoodPlant(t)
	prune := pruneBigFella(t, f.tx)

	row := readHTML(f.page(t, bigFellaID)).byID(scheduleRowID(prune))

	if got := iconsIn(row); !slices.Equal(got, []string{"prune"}) {
		t.Errorf("the Prune schedule row shows the icons %v, want [prune]", got)
	}
}

func TestPlantForm_AScheduleFieldRowShowsTheIconChosenForItsCareType(t *testing.T) {
	f := plantFormOn(t)
	prune := pruneBigFella(t, f.tx)

	rec := f.open(t, newPlantPath, false)

	if got := iconsIn(readHTML(rec.Body.String()).byID(scheduleRowID(prune))); !slices.Equal(got, []string{"prune"}) {
		t.Errorf("the Prune row of the Schedule field shows the icons %v, want [prune]", got)
	}
}

func TestActivity_ARowFilteredToOnePlantShowsTheIconChosenForItsCareType(t *testing.T) {
	f := rosewoodLog(t)
	pruneBigFella(t, f.tx)

	page := f.get(t, activityPath+"?plant="+bigFellaID.String())

	if got := iconsIn(readHTML(page).byID(eventRowID(pruneEventID))); !slices.Equal(got, []string{"prune"}) {
		t.Errorf("the pruning on Activity shows the icons %v, want [prune]", got)
	}
}

func TestCalendar_ADayShowsTheIconChosenForACareTypeDueOnIt(t *testing.T) {
	f := rosewoodCalendar(t)
	pruneBigFella(t, f.tx)

	page := f.get(t, calendarPath, nil)

	today := time.Date(2026, time.September, 3, 0, 0, 0, 0, london())
	link := readHTML(page).first(isTag("a"), attrIs("href", calendarHref(startOfMonth(today), &today)))
	if got := iconsIn(link); !slices.Contains(got, "prune") {
		t.Errorf("3 September shows the icons %v, want prune among them", got)
	}
}
