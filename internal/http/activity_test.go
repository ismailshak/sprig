package http

import (
	"context"
	"html"
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

// rosewoodSamID is a second member of the garden Today is tested on, since a
// log row names who did the care.
var rosewoodSamID = uuid.MustParse("00000000-0000-7000-8000-000000000105")

// fairviewWaterID is the other garden's care type, needed to insert an event
// there and check it is not listed.
var fairviewWaterID = uuid.MustParse("00000000-0000-7000-8000-000000000203")

type logFixture struct {
	handler   *activity
	tx        pgx.Tx
	principal auth.Principal
}

// rosewoodLog inserts the garden's events directly, because events derived
// from the schedules would have no gaps for the log to mark.
func rosewoodLog(t *testing.T) *logFixture {
	t.Helper()

	f := rosewood(t)
	f.exec(t, "DELETE FROM care_event WHERE garden_id = $1", rosewoodID)
	f.exec(t, "INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Sam', 'sam', 'Europe/London')", rosewoodSamID)
	f.exec(t, "INSERT INTO membership (garden_id, user_id, role, digest_hour) VALUES ($1, $2, 'sitter', 8)", rosewoodID, rosewoodSamID)

	events := []struct {
		plant uuid.UUID
		care  uuid.UUID
		by    uuid.UUID
		at    time.Time
		done  bool
		// again is the skip's override interval, zero for any other event.
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

// at returns a time in London, the reader's timezone.
func at(month time.Month, d, hour, minute int) time.Time {
	return time.Date(2026, month, d, hour, minute, 0, 0, london())
}

func (f *logFixture) show(t *testing.T) string {
	t.Helper()
	return f.get(t, activityPath)
}

// get requests the log at a URL and fails on any status but 200.
func (f *logFixture) get(t *testing.T, target string) string {
	t.Helper()

	rec := f.request(t, target)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	return rec.Body.String()
}

func (f *logFixture) request(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	f.handler.show(rec, req)
	return rec
}

// care inserts one care event. The tests write their own history because one
// derived from a schedule has no gaps in it to find.
func (f *logFixture) care(t *testing.T, plantID, careID uuid.UUID, at time.Time) {
	t.Helper()
	f.exec(t, `INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done)
		VALUES ($1, $2, $3, $4, $5, $5, true)`, rosewoodID, plantID, careID, readerID, at)
}

// waterings inserts a watering for each argument, that many days before the
// Thursday the garden is written against.
func (f *logFixture) waterings(t *testing.T, plantID uuid.UUID, daysAgo ...int) {
	t.Helper()
	for _, days := range daysAgo {
		f.care(t, plantID, waterID, thursday.AddDate(0, 0, -days))
	}
}

func (f *logFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.tx.Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("%v\n%s", err, sql)
	}
}

// backlink matches the link in the top bar. The activity log has one only when
// it is filtered to a plant.
var backlink = regexp.MustCompile(`(?s)<a class="backlink" href="([^"]*)">(.*?)</a>`)

// footLink matches a link under the list. On the activity log those are the
// pager's two links. The swap attributes after the href are skipped.
var footLink = regexp.MustCompile(`(?s)<a class="foot-link" href="([^"]*)"[^>]*>(.*?)</a>`)

// pagerLink is the href of the link under the list with this text, or "" when
// the page has no such link.
func pagerLink(page, label string) string {
	for _, m := range footLink.FindAllStringSubmatch(page, -1) {
		if text(m[2]) == label {
			return html.UnescapeString(m[1])
		}
	}
	return ""
}

const (
	olderLink  = "Older activity"
	latestLink = "Latest activity"
)

// follow requests the page the link with this text points at.
func (f *logFixture) follow(t *testing.T, page, label string) string {
	t.Helper()

	href := pagerLink(page, label)
	if href == "" {
		t.Fatalf("the page has no %q link:\n%s", label, text(page))
	}
	return f.get(t, href)
}

// daysBack counts from newest to oldest, for one care a day over that range.
func daysBack(newest, oldest int) []int {
	out := make([]int, 0, oldest-newest+1)
	for d := newest; d <= oldest; d++ {
		out = append(out, d)
	}
	return out
}

// railItemElement matches every item in the log, whichever of the three kinds
// it is. The classes are captured rather than listed, because a deleted row
// has row--done as well.
var railItemElement = regexp.MustCompile(`(?s)<li class="([^"]*)"[^>]*>(.*?)</li>`)

type logEntry struct {
	// kind is the item's class attribute, which starts with one of dayKind,
	// gapKind and eventKind.
	kind string
	text string
}

func logEntries(page string) []logEntry {
	var lines []logEntry
	for _, m := range railItemElement.FindAllStringSubmatch(page, -1) {
		lines = append(lines, logEntry{kind: m[1], text: text(m[2])})
	}
	return lines
}

func textOf(lines []logEntry, kind string) []string {
	var out []string
	for _, l := range lines {
		if strings.HasPrefix(l.kind, kind) {
			out = append(out, l.text)
		}
	}
	return out
}

// dayOf is the title of the day marker above the first row starting with this
// text, and "" when no row does.
func dayOf(page, lead string) string {
	day := ""
	for _, l := range logEntries(page) {
		if strings.HasPrefix(l.kind, dayKind) {
			day, _, _ = strings.Cut(l.text, " · ")
		}
		if strings.HasPrefix(l.text, lead) {
			return day
		}
	}
	return ""
}

const (
	dayKind   = "rail__day"
	gapKind   = "rail__gap"
	eventKind = "row row--event"
)

func TestActivity_EventsAreListedNewestFirst(t *testing.T) {
	f := rosewoodLog(t)

	got := textOf(logEntries(f.show(t)), eventKind)

	want := []string{"Big Fella", "Doris", "Nigel", "Trail Mix", "Sprout", "Spike", "Opuntia microdasys"}
	if len(got) != len(want) {
		t.Fatalf("the log carries %d events, want %d:\n%v", len(got), len(want), got)
	}
	for i, name := range want {
		if !strings.HasPrefix(got[i], name) {
			t.Errorf("event %d reads %q, want it to lead with %q", i, got[i], name)
		}
	}
}

func TestActivity_ADayMarkerCountsThatDaysEvents(t *testing.T) {
	f := rosewoodLog(t)

	got := textOf(logEntries(f.show(t)), dayKind)

	want := []string{"Today · 2", "Yesterday · 1", "21 August · 1", "19 August · 1", "16 August · 1", "12 August · 1"}
	if !slices.Equal(got, want) {
		t.Errorf("the day markers read %v, want %v", got, want)
	}
}

func TestActivity_AGapMarkerSaysHowManyDaysHadNothing(t *testing.T) {
	f := rosewoodLog(t)

	got := textOf(logEntries(f.show(t)), gapKind)

	want := []string{"No activity for 11 days", "No activity for 3 days"}
	if !slices.Equal(got, want) {
		t.Errorf("the log marks %v, want %v", got, want)
	}
}

// The gaps of one and two days in the fixture are under gardenSilence.
func TestActivity_AGapUnderThreeDaysGetsNoMarker(t *testing.T) {
	f := rosewoodLog(t)

	for _, unwanted := range []string{"nothing for 1 day", "nothing for 2 days"} {
		if strings.Contains(f.show(t), unwanted) {
			t.Errorf("the log says %q, and a garden that quiet for a day or two is not silent", unwanted)
		}
	}
}

func TestActivity_AGapMarkerIsPlacedBetweenTheDaysItSeparates(t *testing.T) {
	f := rosewoodLog(t)

	lines := logEntries(f.show(t))

	var before, after string
	for i, l := range lines {
		if l.kind != gapKind || l.text != "No activity for 11 days" {
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
	// Performed on Tuesday and recorded on Thursday, the one event where
	// ordering by performed_at and by recorded_at disagree.
	f.exec(t, `INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done)
		VALUES ($1, $2, $3, $4, $5, $6, true)`, rosewoodID, dorisID, waterID, readerID, at(time.September, 1, 10, 0), thursday)

	got := textOf(logEntries(f.show(t)), dayKind)

	// Ordered by recorded_at, the Tuesday marker would be at the top.
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

	lines := logEntries(f.show(t))

	if got := textOf(lines, dayKind)[0]; got != "Today · 3" {
		t.Errorf("today's marker reads %q, want the half hour past midnight counted under it", got)
	}
	if got := textOf(lines, eventKind)[2]; !strings.Contains(got, "12:30am") {
		t.Errorf("the third event reads %q, want the clock London kept", got)
	}
}

func TestActivity_ARowSaysWhoDidWhatAndWhen(t *testing.T) {
	f := rosewoodLog(t)

	got := textOf(logEntries(f.show(t)), eventKind)[0]

	if want := "Big Fella You watered · 7:30am"; got != want {
		t.Errorf("the newest row reads %q, want %q", got, want)
	}
}

func TestActivity_ASkipSaysWhenItWillBeAskedAgain(t *testing.T) {
	f := rosewoodLog(t)

	got := textOf(logEntries(f.show(t)), eventKind)[1]

	if want := "Doris Sam skipped · 6:15am Reminder in 2 days"; got != want {
		t.Errorf("the skipped row reads %q, want %q", got, want)
	}
}

func TestActivity_ANoteIsQuotedUnderTheRow(t *testing.T) {
	f := rosewoodLog(t)

	got := textOf(logEntries(f.show(t)), eventKind)[2]

	if want := "Nigel You fed · 6:00pm “New pot”"; got != want {
		t.Errorf("the noted row reads %q, want %q", got, want)
	}
}

func TestActivity_APlantWithOnlyABotanicalNameIsShownInItalics(t *testing.T) {
	f := rosewoodLog(t)

	if !strings.Contains(f.show(t), `<span class="row__name row__name--sp">Opuntia microdasys</span>`) {
		t.Error("the botanical name is not marked as one")
	}
}

func TestActivity_OnlyOnePageOfEventsIsShown(t *testing.T) {
	f := rosewoodLog(t)
	// Twenty events newer than everything in the fixture fill the page on
	// their own.
	f.exec(t, `INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done)
		SELECT $1, $2, $3, $4, $5::timestamptz - make_interval(mins => n), $5::timestamptz - make_interval(mins => n), true
		FROM generate_series(1, 20) AS n`, rosewoodID, nigelID, waterID, readerID, at(time.September, 3, 8, 0))

	got := textOf(logEntries(f.show(t)), eventKind)

	if len(got) != logPageSize {
		t.Errorf("the log carries %d events, want %d", len(got), logPageSize)
	}
	if strings.Contains(strings.Join(got, "\n"), "Opuntia") {
		t.Error("the oldest event is on the page, and a page of twenty cannot hold twenty-seven")
	}
}

func TestActivity_AnotherGardensEventsAreNotListed(t *testing.T) {
	f := rosewoodLog(t)
	f.exec(t, "INSERT INTO garden (id, name) VALUES ($1, 'Fairview')", fairviewID)
	f.exec(t, "INSERT INTO plant (id, garden_id, nickname) VALUES ($1, $2, 'Hedge')", fairviewPlantID, fairviewID)
	f.exec(t, "INSERT INTO care_type (id, garden_id, name, slug) VALUES ($1, $2, 'Water', 'water')", fairviewWaterID, fairviewID)
	f.exec(t, `INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done)
		VALUES ($1, $2, $3, $4, $5, $5, true)`, fairviewID, fairviewPlantID, fairviewWaterID, readerID, at(time.September, 3, 8, 0))

	page := f.show(t)

	if strings.Contains(page, "Hedge") {
		t.Error("the log names a plant from another garden")
	}
	if got := len(textOf(logEntries(page), eventKind)); got != 7 {
		t.Errorf("the log carries %d events, want the garden's own 7", got)
	}
}

func TestActivity_AGardenWithNothingLoggedSaysNoActivityYet(t *testing.T) {
	f := rosewoodLog(t)
	f.exec(t, "DELETE FROM care_event WHERE garden_id = $1", rosewoodID)

	page := f.show(t)

	if got := text(page); !strings.Contains(got, "No activity yet") {
		t.Errorf("the empty log reads %q, want it to say there is no activity yet", got)
	}
	if logEntries(page) != nil {
		t.Error("the empty log renders a list")
	}
}

func TestActivity_ASitterInAGardenWithNoPlantsGetsNoAddLink(t *testing.T) {
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

func TestActivity_AGardenWithNoPlantsGetsAnAddPlantLink(t *testing.T) {
	f := rosewoodLog(t)
	f.exec(t, "DELETE FROM care_event WHERE garden_id = $1", rosewoodID)
	f.exec(t, "DELETE FROM plant WHERE garden_id = $1", rosewoodID)

	got := text(f.show(t))

	if !strings.Contains(got, "No plants yet") || !strings.Contains(got, "Add a plant") {
		t.Errorf("the log of a garden with no plants reads %q, want the first run's screen", got)
	}
}

func TestActivity_ALogShorterThanAPageHasNoOlderLink(t *testing.T) {
	f := rosewoodLog(t)

	if got := pagerLink(f.show(t), olderLink); got != "" {
		t.Errorf("the page links to %q as older activity, and seven events fit on one page", got)
	}
}

// One event a day, so no date spans the page boundary and each page can be
// identified by the dates on it.
func TestActivity_TheOlderLinkOpensAtTheEventAfterTheLastOneOnThePage(t *testing.T) {
	f := rosewoodLog(t)
	f.waterings(t, nigelID, daysBack(30, 50)...)

	first := f.show(t)
	second := f.follow(t, first, olderLink)

	firstDays := textOf(logEntries(first), dayKind)
	secondDays := textOf(logEntries(second), dayKind)
	if want := "23 July · 1"; firstDays[len(firstDays)-1] != want {
		t.Errorf("the first page ends on %q, want %q", firstDays[len(firstDays)-1], want)
	}
	if want := "22 July · 1"; secondDays[0] != want {
		t.Errorf("the second page starts on %q, want %q", secondDays[0], want)
	}
}

// This is why the link holds a timestamp rather than an offset.
func TestActivity_TheOlderPageIsUnchangedByCareLoggedAfterTheLinkWasRendered(t *testing.T) {
	f := rosewoodLog(t)
	f.waterings(t, nigelID, daysBack(30, 50)...)

	older := pagerLink(f.show(t), olderLink)
	f.care(t, spikeID, waterID, thursday)
	second := f.get(t, older)

	if got := textOf(logEntries(second), dayKind)[0]; got != "22 July · 1" {
		t.Errorf("the second page starts on %q, want 22 July: an event logged at the top of the log should not move it", got)
	}
	if got := len(textOf(logEntries(second), eventKind)); got != 8 {
		t.Errorf("the second page holds %d events, want the 8 below the first page", got)
	}
}

// Logging several plants as "yesterday at 9am" gives them one timestamp. A
// cursor holding only the timestamp would skip the ones after the first.
func TestActivity_TwoEventsWithOneTimestampAreBothShownAcrossAPageBoundary(t *testing.T) {
	f := rosewoodLog(t)
	f.waterings(t, nigelID, daysBack(30, 41)...)
	tied := thursday.AddDate(0, 0, -50)
	f.care(t, spikeID, waterID, tied)
	f.care(t, sproutID, waterID, tied)

	first := f.show(t)
	second := f.follow(t, first, olderLink)

	firstEvents := textOf(logEntries(first), eventKind)
	secondEvents := textOf(logEntries(second), eventKind)
	if len(firstEvents) != logPageSize {
		t.Fatalf("the first page holds %d events, want %d", len(firstEvents), logPageSize)
	}
	if len(secondEvents) != 1 {
		t.Fatalf("the second page holds %d events, want the one sharing a timestamp with the last row of the first page: %v", len(secondEvents), secondEvents)
	}
	got := []string{firstEvents[len(firstEvents)-1], secondEvents[0]}
	slices.Sort(got)
	want := []string{"Spike You watered · 9:00am", "Sprout You watered · 9:00am"}
	if !slices.Equal(got, want) {
		t.Errorf("the rows either side of the page boundary read %v, want %v", got, want)
	}
}

func TestActivity_TheFirstPageHasNoLatestActivityLink(t *testing.T) {
	f := rosewoodLog(t)
	f.waterings(t, nigelID, daysBack(30, 50)...)

	if got := pagerLink(f.show(t), latestLink); got != "" {
		t.Errorf("the first page links to %q as the latest activity, and it is already the latest activity", got)
	}
}

func TestActivity_APageAfterTheFirstLinksBackToTheLatestActivity(t *testing.T) {
	f := rosewoodLog(t)
	f.waterings(t, nigelID, daysBack(30, 50)...)

	second := f.follow(t, f.show(t), olderLink)

	if got := pagerLink(second, latestLink); got != activityPath {
		t.Errorf("the second page links back to %q, want %q", got, activityPath)
	}
}

// The events the link points past can be deleted while it is on screen.
func TestActivity_ACursorWithNoEventsLeftRedirectsToTheLatestActivity(t *testing.T) {
	f := rosewoodLog(t)
	f.waterings(t, nigelID, daysBack(30, 50)...)
	older := pagerLink(f.show(t), olderLink)
	f.exec(t, "DELETE FROM care_event WHERE garden_id = $1 AND performed_at < $2", rosewoodID, thursday.AddDate(0, 0, -42))

	rec := f.request(t, older)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, text(rec.Body.String()))
	}
	if got := rec.Header().Get("Location"); got != activityPath {
		t.Errorf("the redirect goes to %q, want %q", got, activityPath)
	}
}

func TestActivity_AMalformedBeforeParameterIsNotFound(t *testing.T) {
	f := rosewoodLog(t)

	rec := f.request(t, activityPath+"?before=yesterday")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestActivity_AFilteredRowShowsTheCareAndTheDateInsteadOfThePlantName(t *testing.T) {
	f := rosewoodLog(t)

	got := textOf(logEntries(f.get(t, plantActivityPath(bigFellaID))), eventKind)

	want := []string{"Watered You · today, 7:30am"}
	if !slices.Equal(got, want) {
		t.Errorf("the filtered log reads %v, want %v", got, want)
	}
}

// There would be one heading per row, since a plant is cared for at most once a
// day.
func TestActivity_AFilteredLogHasNoDayHeadings(t *testing.T) {
	f := rosewoodLog(t)
	f.waterings(t, bigFellaID, 10, 20, 30)

	got := logEntries(f.get(t, plantActivityPath(bigFellaID)))

	if days := textOf(got, dayKind); days != nil {
		t.Errorf("the filtered log has the day headings %v, want none", days)
	}
	if events := textOf(got, eventKind); len(events) != 4 {
		t.Errorf("the filtered log holds %d events, want the plant's own 4: %v", len(events), events)
	}
}

func TestActivity_ASkippedCareOnAFilteredRowIsShownAsSkipped(t *testing.T) {
	f := rosewoodLog(t)

	page := f.get(t, plantActivityPath(dorisID))

	if got := textOf(logEntries(page), eventKind)[0]; got != "Skipped Sam · today, 6:15am Reminder in 2 days" {
		t.Errorf("the skipped row reads %q, want it to say Skipped", got)
	}
}

func TestActivity_AFilteredLogLabelsAGapTwiceThePlantsUsualInterval(t *testing.T) {
	f := rosewoodLog(t)
	// Watered every ten days, with one thirty-day gap.
	f.waterings(t, bigFellaID, 10, 20, 30, 60, 70)

	got := textOf(logEntries(f.get(t, plantActivityPath(bigFellaID))), gapKind)

	want := []string{"No activity for 30 days"}
	if !slices.Equal(got, want) {
		t.Errorf("the filtered log labels the gaps %v, want %v", got, want)
	}
}

// The same thirty days on a plant watered every twenty, where it is the usual
// interval and not a gap.
func TestActivity_AFilteredLogDoesNotLabelAGapUnderTwiceThePlantsUsualInterval(t *testing.T) {
	f := rosewoodLog(t)
	f.waterings(t, bigFellaID, 20, 40, 60, 90, 110)

	got := textOf(logEntries(f.get(t, plantActivityPath(bigFellaID))), gapKind)

	if got != nil {
		t.Errorf("the log of a plant watered every twenty days labels the gaps %v, want none", got)
	}
}

// Twice a daily plant's interval is two days, which would label most of its
// rows, so the floor is two weeks whatever the interval.
func TestActivity_AFilteredLogDoesNotLabelAGapOfUnderTwoWeeks(t *testing.T) {
	f := rosewoodLog(t)
	f.waterings(t, bigFellaID, append(daysBack(1, 10), 22)...)

	got := textOf(logEntries(f.get(t, plantActivityPath(bigFellaID))), gapKind)

	if got != nil {
		t.Errorf("the log of a plant watered daily labels the gaps %v, want none: twelve days is under the two-week floor", got)
	}
}

func TestActivity_AFilteredLogLinksBackToThePlant(t *testing.T) {
	f := rosewoodLog(t)

	page := f.get(t, plantActivityPath(bigFellaID))

	m := backlink.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("the filtered log has no back link:\n%s", text(page))
	}
	if got, want := m[1], plantPath(bigFellaID); got != want {
		t.Errorf("the back link points at %q, want %q", got, want)
	}
	if got := text(m[2]); got != "Big Fella" {
		t.Errorf("the back link reads %q, want the name of the plant the log is filtered to", got)
	}
}

func TestActivity_TheOlderLinkOnAFilteredLogKeepsTheFilter(t *testing.T) {
	f := rosewoodLog(t)
	f.waterings(t, bigFellaID, daysBack(1, 25)...)

	second := f.follow(t, f.get(t, plantActivityPath(bigFellaID)), olderLink)

	if got := len(textOf(logEntries(second), eventKind)); got != 6 {
		t.Errorf("the second page holds %d events, want the plant's own 6", got)
	}
}

func TestActivity_TheLatestLinkOnAFilteredLogKeepsTheFilter(t *testing.T) {
	f := rosewoodLog(t)
	f.waterings(t, bigFellaID, daysBack(1, 25)...)

	second := f.follow(t, f.get(t, plantActivityPath(bigFellaID)), olderLink)

	if got, want := pagerLink(second, latestLink), plantActivityPath(bigFellaID); got != want {
		t.Errorf("the second page's Latest activity link points at %q, want %q", got, want)
	}
}

func TestActivity_AFilteredCursorWithNoEventsLeftRedirectsToThePlantsLatestActivity(t *testing.T) {
	f := rosewoodLog(t)
	f.waterings(t, bigFellaID, daysBack(1, 25)...)
	older := pagerLink(f.get(t, plantActivityPath(bigFellaID)), olderLink)
	// The first page ends 19 days back, so this removes every event the link
	// points at.
	f.exec(t, "DELETE FROM care_event WHERE plant_id = $1 AND performed_at < $2", bigFellaID, thursday.AddDate(0, 0, -19))

	rec := f.request(t, older)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, text(rec.Body.String()))
	}
	if got, want := rec.Header().Get("Location"), plantActivityPath(bigFellaID); got != want {
		t.Errorf("the redirect goes to %q, want %q", got, want)
	}
}

func TestActivity_AFilteredLogWithNoEventsSaysNoActivityYet(t *testing.T) {
	f := rosewoodLog(t)
	f.exec(t, "DELETE FROM care_event WHERE plant_id = $1", bigFellaID)

	page := f.get(t, plantActivityPath(bigFellaID))

	if got := text(page); !strings.Contains(got, "No activity yet") {
		t.Errorf("the filtered log with no events reads %q, want it to say there is no activity yet", got)
	}
	if logEntries(page) != nil {
		t.Error("the filtered log with no events still renders a list of rows")
	}
}

func TestActivity_AFilterOnAnotherGardensPlantIsNotFound(t *testing.T) {
	f := rosewoodLog(t)
	f.exec(t, "INSERT INTO garden (id, name) VALUES ($1, 'Fairview')", fairviewID)
	f.exec(t, "INSERT INTO plant (id, garden_id, nickname) VALUES ($1, $2, 'Hedge')", fairviewPlantID, fairviewID)

	rec := f.request(t, plantActivityPath(fairviewPlantID))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestActivity_AMalformedPlantParameterIsNotFound(t *testing.T) {
	f := rosewoodLog(t)

	rec := f.request(t, activityPath+"?plant=big-fella")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestActivity_ASwapAimedAtTheLogBodyGetsTheListAndNotTheWholePage(t *testing.T) {
	f := rosewoodLog(t)
	// The row a delete leaves fetches the log when its window ends and swaps
	// the body with what comes back.
	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, activityPath, nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", logBodyID)
	rec := httptest.NewRecorder()

	f.handler.show(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.HasPrefix(body, "<!doctype html>") {
		t.Errorf("the response is the whole page, want the log body alone:\n%.120s", body)
	}
	if !strings.Contains(body, `id="`+logBodyID+`"`) {
		t.Errorf("the response does not hold the log body:\n%.120s", body)
	}
	if got := len(textOf(logEntries(body), eventKind)); got != 7 {
		t.Errorf("the log body holds %d rows, want the garden's 7", got)
	}
}

func TestActivity_ASwapAimedAtTheFiltersAndTheLogGetsBothAndNotTheWholePage(t *testing.T) {
	f := rosewoodLog(t)
	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, activityPath+"?care=water", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", logID)
	rec := httptest.NewRecorder()

	f.handler.show(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.HasPrefix(body, "<!doctype html>") {
		t.Errorf("the response is the whole page, want the filters and the log alone:\n%.120s", body)
	}
	if !strings.Contains(body, `id="`+logID+`"`) || !strings.Contains(body, `id="`+logBodyID+`"`) || !strings.Contains(body, `id="filter-care"`) {
		t.Errorf("the response does not hold the filters and the log body:\n%.200s", body)
	}
	if !strings.Contains(body, "<details") || strings.Contains(body, "<details open") || strings.Contains(body, " open>") {
		t.Error("the filters are not a closed details element")
	}
	if !strings.Contains(text(body), "Filter · Water") {
		t.Errorf("the summary line does not say the log is filtered to Water:\n%s", text(body))
	}
}

func TestActivity_ARowShowsThePlantsPictureAsItsSquare(t *testing.T) {
	f := rosewoodLog(t)
	photoID := givePicture(t, f.tx, bigFellaID)

	page := f.show(t)

	if got, want := images(page), []string{photoSquarePath(bigFellaID, photoID)}; !slices.Equal(got, want) {
		t.Errorf("the log's images are %v, want Big Fella's square at %v and none on Doris's row", got, want)
	}
}

func TestActivity_TheCareFilterListsThatCareAlone(t *testing.T) {
	f := rosewoodLog(t)

	page := f.get(t, activityPath+"?care=feed")

	rows := textOf(logEntries(page), eventKind)
	if len(rows) != 1 || !strings.HasPrefix(rows[0], "Nigel") {
		t.Errorf("the rows are %v, want Nigel's feed alone", rows)
	}
}

// The range is read in the reader's timezone, London, so an event at 23:30 on
// the last day is in and one at 00:15 the next morning is out.
func TestActivity_TheDateRangeIncludesBothEndsInTheReadersDay(t *testing.T) {
	f := rosewoodLog(t)
	f.care(t, dorisID, waterID, at(time.August, 21, 23, 30))
	f.care(t, bigFellaID, waterID, at(time.August, 22, 0, 15))

	page := f.get(t, activityPath+"?from=2026-08-19&to=2026-08-21")

	rows := textOf(logEntries(page), eventKind)
	if len(rows) != 3 {
		t.Fatalf("the rows are %v, want Doris, Trail Mix and Sprout", rows)
	}
	for i, want := range []string{"Doris", "Trail Mix", "Sprout"} {
		if !strings.HasPrefix(rows[i], want) {
			t.Errorf("row %d is %q, want %s", i, rows[i], want)
		}
	}
}

// The clocks go forward on 29 March in London, so the day the range ends on is
// 23 hours long and the range still stops at midnight.
func TestActivity_ADateRangeOverTheClockChangeEndsAtTheReadersMidnight(t *testing.T) {
	f := rosewoodLog(t)
	f.care(t, dorisID, waterID, at(time.March, 29, 23, 30))
	f.care(t, bigFellaID, waterID, at(time.March, 30, 0, 15))

	page := f.get(t, activityPath+"?from=2026-03-28&to=2026-03-29")

	rows := textOf(logEntries(page), eventKind)
	if len(rows) != 1 || !strings.HasPrefix(rows[0], "Doris") {
		t.Errorf("the rows are %v, want Doris's watering at 23:30 on the 29th alone", rows)
	}
}

func TestActivity_AnOpenEndedRangeFiltersOnTheOneDateGiven(t *testing.T) {
	f := rosewoodLog(t)

	since := textOf(logEntries(f.get(t, activityPath+"?from=2026-09-02")), eventKind)
	until := textOf(logEntries(f.get(t, activityPath+"?to=2026-08-16")), eventKind)

	if len(since) != 3 {
		t.Errorf("from 2 September lists %v, want the three events on the 2nd and 3rd", since)
	}
	if len(until) != 2 {
		t.Errorf("to 16 August lists %v, want Spike and Opuntia", until)
	}
}

func TestActivity_TheOlderLinkKeepsTheFilters(t *testing.T) {
	f := rosewoodLog(t)
	f.waterings(t, nigelID, daysBack(30, 50)...)

	first := f.get(t, activityPath+"?care=water&to=2026-09-03")

	href := pagerLink(first, olderLink)
	if !strings.Contains(href, "care=water") || !strings.Contains(href, "to=2026-09-03") {
		t.Fatalf("the Older link is %q, want the care and the date on it", href)
	}
	second := f.get(t, href)
	if !strings.Contains(pagerLink(second, latestLink), "care=water") {
		t.Errorf("the Latest link is %q, want the care on it", pagerLink(second, latestLink))
	}
	for _, row := range textOf(logEntries(second), eventKind) {
		if strings.Contains(row, "fed") {
			t.Errorf("the older page lists a feed under the water filter: %q", row)
		}
	}
}

func TestActivity_FiltersThatMatchNothingSayNothingMatches(t *testing.T) {
	f := rosewoodLog(t)

	page := f.get(t, activityPath+"?care=feed&from=2026-09-03")

	if !strings.Contains(text(page), "Nothing matches these filters") {
		t.Errorf("the page does not say the filters matched nothing:\n%s", text(page))
	}
	if strings.Contains(text(page), "No activity yet") {
		t.Error("the page says nothing has been logged, and the filters are what hid it")
	}
}

func TestActivity_ACareTypeTheGardenDoesNotHaveIsNotFound(t *testing.T) {
	f := rosewoodLog(t)

	if rec := f.request(t, activityPath+"?care=prune"); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestActivity_AMalformedDateIsNotFound(t *testing.T) {
	f := rosewoodLog(t)

	for _, target := range []string{activityPath + "?from=yesterday", activityPath + "?to=2026-13-01"} {
		if rec := f.request(t, target); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want %d", target, rec.Code, http.StatusNotFound)
		}
	}
}

// filterForm matches the filter form above the list. It is the only form on
// the page that submits to the log's own URL.
var filterForm = regexp.MustCompile(`(?s)<form[^>]*action="/activity"[^>]*>(.*?)</form>`)

func TestActivity_TheFilterFormComesBackWithThePlantAndTheValuesChosen(t *testing.T) {
	f := rosewoodLog(t)

	page := f.get(t, activityPath+"?plant="+bigFellaID.String()+"&care=water&from=2026-08-01")

	form := filterForm.FindStringSubmatch(page)
	if form == nil {
		t.Fatalf("the page has no filter form:\n%s", page)
	}
	for _, want := range []string{
		`<input type="hidden" name="plant" value="` + bigFellaID.String() + `">`,
		`<option value="water" selected>Water</option>`,
		`<option value="feed">Feed</option>`,
		`name="from" value="2026-08-01"`,
		`name="to" value=""`,
	} {
		if !strings.Contains(form[1], want) {
			t.Errorf("the form lacks %s:\n%s", want, form[1])
		}
	}
}

// clearLink matches the Clear link beside the filters and captures its href.
var clearLink = regexp.MustCompile(`<a class="filters__clear" href="([^"]*)"`)

func TestActivity_ClearIsOfferedOnlyWhileAFilterIsOn(t *testing.T) {
	f := rosewoodLog(t)

	plain := f.show(t)
	filtered := f.get(t, activityPath+"?plant="+bigFellaID.String()+"&care=water")

	if clearLink.MatchString(plain) {
		t.Error("the unfiltered log offers Clear")
	}
	clear := clearLink.FindStringSubmatch(filtered)
	if clear == nil || html.UnescapeString(clear[1]) != plantActivityPath(bigFellaID) {
		t.Errorf("the filtered log's Clear is %v, want a link to %s", clear, plantActivityPath(bigFellaID))
	}
}

func TestActivity_TheSummarySaysTheDatesTheLogIsFilteredBetween(t *testing.T) {
	f := rosewoodLog(t)

	page := f.get(t, activityPath+"?from=2026-09-01&to=2026-09-03")

	if !strings.Contains(text(page), "Filter · from 1 Sep · to 3 Sep") {
		t.Errorf("the summary does not say the range:\n%s", text(page))
	}
}

// Now is 3 September 2026 in London, so a date in 2025 is labelled with its
// year and one in 2026 is not.
func TestActivity_ADateInAnotherYearIsSummarisedWithItsYear(t *testing.T) {
	f := rosewoodLog(t)

	page := f.get(t, activityPath+"?from=2025-12-31")

	if !strings.Contains(text(page), "Filter · from 31 Dec 2025") {
		t.Errorf("the summary does not say the year:\n%s", text(page))
	}
}

func TestActivity_ACareTypeTurnedOffStillFiltersItsEvents(t *testing.T) {
	f := rosewoodLog(t)
	f.exec(t, "UPDATE care_type SET archived_at = now() WHERE id = $1", feedID)

	page := f.get(t, activityPath+"?care=feed")

	if rows := textOf(logEntries(page), eventKind); len(rows) != 1 {
		t.Errorf("the rows are %v, want Nigel's feed, because its events are still in the log", rows)
	}
}
