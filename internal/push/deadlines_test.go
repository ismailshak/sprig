package push

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/pgtest"
	"github.com/ismailshak/sprig/internal/store"
)

var (
	kitchenTokenID = uuid.MustParse("00000000-0000-7000-8000-000000000231")
	spareTokenID   = uuid.MustParse("00000000-0000-7000-8000-000000000232")
)

var kitchenToken = store.APIToken{ID: kitchenTokenID, Name: "The kitchen display", Prefix: "sprg_7c1f", ExpiresAt: utc(2026, time.September, 10, 12, 0, 0)}

// giveToken writes a token of the garden, created by Ellie. A nil revokedAt
// leaves the token working.
func giveToken(t *testing.T, db store.DBTX, gardenID, id uuid.UUID, createdAt, expiresAt time.Time, revokedAt *time.Time) {
	t.Helper()

	_, err := db.Exec(t.Context(), `INSERT INTO api_token (id, garden_id, name, token_hash, prefix, created_by, created_at, expires_at, revoked_at)
		VALUES ($1, $2, 'The kitchen display', $7, 'sprg_7c1f', $3, $4, $5, $6)`, id, gardenID, ellieID, createdAt, expiresAt, revokedAt, id.String())
	if err != nil {
		t.Fatalf("writing the token: %v", err)
	}
}

// endSitting turns Sam's membership into a sitting Ellie issued, ending at
// endsAt.
func endSitting(t *testing.T, db store.DBTX, endsAt time.Time) {
	t.Helper()

	_, err := db.Exec(t.Context(), "UPDATE membership SET role = 'sitter', invited_by = $1, expires_at = $2 WHERE id = $3", ellieID, endsAt, samMembershipID)
	if err != nil {
		t.Fatalf("ending Sam's sitting: %v", err)
	}
}

var (
	upstairsID              = uuid.MustParse("00000000-0000-7000-8000-000000000241")
	robinUpstairsMembership = uuid.MustParse("00000000-0000-7000-8000-000000000242")
	samUpstairsMembership   = uuid.MustParse("00000000-0000-7000-8000-000000000243")
	upstairsTokenID         = uuid.MustParse("00000000-0000-7000-8000-000000000244")
)

// seedUpstairs writes a second garden, Upstairs, owned by Robin and with Sam
// as a member. Robin has no browser subscribed, so a deadline query over
// Upstairs returns Sam's row alone.
func seedUpstairs(t *testing.T, db store.DBTX) {
	t.Helper()

	if _, err := db.Exec(t.Context(), "INSERT INTO garden (id, name) VALUES ($1, 'Upstairs')", upstairsID); err != nil {
		t.Fatalf("seeding Upstairs: %v", err)
	}
	_, err := db.Exec(t.Context(), `INSERT INTO membership (id, garden_id, user_id, role, digest_hour) VALUES
		($1, $3, $4, 'owner', 13), ($2, $3, $5, 'member', 8)`,
		robinUpstairsMembership, samUpstairsMembership, upstairsID, robinID, samID)
	if err != nil {
		t.Fatalf("seeding Upstairs: %v", err)
	}
}

func newDeadlines(t *testing.T, db store.DBTX, service *pushService, now func() time.Time) *Deadlines {
	t.Helper()

	deadlines := NewDeadlines(slog.New(slog.DiscardHandler), store.New(db), NewSender(testKeys(t), service.Client()), "https://sprig.example.com/", "/more/tokens", "/more/people")
	deadlines.now = now
	return deadlines
}

// sends returns every ledger row of kind as "handle key", sorted.
func sends(t *testing.T, db store.DBTX, kind string) []string {
	t.Helper()

	rows, err := db.Query(t.Context(), `SELECT app_user.handle || ' ' || notification_send.send_key
		FROM notification_send
		JOIN membership ON membership.id = notification_send.membership_id
		JOIN app_user ON app_user.id = membership.user_id
		WHERE notification_send.kind = $1`, kind)
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

func TestDeadlines_ATokenAWeekFromExpiryNotifiesEveryoneWhoCanManageTokensAndHasABrowser(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	expires := noon.AddDate(0, 0, 7)
	giveToken(t, tx, rosewoodID, kitchenTokenID, noon.AddDate(0, 0, -23), expires, nil)
	deadlines := newDeadlines(t, tx, service, fixed(noon))

	next, err := deadlines.sendDue(t.Context())

	if err != nil {
		t.Fatalf("sendDue: %v", err)
	}
	// Robin can manage tokens and has no browser, so no row is claimed for
	// them.
	if got, want := service.received(), []string{"/ellie-mac", "/ellie-phone", "/sam-phone"}; !slices.Equal(got, want) {
		t.Errorf("the push service received %q, want %q", got, want)
	}
	if got, want := sends(t, tx, tokenExpiringKind), []string{"ellie " + kitchenTokenID.String(), "sam " + kitchenTokenID.String()}; !slices.Equal(got, want) {
		t.Errorf("the ledger holds %q, want %q", got, want)
	}
	if !next.Equal(expires) {
		t.Errorf("the next look is %s, want the expiry at %s", next.UTC(), expires)
	}
}

func TestDeadlines_ATokenMadeWithAWeekOrLessToLiveGetsNoWarning(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	expires := noon.AddDate(0, 0, 7)
	giveToken(t, tx, rosewoodID, kitchenTokenID, noon, expires, nil)
	deadlines := newDeadlines(t, tx, service, fixed(noon.Add(time.Minute)))

	next, err := deadlines.sendDue(t.Context())

	if err != nil {
		t.Fatalf("sendDue: %v", err)
	}
	if got := service.received(); len(got) != 0 {
		t.Errorf("the push service received %q, want nothing: the warning would arrive the moment the token was made", got)
	}
	if !next.Equal(expires) {
		t.Errorf("the next look is %s, want the expiry at %s", next.UTC(), expires)
	}
}

func TestDeadlines_AnExpiredTokenNotifiesOnceAndTheNextLookSendsNothingMore(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	giveToken(t, tx, rosewoodID, kitchenTokenID, noon.AddDate(0, 0, -30), noon.Add(-time.Hour), nil)
	deadlines := newDeadlines(t, tx, service, fixed(noon))

	next, err := deadlines.sendDue(t.Context())
	if err != nil {
		t.Fatalf("sendDue: %v", err)
	}
	// A restart is a second job over the same database.
	if _, err := newDeadlines(t, tx, service, fixed(noon.Add(time.Hour))).sendDue(t.Context()); err != nil {
		t.Fatalf("the second look: %v", err)
	}

	if got, want := service.received(), []string{"/ellie-mac", "/ellie-phone", "/sam-phone"}; !slices.Equal(got, want) {
		t.Errorf("the push service received %q, want %q once", got, want)
	}
	if got, want := sends(t, tx, tokenExpiredKind), []string{"ellie " + kitchenTokenID.String(), "sam " + kitchenTokenID.String()}; !slices.Equal(got, want) {
		t.Errorf("the ledger holds %q, want %q", got, want)
	}
	// The warning's instant is past too, and the token has expired, so the
	// expired notification is the only one sent.
	if got := sends(t, tx, tokenExpiringKind); len(got) != 0 {
		t.Errorf("the ledger holds warnings %q, want none for a token that has already expired", got)
	}
	if !next.IsZero() {
		t.Errorf("the next look is %s, want none: nothing is coming", next.UTC())
	}
}

func TestDeadlines_AWarningMissedWhileTheProcessWasDownIsSentWhileTheTokenStillWorks(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	expires := noon.AddDate(0, 0, 2)
	giveToken(t, tx, rosewoodID, kitchenTokenID, noon.AddDate(0, 0, -28), expires, nil)
	deadlines := newDeadlines(t, tx, service, fixed(noon))

	next, err := deadlines.sendDue(t.Context())

	if err != nil {
		t.Fatalf("sendDue: %v", err)
	}
	if got, want := sends(t, tx, tokenExpiringKind), []string{"ellie " + kitchenTokenID.String(), "sam " + kitchenTokenID.String()}; !slices.Equal(got, want) {
		t.Errorf("the ledger holds %q, want %q: the warning was due five days ago", got, want)
	}
	if !next.Equal(expires) {
		t.Errorf("the next look is %s, want the expiry at %s", next.UTC(), expires)
	}
}

func TestDeadlines_ATokenThatExpiredMoreThanAWeekAgoNotifiesNobody(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	giveToken(t, tx, rosewoodID, kitchenTokenID, noon.AddDate(0, 0, -40), noon.AddDate(0, 0, -7).Add(-time.Hour), nil)
	deadlines := newDeadlines(t, tx, service, fixed(noon))

	next, err := deadlines.sendDue(t.Context())

	if err != nil {
		t.Fatalf("sendDue: %v", err)
	}
	if got := service.received(); len(got) != 0 {
		t.Errorf("the push service received %q, want nothing for a token eight days expired", got)
	}
	if !next.IsZero() {
		t.Errorf("the next look is %s, want none", next.UTC())
	}
}

func TestDeadlines_ARevokedTokenNotifiesNobody(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	revoked := noon.Add(-2 * time.Hour)
	giveToken(t, tx, rosewoodID, kitchenTokenID, noon.AddDate(0, 0, -30), noon.Add(-time.Hour), &revoked)
	deadlines := newDeadlines(t, tx, service, fixed(noon))

	if _, err := deadlines.sendDue(t.Context()); err != nil {
		t.Fatalf("sendDue: %v", err)
	}

	if got := service.received(); len(got) != 0 {
		t.Errorf("the push service received %q, want nothing for a revoked token", got)
	}
}

func TestDeadlines_ATokenExpiringInOneGardenNotifiesNobodyFromAnotherGarden(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	seedFairview(t, tx)
	// Ellie owns Fairview as well as Rosewood. Sam is in Rosewood alone.
	giveToken(t, tx, fairviewID, kitchenTokenID, noon.AddDate(0, 0, -30), noon.Add(-time.Hour), nil)
	deadlines := newDeadlines(t, tx, service, fixed(noon))

	if _, err := deadlines.sendDue(t.Context()); err != nil {
		t.Fatalf("sendDue: %v", err)
	}

	if got, want := service.received(), []string{"/ellie-mac", "/ellie-phone"}; !slices.Equal(got, want) {
		t.Errorf("the push service received %q, want Ellie's browsers alone: Fairview's token is not Sam's to manage", got)
	}
}

func TestDeadlines_ASitterIsNotToldAboutTokens(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	if _, err := tx.Exec(t.Context(), "UPDATE membership SET role = 'sitter' WHERE id = $1", samMembershipID); err != nil {
		t.Fatal(err)
	}
	giveToken(t, tx, rosewoodID, kitchenTokenID, noon.AddDate(0, 0, -30), noon.Add(-time.Hour), nil)
	deadlines := newDeadlines(t, tx, service, fixed(noon))

	if _, err := deadlines.sendDue(t.Context()); err != nil {
		t.Fatalf("sendDue: %v", err)
	}

	if got, want := service.received(), []string{"/ellie-mac", "/ellie-phone"}; !slices.Equal(got, want) {
		t.Errorf("the push service received %q, want Ellie's browsers alone: a sitter cannot manage tokens", got)
	}
}

func TestDeadlines_TheNextLookIsTheEarliestInstantStillToCome(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	// The kitchen display's warning is three days off. The spare expires
	// tomorrow and was made with a week to live, so its expiry is the only
	// instant it has.
	giveToken(t, tx, rosewoodID, kitchenTokenID, noon.AddDate(0, 0, -20), noon.AddDate(0, 0, 10), nil)
	giveToken(t, tx, rosewoodID, spareTokenID, noon.AddDate(0, 0, -6), noon.AddDate(0, 0, 1), nil)
	deadlines := newDeadlines(t, tx, service, fixed(noon))

	next, err := deadlines.sendDue(t.Context())

	if err != nil {
		t.Fatalf("sendDue: %v", err)
	}
	if got := service.received(); len(got) != 0 {
		t.Errorf("the push service received %q, want nothing yet", got)
	}
	if want := noon.AddDate(0, 0, 1); !next.Equal(want) {
		t.Errorf("the next look is %s, want the spare's expiry at %s", next.UTC(), want)
	}
}

func TestDeadlines_ASittingEndedIsSentToTheSitterAndToWhoeverInvitedThemOnce(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	ended := noon.Add(-time.Hour)
	endSitting(t, tx, ended)
	deadlines := newDeadlines(t, tx, service, fixed(noon))

	next, err := deadlines.sendDue(t.Context())
	if err != nil {
		t.Fatalf("sendDue: %v", err)
	}
	if _, err := newDeadlines(t, tx, service, fixed(noon.Add(time.Hour))).sendDue(t.Context()); err != nil {
		t.Fatalf("the second look: %v", err)
	}

	if got, want := service.received(), []string{"/ellie-mac", "/ellie-phone", "/sam-phone"}; !slices.Equal(got, want) {
		t.Errorf("the push service received %q, want %q once", got, want)
	}
	key := sittingKey(samMembershipID, ended)
	if got, want := sends(t, tx, sittingEndedKind), []string{"ellie " + key, "sam " + key}; !slices.Equal(got, want) {
		t.Errorf("the ledger holds %q, want %q", got, want)
	}
	if !next.IsZero() {
		t.Errorf("the next look is %s, want none", next.UTC())
	}
}

func TestDeadlines_ASittingRenewedBeforeItsEndDateMovesTheNextLookAndSendsNothing(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	endSitting(t, tx, noon.AddDate(0, 0, 1))
	deadlines := newDeadlines(t, tx, service, fixed(noon))
	if _, err := deadlines.sendDue(t.Context()); err != nil {
		t.Fatalf("the first look: %v", err)
	}

	renewed := noon.AddDate(0, 0, 3)
	endSitting(t, tx, renewed)
	next, err := deadlines.sendDue(t.Context())

	if err != nil {
		t.Fatalf("sendDue: %v", err)
	}
	if got := service.received(); len(got) != 0 {
		t.Errorf("the push service received %q, want nothing", got)
	}
	if !next.Equal(renewed) {
		t.Errorf("the next look is %s, want the new end at %s", next.UTC(), renewed)
	}
}

func TestDeadlines_ASittingRenewedPastAnEndDateAlreadySentForIsSentForAgainWhenTheNewOnePasses(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	first := noon.Add(-time.Hour)
	endSitting(t, tx, first)
	if _, err := newDeadlines(t, tx, service, fixed(noon)).sendDue(t.Context()); err != nil {
		t.Fatalf("the first look: %v", err)
	}

	second := noon.AddDate(0, 0, 1)
	endSitting(t, tx, second)
	if _, err := newDeadlines(t, tx, service, fixed(noon.AddDate(0, 0, 2))).sendDue(t.Context()); err != nil {
		t.Fatalf("the second look: %v", err)
	}

	want := []string{"ellie " + sittingKey(samMembershipID, first), "ellie " + sittingKey(samMembershipID, second),
		"sam " + sittingKey(samMembershipID, first), "sam " + sittingKey(samMembershipID, second)}
	if got := sends(t, tx, sittingEndedKind); !slices.Equal(got, want) {
		t.Errorf("the ledger holds %q, want %q", got, want)
	}
	if got := service.received(); len(got) != 6 {
		t.Errorf("the push service received %q, want the three browsers twice", got)
	}
}

func TestDeadlines_ASittingThatEndedMoreThanAWeekAgoIsNotSentFor(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	endSitting(t, tx, noon.AddDate(0, 0, -7).Add(-time.Hour))
	deadlines := newDeadlines(t, tx, service, fixed(noon))

	if _, err := deadlines.sendDue(t.Context()); err != nil {
		t.Fatalf("sendDue: %v", err)
	}

	if got := service.received(); len(got) != 0 {
		t.Errorf("the push service received %q, want nothing for a sitting eight days over", got)
	}
}

func TestDeadlines_AnInviterWhoseOwnAccessHasEndedIsNotToldTheSittingEnded(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	endSitting(t, tx, noon.Add(-time.Hour))
	// Ellie's own access ended long enough ago that her membership is not a
	// sitting the job sends for either.
	if _, err := tx.Exec(t.Context(), "UPDATE membership SET expires_at = $1 WHERE id = $2", noon.AddDate(0, 0, -30), ellieMembershipID); err != nil {
		t.Fatal(err)
	}
	deadlines := newDeadlines(t, tx, service, fixed(noon))

	if _, err := deadlines.sendDue(t.Context()); err != nil {
		t.Fatalf("sendDue: %v", err)
	}

	if got, want := service.received(), []string{"/sam-phone"}; !slices.Equal(got, want) {
		t.Errorf("the push service received %q, want Sam's phone alone", got)
	}
}

func TestDeadlines_AnOwnerWhoDidNotIssueTheSittingIsNotToldItEnded(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	// Sam's sitting was issued by Robin, who has no browser. Ellie owns
	// Rosewood and issued nothing.
	if _, err := tx.Exec(t.Context(), "UPDATE membership SET role = 'sitter', invited_by = $1, expires_at = $2 WHERE id = $3", robinID, noon.Add(-time.Hour), samMembershipID); err != nil {
		t.Fatal(err)
	}
	deadlines := newDeadlines(t, tx, service, fixed(noon))

	if _, err := deadlines.sendDue(t.Context()); err != nil {
		t.Fatalf("sendDue: %v", err)
	}

	if got, want := service.received(), []string{"/sam-phone"}; !slices.Equal(got, want) {
		t.Errorf("the push service received %q, want Sam's phone alone", got)
	}
}

func TestListTokenDeadlines_EachRowNamesTheGardensOwnerAndWhetherTheRecipientIsTheOwner(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	seedUpstairs(t, tx)
	made, expires := noon.AddDate(0, 0, -30), noon.AddDate(0, 0, 3)
	giveToken(t, tx, rosewoodID, kitchenTokenID, made, expires, nil)
	giveToken(t, tx, upstairsID, upstairsTokenID, made, expires, nil)

	rows, err := store.New(tx).ListTokenDeadlines(t.Context(), store.ListTokenDeadlinesParams{
		Now:        noon,
		Capability: string(auth.TokenManage),
		Since:      noon.AddDate(0, 0, -7),
	})

	if err != nil {
		t.Fatalf("listing the tokens: %v", err)
	}
	// Ellie owns Rosewood and Robin owns Upstairs, so Sam's two rows name
	// different people.
	got := make([]string, 0, len(rows))
	for _, row := range rows {
		got = append(got, fmt.Sprintf("%s %s %s %t", row.Handle, row.GardenName, row.OwnerName, row.RecipientOwns))
	}
	want := []string{"ellie Rosewood Ellie true", "sam Rosewood Ellie false", "sam Upstairs Robin false"}
	if !slices.Equal(got, want) {
		t.Errorf("the rows are %q, want %q", got, want)
	}
}

func TestListSittingDeadlines_EachRowNamesTheGardensOwnerAndWhetherTheRecipientIsTheOwner(t *testing.T) {
	tx := pgtest.Tx(t, migrateSchema)
	service := newPushService(t, http.StatusCreated)
	seedRosewood(t, tx, service)
	seedUpstairs(t, tx)
	ended := noon.Add(-time.Hour)
	endSitting(t, tx, ended)
	// Sam is a sitter in Upstairs too, issued by Robin. It ends at the same
	// instant as the Rosewood sitting.
	if _, err := tx.Exec(t.Context(), "UPDATE membership SET role = 'sitter', invited_by = $1, expires_at = $2 WHERE id = $3", robinID, ended, samUpstairsMembership); err != nil {
		t.Fatal(err)
	}

	rows, err := store.New(tx).ListSittingDeadlines(t.Context(), noon, noon.AddDate(0, 0, -7))

	if err != nil {
		t.Fatalf("listing the sittings: %v", err)
	}
	// Ellie owns Rosewood and Robin owns Upstairs, so Sam's two rows name
	// different people.
	got := make([]string, 0, len(rows))
	for _, row := range rows {
		got = append(got, fmt.Sprintf("%s %s %s %t", row.Handle, row.GardenName, row.OwnerName, row.RecipientOwns))
	}
	want := []string{"sam Rosewood Ellie false", "ellie Rosewood Ellie true", "sam Upstairs Robin false"}
	if !slices.Equal(got, want) {
		t.Errorf("the rows are %q, want %q", got, want)
	}
}

func TestTokenExpiringNotification_NamesTheTokenByNameAndPrefixWithTheDateInTheRecipientsZone(t *testing.T) {
	// Noon UTC on the 10th is the small hours of the 11th in Auckland.
	auckland, err := time.LoadLocation("Pacific/Auckland")
	if err != nil {
		t.Fatal(err)
	}

	got := tokenExpiringNotification("Ellie’s Rosewood", kitchenToken, auckland, "https://sprig.example.com/more/tokens")

	want := Notification{Title: "Token expires soon", Body: "The kitchen display (sprg_7c1f…) in Ellie’s Rosewood expires on 11 Sep.", URL: "https://sprig.example.com/more/tokens"}
	if got != want {
		t.Errorf("the notification is %+v, want %+v", got, want)
	}
}

func TestTokenExpiredNotification_SaysTheTokenHasExpiredAndOpensTokens(t *testing.T) {
	got := tokenExpiredNotification("Rosewood", kitchenToken, "https://sprig.example.com/more/tokens")

	want := Notification{Title: "Token expired", Body: "The kitchen display (sprg_7c1f…) in Rosewood has expired.", URL: "https://sprig.example.com/more/tokens"}
	if got != want {
		t.Errorf("the notification is %+v, want %+v", got, want)
	}
}

func TestSittingEndedNotification_TheSittersOpensNothingAndTheInvitersOpensPeople(t *testing.T) {
	// The inviter here is Ellie, the owner, and the sitter is not.
	row := store.ListSittingDeadlinesRow{SitterName: "Sam", GardenName: "Rosewood", OwnerName: "Ellie", IsSitter: true}
	people := "https://sprig.example.com/more/people"

	sitter := sittingEndedNotification(row, people)
	row.IsSitter, row.RecipientOwns = false, true
	inviter := sittingEndedNotification(row, people)

	if want := (Notification{Title: "Access ended", Body: "Your access to Ellie’s Rosewood has ended."}); sitter != want {
		t.Errorf("the sitter's notification is %+v, want %+v", sitter, want)
	}
	if want := (Notification{Title: "Access ended", Body: "Sam no longer has access to Rosewood.", URL: people}); inviter != want {
		t.Errorf("the inviter's notification is %+v, want %+v", inviter, want)
	}
}

func TestRun_ATokenCreatedNotifiesAtItsExpiryWithoutWaitingForTheTimer(t *testing.T) {
	service := newPushService(t, http.StatusCreated)
	pool := freshRosewood(t, service)
	started := time.Now()
	deadlines := newDeadlines(t, pool, service, shifted(noon, started))
	startJob(t, deadlines.Run)
	time.Sleep(200 * time.Millisecond)
	if got := service.received(); len(got) != 0 {
		t.Fatalf("the push service received %q with no token in the garden", got)
	}

	// Ellie creates a token that expires half a second from now, and the
	// handler wakes the job.
	expires := noon.Add(time.Since(started) + 500*time.Millisecond)
	giveToken(t, pool, rosewoodID, kitchenTokenID, noon.AddDate(0, 0, -30), expires, nil)
	deadlines.Wake()

	waitForSends(t, service, 3, 5*time.Second)
	if got, want := service.received(), []string{"/ellie-mac", "/ellie-phone", "/sam-phone"}; !slices.Equal(got, want) {
		t.Errorf("the push service received %q, want %q", got, want)
	}
}

// startJob runs run until the test ends.
func startJob(t *testing.T, run func(context.Context)) {
	t.Helper()

	ctx, stop := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		run(ctx)
	}()
	t.Cleanup(func() {
		stop()
		<-done
	})
}
