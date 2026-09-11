package push

import (
	"log/slog"
	"net/http"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/pgtest"
	"github.com/ismailshak/sprig/internal/schedule"
	"github.com/ismailshak/sprig/internal/store"
)

func line(care, plant string, state schedule.State) schedule.Line {
	return schedule.Line{
		CareType: store.CareType{Name: care},
		Plant:    store.Plant{Nickname: &plant},
		State:    state,
	}
}

func TestDigestOf_ListsWhatIsOverdueOrDueTodayGroupedByCare(t *testing.T) {
	lines := []schedule.Line{
		line("Water", "Big Fella", schedule.Overdue),
		line("Feed", "Sprout", schedule.DueToday),
		line("Water", "Doris", schedule.DueToday),
		line("Water", "Nigel", schedule.Overdue),
		line("Water", "Spike", schedule.Upcoming),
		line("Feed", "Fern", schedule.Dormant),
		line("Feed", "Opuntia", schedule.Spent),
	}

	got, items := digestOf("Rosewood", lines, "https://sprig.example.com/")

	want := Notification{Title: "Rosewood", Body: "Water Big Fella, Doris and Nigel. Feed Sprout.", URL: "https://sprig.example.com/"}
	if got != want {
		t.Errorf("digestOf = %+v, want %+v", got, want)
	}
	if items != 4 {
		t.Errorf("items = %d, want 4", items)
	}
}

func TestDigestOf_NothingDueIsNoDigest(t *testing.T) {
	lines := []schedule.Line{line("Water", "Spike", schedule.Upcoming)}

	_, items := digestOf("Rosewood", lines, "https://sprig.example.com/")

	if items != 0 {
		t.Errorf("items = %d, want 0", items)
	}
}

func TestDigestOf_ACareTypeNamedInLowerCaseStartsItsSentenceCapitalised(t *testing.T) {
	lines := []schedule.Line{line("dust", "Nigel", schedule.DueToday)}

	got, _ := digestOf("Rosewood", lines, "https://sprig.example.com/")

	if want := "Dust Nigel."; got.Body != want {
		t.Errorf("the body is %q, want %q", got.Body, want)
	}
}

var (
	rosewoodID = uuid.MustParse("00000000-0000-7000-8000-000000000201")
	ellieID    = uuid.MustParse("00000000-0000-7000-8000-000000000202")
	samID      = uuid.MustParse("00000000-0000-7000-8000-000000000203")
	robinID    = uuid.MustParse("00000000-0000-7000-8000-000000000204")
	waterID    = uuid.MustParse("00000000-0000-7000-8000-000000000205")
	bigFellaID = uuid.MustParse("00000000-0000-7000-8000-000000000206")
	dorisID    = uuid.MustParse("00000000-0000-7000-8000-000000000207")
	spikeID    = uuid.MustParse("00000000-0000-7000-8000-000000000208")
	sproutID   = uuid.MustParse("00000000-0000-7000-8000-000000000209")

	ellieMembershipID = uuid.MustParse("00000000-0000-7000-8000-000000000211")
	samMembershipID   = uuid.MustParse("00000000-0000-7000-8000-000000000212")
	robinMembershipID = uuid.MustParse("00000000-0000-7000-8000-000000000213")

	fairviewID              = uuid.MustParse("00000000-0000-7000-8000-000000000221")
	fairviewWaterID         = uuid.MustParse("00000000-0000-7000-8000-000000000222")
	fernID                  = uuid.MustParse("00000000-0000-7000-8000-000000000223")
	ellieFairviewMembership = uuid.MustParse("00000000-0000-7000-8000-000000000224")
)

// noon is 12:00 UTC on Thursday 3 September 2026: 13:00 in London and 08:00
// in New York. Ellie's hour is 13 and Sam's is 8, so both digests are due at
// this one instant.
var noon = utc(2026, time.September, 3, 12, 0, 0)

// seedRosewood writes one garden with three members and three watering
// schedules: Big Fella overdue, Doris due today, and Spike due next week.
// Ellie has two subscribed browsers and Sam one. Robin has the digest on and
// no browser. Every subscription's endpoint is a path on service.
func seedRosewood(t *testing.T, db store.DBTX, service *pushService) {
	t.Helper()

	ctx := t.Context()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seeding: %v\n%s", err, sql)
		}
	}

	exec("INSERT INTO garden (id, name) VALUES ($1, 'Rosewood')", rosewoodID)
	exec(`INSERT INTO app_user (id, display_name, handle, timezone) VALUES
		($1, 'Ellie', 'ellie', 'Europe/London'), ($2, 'Sam', 'sam', 'America/New_York'), ($3, 'Robin', 'robin', 'Europe/London')`,
		ellieID, samID, robinID)
	exec(`INSERT INTO membership (id, garden_id, user_id, role, digest_hour) VALUES
		($1, $4, $5, 'owner', 13), ($2, $4, $6, 'member', 8), ($3, $4, $7, 'member', 13)`,
		ellieMembershipID, samMembershipID, robinMembershipID, rosewoodID, ellieID, samID, robinID)
	exec(`INSERT INTO notification_preference (membership_id, kind, enabled) VALUES
		($1, 'digest', true), ($1, 'activity', false), ($2, 'digest', true), ($2, 'activity', false), ($3, 'digest', true), ($3, 'activity', false)`,
		ellieMembershipID, samMembershipID, robinMembershipID)
	exec("INSERT INTO care_type (id, garden_id, name, slug) VALUES ($1, $2, 'Water', 'water')", waterID, rosewoodID)

	plants := []struct {
		id          uuid.UUID
		nickname    string
		lastWatered time.Time
	}{
		{bigFellaID, "Big Fella", utc(2026, time.August, 22, 12, 0, 0)},
		{dorisID, "Doris", utc(2026, time.August, 24, 12, 0, 0)},
		{spikeID, "Spike", utc(2026, time.August, 30, 12, 0, 0)},
	}
	for _, p := range plants {
		exec("INSERT INTO plant (id, garden_id, nickname) VALUES ($1, $2, $3)", p.id, rosewoodID, p.nickname)
		exec("INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit, set_at) VALUES ($1, $2, $3, 10, 'day', $4)",
			rosewoodID, p.id, waterID, p.lastWatered.AddDate(0, 0, -10))
		exec("INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done) VALUES ($1, $2, $3, $4, $5, $5, true)",
			rosewoodID, p.id, waterID, ellieID, p.lastWatered)
	}

	for _, b := range []struct {
		owner uuid.UUID
		path  string
	}{{ellieID, "/ellie-phone"}, {ellieID, "/ellie-mac"}, {samID, "/sam-phone"}} {
		subscription := browserSubscription(t, service.URL+b.path)
		exec("INSERT INTO push_subscription (user_id, endpoint, p256dh_key, auth_key, user_agent) VALUES ($1, $2, $3, $4, $5)",
			b.owner, subscription.Endpoint, subscription.P256dhKey, subscription.AuthKey, "Mozilla/5.0 (iPhone) Safari")
	}
}

// seedFairview writes a second garden with Ellie as its only member, on the
// hour she has on Rosewood, and one plant overdue for watering. She keeps the
// browsers she subscribed on Rosewood, because a subscription belongs to the
// account.
func seedFairview(t *testing.T, db store.DBTX) {
	t.Helper()

	ctx := t.Context()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seeding: %v\n%s", err, sql)
		}
	}

	exec("INSERT INTO garden (id, name) VALUES ($1, 'Fairview')", fairviewID)
	exec("INSERT INTO membership (id, garden_id, user_id, role, digest_hour) VALUES ($1, $2, $3, 'owner', 13)",
		ellieFairviewMembership, fairviewID, ellieID)
	exec(`INSERT INTO notification_preference (membership_id, kind, enabled) VALUES ($1, 'digest', true), ($1, 'activity', false)`,
		ellieFairviewMembership)
	exec("INSERT INTO care_type (id, garden_id, name, slug) VALUES ($1, $2, 'Water', 'water')", fairviewWaterID, fairviewID)
	exec("INSERT INTO plant (id, garden_id, nickname) VALUES ($1, $2, 'Fern')", fernID, fairviewID)

	watered := utc(2026, time.August, 22, 12, 0, 0)
	exec("INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit, set_at) VALUES ($1, $2, $3, 10, 'day', $4)",
		fairviewID, fernID, fairviewWaterID, watered.AddDate(0, 0, -10))
	exec("INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done) VALUES ($1, $2, $3, $4, $5, $5, true)",
		fairviewID, fernID, fairviewWaterID, ellieID, watered)
}

func newDigest(t *testing.T, db store.DBTX, service *pushService, now func() time.Time) *Digest {
	t.Helper()

	digest := NewDigest(slog.New(slog.DiscardHandler), store.New(db), NewSender(testKeys(t), service.Client()), "https://sprig.example.com", "/?from=digest", "/remind-again")
	digest.now = now
	return digest
}

func fixed(at time.Time) func() time.Time {
	return func() time.Time { return at }
}

// ledger returns every digest in the send ledger as "handle garden date",
// sorted.
func ledger(t *testing.T, db store.DBTX) []string {
	t.Helper()

	rows, err := db.Query(t.Context(), `SELECT app_user.handle || ' ' || garden.name || ' ' || notification_send.send_key
		FROM notification_send
		JOIN membership ON membership.id = notification_send.membership_id
		JOIN app_user ON app_user.id = membership.user_id
		JOIN garden ON garden.id = membership.garden_id
		WHERE notification_send.kind = 'digest'`)
	if err != nil {
		t.Fatalf("reading the ledger: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var entry string
		if err := rows.Scan(&entry); err != nil {
			t.Fatalf("reading the ledger: %v", err)
		}
		out = append(out, entry)
	}
	slices.Sort(out)
	return out
}

// browsers returns every subscription as "path sent" or "path unsent",
// sorted, where path is the endpoint's path on the test push service.
func browsers(t *testing.T, db store.DBTX) []string {
	t.Helper()

	rows, err := db.Query(t.Context(), `SELECT regexp_replace(endpoint, '^https?://[^/]+', '') || CASE WHEN last_sent_at IS NULL THEN ' unsent' ELSE ' sent' END
		FROM push_subscription`)
	if err != nil {
		t.Fatalf("reading the browsers: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var entry string
		if err := rows.Scan(&entry); err != nil {
			t.Fatalf("reading the browsers: %v", err)
		}
		out = append(out, entry)
	}
	slices.Sort(out)
	return out
}

func TestSendDue_SendsEachMemberOneDigestAtTheirHourInTheirTimezone(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	digest := newDigest(t, tx, service, fixed(noon.Add(30*time.Second)))

	next, err := digest.sendDue(t.Context())

	if err != nil {
		t.Fatalf("sendDue: %v", err)
	}
	if got, want := service.received(), []string{"/ellie-mac", "/ellie-phone", "/sam-phone"}; !slices.Equal(got, want) {
		t.Errorf("the push service received %q, want %q", got, want)
	}
	if got, want := ledger(t, tx), []string{"ellie Rosewood 2026-09-03", "sam Rosewood 2026-09-03"}; !slices.Equal(got, want) {
		t.Errorf("the ledger holds %q, want %q", got, want)
	}
	if got, want := browsers(t, tx), []string{"/ellie-mac sent", "/ellie-phone sent", "/sam-phone sent"}; !slices.Equal(got, want) {
		t.Errorf("the browsers are %q, want %q", got, want)
	}
	// Both hours fall at noon UTC again tomorrow.
	if want := noon.AddDate(0, 0, 1); !next.Equal(want) {
		t.Errorf("the next send is %s, want %s", next.UTC(), want)
	}
}

func TestSendDue_LookingAgainTheSameDaySendsNothingMore(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	digest := newDigest(t, tx, service, fixed(noon.Add(30*time.Second)))
	if _, err := digest.sendDue(t.Context()); err != nil {
		t.Fatalf("the first look: %v", err)
	}

	// A second job over the same database is what happens on a restart.
	restarted := newDigest(t, tx, service, fixed(noon.Add(20*time.Minute)))
	if _, err := restarted.sendDue(t.Context()); err != nil {
		t.Fatalf("the second look: %v", err)
	}

	if got := service.received(); len(got) != 3 {
		t.Errorf("the push service received %q, want the three from the first look and nothing more", got)
	}
}

func TestSendDue_AStartWithinAnHourOfTheHourSendsTheDigestAtOnce(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	digest := newDigest(t, tx, service, fixed(noon.Add(59*time.Minute)))

	if _, err := digest.sendDue(t.Context()); err != nil {
		t.Fatalf("sendDue: %v", err)
	}

	if got := service.received(); len(got) != 3 {
		t.Errorf("the push service received %q, want all three browsers", got)
	}
}

func TestSendDue_AStartAnHourOrMoreAfterTheHourDropsTheDigest(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	digest := newDigest(t, tx, service, fixed(noon.Add(time.Hour)))

	next, err := digest.sendDue(t.Context())

	if err != nil {
		t.Fatalf("sendDue: %v", err)
	}
	if got := service.received(); len(got) != 0 {
		t.Errorf("the push service received %q, want nothing", got)
	}
	if got := ledger(t, tx); len(got) != 0 {
		t.Errorf("the ledger holds %q, want nothing, so a day nobody was sent is not marked as sent", got)
	}
	if want := noon.AddDate(0, 0, 1); !next.Equal(want) {
		t.Errorf("the next send is %s, want tomorrow's %s", next.UTC(), want)
	}
}

func TestSendDue_ABrowserThePushServiceHasDroppedIsDeletedAndTheRestStillSent(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	service.statuses["/ellie-mac"] = http.StatusGone
	seedRosewood(t, tx, service)
	digest := newDigest(t, tx, service, fixed(noon))

	if _, err := digest.sendDue(t.Context()); err != nil {
		t.Fatalf("sendDue: %v", err)
	}

	if got, want := browsers(t, tx), []string{"/ellie-phone sent", "/sam-phone sent"}; !slices.Equal(got, want) {
		t.Errorf("the browsers are %q, want %q", got, want)
	}
	if got, want := ledger(t, tx), []string{"ellie Rosewood 2026-09-03", "sam Rosewood 2026-09-03"}; !slices.Equal(got, want) {
		t.Errorf("the ledger holds %q, want %q", got, want)
	}
}

func TestSendDue_APushServiceErrorKeepsTheBrowserAndDoesNotSendAgain(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	service.statuses["/ellie-mac"] = http.StatusBadGateway
	seedRosewood(t, tx, service)
	digest := newDigest(t, tx, service, fixed(noon))

	if _, err := digest.sendDue(t.Context()); err != nil {
		t.Fatalf("sendDue: %v", err)
	}
	if _, err := digest.sendDue(t.Context()); err != nil {
		t.Fatalf("the second look: %v", err)
	}

	if got, want := browsers(t, tx), []string{"/ellie-mac unsent", "/ellie-phone sent", "/sam-phone sent"}; !slices.Equal(got, want) {
		t.Errorf("the browsers are %q, want %q", got, want)
	}
	if got, want := service.received(), []string{"/ellie-mac", "/ellie-phone", "/sam-phone"}; !slices.Equal(got, want) {
		t.Errorf("the push service received %q, want %q with the refused one not tried again", got, want)
	}
}

func TestSendDue_ADayWithNothingDueSendsNothingAndStillWritesTheLedgerRow(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	// Everything was watered this morning.
	if _, err := tx.Exec(t.Context(), "UPDATE care_event SET performed_at = $1, recorded_at = $1", noon.Add(-2*time.Hour)); err != nil {
		t.Fatalf("watering everything: %v", err)
	}
	digest := newDigest(t, tx, service, fixed(noon))

	next, err := digest.sendDue(t.Context())

	if err != nil {
		t.Fatalf("sendDue: %v", err)
	}
	if got := service.received(); len(got) != 0 {
		t.Errorf("the push service received %q, want nothing on a day with nothing due", got)
	}
	if got, want := ledger(t, tx), []string{"ellie Rosewood 2026-09-03", "sam Rosewood 2026-09-03"}; !slices.Equal(got, want) {
		t.Errorf("the ledger holds %q, want %q so the day is not looked at again", got, want)
	}
	if want := noon.AddDate(0, 0, 1); !next.Equal(want) {
		t.Errorf("the next send is %s, want tomorrow's %s", next.UTC(), want)
	}
}

func TestSendDue_AMemberOfTwoGardensGetsADigestForEach(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	seedFairview(t, tx)
	digest := newDigest(t, tx, service, fixed(noon))

	if _, err := digest.sendDue(t.Context()); err != nil {
		t.Fatalf("sendDue: %v", err)
	}

	want := []string{"ellie Fairview 2026-09-03", "ellie Rosewood 2026-09-03", "sam Rosewood 2026-09-03"}
	if got := ledger(t, tx); !slices.Equal(got, want) {
		t.Errorf("the ledger holds %q, want %q", got, want)
	}
	// Each of Ellie's browsers is sent Rosewood's digest and Fairview's.
	wantSent := []string{"/ellie-mac", "/ellie-mac", "/ellie-phone", "/ellie-phone", "/sam-phone"}
	if got := service.received(); !slices.Equal(got, wantSent) {
		t.Errorf("the push service received %q, want %q", got, wantSent)
	}
}

func TestSendDue_AGardenWithNothingDueSendsNothingWhenTheMembersOtherGardenHasCareDue(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	seedFairview(t, tx)
	// Fairview's one plant was watered this morning. Rosewood's two are still
	// due, and they belong to the same person.
	if _, err := tx.Exec(t.Context(), "UPDATE care_event SET performed_at = $1, recorded_at = $1 WHERE garden_id = $2", noon.Add(-2*time.Hour), fairviewID); err != nil {
		t.Fatalf("watering Fairview: %v", err)
	}
	digest := newDigest(t, tx, service, fixed(noon))

	if _, err := digest.sendDue(t.Context()); err != nil {
		t.Fatalf("sendDue: %v", err)
	}

	want := []string{"/ellie-mac", "/ellie-phone", "/sam-phone"}
	if got := service.received(); !slices.Equal(got, want) {
		t.Errorf("the push service received %q, want %q, with nothing sent for Fairview", got, want)
	}
	if got := ledger(t, tx); !slices.Contains(got, "ellie Fairview 2026-09-03") {
		t.Errorf("the ledger holds %q, want Fairview's day among them so it is not looked at again", got)
	}
}

func TestSendDue_AMemberWithTheDigestOffIsSentNothing(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	ctx := t.Context()
	if _, err := tx.Exec(ctx, "UPDATE notification_preference SET enabled = false WHERE membership_id = $1 AND kind = 'digest'", ellieMembershipID); err != nil {
		t.Fatalf("turning Ellie's digest off: %v", err)
	}
	digest := newDigest(t, tx, service, fixed(noon))

	if _, err := digest.sendDue(ctx); err != nil {
		t.Fatalf("sendDue: %v", err)
	}

	if got, want := service.received(), []string{"/sam-phone"}; !slices.Equal(got, want) {
		t.Errorf("the push service received %q, want %q", got, want)
	}
	if got, want := ledger(t, tx), []string{"sam Rosewood 2026-09-03"}; !slices.Equal(got, want) {
		t.Errorf("the ledger holds %q, want %q", got, want)
	}
}

func TestSendDue_AMemberWhoseMembershipHasEndedIsSentNothing(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	ctx := t.Context()
	if _, err := tx.Exec(ctx, "UPDATE membership SET expires_at = $1 WHERE id = $2", noon.Add(-time.Minute), samMembershipID); err != nil {
		t.Fatalf("ending Sam's membership: %v", err)
	}
	digest := newDigest(t, tx, service, fixed(noon))

	if _, err := digest.sendDue(ctx); err != nil {
		t.Fatalf("sendDue: %v", err)
	}

	if got, want := service.received(), []string{"/ellie-mac", "/ellie-phone"}; !slices.Equal(got, want) {
		t.Errorf("the push service received %q, want %q", got, want)
	}
	if got, want := ledger(t, tx), []string{"ellie Rosewood 2026-09-03"}; !slices.Equal(got, want) {
		t.Errorf("the ledger holds %q, want %q", got, want)
	}
}

func TestSendDue_WithNobodyToSendToThereIsNoNextSend(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	ctx := t.Context()
	if _, err := tx.Exec(ctx, "UPDATE notification_preference SET enabled = false WHERE kind = 'digest'"); err != nil {
		t.Fatalf("turning every digest off: %v", err)
	}
	digest := newDigest(t, tx, service, fixed(noon))

	next, err := digest.sendDue(ctx)

	if err != nil {
		t.Fatalf("sendDue: %v", err)
	}
	if got := service.received(); len(got) != 0 {
		t.Errorf("the push service received %q, want nothing", got)
	}
	if !next.IsZero() {
		t.Errorf("the next send is %s, want none with nobody to send to", next.UTC())
	}
}

func TestSendDue_ABrowserSubscribedWithinTheHourAfterTheHourGetsThatDaysDigest(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	ctx := t.Context()
	digest := newDigest(t, tx, service, fixed(noon))
	if _, err := digest.sendDue(ctx); err != nil {
		t.Fatalf("the look at Robin's hour: %v", err)
	}
	if slices.Contains(ledger(t, tx), "robin Rosewood 2026-09-03") {
		t.Fatal("Robin's day was claimed with no browser to send it to")
	}

	subscription := browserSubscription(t, service.URL+"/robin-phone")
	if _, err := tx.Exec(ctx, "INSERT INTO push_subscription (user_id, endpoint, p256dh_key, auth_key) VALUES ($1, $2, $3, $4)",
		robinID, subscription.Endpoint, subscription.P256dhKey, subscription.AuthKey); err != nil {
		t.Fatalf("subscribing Robin's browser: %v", err)
	}
	later := newDigest(t, tx, service, fixed(noon.Add(30*time.Minute)))
	if _, err := later.sendDue(ctx); err != nil {
		t.Fatalf("the look half an hour later: %v", err)
	}

	if got, want := service.received(), []string{"/ellie-mac", "/ellie-phone", "/robin-phone", "/sam-phone"}; !slices.Equal(got, want) {
		t.Errorf("the push service received %q, want %q", got, want)
	}
	if got := ledger(t, tx); !slices.Contains(got, "robin Rosewood 2026-09-03") {
		t.Errorf("the ledger holds %q, want Robin's day among them", got)
	}
}

// shifted returns a clock reading at when the real clock reads started. It
// runs at the speed of the real one, so a timer set from it fires when it
// should.
func shifted(at, started time.Time) func() time.Time {
	return func() time.Time { return at.Add(time.Since(started)) }
}

// freshRosewood seeds Rosewood in a database of its own and returns a pool on
// it. The job runs in a goroutine, and a test transaction cannot be shared
// with one.
func freshRosewood(t *testing.T, service *pushService) store.DBTX {
	t.Helper()

	pool, err := store.Open(t.Context(), pgtest.Fresh(t, migrateSchema))
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	t.Cleanup(pool.Close)
	seedRosewood(t, pool, service)
	return pool
}

// waitForFirstSend returns the time the push service was sent its first
// message. It fails the test if none arrives within the deadline.
func waitForFirstSend(t *testing.T, service *pushService, within time.Duration) time.Time {
	t.Helper()

	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if at, ok := service.firstReceived(); ok {
			return at
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the push service received nothing within %s, want a digest", within)
	return time.Time{}
}

// waitForSends fails the test unless the push service has received want
// messages within the deadline.
func waitForSends(t *testing.T, service *pushService, want int, within time.Duration) {
	t.Helper()

	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if len(service.received()) >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the push service received %q within %s, want %d messages", service.received(), within, want)
}

func TestRun_ADigestGoesOutWithinASecondOfTheHour(t *testing.T) {
	service := newPushService(t, http.StatusCreated)
	pool := freshRosewood(t, service)
	// Both members' hours are half a second away, so the timer sends rather
	// than the first look. The deadline is long enough that a slow machine
	// fails on how late the digest was and not on how long the wait was.
	started := time.Now()
	hour := started.Add(500 * time.Millisecond)
	startJob(t, newDigest(t, pool, service, shifted(noon.Add(-500*time.Millisecond), started)).Run)

	sent := waitForFirstSend(t, service, 20*time.Second)

	if late := sent.Sub(hour); late > time.Second {
		t.Errorf("the first digest went out %s after the hour, want under a second", late.Truncate(time.Millisecond))
	}
}

func TestRun_AnHourChangedTakesEffectWithoutWaitingForTheTimer(t *testing.T) {
	service := newPushService(t, http.StatusCreated)
	pool := freshRosewood(t, service)
	ctx := t.Context()
	// Nobody's hour comes before this evening, so the timer is hours off.
	if _, err := pool.Exec(ctx, "UPDATE membership SET digest_hour = 20"); err != nil {
		t.Fatalf("moving every hour to the evening: %v", err)
	}
	digest := newDigest(t, pool, service, shifted(noon.Add(30*time.Second), time.Now()))
	startJob(t, digest.Run)
	time.Sleep(200 * time.Millisecond)
	if got := service.received(); len(got) != 0 {
		t.Fatalf("the push service received %q before anybody's hour", got)
	}

	// Ellie moves her hour to 13 in London, which was half a minute ago.
	if _, err := pool.Exec(ctx, "UPDATE membership SET digest_hour = 13 WHERE id = $1", ellieMembershipID); err != nil {
		t.Fatalf("changing Ellie's hour: %v", err)
	}
	digest.Wake()

	waitForSends(t, service, 2, 2*time.Second)
	if got, want := service.received(), []string{"/ellie-mac", "/ellie-phone"}; !slices.Equal(got, want) {
		t.Errorf("the push service received %q, want %q", got, want)
	}
}

// remindAgain sets when Ellie asked for Rosewood's digest again.
func remindAgain(t *testing.T, db store.DBTX, at time.Time) {
	t.Helper()
	if _, err := db.Exec(t.Context(), "UPDATE membership SET remind_again_at = $1 WHERE id = $2", at, ellieMembershipID); err != nil {
		t.Fatalf("setting Ellie's Remind me again time: %v", err)
	}
}

// remindAgainAt returns Ellie's Remind me again time on Rosewood, or the zero
// time when none is waiting.
func remindAgainAt(t *testing.T, db store.DBTX) time.Time {
	t.Helper()
	var at *time.Time
	if err := db.QueryRow(t.Context(), "SELECT remind_again_at FROM membership WHERE id = $1", ellieMembershipID).Scan(&at); err != nil {
		t.Fatalf("reading Ellie's Remind me again time: %v", err)
	}
	if at == nil {
		return time.Time{}
	}
	return *at
}

// againLedger returns every resend in the send ledger as "handle garden
// instant", sorted.
func againLedger(t *testing.T, db store.DBTX) []string {
	t.Helper()

	rows, err := db.Query(t.Context(), `SELECT app_user.handle || ' ' || garden.name || ' ' || notification_send.send_key
		FROM notification_send
		JOIN membership ON membership.id = notification_send.membership_id
		JOIN app_user ON app_user.id = membership.user_id
		JOIN garden ON garden.id = membership.garden_id
		WHERE notification_send.kind = 'digest_again'`)
	if err != nil {
		t.Fatalf("reading the ledger: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var entry string
		if err := rows.Scan(&entry); err != nil {
			t.Fatalf("reading the ledger: %v", err)
		}
		out = append(out, entry)
	}
	slices.Sort(out)
	return out
}

func TestSendDue_TheNextSendIsTheEarlierOfTheHourAndTheRemindMeAgainTime(t *testing.T) {
	cases := []struct {
		name  string
		again time.Time
		want  time.Time
	}{
		{"a time before the hour", noon.Add(-time.Hour), noon.Add(-time.Hour)},
		{"a time after the hour", noon.Add(time.Hour), noon},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tx := pgtest.Tx(t, migrateSchema)
			service := newPushService(t, http.StatusCreated)
			seedRosewood(t, tx, service)
			remindAgain(t, tx, c.again)
			digest := newDigest(t, tx, service, fixed(noon.Add(-3*time.Hour)))

			next, err := digest.sendDue(t.Context())

			if err != nil {
				t.Fatalf("sendDue: %v", err)
			}
			if !next.Equal(c.want) {
				t.Errorf("the next send is %s, want %s", next.UTC(), c.want)
			}
			if got := service.received(); len(got) != 0 {
				t.Errorf("the push service received %q, want nothing before either time", got)
			}
		})
	}
}

func TestSendDue_TheDigestIsSentAgainOnceAtTheChosenTimeAcrossARestart(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	// The morning digest has gone out and Ellie asked for it again at 15:00
	// in London.
	if _, err := tx.Exec(t.Context(), "INSERT INTO notification_send (membership_id, kind, send_key) VALUES ($1, 'digest', '2026-09-03'), ($2, 'digest', '2026-09-03')", ellieMembershipID, samMembershipID); err != nil {
		t.Fatalf("writing the morning's ledger rows: %v", err)
	}
	again := noon.Add(2 * time.Hour)
	remindAgain(t, tx, again)

	digest := newDigest(t, tx, service, fixed(again.Add(time.Second)))
	next, err := digest.sendDue(t.Context())
	if err != nil {
		t.Fatalf("sendDue: %v", err)
	}

	if got, want := service.received(), []string{"/ellie-mac", "/ellie-phone"}; !slices.Equal(got, want) {
		t.Errorf("the push service received %q, want %q", got, want)
	}
	if got, want := againLedger(t, tx), []string{"ellie Rosewood 2026-09-03T14:00:00Z"}; !slices.Equal(got, want) {
		t.Errorf("the ledger holds %q, want %q", got, want)
	}
	if got := remindAgainAt(t, tx); !got.IsZero() {
		t.Errorf("Ellie's Remind me again time is still %s, want it cleared", got.UTC())
	}
	if want := noon.AddDate(0, 0, 1); !next.Equal(want) {
		t.Errorf("the next send is %s, want tomorrow's %s", next.UTC(), want)
	}

	// The same time set again after a restart sends nothing, because the
	// ledger row for that instant is already written.
	remindAgain(t, tx, again)
	restarted := newDigest(t, tx, service, fixed(again.Add(time.Minute)))
	if _, err := restarted.sendDue(t.Context()); err != nil {
		t.Fatalf("the look after the restart: %v", err)
	}
	if got := service.received(); len(got) != 2 {
		t.Errorf("the push service received %q, want the two from the first look and nothing more", got)
	}
	if got := remindAgainAt(t, tx); !got.IsZero() {
		t.Errorf("Ellie's Remind me again time is still %s after the restart, want it cleared", got.UTC())
	}
}

func TestSendDue_TheDigestIsNotSentAgainWhenNothingIsLeftDue(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	again := noon.Add(2 * time.Hour)
	remindAgain(t, tx, again)
	// Everything was watered after the morning digest.
	if _, err := tx.Exec(t.Context(), "UPDATE care_event SET performed_at = $1, recorded_at = $1", noon.Add(time.Hour)); err != nil {
		t.Fatalf("watering everything: %v", err)
	}
	digest := newDigest(t, tx, service, fixed(again))

	if _, err := digest.sendDue(t.Context()); err != nil {
		t.Fatalf("sendDue: %v", err)
	}

	if got := service.received(); len(got) != 0 {
		t.Errorf("the push service received %q, want nothing with nothing left due", got)
	}
	if got := remindAgainAt(t, tx); !got.IsZero() {
		t.Errorf("Ellie's Remind me again time is still %s, want it cleared", got.UTC())
	}
}

func TestSendDue_ARemindMeAgainTimeAnHourOrMorePastIsClearedAndNotSent(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	if _, err := tx.Exec(t.Context(), "INSERT INTO notification_send (membership_id, kind, send_key) VALUES ($1, 'digest', '2026-09-03'), ($2, 'digest', '2026-09-03')", ellieMembershipID, samMembershipID); err != nil {
		t.Fatalf("writing the morning's ledger rows: %v", err)
	}
	again := noon.Add(2 * time.Hour)
	remindAgain(t, tx, again)
	digest := newDigest(t, tx, service, fixed(again.Add(time.Hour)))

	if _, err := digest.sendDue(t.Context()); err != nil {
		t.Fatalf("sendDue: %v", err)
	}

	if got := service.received(); len(got) != 0 {
		t.Errorf("the push service received %q, want nothing an hour late", got)
	}
	if got := remindAgainAt(t, tx); !got.IsZero() {
		t.Errorf("Ellie's Remind me again time is still %s, want it cleared so the job stops looking at it", got.UTC())
	}
}

func TestSendDue_AResendAfterMidnightSendsTheNewDaysDigest(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	// Every seeded plant was watered at Ellie's hour, so nothing is due on 3
	// September. Sprout is due on the 4th.
	if _, err := tx.Exec(t.Context(), "UPDATE care_event SET performed_at = $1, recorded_at = $1", noon); err != nil {
		t.Fatalf("watering everything: %v", err)
	}
	watered := utc(2026, time.August, 25, 12, 0, 0)
	if _, err := tx.Exec(t.Context(), "INSERT INTO plant (id, garden_id, nickname) VALUES ($1, $2, 'Sprout')", sproutID, rosewoodID); err != nil {
		t.Fatalf("planting Sprout: %v", err)
	}
	if _, err := tx.Exec(t.Context(), `INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit, set_at) VALUES ($1, $2, $3, 10, 'day', $4)`,
		rosewoodID, sproutID, waterID, watered.AddDate(0, 0, -10)); err != nil {
		t.Fatalf("scheduling Sprout: %v", err)
	}
	if _, err := tx.Exec(t.Context(), `INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done) VALUES ($1, $2, $3, $4, $5, $5, true)`,
		rosewoodID, sproutID, waterID, ellieID, watered); err != nil {
		t.Fatalf("watering Sprout: %v", err)
	}
	// Ellie pressed In 1 hour at 23:30 in London, so the resend is due at
	// 00:30 the next morning.
	again := utc(2026, time.September, 3, 23, 30, 0)
	remindAgain(t, tx, again)
	digest := newDigest(t, tx, service, fixed(again))

	next, err := digest.sendDue(t.Context())

	if err != nil {
		t.Fatalf("sendDue: %v", err)
	}
	// Nothing was due on the 3rd, so a send at all is a digest built on the
	// 4th, the day Sprout is due.
	if got, want := service.received(), []string{"/ellie-mac", "/ellie-phone"}; !slices.Equal(got, want) {
		t.Errorf("the push service received %q, want %q", got, want)
	}
	if got, want := againLedger(t, tx), []string{"ellie Rosewood 2026-09-03T23:30:00Z"}; !slices.Equal(got, want) {
		t.Errorf("the ledger holds %q, want %q", got, want)
	}
	if got := remindAgainAt(t, tx); !got.IsZero() {
		t.Errorf("Ellie's Remind me again time is still %s, want it cleared", got.UTC())
	}
	if want := noon.AddDate(0, 0, 1); !next.Equal(want) {
		t.Errorf("the next send is %s, want the 4th's own digest at %s", next.UTC(), want)
	}
}

func TestBuild_TheDigestHasTheGardensTagAndTheTwoDelayButtons(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	digest := newDigest(t, tx, service, fixed(noon))
	members, err := digest.queries.ListDigestMembers(t.Context(), noon)
	if err != nil {
		t.Fatalf("listing the members: %v", err)
	}
	ellie := members[0]
	if ellie.MembershipID != ellieMembershipID {
		t.Fatalf("the first member is %s, want Ellie", ellie.Handle)
	}

	got, items, err := digest.build(t.Context(), digest.queries, ellie, zone(t, "Europe/London"), noon)

	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if items != 2 {
		t.Errorf("items = %d, want 2", items)
	}
	if want := "digest-" + rosewoodID.String(); got.Tag != want {
		t.Errorf("the tag is %q, want %q", got.Tag, want)
	}
	if want := "https://sprig.example.com/?from=digest"; got.URL != want {
		t.Errorf("the URL is %q, want %q", got.URL, want)
	}
	if got.Again == nil {
		t.Fatal("the digest has no Remind me again form")
	}
	if want := "https://sprig.example.com/remind-again"; got.Again.URL != want {
		t.Errorf("the form posts to %q, want %q", got.Again.URL, want)
	}
	var labels, actions []string
	for _, delay := range got.Again.Delays {
		labels = append(labels, delay.Label)
		actions = append(actions, delay.Action)
	}
	if want := []string{"In 1 hour", "In 2 hours"}; !slices.Equal(labels, want) {
		t.Errorf("the banner offers %q, want %q", labels, want)
	}
	if want := []string{"Remind me again in 1 hour", "Remind me again in 2 hours"}; !slices.Equal(actions, want) {
		t.Errorf("the notification offers %q, want %q", actions, want)
	}
}

func TestRun_ARemindMeAgainTimeSetTakesEffectWithoutWaitingForTheTimer(t *testing.T) {
	service := newPushService(t, http.StatusCreated)
	pool := freshRosewood(t, service)
	ctx := t.Context()
	// Nobody's hour comes before this evening, so the timer is hours off.
	if _, err := pool.Exec(ctx, "UPDATE membership SET digest_hour = 20"); err != nil {
		t.Fatalf("moving every hour to the evening: %v", err)
	}
	started := time.Now()
	digest := newDigest(t, pool, service, shifted(noon, started))
	startJob(t, digest.Run)
	time.Sleep(200 * time.Millisecond)
	if got := service.received(); len(got) != 0 {
		t.Fatalf("the push service received %q before anybody's hour", got)
	}

	// Ellie asks for the digest again half a second from now.
	remindAgain(t, pool, noon.Add(time.Since(started)).Add(500*time.Millisecond))
	digest.Wake()

	waitForSends(t, service, 2, 5*time.Second)
	if got, want := service.received(), []string{"/ellie-mac", "/ellie-phone"}; !slices.Equal(got, want) {
		t.Errorf("the push service received %q, want %q", got, want)
	}
}
