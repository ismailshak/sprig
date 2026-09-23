package http

import "net/http"

// appearancePage is the Appearance page: three radio buttons for light, dark
// and the system setting. The page's script stores the choice in the browser.
// No handler reads or writes it.
type appearancePage struct {
	Bar   topbar
	Modes []appearanceMode
}

// appearanceMode is one of the three radio buttons. Value is the string the
// page's script stores in the browser. On is set on System, because a page
// with no script running follows the system setting.
type appearanceMode struct {
	Value string
	Label string
	Note  string
	On    bool
}

func appearance(templates *Templates) http.HandlerFunc {
	page := appearancePage{
		Bar: moreBar("Appearance"),
		Modes: []appearanceMode{
			{Value: "light", Label: "Light"},
			{Value: "dark", Label: "Dark"},
			{Value: "system", Label: "System", Note: "Follows your device’s setting.", On: true},
		},
	}
	return func(w http.ResponseWriter, r *http.Request) {
		templates.render(w, r, view{page: "appearance"}, page)
	}
}
