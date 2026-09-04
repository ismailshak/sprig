package store

// DisplayName is the first of the plant's nickname, common name and botanical
// name that is set. The plant form holds the rule that one of the three is set,
// so an empty result is a row written some other way.
func (p Plant) DisplayName() string {
	for _, name := range []*string{p.Nickname, p.CommonName, p.BotanicalName} {
		if isSet(name) {
			return *name
		}
	}
	return ""
}

// BotanicalOnly reports whether the botanical name is the only one the plant
// has, which is when it leads the interface and is set in italic.
func (p Plant) BotanicalOnly() bool {
	return !isSet(p.Nickname) && !isSet(p.CommonName) && isSet(p.BotanicalName)
}

func isSet(name *string) bool {
	return name != nil && *name != ""
}
