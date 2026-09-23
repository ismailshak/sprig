package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// choresGarden returns the seeded Rosewood fixture, the handler, and a
// principal holding an API token. The account that created the token is in
// Europe/London, so the dates in the response are London's.
func choresGarden(t *testing.T) (*todayFixture, *chores, auth.Principal) {
	t.Helper()

	f := rosewood(t)
	handler := &chores{logger: testLogger, queries: f.handler.queries, now: f.handler.now}
	principal := auth.Principal{
		Garden:   f.principal.Garden,
		APIToken: &store.APIToken{Name: "The kitchen display", CreatedBy: readerID},
	}
	return f, handler, principal
}

// poll sends GET /api/chores to the handler and fails the test unless it
// returns 200. It returns the recorder so a test can read the headers as well
// as the body.
func poll(t *testing.T, handler *chores, principal auth.Principal) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, principal)
	rec := httptest.NewRecorder()
	handler.show(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, choresPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	return rec
}

func decodeChores(t *testing.T, body string) choresResponse {
	t.Helper()

	var response choresResponse
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		t.Fatalf("the body is not the JSON the display reads: %v\n%s", err, body)
	}
	return response
}

func TestChores_TheOverdueAndDueTodayCaresAreTheChoresAndTheRestAreUpcoming(t *testing.T) {
	_, handler, principal := choresGarden(t)

	body := poll(t, handler, principal).Body.String()

	// The whole body is compared as text, because a device's template reads
	// these exact field names.
	want := `{"garden":"Rosewood","date":"2026-09-03","chores":[` +
		`{"plant":"Big Fella","location":"Living room","care":"Water","due":"2026-09-01","late":"2 days late"},` +
		`{"plant":"Doris","location":"Bedroom","care":"Water","due":"2026-09-03","late":""},` +
		`{"plant":"Nigel","location":"Bathroom","care":"Water","due":"2026-09-03","late":""}],` +
		`"upcoming":[` +
		`{"plant":"Trail Mix","location":"Kitchen","care":"Water","due":"2026-09-04","when":"tomorrow"},` +
		`{"plant":"Opuntia microdasys","location":"Windowsill","care":"Water","due":"2026-09-07","when":"Monday"},` +
		`{"plant":"Sprout","location":"","care":"Water","due":"2026-09-08","when":"Tuesday"},` +
		`{"plant":"Nigel","location":"Bathroom","care":"Feed","due":"2026-09-10","when":"in 7 days"},` +
		`{"plant":"Spike","location":"Windowsill","care":"Water","due":"2026-09-15","when":"in 12 days"}]}`
	if body != want {
		t.Errorf("the body reads\n%s\nwant\n%s", body, want)
	}
}

func TestChores_CaresDueTheSameDayAreListedByPlantName(t *testing.T) {
	f, handler, principal := choresGarden(t)
	// Spike's watering is due on the 15th. These intervals move Big Fella's
	// and Doris's there too. The schedules arrive ordered by location, so Doris
	// in the Bedroom comes before Big Fella in the Living room, and a sort on
	// the day alone would leave her first.
	f.exec(t, "UPDATE care_schedule SET interval_count = 24 WHERE plant_id = $1", bigFellaID)
	f.exec(t, "UPDATE care_schedule SET interval_count = 33 WHERE plant_id = $1", dorisID)

	got := decodeChores(t, poll(t, handler, principal).Body.String())

	var names []string
	for _, u := range got.Upcoming[len(got.Upcoming)-3:] {
		names = append(names, u.Plant)
	}
	if want := []string{"Big Fella", "Doris", "Spike"}; !slices.Equal(names, want) {
		t.Errorf("the three cares due on the 15th read %v, want %v", names, want)
	}
}

func TestChores_TheResponseIsJSONThatNothingMayCache(t *testing.T) {
	_, handler, principal := choresGarden(t)

	rec := poll(t, handler, principal)

	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store, so nothing between the display and the app keeps a copy", got)
	}
}

func TestChores_APlantWithTwoCaresDueIsListedOncePerCareWithTheOverdueOneFirst(t *testing.T) {
	f, handler, principal := choresGarden(t)
	// Nigel's feed is every three weeks, and this moves its start to 22 days
	// ago, so the feed is a day overdue while his watering is due today.
	f.exec(t, "UPDATE care_schedule SET set_at = $1 WHERE plant_id = $2 AND care_type_id = $3", day(time.August, 12), nigelID, feedID)

	got := decodeChores(t, poll(t, handler, principal).Body.String())

	var nigel []chore
	for _, c := range got.Chores {
		if c.Plant == "Nigel" {
			nigel = append(nigel, c)
		}
	}
	want := []chore{
		{Plant: "Nigel", Location: "Bathroom", Care: "Feed", Due: "2026-09-02", Late: "1 day late"},
		{Plant: "Nigel", Location: "Bathroom", Care: "Water", Due: "2026-09-03"},
	}
	if !reflect.DeepEqual(nigel, want) {
		t.Errorf("Nigel's chores read\n%+v\nwant\n%+v", nigel, want)
	}
	if got.Chores[0].Plant != "Big Fella" || got.Chores[1].Plant != "Nigel" {
		t.Errorf("the list opens with %s then %s, and the overdue plants come first, most overdue at the top", got.Chores[0].Plant, got.Chores[1].Plant)
	}
}

func TestChores_ACareAnchoredToAMonthIsDatedTheFirstOfItAndReadsOverdueSinceTheMonth(t *testing.T) {
	f, handler, principal := choresGarden(t)
	// Spike's feed is anchored to August with no day in it. Such a schedule
	// is overdue only once its month has ended, so on 3 September it is.
	f.exec(t, `INSERT INTO care_schedule (garden_id, plant_id, care_type_id, anchor_date, anchor_precision, set_at)
		VALUES ($1, $2, $3, '2026-08-01', 'month', $4)`, rosewoodID, spikeID, feedID, day(time.July, 1))

	got := decodeChores(t, poll(t, handler, principal).Body.String())

	want := chore{Plant: "Spike", Location: "Windowsill", Care: "Feed", Due: "2026-08-01", Late: "overdue since August"}
	if got.Chores[0] != want {
		t.Errorf("the list opens with\n%+v\nwant\n%+v", got.Chores[0], want)
	}
}

func TestChores_APlantInAnotherGardenIsNotInTheList(t *testing.T) {
	f, handler, principal := choresGarden(t)
	// Fairview's Hedge is further past due than any of Rosewood's plants, so
	// a query that stopped filtering on the garden would put it at the top.
	f.exec(t, "INSERT INTO garden (id, name) VALUES ($1, 'Fairview')", fairviewID)
	f.exec(t, "INSERT INTO plant (id, garden_id, nickname) VALUES ($1, $2, 'Hedge')", fairviewPlantID, fairviewID)
	f.exec(t, "INSERT INTO care_type (id, garden_id, name, slug) VALUES ($1, $2, 'Water', 'water')", fairviewWaterID, fairviewID)
	f.exec(t, "INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit, set_at) VALUES ($1, $2, $3, 7, 'day', $4)",
		fairviewID, fairviewPlantID, fairviewWaterID, day(time.July, 1))

	got := decodeChores(t, poll(t, handler, principal).Body.String())

	for _, c := range got.Chores {
		if c.Plant == "Hedge" {
			t.Error("Fairview's Hedge is in the list a Rosewood token reads")
		}
	}
	if len(got.Chores) != 3 {
		t.Errorf("the list holds %d chores, want Rosewood's 3:\n%+v", len(got.Chores), got.Chores)
	}
}

func TestChores_AGardenWithNothingScheduledSendsEmptyListsRatherThanNull(t *testing.T) {
	f, handler, principal := choresGarden(t)
	f.exec(t, "DELETE FROM care_schedule WHERE garden_id = $1", rosewoodID)

	body := poll(t, handler, principal).Body.String()

	want := `{"garden":"Rosewood","date":"2026-09-03","chores":[],"upcoming":[]}`
	if body != want {
		t.Errorf("the body reads\n%s\nwant\n%s", body, want)
	}
}

// chorePolls returns a function that sends GET /api/chores with the given
// bearer token, and the number of token lookups those requests have cost.
// Every request comes from the same address, so only the token can separate
// them.
func chorePolls(t *testing.T) (func(token string) *httptest.ResponseRecorder, *int) {
	t.Helper()

	queries := routeQueries(t)
	lookups := 0
	tokens := ResolverFunc(func(context.Context, time.Time, string) (auth.Principal, error) {
		lookups++
		return tokenPrincipal(), nil
	})
	deps := testDependencies(t)
	deps.Tokens = tokens
	deps.Queries = queries
	handler := New(deps)

	return func(token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, choresPath, nil)
		req.RemoteAddr = "203.0.113.7:4444"
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}, &lookups
}

func TestChores_ASeventhPollInAMinuteIsRefusedBeforeTheTokenIsLookedUp(t *testing.T) {
	pollWith, lookups := chorePolls(t)
	for i := range 6 {
		if rec := pollWith("kitchen"); rec.Code != http.StatusOK {
			t.Fatalf("poll %d: status = %d, want %d:\n%s", i+1, rec.Code, http.StatusOK, rec.Body.String())
		}
	}
	if *lookups != 6 {
		t.Fatalf("six polls looked the token up %d times", *lookups)
	}

	rec := pollWith("kitchen")

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the seventh poll in a minute: status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("the refusal sets no Retry-After")
	}
	if *lookups != 6 {
		t.Errorf("the refused poll looked the token up, and the limit is there so it costs no query")
	}
}

func TestChores_TwoTokensFromOneAddressGetSeparateBudgets(t *testing.T) {
	pollWith, _ := chorePolls(t)
	for range 7 {
		pollWith("kitchen")
	}

	rec := pollWith("hallway")

	if rec.Code != http.StatusOK {
		t.Errorf("the hallway display got %d once the kitchen display had spent its budget from the same address, want %d", rec.Code, http.StatusOK)
	}
}
