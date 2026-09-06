package http

import (
	"net/http"
	"net/url"
	"regexp"
	"testing"
)

var (
	nameValue     = regexp.MustCompile(`<input class="input" id="name" name="name" type="text" value="([^"]*)">`)
	zoneOption    = regexp.MustCompile(`<option value="([^"]+)"( selected)?>([^<]+)</option>`)
	fieldErrorRow = regexp.MustCompile(`<p class="field__error">(.*?)</p>`)
)

type zoneChoice struct {
	value string
	label string
	on    bool
}

func zoneOptionsOf(page string) []zoneChoice {
	var out []zoneChoice
	for _, m := range zoneOption.FindAllStringSubmatch(page, -1) {
		out = append(out, zoneChoice{value: m[1], label: m[3], on: m[2] != ""})
	}
	return out
}

func selectedZone(t *testing.T, page string) zoneChoice {
	t.Helper()

	for _, zone := range zoneOptionsOf(page) {
		if zone.on {
			return zone
		}
	}
	t.Fatalf("no timezone is selected:\n%s", page)
	return zoneChoice{}
}

func TestAccount_TheFormOpensOnTheNameAndZoneTheAccountHolds(t *testing.T) {
	f := moreGarden(t)

	page := f.page(t, f.handler.account, accountPath)

	if got := nameValue.FindStringSubmatch(page); got == nil || got[1] != "Ellie" {
		t.Errorf("the display name field holds %v, want Ellie", got)
	}
	if got := selectedZone(t, page); got.value != "Europe/London" {
		t.Errorf("the zone selected is %q, want Europe/London", got.value)
	}
}

func TestAccount_AZoneTheSelectDoesNotListIsStillOfferedAndSelected(t *testing.T) {
	f := moreGarden(t)
	f.principal.User.Timezone = "Europe/Lisbon"

	page := f.page(t, f.handler.account, accountPath)

	if got := selectedZone(t, page); got.value != "Europe/Lisbon" {
		t.Errorf("the zone selected is %q, want Europe/Lisbon", got.value)
	}
}

func TestAccount_AZoneReadsWithoutTheUnderscore(t *testing.T) {
	f := moreGarden(t)

	page := f.page(t, f.handler.account, accountPath)

	for _, zone := range zoneOptionsOf(page) {
		if zone.value == "America/New_York" && zone.label != "America/New York" {
			t.Errorf("America/New_York reads %q, want %q", zone.label, "America/New York")
		}
	}
}

func TestAccount_SavingWritesTheNameAndTheZone(t *testing.T) {
	f := moreGarden(t)

	rec := f.do(t, f.handler.saveAccount, accountPath, url.Values{"name": {"  Eleanor  "}, "timezone": {"Asia/Tokyo"}})

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != accountPath {
		t.Fatalf("status = %d to %q, want %d to %s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, accountPath)
	}
	var name, zone string
	if err := f.tx.QueryRow(t.Context(), "SELECT display_name, timezone FROM app_user WHERE id = $1", moreUserID).Scan(&name, &zone); err != nil {
		t.Fatalf("reading the account back: %v", err)
	}
	if name != "Eleanor" || zone != "Asia/Tokyo" {
		t.Errorf("the account holds %q in %q, want %q in %q", name, zone, "Eleanor", "Asia/Tokyo")
	}
}

func TestAccount_AnEmptyDisplayNameIsRefusedWithTheReasonUnderTheField(t *testing.T) {
	f := moreGarden(t)

	rec := f.do(t, f.handler.saveAccount, accountPath, url.Values{"name": {"   "}, "timezone": {"Asia/Tokyo"}})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if got := fieldErrorRow.FindStringSubmatch(rec.Body.String()); got == nil || text(got[1]) != nameMissing {
		t.Errorf("the form says %v, want %q", got, nameMissing)
	}
	var name string
	if err := f.tx.QueryRow(t.Context(), "SELECT display_name FROM app_user WHERE id = $1", moreUserID).Scan(&name); err != nil {
		t.Fatalf("reading the account back: %v", err)
	}
	if name != "Ellie" {
		t.Errorf("the refused post left the name as %q, want Ellie", name)
	}
}

func TestAccount_TheRefusedFormComesBackWithWhatWasTyped(t *testing.T) {
	f := moreGarden(t)

	rec := f.do(t, f.handler.saveAccount, accountPath, url.Values{"name": {""}, "timezone": {"Asia/Tokyo"}})

	if got := selectedZone(t, rec.Body.String()); got.value != "Asia/Tokyo" {
		t.Errorf("the zone selected is %q, want the posted Asia/Tokyo", got.value)
	}
}

func TestAccount_AZoneTheSelectDidNotOfferIsRefused(t *testing.T) {
	f := moreGarden(t)

	rec := f.do(t, f.handler.saveAccount, accountPath, url.Values{"name": {"Eleanor"}, "timezone": {"Mars/Olympus_Mons"}})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	var name, zone string
	if err := f.tx.QueryRow(t.Context(), "SELECT display_name, timezone FROM app_user WHERE id = $1", moreUserID).Scan(&name, &zone); err != nil {
		t.Fatalf("reading the account back: %v", err)
	}
	if name != "Ellie" || zone != "Europe/London" {
		t.Errorf("the refused post left the account as %q in %q, want Ellie in Europe/London", name, zone)
	}
}
