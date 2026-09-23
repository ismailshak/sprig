package http

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

var (
	kitchenTokenID = uuid.MustParse("00000000-0000-7000-8000-000000000350")
	spareTokenID   = uuid.MustParse("00000000-0000-7000-8000-000000000351")
	otherTokenID   = uuid.MustParse("00000000-0000-7000-8000-000000000352")
)

type tokensFixture struct {
	*moreFixture
	handler *tokens
}

func openTokens(t *testing.T) *tokensFixture {
	t.Helper()

	f := moreGarden(t)
	return &tokensFixture{
		moreFixture: f,
		handler:     &tokens{logger: testLogger, queries: store.New(f.tx), templates: testTemplates(), now: func() time.Time { return thursday }},
	}
}

// tokenGarden gives Rosewood the two rows the Tokens page has to tell apart:
// the kitchen display, which works and was used today, and the spare, which
// ran out on its own six days ago and has never been used. Fairview holds one
// too, so another garden's row is in reach of every query the page makes.
func tokenGarden(t *testing.T) *tokensFixture {
	t.Helper()

	f := openTokens(t)
	f.exec(t, `INSERT INTO api_token (id, garden_id, name, token_hash, prefix, created_by, created_at, expires_at, last_used_at) VALUES
		($1, $2, 'The kitchen display', 'kitchen', 'sprg_7c1f', $3, $4, $5, $6),
		($7, $2, 'The spare display', 'spare', 'sprg_2ea8', $3, $8, $9, NULL)`,
		kitchenTokenID, moreGardenID, moreUserID, thursday.AddDate(0, 0, -40), thursday.AddDate(0, 0, 50), thursday,
		spareTokenID, thursday.AddDate(0, 0, -96), thursday.AddDate(0, 0, -6))
	f.exec(t, `INSERT INTO api_token (id, garden_id, name, token_hash, prefix, created_by, expires_at)
		VALUES ($1, $2, 'The hallway display', 'other-garden', 'sprg_0000', $3, $4)`,
		otherTokenID, otherGardenID, otherUserID, thursday.AddDate(0, 0, 30))
	return f
}

func TestTokens_ALiveRowOffersRevokeAndAnExpiredOneOffersRemove(t *testing.T) {
	f := tokenGarden(t)

	rows := tokensShown(f.page(t, f.handler.show, TokensPath))

	// The newest row is at the top. A date inside the last week is named by its
	// day, and the spare ran out on the Friday.
	want := []shownToken{
		{says: "The kitchen display sprg_7c1f… · Last used today Expires 23 Oct", drop: "Revoke"},
		{says: "The spare display sprg_2ea8… · Never used Expired Friday", drop: "Remove"},
	}
	if !slices.Equal(rows, want) {
		t.Errorf("the list reads\n%+v\nwant\n%+v", rows, want)
	}
}

func TestTokens_TheNoteUnderTheListSaysWhatHasHappenedOnceARowHasRunOut(t *testing.T) {
	f := tokenGarden(t)

	page := f.page(t, f.handler.show, TokensPath)
	if !strings.Contains(text(page), "An expired token no longer works") {
		t.Errorf("the note does not say a row has already run out:\n%s", page)
	}

	f.exec(t, "DELETE FROM api_token WHERE id = $1", spareTokenID)
	page = f.page(t, f.handler.show, TokensPath)

	if strings.Contains(text(page), "An expired token no longer works") {
		t.Errorf("with every token working, the page still has the note about an expired one:\n%s", page)
	}
}

func TestTokens_ANewTokenIsShownOnceAndOnlyItsHashIsStored(t *testing.T) {
	f := tokenGarden(t)

	rec := f.do(t, f.handler.createToken, TokensPath, url.Values{"name": {"The greenhouse pi"}, "expiry": {"30"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	page := rec.Body.String()
	token := secretValue(t, page)

	var stored, prefix string
	var expires time.Time
	row := f.tx.QueryRow(t.Context(), "SELECT token_hash, prefix, expires_at FROM api_token WHERE name = 'The greenhouse pi'")
	if err := row.Scan(&stored, &prefix, &expires); err != nil {
		t.Fatal(err)
	}
	if stored != auth.HashToken(token) {
		t.Error("the row does not hold the hash of the token that was shown")
	}
	if !strings.HasPrefix(token, prefix) {
		t.Errorf("the token %q does not start with the prefix %q the list shows", token, prefix)
	}
	if want := thursday.AddDate(0, 0, 30); !expires.Equal(want) {
		t.Errorf("the token stops working at %s, want %s", expires, want)
	}
	if !strings.Contains(text(page), "It expires on 3 Oct") {
		t.Errorf("the sentence under the token does not name the day it stops working:\n%s", page)
	}
	if strings.Contains(f.page(t, f.handler.show, TokensPath), token) {
		t.Error("the token is on the page again on the next request, and it is shown exactly once")
	}
}

func TestTokens_ANewTokensCopyButtonHoldsTheTokenShown(t *testing.T) {
	f := tokenGarden(t)

	rec := f.do(t, f.handler.createToken, TokensPath, url.Values{"name": {"The greenhouse pi"}, "expiry": {"30"}})

	page := rec.Body.String()
	// Copy is rendered hidden. A browser running no script shows no Copy.
	// data-copy is the value the script puts on the clipboard.
	copyButton := readHTML(page).first(isTag("button"), hasAttr("data-copy"))
	if !copyButton.has("hidden") {
		t.Fatalf("the page has no hidden Copy button:\n%s", page)
	}
	if got, want := copyButton.attr("data-copy"), secretValue(t, page); got != want {
		t.Errorf("Copy puts %q on the clipboard, want the token shown, %q", got, want)
	}
}

func TestTokens_AnExpiryLongerThanNinetyDaysIsRefusedAndNoTokenIsWritten(t *testing.T) {
	f := tokenGarden(t)

	rec := f.do(t, f.handler.createToken, TokensPath, url.Values{"name": {"The greenhouse pi"}, "expiry": {"365"}})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := tokensInGarden(t, f.moreFixture); got != 2 {
		t.Errorf("the garden holds %d tokens, and a refused lifetime writes none", got)
	}
}

func TestTokens_ATokenWithNoNameIsRefusedWithTheChosenExpiryStillSelected(t *testing.T) {
	f := tokenGarden(t)

	rec := f.do(t, f.handler.createToken, TokensPath, url.Values{"name": {"  "}, "expiry": {"90"}})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	page := rec.Body.String()
	if !strings.Contains(text(page), tokenNameMissing) {
		t.Errorf("the message under the name field is missing:\n%s", page)
	}
	if got := selectedOption(page); got != "90" {
		t.Errorf("the expiry select reads %q after the refusal, want the 90 that was chosen", got)
	}
	if got := tokensInGarden(t, f.moreFixture); got != 2 {
		t.Errorf("the garden holds %d tokens, and a token with no name writes none", got)
	}
}

func TestTokens_TheExpirySelectOffersThirtyDaysUntilSomethingElseIsChosen(t *testing.T) {
	f := tokenGarden(t)

	page := f.page(t, f.handler.show, TokensPath)

	if got := lifeOptionsShown(page); !slices.Equal(got, []string{"7", "30", "60", "90"}) {
		t.Errorf("the expiry select offers %v, want 7, 30, 60 and 90 days", got)
	}
	if got := selectedOption(page); got != "30" {
		t.Errorf("the expiry select starts on %q, want 30", got)
	}
}

func TestTokens_RevokingATokenTakesItOutOfTheListAndLeavesTheRest(t *testing.T) {
	f := tokenGarden(t)

	rec := f.remove(t, f.handler.revokeToken, "token", kitchenTokenID, revokeTokenPath(kitchenTokenID))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	rows := tokensShown(f.page(t, f.handler.show, TokensPath))
	if len(rows) != 1 || !strings.HasPrefix(rows[0].says, "The spare display ") {
		t.Errorf("the list reads %+v after revoking the kitchen display", rows)
	}
}

func TestTokens_RevokingATokenThatIsAlreadyRevokedIsNotFound(t *testing.T) {
	f := tokenGarden(t)
	f.remove(t, f.handler.revokeToken, "token", kitchenTokenID, revokeTokenPath(kitchenTokenID))

	rec := f.remove(t, f.handler.revokeToken, "token", kitchenTokenID, revokeTokenPath(kitchenTokenID))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d; the row is no longer on the page to press", rec.Code, http.StatusNotFound)
	}
}

func tokensInGarden(t *testing.T, f *moreFixture) int {
	t.Helper()

	var count int
	row := f.tx.QueryRow(t.Context(), "SELECT count(*) FROM api_token WHERE garden_id = $1", moreGardenID)
	if err := row.Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// shownToken is one row of the Tokens list.
type shownToken struct {
	// says is the row's text without its button: the name, the prefix, when it
	// was last used and when it expires.
	says string
	// drop is the text of the row's one button.
	drop string
}

// tokensShown reads the Tokens list in page order. A row is a list item holding
// a button.
func tokensShown(page string) []shownToken {
	var out []shownToken
	for _, row := range readHTML(page).all(isTag("li")) {
		button := row.first(isTag("button"))
		if button == nil {
			continue
		}
		out = append(out, shownToken{says: textOutside(row, "button"), drop: button.text()})
	}
	return out
}

// lifeOptionsShown reads the values the expiry select offers, in page order.
func lifeOptionsShown(page string) []string {
	var out []string
	for _, option := range readHTML(page).byID("expiry").all(isTag("option")) {
		out = append(out, option.attr("value"))
	}
	return out
}

func selectedOption(page string) string {
	return selectedValue(readHTML(page).byID("expiry"))
}

func TestTokens_ARevokeSentAsASwapGetsThePageUnderTheBarWithoutThatRow(t *testing.T) {
	f := tokenGarden(t)

	rec := f.swap(t, f.handler.revokeToken, revokeTokenPath(kitchenTokenID), tokensID, "token", kitchenTokenID.String(), url.Values{})

	rows := tokensShown(fragment(t, rec, tokensID))
	if len(rows) != 1 || !strings.HasPrefix(rows[0].says, "The spare display ") {
		t.Errorf("the list reads %+v after revoking the kitchen display", rows)
	}
}

func TestTokens_ACreateSentAsASwapGetsThePageUnderTheBarWithTheNewToken(t *testing.T) {
	f := tokenGarden(t)

	rec := f.swap(t, f.handler.createToken, TokensPath, tokensID, "", "", url.Values{"name": {"The greenhouse pi"}, "expiry": {"30"}})

	body := fragment(t, rec, tokensID)
	if !strings.Contains(text(body), "Your new token") {
		t.Errorf("the response does not show the new token:\n%s", text(body))
	}
}

func TestTokens_CreatingAndRevokingATokenEachWakeTheJobs(t *testing.T) {
	f := tokenGarden(t)
	woken := 0
	f.handler.wake = countingWake(&woken)

	f.do(t, f.handler.createToken, TokensPath, url.Values{"name": {"The greenhouse pi"}, "expiry": {"30"}})
	if woken != 1 {
		t.Errorf("creating a token woke the jobs %d times, want 1", woken)
	}

	f.remove(t, f.handler.revokeToken, "token", kitchenTokenID, revokeTokenPath(kitchenTokenID))
	if woken != 2 {
		t.Errorf("revoking a token woke the jobs %d times in all, want 2", woken)
	}
}

func TestTokens_ARefusedCreateDoesNotWakeTheJobs(t *testing.T) {
	f := tokenGarden(t)
	woken := 0
	f.handler.wake = countingWake(&woken)

	f.do(t, f.handler.createToken, TokensPath, url.Values{"name": {""}, "expiry": {"30"}})

	if woken != 0 {
		t.Errorf("a create with no name woke the jobs %d times, want 0", woken)
	}
}
