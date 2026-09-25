package auth

// Capability is the name of one action a role may take, spelled as the
// capability table stores it.
type Capability string

// The capability table holds one row per constant below.
const (
	PlantCreate     Capability = "plant.create"
	PlantEdit       Capability = "plant.edit"
	PlantArchive    Capability = "plant.archive"
	ScheduleEdit    Capability = "schedule.edit"
	CareLog         Capability = "care.log"
	CareEditOwn     Capability = "care.edit_own"
	CareDeleteOwn   Capability = "care.delete_own"
	CareEditAny     Capability = "care.edit_any"
	CareDeleteAny   Capability = "care.delete_any"
	PhotoAdd        Capability = "photo.add"
	PhotoSetProfile Capability = "photo.set_profile"
	PhotoDeleteOwn  Capability = "photo.delete_own"
	PhotoDeleteAny  Capability = "photo.delete_any"
	GardenEdit      Capability = "garden.edit"
	GardenDelete    Capability = "garden.delete"
	CareTypeManage  Capability = "care_type.manage"
	MemberInvite    Capability = "member.invite"
	MemberManage    Capability = "member.manage"
	TokenManage     Capability = "token.manage"
	// SittingView shows on the calendar the days of access of every member
	// with an end date. Without it a person sees only their own.
	SittingView Capability = "sitting.view"
)

// A test compares allCapabilities against the capability table row for row.
var allCapabilities = []Capability{
	PlantCreate, PlantEdit, PlantArchive, ScheduleEdit,
	CareLog, CareEditOwn, CareDeleteOwn, CareEditAny, CareDeleteAny,
	PhotoAdd, PhotoSetProfile, PhotoDeleteOwn, PhotoDeleteAny,
	GardenEdit, GardenDelete, CareTypeManage, MemberInvite, MemberManage, TokenManage, SittingView,
}

// Capabilities is the set of capabilities a role grants. A nil set grants nothing.
type Capabilities map[Capability]bool

// NewCapabilities builds a set from capability names.
func NewCapabilities(names []string) Capabilities {
	set := make(Capabilities, len(names))
	for _, name := range names {
		set[Capability(name)] = true
	}
	return set
}
