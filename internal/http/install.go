package http

import "net/http"

// installPlatform is one device's chip label, install steps and the note under
// them.
type installPlatform struct {
	value string
	label string
	steps []string
	// why is the note under the steps. On iPhone an installed app is the only
	// way to get notifications. On Android and desktop it is a preference.
	why string
}

// iPhonePlatform is the iPhone's steps and note, rendered by the Install page
// and the Reminders page. On an iPhone an installed app is the only way to get
// notifications.
var iPhonePlatform = installPlatform{
	value: "iphone",
	label: "iPhone",
	steps: []string{
		"Open sprig in Safari. Other iOS browsers can’t install it.",
		"Tap the Share button at the bottom of the screen.",
		"Scroll down and choose Add to Home Screen.",
		"Open sprig from your Home Screen from now on.",
	},
	why: "On iPhone, notifications only work from the installed app.",
}

// platforms is the three sets of steps, in the order the chips offer them. The
// platform is chosen on the page rather than read off the User-Agent, because
// this URL is often sent in a message and opened on a device other than the one
// being set up.
var platforms = []installPlatform{
	iPhonePlatform,
	{
		value: "android",
		label: "Android",
		steps: []string{
			"Open sprig in Chrome.",
			"Tap the three dots at the top right.",
			"Choose Install app, or Add to Home screen.",
		},
		why: "On Android, notifications work in the browser too. Installing adds an app icon and a full-screen view.",
	},
	{
		value: "desktop",
		label: "Computer",
		steps: []string{
			"In Chrome or Edge, click the install icon at the right of the address bar.",
			"In Safari on a Mac, choose File, then Add to Dock.",
		},
		why: "Optional. A browser tab works just as well.",
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

// install renders the Install sprig page with the steps for the platform in
// the query string. Any other value, or none, gets the iPhone's steps.
func install(templates *Templates) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		templates.render(w, r, view{page: "install"}, newInstallPage(r.URL.Query().Get("platform")))
	}
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
		page.Steps, page.Why = iPhonePlatform.steps, iPhonePlatform.why
	}
	return page
}
