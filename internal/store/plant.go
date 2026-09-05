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

// OtherName is whichever of the plant's names DisplayName did not use, the
// common one ahead of the botanical one, and empty on a plant down to a single
// name.
func (p Plant) OtherName() (name string, botanical bool) {
	display := p.DisplayName()
	if isSet(p.CommonName) && *p.CommonName != display {
		return *p.CommonName, false
	}
	if isSet(p.BotanicalName) && *p.BotanicalName != display {
		return *p.BotanicalName, true
	}
	return "", false
}
