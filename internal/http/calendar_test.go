package http

import (
	"cmp"
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

type calendarFixture struct {
	handler   *activity
	tx        pgx.Tx
	principal auth.Principal
}

// rosewoodCalendar is the garden Today is tested on, with the calendar read on
// the same Thursday, 3 September. Every plant was last watered in August, so
// August holds logged care and September holds care that is due.
func rosewoodCalendar(t *testing.T) *calendarFixture {
	t.Helper()

	f := rosewood(t)
	return &calendarFixture{
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

// get requests target with headers set on the request, and fails on any status
// but 200.
func (f *calendarFixture) get(t *testing.T, target string, headers map[string]string) string {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	f.handler.calendar(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	return rec.Body.String()
}

func (f *calendarFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	mustExec(t, f.tx, sql, args...)
}

// dayLinkName is the accessible name of the link on a day in the grid, or ""
// when the day is not a link. day is midnight in London.
func dayLinkName(page string, day time.Time) string {
	return readHTML(page).first(isTag("a"), attrIs("href", calendarHref(startOfMonth(day), &day))).attr("aria-label")
}

// monthTitle is the month's name above the grid.
func monthTitle(page string) string {
	return readHTML(page).byID(calendarID).first(isTag("h2")).text()
}

type plantLink struct {
	href string
	text string
}

// sheetLinks returns each link in the open day's sheet, in page order.
func sheetLinks(t *testing.T, page string) []plantLink {
	t.Helper()

	sheet := readHTML(page).byID(sheetID)
	if sheet == nil || sheet.tag != "dialog" {
		t.Fatalf("the page has no open sheet:\n%s", text(page))
	}
	var links []plantLink
	for _, a := range sheet.all(isTag("a")) {
		links = append(links, plantLink{href: a.attr("href"), text: a.text()})
	}
	return links
}

func TestCalendar_ADayLinkCountsTheCaresDueOnIt(t *testing.T) {
	f := rosewoodCalendar(t)

	page := f.get(t, calendarPath, nil)

	for d, want := range map[int]string{
		// Big Fella was due on 1 September. The day has passed, so his
		// watering is counted on today with Doris's and Nigel's.
		1: "",
		3: "Thursday 3 September, 3 due",
		// Big Fella's next watering is 10 days after today. Trail Mix's second
		// is 9 days after 4 September.
		13: "Sunday 13 September, 2 due",
	} {
		if got := dayLinkName(page, at(time.September, d, 0, 0)); got != want {
			t.Errorf("the link on %d September is named %q, want %q", d, got, want)
		}
	}
}

func TestCalendar_APastDayLinkCountsTheCareLoggedOnIt(t *testing.T) {
	f := rosewoodCalendar(t)

	page := f.get(t, calendarPath+"?month=2026-08", nil)

	if got, want := dayLinkName(page, at(time.August, 22, 0, 0)), "Saturday 22 August, 1 logged"; got != want {
		t.Errorf("the link on 22 August is named %q, want %q", got, want)
	}
	for _, link := range readHTML(page).byID(calendarID).all(isTag("a")) {
		if strings.Contains(link.attr("aria-label"), " due") {
			t.Errorf("a day in August counts care due: %q", link.attr("aria-label"))
		}
	}
}

func TestCalendar_TodayCountsCareLoggedAndCareStillDue(t *testing.T) {
	f := rosewoodCalendar(t)
	// Big Fella's overdue watering, logged this morning. His next is ten days
	// away.
	f.exec(t, "INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done) VALUES ($1, $2, $3, $4, $5, $5, true)",
		rosewoodID, bigFellaID, waterID, readerID, thursday)

	page := f.get(t, calendarPath, nil)

	if got, want := dayLinkName(page, at(time.September, 3, 0, 0)), "Thursday 3 September, 1 logged, 2 due"; got != want {
		t.Errorf("the link on today is named %q, want %q", got, want)
	}
}

func TestCalendar_ADaysSheetLinksEachCareDueToItsPlant(t *testing.T) {
	f := rosewoodCalendar(t)

	page := f.get(t, calendarPath+"?month=2026-09&day=2026-09-03", nil)

	if got, want := readHTML(page).byID(sheetID).first(isTag("h2")).text(), "Thursday 3 September"; got != want {
		t.Errorf("the sheet is headed %q, want %q", got, want)
	}
	want := []plantLink{
		{plantPath(bigFellaID), "Big Fella Water · 2 days late"},
		{plantPath(dorisID), "Doris Water"},
		{plantPath(nigelID), "Nigel Water"},
	}
	if got := sheetLinks(t, page); !slices.Equal(got, want) {
		t.Errorf("the sheet links\n%v\nwant\n%v", got, want)
	}
}

func TestCalendar_ACadenceDateAfterTheNextOneIsMarkedEstimated(t *testing.T) {
	f := rosewoodCalendar(t)

	page := f.get(t, calendarPath+"?month=2026-09&day=2026-09-13", nil)

	want := []plantLink{
		{plantPath(bigFellaID), "Big Fella Water · Estimated"},
		{plantPath(trailMixID), "Trail Mix Water · Estimated"},
	}
	if got := sheetLinks(t, page); !slices.Equal(got, want) {
		t.Errorf("the sheet links\n%v\nwant\n%v", got, want)
	}
}

func TestCalendar_ADaysSheetListsTheCareLoggedThatDay(t *testing.T) {
	f := rosewoodCalendar(t)

	page := f.get(t, calendarPath+"?month=2026-08&day=2026-08-22", nil)

	want := []plantLink{{plantPath(bigFellaID), "Big Fella You watered · 12:00pm"}}
	if got := sheetLinks(t, page); !slices.Equal(got, want) {
		t.Errorf("the sheet links\n%v\nwant\n%v", got, want)
	}
	var headings []string
	for _, h := range readHTML(page).byID(sheetID).all(isTag("h3")) {
		headings = append(headings, h.text())
	}
	if want := []string{"Logged"}; !slices.Equal(headings, want) {
		t.Errorf("the sheet's headings are %v, want %v", headings, want)
	}
}

func TestCalendar_AMonthPreciseCareIsListedAboveTheGridAndOnNoDay(t *testing.T) {
	f := rosewoodCalendar(t)
	// A feed planned for some time in October.
	f.exec(t, "INSERT INTO care_schedule (garden_id, plant_id, care_type_id, anchor_date, anchor_precision) VALUES ($1, $2, $3, '2026-10-01', 'month')",
		rosewoodID, dorisID, feedID)

	page := f.get(t, calendarPath+"?month=2026-10", nil)

	section := readHTML(page).first(isTag("section"), attrIs("aria-labelledby", "month-due"))
	if got, want := section.text(), "Due in October Doris Feed"; got != want {
		t.Errorf("the section above the grid reads %q, want %q", got, want)
	}
	// Nigel's watering and feed and Trail Mix's watering fall on 1 October.
	if got, want := dayLinkName(page, at(time.October, 1, 0, 0)), "Thursday 1 October, 3 due"; got != want {
		t.Errorf("the link on 1 October is named %q, want %q", got, want)
	}
}

func TestCalendar_PreviousAndNextLinkToTheMonthsEitherSide(t *testing.T) {
	f := rosewoodCalendar(t)

	doc := readHTML(f.get(t, calendarPath+"?month=2026-12", nil))

	if got, want := doc.first(isTag("a"), attrIs("aria-label", "Previous month")).attr("href"), "/activity/calendar?month=2026-11"; got != want {
		t.Errorf("Previous links to %q, want %q", got, want)
	}
	if got, want := doc.first(isTag("a"), attrIs("aria-label", "Next month")).attr("href"), "/activity/calendar?month=2027-01"; got != want {
		t.Errorf("Next links to %q, want %q", got, want)
	}
}

func TestCalendar_TheMonthTwelveMonthsAheadHasNoNextLink(t *testing.T) {
	f := rosewoodCalendar(t)

	page := f.get(t, calendarPath+"?month=2027-09", nil)

	if got, want := monthTitle(page), "September 2027"; got != want {
		t.Errorf("the calendar shows %q, want %q", got, want)
	}
	if next := readHTML(page).first(isTag("a"), attrIs("aria-label", "Next month")); next != nil {
		t.Errorf("the last month has a Next link to %q", next.attr("href"))
	}
}

func TestCalendar_AnUnreadableOrTooLateMonthRendersTheCurrentMonth(t *testing.T) {
	f := rosewoodCalendar(t)

	for _, month := range []string{"2027-10", "2026-13", "September", ""} {
		t.Run(month, func(t *testing.T) {
			page := f.get(t, calendarPath+"?month="+month, nil)

			if got, want := monthTitle(page), "September 2026"; got != want {
				t.Errorf("the calendar shows %q, want %q", got, want)
			}
		})
	}
}

func TestCalendar_TheCurrentMonthIsTheOneInTheReadersTimezone(t *testing.T) {
	f := rosewoodCalendar(t)
	// 23:30 UTC on 31 August is 00:30 on 1 September in London.
	f.handler.now = func() time.Time { return time.Date(2026, time.August, 31, 23, 30, 0, 0, time.UTC) }

	page := f.get(t, calendarPath, nil)

	if got, want := monthTitle(page), "September 2026"; got != want {
		t.Errorf("the calendar shows %q, want %q", got, want)
	}
}

func TestCalendar_ADayOutsideTheMonthOpensNoSheet(t *testing.T) {
	f := rosewoodCalendar(t)

	page := f.get(t, calendarPath+"?month=2026-09&day=2026-10-01", nil)

	if sheet := readHTML(page).byID(sheetID); sheet.tag == "dialog" {
		t.Errorf("the page has a sheet open:\n%s", sheet.text())
	}
}

func TestCalendar_AnotherGardensCareIsNotCounted(t *testing.T) {
	f := rosewoodCalendar(t)
	insertGarden(t, f.tx, fairviewID, "Fairview", store.CareType{ID: fairviewWaterID, Name: "Water", Slug: "water"})
	f.exec(t, "INSERT INTO plant (id, garden_id, nickname) VALUES ($1, $2, 'Hedge')", fairviewPlantID, fairviewID)
	f.exec(t, "INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done) VALUES ($1, $2, $3, $4, $5, $5, true)",
		fairviewID, fairviewPlantID, fairviewWaterID, readerID, day(time.August, 22))

	page := f.get(t, calendarPath+"?month=2026-08", nil)

	if got, want := dayLinkName(page, at(time.August, 22, 0, 0)), "Saturday 22 August, 1 logged"; got != want {
		t.Errorf("the link on 22 August is named %q, want %q", got, want)
	}
}

func TestCalendar_NextSwapsTheMonthAloneAndAnnouncesIt(t *testing.T) {
	f := rosewoodCalendar(t)

	body := f.get(t, calendarPath+"?month=2026-10", map[string]string{"HX-Request": "true", "HX-Target": calendarID})

	doc := readHTML(body)
	if doc.byID(calendarID) == nil {
		t.Fatalf("the swap has no element with the id %q:\n%s", calendarID, body)
	}
	if h1 := doc.first(isTag("h1")); h1 != nil {
		t.Errorf("the swap renders the page's heading %q", h1.text())
	}
	if got, want := outOfBandStatus(body).text(), "October 2026."; got != want {
		t.Errorf("the swap announces %q, want %q", got, want)
	}
}

func TestCalendar_ADaysLinkSwapsTheSheetAlone(t *testing.T) {
	f := rosewoodCalendar(t)

	body := f.get(t, calendarPath+"?month=2026-09&day=2026-09-03", map[string]string{"HX-Request": "true", "HX-Target": sheetID})

	doc := readHTML(body)
	if sheet := doc.byID(sheetID); sheet == nil || sheet.tag != "dialog" {
		t.Fatalf("the swap has no open sheet:\n%s", body)
	}
	if doc.byID(calendarID) != nil {
		t.Errorf("the swap renders the month as well as the sheet")
	}
}

func TestCalendar_CareLoggedAfterMidnightInTheReadersTimezoneIsOnThatDay(t *testing.T) {
	f := rosewoodCalendar(t)
	// 00:30 on 1 September in London is 23:30 on 31 August in UTC.
	f.exec(t, "INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done) VALUES ($1, $2, $3, $4, $5, $5, true)",
		rosewoodID, spikeID, waterID, readerID, at(time.September, 1, 0, 30))

	september := f.get(t, calendarPath+"?month=2026-09", nil)
	august := f.get(t, calendarPath+"?month=2026-08", nil)

	if got, want := dayLinkName(september, at(time.September, 1, 0, 0)), "Tuesday 1 September, 1 logged"; got != want {
		t.Errorf("the link on 1 September is named %q, want %q", got, want)
	}
	if got := dayLinkName(august, at(time.August, 31, 0, 0)); got != "" {
		t.Errorf("31 August is a link named %q, want no link", got)
	}
}

func TestCalendar_ADayShowsThreePlantsAndCountsTheRest(t *testing.T) {
	f := rosewoodCalendar(t)
	// A weekly feed for Trail Mix, first due today. With Big Fella's overdue
	// watering and Doris's and Nigel's, today has four cares.
	f.exec(t, "INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit, set_at) VALUES ($1, $2, $3, 1, 'week', $4)",
		rosewoodID, trailMixID, feedID, day(time.August, 27))

	page := f.get(t, calendarPath, nil)

	label := "Thursday 3 September, 4 due"
	today := readHTML(page).first(isTag("a"), attrIs("aria-label", label))
	if got, want := today.text(), "3 Big Fella Doris Nigel +1"; got != want {
		t.Errorf("the link named %q reads %q, want %q", label, got, want)
	}
}

func TestCalendar_TodayOnAnotherMonthLinksToTheCurrentMonth(t *testing.T) {
	f := rosewoodCalendar(t)

	page := f.get(t, calendarPath+"?month=2026-12", nil)

	today := readHTML(page).first(isTag("a"), textIs("Today"), attrIs("hx-target", "#"+calendarID))
	if got, want := today.attr("href"), "/activity/calendar"; got != want {
		t.Errorf("Today links to %q, want %q", got, want)
	}
}

var (
	calendarJoID           = uuid.MustParse("00000000-0000-7000-8000-000000000501")
	calendarClareID        = uuid.MustParse("00000000-0000-7000-8000-000000000502")
	calendarSamSittingID   = uuid.MustParse("00000000-0000-7000-8000-000000000503")
	calendarJoSittingID    = uuid.MustParse("00000000-0000-7000-8000-000000000504")
	calendarClareSittingID = uuid.MustParse("00000000-0000-7000-8000-000000000505")
)

// calendarSitter is a person with a sitter's membership in a garden.
type calendarSitter struct {
	userID, membershipID uuid.UUID
	name                 string
	// timezone is Europe/London, the reader's, when empty.
	timezone string
	// from is when the membership was made. until is when it ends, or nil for
	// a membership with no end date.
	from  time.Time
	until *time.Time
}

func (f *calendarFixture) addSitter(t *testing.T, gardenID uuid.UUID, s calendarSitter) {
	t.Helper()
	timezone := cmp.Or(s.timezone, "Europe/London")
	f.exec(t, "INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, $2, $3, $4)", s.userID, s.name, strings.ToLower(s.name), timezone)
	f.exec(t, "INSERT INTO membership (id, garden_id, user_id, role, created_at, expires_at, digest_hour) VALUES ($1, $2, $3, 'sitter', $4, $5, 8)",
		s.membershipID, gardenID, s.userID, s.from, s.until)
}

// asSitter makes the fixture's requests come from the sitter whose membership
// is membershipID. Its only capability is care.log.
func (f *calendarFixture) asSitter(userID, membershipID uuid.UUID) {
	f.principal = auth.Principal{
		User:         store.AppUser{ID: userID, Timezone: "Europe/London"},
		Garden:       f.principal.Garden,
		Membership:   store.Membership{ID: membershipID, GardenID: rosewoodID, UserID: userID, Role: "sitter", DigestHour: 8},
		Capabilities: auth.Capabilities{auth.CareLog: true},
	}
}

// samAndJo gives the garden two sitters whose days overlap: Sam from 25
// August to 9 September, and Jo from 5 to 19 September. Each end date is
// midnight on the day after, as an invite or People sets it.
func (f *calendarFixture) samAndJo(t *testing.T) {
	t.Helper()
	f.addSitter(t, rosewoodID, calendarSitter{userID: rosewoodSamID, membershipID: calendarSamSittingID, name: "Sam", from: day(time.August, 25), until: ptr(at(time.September, 10, 0, 0))})
	f.addSitter(t, rosewoodID, calendarSitter{userID: calendarJoID, membershipID: calendarJoSittingID, name: "Jo", from: day(time.September, 5), until: ptr(at(time.September, 20, 0, 0))})
}

// dayLabels returns the accessible name of every day link in the month.
func dayLabels(page string) []string {
	var labels []string
	for _, a := range readHTML(page).byID(calendarID).all(isTag("a")) {
		if strings.HasPrefix(a.attr("href"), calendarPath+"?") && a.attr("aria-label") != "" {
			labels = append(labels, a.attr("aria-label"))
		}
	}
	return labels
}

// sheetSitting returns the text of each row under Sitting in the open day's
// sheet. They are the rows without a link.
func sheetSitting(t *testing.T, page string) []string {
	t.Helper()

	sheet := readHTML(page).byID(sheetID)
	if sheet == nil || sheet.tag != "dialog" {
		t.Fatalf("the page has no open sheet:\n%s", text(page))
	}
	var rows []string
	for _, li := range sheet.all(isTag("li")) {
		if li.first(isTag("a")) == nil {
			rows = append(rows, li.text())
		}
	}
	return rows
}

func TestCalendar_ADayLinkNamesEachSitterWhoseAccessCoversIt(t *testing.T) {
	f := rosewoodCalendar(t)
	f.samAndJo(t)
	f.principal.Capabilities[auth.SittingView] = true

	page := f.get(t, calendarPath, nil)

	for d, want := range map[int]string{
		// No care on 2 September, so the link exists for the sitting alone.
		2: "Wednesday 2 September, Sam sitting",
		3: "Thursday 3 September, 3 due, Sam sitting",
		7: "Monday 7 September, 2 due, Sam and Jo sitting",
		9: "Wednesday 9 September, Sam and Jo sitting",
		// Sam's access ends as 10 September begins.
		10: "Thursday 10 September, 1 due, Jo sitting",
		19: "Saturday 19 September, 2 due, Jo sitting",
		20: "",
	} {
		if got := dayLinkName(page, at(time.September, d, 0, 0)); got != want {
			t.Errorf("the link on %d September is named %q, want %q", d, got, want)
		}
	}
}

func TestCalendar_ADaysSheetNamesEachSitterWhoseAccessCoversIt(t *testing.T) {
	f := rosewoodCalendar(t)
	f.samAndJo(t)
	f.principal.Capabilities[auth.SittingView] = true

	page := f.get(t, calendarPath+"?month=2026-09&day=2026-09-07", nil)

	if got, want := sheetSitting(t, page), []string{"Sam until 10 Sep", "Jo until 20 Sep"}; !slices.Equal(got, want) {
		t.Errorf("the sheet's Sitting rows are %q, want %q", got, want)
	}
}

func TestCalendar_ASittersLastDayIsTheDayBeforeTheirEndDateInTheirOwnTimezone(t *testing.T) {
	for _, c := range []struct {
		name, reader, sitter string
	}{
		// Midnight in London on 20 September is 19:00 on the 19th in New York.
		{name: "a New York reader and a London sitter", reader: "America/New_York", sitter: "Europe/London"},
		// Midnight in New York on 20 September is 05:00 on the 20th in London.
		{name: "a London reader and a New York sitter", reader: "Europe/London", sitter: "America/New_York"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := rosewoodCalendar(t)
			sitterLoc, err := time.LoadLocation(c.sitter)
			if err != nil {
				t.Fatal(err)
			}
			f.addSitter(t, rosewoodID, calendarSitter{
				userID: calendarJoID, membershipID: calendarJoSittingID, name: "Jo", timezone: c.sitter,
				from:  time.Date(2026, time.September, 5, 12, 0, 0, 0, sitterLoc),
				until: ptr(time.Date(2026, time.September, 20, 0, 0, 0, 0, sitterLoc)),
			})
			f.principal.Capabilities[auth.SittingView] = true
			f.principal.User.Timezone = c.reader
			readerLoc, err := time.LoadLocation(c.reader)
			if err != nil {
				t.Fatal(err)
			}

			page := f.get(t, calendarPath+"?month=2026-09&day=2026-09-19", nil)

			if got := dayLinkName(page, time.Date(2026, time.September, 19, 0, 0, 0, 0, readerLoc)); !strings.HasSuffix(got, "Jo sitting") {
				t.Errorf("the link on 19 September is named %q, want it to end \"Jo sitting\"", got)
			}
			if got := dayLinkName(page, time.Date(2026, time.September, 20, 0, 0, 0, 0, readerLoc)); strings.Contains(got, "sitting") {
				t.Errorf("the link on 20 September is named %q, want no sitting", got)
			}
			if got, want := sheetSitting(t, page), []string{"Jo until 20 Sep"}; !slices.Equal(got, want) {
				t.Errorf("the sheet's Sitting rows are %q, want %q", got, want)
			}
		})
	}
}

// calendarDayOn returns the grid's cell for day d of the month.
func calendarDayOn(t *testing.T, page calendarPage, d int) calendarDay {
	t.Helper()
	for _, week := range page.Weeks {
		for _, day := range week {
			if day.Number == d {
				return day
			}
		}
	}
	t.Fatalf("the grid has no day %d", d)
	return calendarDay{}
}

func TestCalendar_TwoSittersOnOneDayHaveTwoBandsInDifferentColours(t *testing.T) {
	now := thursday.In(london())
	sittings := []sitting{
		{Name: "Sam", First: at(time.August, 25, 0, 0), Last: at(time.September, 9, 0, 0)},
		{Name: "Jo", First: at(time.September, 5, 0, 0), Last: at(time.September, 19, 0, 0)},
	}
	owner := auth.Principal{Capabilities: auth.Capabilities{auth.SittingView: true}}

	page := newCalendarPage(owner, calendarQuery{month: startOfMonth(now)}, nil, nil, sittings, nil, now)

	for d, want := range map[int][]calendarBand{
		4:  {{Covered: true, Colour: 1}, {}},
		5:  {{Covered: true, Colour: 1}, {Covered: true, Colour: 2, Starts: true}},
		7:  {{Covered: true, Colour: 1}, {Covered: true, Colour: 2}},
		9:  {{Covered: true, Colour: 1, Ends: true}, {Covered: true, Colour: 2}},
		10: {{}, {Covered: true, Colour: 2}},
		19: {{}, {Covered: true, Colour: 2, Ends: true}},
		20: {{}, {}},
	} {
		day := calendarDayOn(t, page, d)
		if !slices.Equal(day.Bands, want) {
			t.Errorf("the bands on %d September are %+v, want %+v", d, day.Bands, want)
		}
		if day.Sitting {
			t.Errorf("%d September is marked as the reader's own sitting", d)
		}
	}
}

func TestCalendar_ASitterHasTheirOwnDaysMarkedAndNoBands(t *testing.T) {
	now := thursday.In(london())
	sittings := []sitting{{Name: "You", You: true, First: at(time.August, 25, 0, 0), Last: at(time.September, 9, 0, 0)}}
	sitter := auth.Principal{Capabilities: auth.Capabilities{auth.CareLog: true}}

	page := newCalendarPage(sitter, calendarQuery{month: startOfMonth(now)}, nil, nil, sittings, nil, now)

	for d, want := range map[int]bool{1: true, 9: true, 10: false} {
		day := calendarDayOn(t, page, d)
		if day.Sitting != want {
			t.Errorf("%d September is marked as the reader's own sitting: %t, want %t", d, day.Sitting, want)
		}
		if len(day.Bands) != 0 {
			t.Errorf("the sitter's calendar has bands on %d September: %+v", d, day.Bands)
		}
	}
}

func TestCalendar_ASitterSeesTheirOwnDaysAndNoOtherSitters(t *testing.T) {
	f := rosewoodCalendar(t)
	f.samAndJo(t)
	f.asSitter(rosewoodSamID, calendarSamSittingID)

	page := f.get(t, calendarPath+"?month=2026-09&day=2026-09-07", nil)

	if got, want := dayLinkName(page, at(time.September, 7, 0, 0)), "Monday 7 September, 2 due, you’re sitting"; got != want {
		t.Errorf("the link on 7 September is named %q, want %q", got, want)
	}
	if got, want := dayLinkName(page, at(time.September, 10, 0, 0)), "Thursday 10 September, 1 due"; got != want {
		t.Errorf("the link on 10 September is named %q, want %q", got, want)
	}
	if got, want := sheetSitting(t, page), []string{"You until 10 Sep"}; !slices.Equal(got, want) {
		t.Errorf("the sheet's Sitting rows are %q, want %q", got, want)
	}
	if strings.Contains(text(page), "Jo") {
		t.Errorf("the page names Jo, whose days another sitter may not see:\n%s", text(page))
	}
}

func TestCalendar_ASitterWithNoEndDateIsNotNamedOnAnyDay(t *testing.T) {
	f := rosewoodCalendar(t)
	f.addSitter(t, rosewoodID, calendarSitter{userID: calendarJoID, membershipID: calendarJoSittingID, name: "Jo", from: day(time.August, 25)})
	f.principal.Capabilities[auth.SittingView] = true

	for _, label := range dayLabels(f.get(t, calendarPath, nil)) {
		if strings.Contains(label, "sitting") {
			t.Errorf("the owner's calendar has a day named %q", label)
		}
	}

	f.asSitter(calendarJoID, calendarJoSittingID)
	for _, label := range dayLabels(f.get(t, calendarPath, nil)) {
		if strings.Contains(label, "sitting") {
			t.Errorf("the sitter's own calendar has a day named %q", label)
		}
	}
}

func TestCalendar_AnEndedSittingIsShownOnItsMonthAndNotTheCurrentOne(t *testing.T) {
	f := rosewoodCalendar(t)
	f.addSitter(t, rosewoodID, calendarSitter{userID: calendarClareID, membershipID: calendarClareSittingID, name: "Clare", from: day(time.August, 1), until: ptr(at(time.August, 15, 0, 0))})
	f.principal.Capabilities[auth.SittingView] = true

	august := f.get(t, calendarPath+"?month=2026-08&day=2026-08-14", nil)

	if got, want := dayLinkName(august, at(time.August, 14, 0, 0)), "Friday 14 August, Clare sitting"; got != want {
		t.Errorf("the link on 14 August is named %q, want %q", got, want)
	}
	if got, want := dayLinkName(august, at(time.August, 15, 0, 0)), ""; got != want {
		t.Errorf("the link on 15 August is named %q, want %q", got, want)
	}
	if got, want := sheetSitting(t, august), []string{"Clare Access ended 15 Aug"}; !slices.Equal(got, want) {
		t.Errorf("the sheet's Sitting rows are %q, want %q", got, want)
	}
	for _, label := range dayLabels(f.get(t, calendarPath, nil)) {
		if strings.Contains(label, "sitting") {
			t.Errorf("September has a day named %q", label)
		}
	}
}

func TestCalendar_AnotherGardensSitterIsNotNamedOnAnyDay(t *testing.T) {
	f := rosewoodCalendar(t)
	insertGarden(t, f.tx, fairviewID, "Fairview", store.CareType{ID: fairviewWaterID, Name: "Water", Slug: "water"})
	f.addSitter(t, fairviewID, calendarSitter{userID: calendarJoID, membershipID: calendarJoSittingID, name: "Jo", from: day(time.August, 25), until: ptr(at(time.September, 20, 0, 0))})
	f.principal.Capabilities[auth.SittingView] = true

	for _, label := range dayLabels(f.get(t, calendarPath, nil)) {
		if strings.Contains(label, "sitting") {
			t.Errorf("the calendar has a day named %q", label)
		}
	}
}
