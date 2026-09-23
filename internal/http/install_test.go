package http

import (
	"slices"
	"strings"
	"testing"
)

type platformChip struct {
	value string
	label string
	on    bool
}

// chipsOf returns the platform chips on the Install page, in page order.
func chipsOf(page string) []platformChip {
	var out []platformChip
	for _, button := range readHTML(page).all(isTag("button"), attrIs("name", "platform")) {
		out = append(out, platformChip{value: button.attr("value"), label: button.text(), on: button.attr("aria-pressed") == "true"})
	}
	return out
}

func pressedChip(t *testing.T, page string) platformChip {
	t.Helper()

	for _, c := range chipsOf(page) {
		if c.on {
			return c
		}
	}
	t.Fatalf("no platform is pressed:\n%s", page)
	return platformChip{}
}

func TestInstall_OffersThreePlatformsAndOpensOnTheIPhone(t *testing.T) {
	f := moreGarden(t)

	page := f.page(t, install(testTemplates()), installPath)

	var labels []string
	for _, c := range chipsOf(page) {
		labels = append(labels, c.label)
	}
	if want := []string{"iPhone", "Android", "Computer"}; !slices.Equal(labels, want) {
		t.Errorf("the chips are %v, want %v", labels, want)
	}
	if got := pressedChip(t, page); got.value != "iphone" {
		t.Errorf("the platform pressed is %q, want iphone", got.value)
	}
	if !strings.Contains(text(page), "Add to Home Screen") {
		t.Errorf("the iPhone's steps are not on the page:\n%s", text(page))
	}
}

func TestInstall_ThePlatformInTheQueryStringIsTheOneShown(t *testing.T) {
	f := moreGarden(t)

	page := f.page(t, install(testTemplates()), installPath+"?platform=android")

	if got := pressedChip(t, page); got.value != "android" {
		t.Errorf("the platform pressed is %q, want android", got.value)
	}
	var steps []string
	for _, step := range readHTML(page).first(isTag("ol")).all(isTag("li")) {
		steps = append(steps, step.text())
	}
	if len(steps) != 3 || !strings.Contains(steps[0], "Chrome") {
		t.Errorf("the steps shown are %v, want Android's three", steps)
	}
}

func TestInstall_APlatformTheChipsDoNotOfferFallsBackToTheIPhone(t *testing.T) {
	f := moreGarden(t)

	page := f.page(t, install(testTemplates()), installPath+"?platform=blackberry")

	if got := pressedChip(t, page); got.value != "iphone" {
		t.Errorf("the platform pressed is %q, want iphone", got.value)
	}
}

func TestInstall_HasTheTabBar(t *testing.T) {
	f := moreGarden(t)

	page := f.page(t, install(testTemplates()), installPath)

	if readHTML(page).first(isTag("nav")) == nil {
		t.Error("the page has no tab bar, and it is reached from More")
	}
}
