package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// rosewoodSamID is a second member of the garden Today is tested on because a
// row on the strand names who did the care.
var rosewoodSamID = uuid.MustParse("00000000-0000-7000-8000-000000000105")

// fairviewWaterID is the other garden's care type because an event needs one
// before the strand can be shown leaving that garden out.
var fairviewWaterID = uuid.MustParse("00000000-0000-7000-8000-000000000203")

type logFixture struct {
	handler   *activity
	tx        pgx.Tx
	principal auth.Principal
}

// rosewoodLog writes the garden's history by hand because a history derived
// from the schedules holds no silence for the strand to draw.
func rosewoodLog(t *testing.T) *logFixture {
	t.Helper()

	f := rosewood(t)
	f.exec(t, "DELETE FROM care_event WHERE garden_id = $1", rosewoodID)
	f.exec(t, "INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Sam', 'sam', 'Europe/London')", rosewoodSamID)
	f.exec(t, "INSERT INTO membership (garden_id, user_id, role) VALUES ($1, $2, 'sitter')", rosewoodID, rosewoodSamID)

	events := []struct {
		plant uuid.UUID
		care  uuid.UUID
		by    uuid.UUID
		at    time.Time
		done  bool
		// again is the skip's re-check and zero on any other event.
		again int
		note  string
	}{
		{bigFellaID, waterID, readerID, at(time.September, 3, 7, 30), true, 0, ""},    // today
		{dorisID, waterID, rosewoodSamID, at(time.September, 3, 6, 15), false, 2, ""}, // today, and the day's second
		{nigelID, feedID, readerID, at(time.September, 2, 18, 0), true, 0, "New pot"}, // yesterday, so no quiet day
		{trailMixID, waterID, readerID, at(time.August, 21, 9, 0), true, 0, ""},       // 13 days back, 11 quiet days
		{sproutID, waterID, readerID, at(time.August, 19, 9, 0), true, 0, ""},         // 15 days back, 1 quiet day
		{spikeID, waterID, readerID, at(time.August, 16, 9, 0), true, 0, ""},          // 18 days back, 2 quiet days
		{opuntiaID, waterID, readerID, at(time.August, 12, 9, 0), true, 0, ""},        // 22 days back, 3 quiet days
	}
	for _, e := range events {
		f.exec(t, `INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done, note, override_interval_days)
			VALUES ($1, $2, $3, $4, $5, $5, $6, nullif($7, ''), nullif($8::integer, 0))`,
			rosewoodID, e.plant, e.care, e.by, e.at, e.done, e.note, e.again)
	}

	return &logFixture{
		handler: &activity{
			logger:    testLogger,
			queries:   store.New(f.tx),
			templates: testTemplates(),
			now:       func() time.Time { return thursday },
		},
		tx:        f.tx,
		principal: f.principal,
	}
}

// at is an instant in London, the zone the reader keeps.
func at(month time.Month, d, hour, minute int) time.Time {
	return time.Date(2026, month, d, hour, minute, 0, 0, london())
}

func (f *logFixture) show(t *testing.T) string {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, activityPath, nil)
	rec := httptest.NewRecorder()
	f.handler.show(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	return rec.Body.String()
}

func (f *logFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.tx.Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("%v\n%s", err, sql)
	}
}

// railItemElement matches every item on the strand, whichever of the three it is.
var railItemElement = regexp.MustCompile(`(?s)<li class="(rail__day|rail__gap|row row--event)"[^>]*>(.*?)</li>`)

type strandLine struct {
	kind string
	text string
}

func strandLines(page string) []strandLine {
	var lines []strandLine
	for _, m := range railItemElement.FindAllStringSubmatch(page, -1) {
		lines = append(lines, strandLine{kind: m[1], text: text(m[2])})
	}
	return lines
}

func textOf(lines []strandLine, kind string) []string {
	var out []string
	for _, l := range lines {
		if l.kind == kind {
			out = append(out, l.text)
		}
	}
	return out
}

const (
	dayKind   = "rail__day"
	gapKind   = "rail__gap"
	eventKind = "row row--event"
)

func TestActivity_TheStrandRunsFromTheNewestEventToTheOldest(t *testing.T) {
	f := rosewoodLog(t)

	got := textOf(strandLines(f.show(t)), eventKind)

	want := []string{"Big Fella", "Doris", "Nigel", "Trail Mix", "Sprout", "Spike", "Opuntia microdasys"}
	if len(got) != len(want) {
		t.Fatalf("the strand carries %d events, want %d:\n%v", len(got), len(want), got)
	}
	for i, name := range want {
		if !strings.HasPrefix(got[i], name) {
			t.Errorf("event %d reads %q, want it to lead with %q", i, got[i], name)
		}
	}
}

func TestActivity_ADayMarkerCountsWhatTheDayHolds(t *testing.T) {
	f := rosewoodLog(t)

	got := textOf(strandLines(f.show(t)), dayKind)

	want := []string{"Today · 2", "Yesterday · 1", "21 August · 1", "19 August · 1", "16 August · 1", "12 August · 1"}
	if !slices.Equal(got, want) {
		t.Errorf("the day markers read %v, want %v", got, want)
	}
}

func TestActivity_ASilenceIsNamedByTheDaysItHeld(t *testing.T) {
	f := rosewoodLog(t)

	got := textOf(strandLines(f.show(t)), gapKind)

	want := []string{"nothing for 11 days", "nothing for 3 days"}
	if !slices.Equal(got, want) {
		t.Errorf("the strand marks %v, want %v", got, want)
	}
}

// The one and two quiet days in the history are under gardenSilence.
func TestActivity_AQuietDayOrTwoIsNotASilence(t *testing.T) {
	f := rosewoodLog(t)

	for _, unwanted := range []string{"nothing for 1 day", "nothing for 2 days"} {
		if strings.Contains(f.show(t), unwanted) {
			t.Errorf("the strand says %q, and a garden that quiet for a day or two is not silent", unwanted)
		}
	}
}

func TestActivity_ASilenceIsDrawnBetweenTheDaysItSeparates(t *testing.T) {
	f := rosewoodLog(t)

	lines := strandLines(f.show(t))

	var before, after string
	for i, l := range lines {
		if l.kind != gapKind || l.text != "nothing for 11 days" {
			continue
		}
		before, after = lines[i-1].text, lines[i+1].text
	}
	if !strings.HasPrefix(before, "Nigel") || after != "21 August · 1" {
		t.Errorf("the eleven days sit between %q and %q, want them under yesterday's last event and over 21 August", before, after)
	}
}

func TestActivity_AnEventIsUnderTheDayItWasPerformed(t *testing.T) {
	f := rosewoodLog(t)
	// Performed on Tuesday and recorded on Thursday, the one event where the
	// two orderings disagree.
	f.exec(t, `INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done)
		VALUES ($1, $2, $3, $4, $5, $6, true)`, rosewoodID, dorisID, waterID, readerID, at(time.September, 1, 10, 0), thursday)

	got := textOf(strandLines(f.show(t)), dayKind)

	// Under recorded_at the Tuesday marker would sit at the top of the strand.
	want := []string{"Today · 2", "Yesterday · 1", "Tuesday · 1", "21 August · 1", "19 August · 1", "16 August · 1", "12 August · 1"}
	if !slices.Equal(got, want) {
		t.Errorf("the day markers read %v, want %v", got, want)
	}
}

func TestActivity_AnEventAfterMidnightBelongsToTheReadersDay(t *testing.T) {
	f := rosewoodLog(t)
	// Half past midnight in London is the previous evening in UTC.
	f.exec(t, `INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done)
		VALUES ($1, $2, $3, $4, $5, $5, true)`, rosewoodID, sproutID, waterID, readerID, at(time.September, 3, 0, 30))

	lines := strandLines(f.show(t))

	if got := textOf(lines, dayKind)[0]; got != "Today · 3" {
		t.Errorf("today's marker reads %q, want the half hour past midnight counted under it", got)
	}
	if got := textOf(lines, eventKind)[2]; !strings.Contains(got, "12:30am") {
		t.Errorf("the third event reads %q, want the clock London kept", got)
	}
}

func TestActivity_ARowSaysWhoDidWhatAndWhen(t *testing.T) {
	f := rosewoodLog(t)

	got := textOf(strandLines(f.show(t)), eventKind)[0]

	if want := "Big Fella You watered · 7:30am"; got != want {
		t.Errorf("the newest row reads %q, want %q", got, want)
	}
}

func TestActivity_ASkipSaysWhenItWillBeAskedAgain(t *testing.T) {
	f := rosewoodLog(t)

	got := textOf(strandLines(f.show(t)), eventKind)[1]

	if want := "Doris Sam skipped · 6:15am Asking again in 2 days"; got != want {
		t.Errorf("the skipped row reads %q, want %q", got, want)
	}
}

func TestActivity_ANoteIsQuotedUnderTheRow(t *testing.T) {
	f := rosewoodLog(t)

	got := textOf(strandLines(f.show(t)), eventKind)[2]

	if want := "Nigel You fed · 6:00pm “New pot”"; got != want {
		t.Errorf("the noted row reads %q, want %q", got, want)
	}
}

func TestActivity_APlantDownToItsBotanicalNameIsSetInItalic(t *testing.T) {
	f := rosewoodLog(t)

	if !strings.Contains(f.show(t), `<span class="row__name row__name--sp">Opuntia microdasys</span>`) {
		t.Error("the botanical name is not marked as one")
	}
}

func TestActivity_OnePageOfEventsIsDrawn(t *testing.T) {
	f := rosewoodLog(t)
	// Twenty events newer than everything above fill the page on their own.
	f.exec(t, `INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done)
		SELECT $1, $2, $3, $4, $5::timestamptz - make_interval(mins => n), $5::timestamptz - make_interval(mins => n), true
		FROM generate_series(1, 20) AS n`, rosewoodID, nigelID, waterID, readerID, at(time.September, 3, 8, 0))

	got := textOf(strandLines(f.show(t)), eventKind)

	if len(got) != strandPage {
		t.Errorf("the strand carries %d events, want %d", len(got), strandPage)
	}
	if strings.Contains(strings.Join(got, "\n"), "Opuntia") {
		t.Error("the oldest event is on the page, and a page of twenty cannot hold twenty-seven")
	}
}

func TestActivity_AnotherGardensEventsAreNotOnTheStrand(t *testing.T) {
	f := rosewoodLog(t)
	f.exec(t, "INSERT INTO garden (id, name) VALUES ($1, 'Fairview')", fairviewID)
	f.exec(t, "INSERT INTO plant (id, garden_id, nickname) VALUES ($1, $2, 'Hedge')", fairviewPlantID, fairviewID)
	f.exec(t, "INSERT INTO care_type (id, garden_id, name, slug) VALUES ($1, $2, 'Water', 'water')", fairviewWaterID, fairviewID)
	f.exec(t, `INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done)
		VALUES ($1, $2, $3, $4, $5, $5, true)`, fairviewID, fairviewPlantID, fairviewWaterID, readerID, at(time.September, 3, 8, 0))

	page := f.show(t)

	if strings.Contains(page, "Hedge") {
		t.Error("the strand names a plant from another garden")
	}
	if got := len(textOf(strandLines(page), eventKind)); got != 7 {
		t.Errorf("the strand carries %d events, want the garden's own 7", got)
	}
}

func TestActivity_AGardenWithNothingRecordedSaysSo(t *testing.T) {
	f := rosewoodLog(t)
	f.exec(t, "DELETE FROM care_event WHERE garden_id = $1", rosewoodID)

	page := f.show(t)

	if got := text(page); !strings.Contains(got, "Nothing recorded yet") {
		t.Errorf("the empty log reads %q, want it to say nothing is recorded", got)
	}
	if strandLines(page) != nil {
		t.Error("the empty log draws a strand")
	}
}

func TestActivity_ASitterWithNoPlantsIsOfferedNothing(t *testing.T) {
	f := rosewoodLog(t)
	f.principal.Capabilities = auth.Capabilities{}
	f.exec(t, "DELETE FROM care_event WHERE garden_id = $1", rosewoodID)
	f.exec(t, "DELETE FROM plant WHERE garden_id = $1", rosewoodID)

	got := f.show(t)

	if !strings.Contains(got, "No plants yet") {
		t.Errorf("the log reads %q, want the empty garden", text(got))
	}
	if strings.Contains(got, newPlantPath) {
		t.Errorf("a sitter is offered the way to add a plant:\n%s", text(got))
	}
}

func TestActivity_AGardenWithNoPlantsIsOfferedItsFirst(t *testing.T) {
	f := rosewoodLog(t)
	f.exec(t, "DELETE FROM care_event WHERE garden_id = $1", rosewoodID)
	f.exec(t, "DELETE FROM plant WHERE garden_id = $1", rosewoodID)

	got := text(f.show(t))

	if !strings.Contains(got, "No plants yet") || !strings.Contains(got, "Add a plant") {
		t.Errorf("the log of a garden with no plants reads %q, want the first run's screen", got)
	}
}
