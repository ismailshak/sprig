package store

// DisplayName returns the first of nickname, common name and botanical name
// that is set. The plant form requires one of the three, so an empty result
// means the row was written some other way.
func (p Plant) DisplayName() string {
	for _, name := range []*string{p.Nickname, p.CommonName, p.BotanicalName} {
		if isSet(name) {
			return *name
		}
	}
	return ""
}

// BotanicalOnly reports whether the botanical name is the only name the plant
// has. Pages show a botanical name in italics when it is the heading.
func (p Plant) BotanicalOnly() bool {
	return !isSet(p.Nickname) && !isSet(p.CommonName) && isSet(p.BotanicalName)
}

func isSet(name *string) bool {
	return name != nil && *name != ""
}

// OtherName returns the plant's second name, the one DisplayName did not use.
// The common name is preferred over the botanical name. It is empty for a
// plant with only one name. botanical is true when the returned name is the
// botanical one.
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
