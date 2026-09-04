package store

// DisplayName is the first of the plant's nickname, common name and botanical
// name that is set. The plant form holds the rule that one of the three is set,
// so an empty result is a row written some other way.
func (p Plant) DisplayName() string {
	for _, name := range []*string{p.Nickname, p.CommonName, p.BotanicalName} {
		if name != nil && *name != "" {
			return *name
		}
	}
	return ""
}
