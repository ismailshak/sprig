package http

// careIcons are the icons a care type the garden added can be given, in the
// order the Icon field on the Garden page lists them. Value is what
// care_type.icon holds. Label is the radio's accessible name. The Feed and
// Repot icons are left out so that those two care types keep icons no other
// type has.
var careIcons = []option{
	{Value: "water", Label: "Water drop"},
	{Value: "prune", Label: "Shears"},
	{Value: "mist", Label: "Spray bottle"},
	{Value: "rotate", Label: "Turning arrow"},
	{Value: "pest", Label: "Bug"},
	{Value: "clean", Label: "Leaf with sparkle"},
	{Value: "light", Label: "Sun"},
	{Value: "photo", Label: "Camera"},
	{Value: "temperature", Label: "Thermometer"},
	{Value: "inspect", Label: "Magnifying glass"},
	{Value: "harvest", Label: "Basket"},
	{Value: "propagate", Label: "Cutting in a jar"},
	{Value: "checkmark", Label: "Checkmark"},
	{Value: "pencil", Label: "Pencil"},
	{Value: "star", Label: "Star"},
	{Value: "leaf", Label: "Leaf"},
}

// defaultCareIcon is the icon checked on the row for a new care type. It must
// match the default of the care_type.icon column.
const defaultCareIcon = "water"

// careIconMissing is shown under the Icon field when the posted icon is not
// one of careIcons.
const careIconMissing = "Choose an icon."

// careIconOptions returns the Icon field's radios with chosen checked.
func careIconOptions(chosen string) []option {
	options := make([]option, len(careIcons))
	for i, icon := range careIcons {
		options[i] = option{Value: icon.Value, Label: icon.Label, On: icon.Value == chosen}
	}
	return options
}

func knownCareIcon(name string) bool {
	for _, icon := range careIcons {
		if icon.Value == name {
			return true
		}
	}
	return false
}

// seededIcon returns the icon of the care type in seededCareTypes with this
// slug. It returns false when none has it.
func seededIcon(slug string) (string, bool) {
	for _, care := range seededCareTypes {
		if care.slug == slug {
			return care.icon, true
		}
	}
	return "", false
}
