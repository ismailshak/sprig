package http

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

var (
	platformChipElement = regexp.MustCompile(`<button class="chip" name="platform" value="([^"]+)" aria-pressed="(true|false)">([^<]+)</button>`)
	installStep         = regexp.MustCompile(`<li>([^<]+)</li>`)
)

type platformChip struct {
	value string
	label string
	on    bool
}

func chipsOf(page string) []platformChip {
	var out []platformChip
	for _, m := range platformChipElement.FindAllStringSubmatch(page, -1) {
		out = append(out, platformChip{value: m[1], label: text(m[3]), on: m[2] == "true"})
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

	page := f.page(t, f.handler.install, installPath)

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

	page := f.page(t, f.handler.install, installPath+"?platform=android")

	if got := pressedChip(t, page); got.value != "android" {
		t.Errorf("the platform pressed is %q, want android", got.value)
	}
	steps := installStep.FindAllStringSubmatch(page, -1)
	if len(steps) != 3 || !strings.Contains(text(steps[0][1]), "Chrome") {
		t.Errorf("the steps shown are %v, want Android's three", steps)
	}
}

func TestInstall_APlatformTheChipsDoNotOfferFallsBackToTheIPhone(t *testing.T) {
	f := moreGarden(t)

	page := f.page(t, f.handler.install, installPath+"?platform=blackberry")

	if got := pressedChip(t, page); got.value != "iphone" {
		t.Errorf("the platform pressed is %q, want iphone", got.value)
	}
}

func TestInstall_AfterAnInviteHasNoTabBarAndNoBackLinkAndEndsInALinkIntoTheGarden(t *testing.T) {
	f := moreGarden(t)

	page := f.page(t, f.handler.install, afterInvitePath)

	if strings.Contains(page, "nav__item") {
		t.Error("the page renders the tab bar, and there is no garden open yet")
	}
	if strings.Contains(page, `class="backlink"`) {
		t.Error("the page has a back link, and it was not reached from More")
	}
	if !strings.Contains(page, `href="/">Continue<`) {
		t.Errorf("the page does not end in a link into the garden:\n%s", page)
	}
	if !strings.Contains(page, `<input type="hidden" name="after" value="invite">`) {
		t.Error("the chips drop the after parameter, so choosing a platform would bring the tab bar back")
	}
	if got := pressedChip(t, page); got.value != "iphone" {
		t.Errorf("the platform pressed is %q, want iphone", got.value)
	}
}

func TestInstall_FromMoreHasTheTabBarAndNoLinkIntoTheGarden(t *testing.T) {
	f := moreGarden(t)

	page := f.page(t, f.handler.install, installPath)

	if !strings.Contains(page, "nav__item") {
		t.Error("the page has no tab bar, and it was reached from More")
	}
	if strings.Contains(page, "Go to the garden") {
		t.Error("the page ends in a link into the garden, and the tab bar already opens it")
	}
}
