package store

// A care type's slug is a path segment in a URL, so it is cut to 32 characters
// rather than left as long as the name.
const maxCareTypeSlugLength = 32

// CareTypeSlug is the slug written with a new care type. Code refers to a care
// type by slug and events refer to its row, so it is generated once here and a
// rename leaves it alone. It is empty for a name with no letter or digit in
// it, and the Garden page refuses that name rather than storing a care type
// nothing can refer to.
func CareTypeSlug(name string) string {
	return slugify(name, maxCareTypeSlugLength)
}
