package http

import "net/http"

// platforms is the three sets of steps, in the order the chips offer them. The
// platform is chosen on the page rather than read off the User-Agent, because
// half the time this URL arrives in a message and is read on a device that is
// not the one being set up.
var platforms = []struct {
	value string
	label string
	steps []string
	// why is the note under the steps. On an iPhone an installed app is what
	// makes notifications possible at all; on the other two it is a preference.
	why string
}{
	{
		value: "iphone",
		label: "iPhone",
		steps: []string{
			"Open sprig in Safari. It has to be Safari — other browsers on iOS cannot install it.",
			"Tap the Share button at the bottom of the screen.",
			"Scroll down and choose Add to Home Screen.",
			"Open sprig from the Home Screen from now on.",
		},
		why: "On an iPhone this is what makes notifications possible at all. Until it is done, the switches on the notifications page are not there to turn on.",
	},
	{
		value: "android",
		label: "Android",
		steps: []string{
			"Open sprig in Chrome.",
			"Tap the three dots at the top right.",
			"Choose Install app, or Add to Home screen.",
		},
		why: "Notifications work in the browser on Android either way. Installing gets it its own icon and no address bar.",
	},
	{
		value: "desktop",
		label: "Computer",
		steps: []string{
			"In Chrome or Edge, click the install icon at the right of the address bar.",
			"In Safari on a Mac, choose File, then Add to Dock.",
		},
		why: "Optional. A tab works exactly as well, and this is mostly about not losing it among thirty others.",
	},
}

type installPage struct {
	Bar topbar
	// Action is the URL the chips submit to. It is this page, with the chosen
	// platform in the query string.
	Action string
	Chips  []chip
	Steps  []string
	Why    string
}

// install renders the steps for the platform in the query string, and the
// iPhone's for anything else, including a first visit with no query string at
// all.
func (h *more) install(w http.ResponseWriter, r *http.Request) {
	h.templates.render(w, r, view{page: "install"}, newInstallPage(r.URL.Query().Get("platform")))
}

func newInstallPage(chosen string) installPage {
	page := installPage{Bar: moreBar("Install sprig"), Action: installPath}
	offered := false
	for _, platform := range platforms {
		on := platform.value == chosen
		offered = offered || on
		if on {
			page.Steps, page.Why = platform.steps, platform.why
		}
		page.Chips = append(page.Chips, chip{Value: platform.value, Label: platform.label, On: on})
	}
	// A platform the chips do not offer, and a first visit with no query
	// string, both get the iPhone's steps with its own chip pressed.
	if !offered {
		page.Chips[0].On = true
		page.Steps, page.Why = platforms[0].steps, platforms[0].why
	}
	return page
}
