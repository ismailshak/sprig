package http

import "net/http"

// platforms is the three sets of steps, in the order the chips offer them. The
// platform is chosen on the page rather than read off the User-Agent, because
// this URL is often sent in a message and opened on a device other than the one
// being set up.
var platforms = []struct {
	value string
	label string
	steps []string
	// why is the note under the steps. On iPhone an installed app is the only
	// way to get notifications. On Android and desktop it is a preference.
	why string
}{
	{
		value: "iphone",
		label: "iPhone",
		steps: []string{
			"Open sprig in Safari. Other iOS browsers can’t install it.",
			"Tap the Share button at the bottom of the screen.",
			"Scroll down and choose Add to Home Screen.",
			"Open sprig from your Home Screen from now on.",
		},
		why: "On iPhone, notifications only work from the installed app.",
	},
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

// afterInvite is the value of the after query parameter that /install takes
// when it is reached at the end of redeeming an invite.
const afterInvite = "invite"

type installPage struct {
	Bar topbar
	// Action is the URL the chips submit to. It is this page, with the chosen
	// platform in the query string.
	Action string
	Chips  []chip
	Steps  []string
	Why    string
	// AfterInvite is true when the page is reached at the end of redeeming an
	// invite. It is then rendered with no tab bar and no back link, and ends in
	// the Continue link. The form the chips submit keeps the after parameter in
	// a hidden input, so choosing a platform stays on this version of the page.
	AfterInvite bool
	// Today is the URL the Continue link points at.
	Today string
}

// install renders the steps for the platform in the query string, and the
// iPhone's for anything else, including a first visit with no query string at
// all.
func (h *more) install(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	h.templates.render(w, r, view{page: "install"}, newInstallPage(query.Get("platform"), query.Get("after") == afterInvite))
}

func newInstallPage(chosen string, fromInvite bool) installPage {
	page := installPage{Bar: moreBar("Install sprig"), Action: installPath, AfterInvite: fromInvite, Today: todayPath}
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
