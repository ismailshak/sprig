package push

import (
	"context"
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

	digest := NewDigest(slog.New(slog.DiscardHandler), store.New(db), NewSender(testKeys(t), service.Client()), "https://sprig.example.com")
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

// start runs the job until the test ends.
func start(t *testing.T, digest *Digest) {
	t.Helper()

	ctx, stop := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		digest.Run(ctx)
	}()
	t.Cleanup(func() {
		stop()
		<-done
	})
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
	start(t, newDigest(t, pool, service, shifted(noon.Add(-500*time.Millisecond), started)))

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
	start(t, digest)
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
