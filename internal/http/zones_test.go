package http

import (
	"bytes"
	"html/template"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ismailshak/sprig/web"
)

func TestZones_EveryZoneOfferedIsOneTheZoneDatabaseCanLoad(t *testing.T) {
	for _, zone := range zones {
		if _, err := time.LoadLocation(zone); err != nil {
			t.Errorf("%s: %v", zone, err)
		}
	}
}

// A wrong alias would pre-select the wrong zone for everybody whose browser
// reports it. Two names are the same zone when their offsets agree all year,
// so the check samples every month rather than one date.
func TestZones_EachICUAliasIsTheSameZoneAsTheOneItIsListedUnder(t *testing.T) {
	for listed, alias := range zoneAlias {
		if !slices.Contains(zones, listed) {
			t.Errorf("%s has an alias but is not offered", listed)
			continue
		}
		want, err := time.LoadLocation(listed)
		if err != nil {
			t.Fatalf("%s: %v", listed, err)
		}
		got, err := time.LoadLocation(alias)
		if err != nil {
			t.Errorf("%s: %v", alias, err)
			continue
		}
		for month := time.January; month <= time.December; month++ {
			at := time.Date(2026, month, 15, 12, 0, 0, 0, time.UTC)
			_, wantOffset := at.In(want).Zone()
			_, gotOffset := at.In(got).Zone()
			if gotOffset != wantOffset {
				t.Errorf("%s is %d seconds from UTC in %s and %s is %d", alias, gotOffset, month, listed, wantOffset)
			}
		}
	}
}

func TestZoneOptions_WithNoZoneHeldTheFirstOptionIsAnEmptyOneLabelledTimezone(t *testing.T) {
	options := zoneOptions("")

	if first := options[0]; first.Value != "" || first.Label != "Timezone" || !first.On {
		t.Errorf("the first option is %+v, want an empty value labelled Timezone and selected", first)
	}
	for _, option := range options[1:] {
		if option.On {
			t.Errorf("%s is selected as well, want only the empty option selected", option.Value)
		}
	}
}

func TestZoneOptions_AHeldZoneIsSelectedAndTheEmptyOptionIsGone(t *testing.T) {
	options := zoneOptions("Asia/Tokyo")

	if first := options[0]; first.Value == "" {
		t.Errorf("the first option is %+v, want a zone", first)
	}
	var selected []string
	for _, option := range options {
		if option.On {
			selected = append(selected, option.Value)
		}
	}
	if len(selected) != 1 || selected[0] != "Asia/Tokyo" {
		t.Errorf("the options selected are %v, want Asia/Tokyo alone", selected)
	}
}

func TestZoneOptions_AHeldZoneTheListDoesNotHaveIsOfferedLastAndSelected(t *testing.T) {
	options := zoneOptions("Europe/Belfast")
	last := options[len(options)-1]
	if last.Value != "Europe/Belfast" || !last.On {
		t.Errorf("the last option is %+v, want Europe/Belfast selected", last)
	}
}

func TestZoneOptions_AZoneICUNamesDifferentlyHasThatNameAsItsAlias(t *testing.T) {
	for _, option := range zoneOptions("") {
		if option.Value == "Asia/Kolkata" && option.Also != "Asia/Calcutta" {
			t.Errorf("Asia/Kolkata's alias is %q, want Asia/Calcutta", option.Also)
		}
	}
}

func renderTimezoneField(t *testing.T, field timezoneField) string {
	t.Helper()
	set, err := template.ParseFS(web.Templates, "partials/*.html")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := set.ExecuteTemplate(&buf, "timezone-field", field); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestTimezoneField_TheSelectOnAFormNobodyHasAnsweredIsMarkedDataPropose(t *testing.T) {
	markup := renderTimezoneField(t, timezoneField{Zones: zoneOptions(""), Propose: true})

	if want := `<select class="input" id="timezone" name="timezone" data-propose>`; !strings.Contains(markup, want) {
		t.Errorf("the select does not read %s, and the script proposes a zone on no other select", want)
	}
}

func TestTimezoneField_TheSelectOnAFormNobodyHasAnsweredOpensOnAnEmptyOptionLabelledTimezone(t *testing.T) {
	markup := renderTimezoneField(t, timezoneField{Zones: zoneOptions(""), Propose: true})

	if want := `<option value="" selected>Timezone</option>`; !strings.Contains(markup, want) {
		t.Errorf("the select does not open on %s, so a form with no script posts the first zone in the list", want)
	}
}

func TestTimezoneField_AZoneICUNamesDifferentlyRendersThatNameInDataAlso(t *testing.T) {
	markup := renderTimezoneField(t, timezoneField{Zones: zoneOptions("")})

	if want := `<option value="Asia/Kolkata" data-also="Asia/Calcutta">`; !strings.Contains(markup, want) {
		t.Errorf("no option reads %s, and Chrome and Safari report Asia/Calcutta", want)
	}
}
