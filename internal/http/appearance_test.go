package http

import (
	"slices"
	"strings"
	"testing"
)

func TestAppearance_OffersLightDarkAndSystemWithSystemChecked(t *testing.T) {
	f := moreGarden(t)

	page := f.page(t, appearance(testTemplates()), appearancePath)

	var values, checked []string
	for _, radio := range readHTML(page).all(isTag("input"), attrIs("name", "mode")) {
		values = append(values, radio.attr("value"))
		if radio.has("checked") {
			checked = append(checked, radio.attr("value"))
		}
		// The page's script enables the radios. Without it they stay disabled.
		if !radio.has("disabled") {
			t.Errorf("the %s radio is rendered enabled, want disabled", radio.attr("value"))
		}
	}
	if want := []string{"light", "dark", "system"}; !slices.Equal(values, want) {
		t.Errorf("the radios are %v, want %v", values, want)
	}
	if want := []string{"system"}; !slices.Equal(checked, want) {
		t.Errorf("the checked radios are %v, want %v, because without a script the page follows the system", checked, want)
	}
	if !strings.Contains(text(page), "Without JavaScript, sprig follows your device’s light or dark setting.") {
		t.Errorf("the page does not say what happens without JavaScript:\n%s", text(page))
	}
}
