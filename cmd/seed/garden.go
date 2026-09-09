package main

import (
	"fmt"
	"time"
	"uuid"

	engine "github.com/ismailshak/sprig/internal/schedule"
)

// Table numbers for seedID. Seeded ids are fixed rather than generated, so two
// runs produce the same rows and a test can refer to one by id. Each id is a
// well-formed version 7 UUID whose timestamp bits hold the table number and a
// counter, so ids sort in the order they were written.
const (
	tableGarden = iota + 1
	tableUser
	tableMembership
	tableCareType
	tablePlant
	tableCareSchedule
	tableCareEvent
	tablePasskeyCredential
	tableInvite
	tableRecoveryCode
	tableAPIToken
	tablePushSubscription
)

func seedID(table, n int) uuid.UUID {
	return uuid.MustParse(fmt.Sprintf("00000000-0000-7000-8000-%02d%010d", table, n))
}

type person struct {
	id       uuid.UUID
	name     string
	handle   string
	timezone string
	// daysOld is how many days before the reference date the account was created.
	daysOld int
}

// Robin is in a different timezone from the other two, so a query or digest
// that applied one zone to everybody would give a wrong answer for Robin.
//
// Jo's membership in the Home garden ends in eight days and Clare's ended
// twelve days ago. They give People the two states a membership with no end
// date has not got.
var (
	ellie = person{id: seedID(tableUser, 1), name: "Ellie", handle: "ellie", timezone: "Europe/London", daysOld: 731}
	sam   = person{id: seedID(tableUser, 2), name: "Sam", handle: "sam", timezone: "Europe/London", daysOld: 700}
	robin = person{id: seedID(tableUser, 3), name: "Robin", handle: "robin", timezone: "Europe/Lisbon", daysOld: 366}
	jo    = person{id: seedID(tableUser, 4), name: "Jo", handle: "jo", timezone: "Europe/London", daysOld: 30}
	clare = person{id: seedID(tableUser, 5), name: "Clare", handle: "clare", timezone: "Europe/London", daysOld: 400}
)

type membership struct {
	id     uuid.UUID
	person *person
	role   string
	// invitedBy is nil for the person who created the garden.
	invitedBy *person
	daysOld   int
	// expiresInDays is how many days after the reference date the membership
	// ends. Zero means it is permanent.
	expiresInDays int

	// digestOff turns the daily digest off and activity turns the activity
	// notification on. They are named this way round so that their zero values
	// are what the app gives a new membership: the digest on, activity off.
	digestOff bool
	activity  bool
}

type careType struct {
	id   uuid.UUID
	name string
	slug string
	// archivedDaysAgo is set on a care type that was tried and turned off. A
	// type with events cannot be deleted, only archived, which removes it from
	// scheduling and keeps its history readable.
	archivedDaysAgo int
}

// schedule is one of the three shapes the schema allows, distinguished the same
// way the schema distinguishes them, by which fields are set:
//
//	cadence   an interval and no anchor      every ten days, counted from the last one
//	anchored  an interval and an anchor      every year, on the first of May
//	one-off   an anchor and no interval      in March, and then we will see
type schedule struct {
	id   uuid.UUID
	slug string

	// count and unit are the interval. A one-off leaves both zero.
	count int
	unit  string

	// dueIn is how many days after the reference date a cadence next falls
	// due. The event history is worked backwards from it. Negative means
	// overdue.
	dueIn int

	// seasonStart and seasonEnd are inclusive months and may wrap the year:
	// March to September is 3 and 9, November to February is 11 and 2. Zero
	// means the schedule runs all year.
	seasonStart, seasonEnd int

	// The anchor date of an anchored or one-off schedule. anchorYear is
	// relative to the reference year, so the fixture keeps its place in the
	// calendar whichever year the seed runs. A zero anchorDay means month
	// precision, because storing "sometime in March" as the 1st would invent
	// a precision nobody gave.
	anchorMonth time.Month
	anchorDay   int
	anchorYear  int

	// setDaysAgo is how many days before the reference date the schedule was
	// set. Zero means the day after the plant was created. A one-off sets it
	// explicitly, because care performed after set_at completes the one-off.
	setDaysAgo int
}

// extraEvent is a care event with no schedule behind it.
type extraEvent struct {
	slug    string
	daysAgo int
}

type plant struct {
	id            uuid.UUID
	nickname      string
	commonName    string
	botanicalName string
	location      string
	acquiredYear  int
	acquiredMonth int

	sun        string
	waterNeeds string
	feedNeeds  string
	soil       string
	climate    string
	pot        string
	notes      string

	// archivedDaysAgo is set on a plant no longer in the garden. Plants are
	// archived rather than deleted, so their history stays readable.
	archivedDaysAgo int

	schedules   []schedule
	extraEvents []extraEvent
}

// displayName is the first of nickname, common name and botanical name that is
// set, matching store.Plant.DisplayName.
func (p *plant) displayName() string {
	switch {
	case p.nickname != "":
		return p.nickname
	case p.commonName != "":
		return p.commonName
	default:
		return p.botanicalName
	}
}

type garden struct {
	id      uuid.UUID
	name    string
	daysOld int

	// The owner comes first. history attributes events to the first two
	// members, so a garden's history is not all the owner's.
	members   []membership
	careTypes []careType
	plants    []plant
	invites   []invite
	tokens    []apiToken
}

// home is the garden the prototype shows, with its plants, rooms, names,
// people and schedules.
//
// Only the schedules are written here. The event history is derived from them
// by history, because the app computes the next occurrence as the last event
// plus the interval, and a hand-written log could show a watering on Activity
// that disagreed with the due dates on the plant list.
func home() garden {
	return garden{
		id:      seedID(tableGarden, 1),
		name:    "Home",
		daysOld: 730,
		members: []membership{
			{id: seedID(tableMembership, 1), person: &ellie, role: "owner", daysOld: 730},
			{id: seedID(tableMembership, 2), person: &sam, role: "member", invitedBy: &ellie, daysOld: 700, digestOff: true},
			{id: seedID(tableMembership, 5), person: &jo, role: "sitter", invitedBy: &ellie, daysOld: 20, expiresInDays: 8, digestOff: true},
			// A membership that has already ended. The row stays on People and
			// is greyed, so the date can be moved forward instead of a second
			// invite being issued.
			{id: seedID(tableMembership, 6), person: &clare, role: "sitter", invitedBy: &ellie, daysOld: 400, expiresInDays: -12, digestOff: true},
		},
		careTypes: []careType{
			{id: seedID(tableCareType, 1), name: "Water", slug: "water"},
			{id: seedID(tableCareType, 2), name: "Feed", slug: "feed"},
			{id: seedID(tableCareType, 3), name: "Repot", slug: "repot"},
			// Tried and turned off. It still has events, so it cannot be
			// deleted, only archived.
			{id: seedID(tableCareType, 4), name: "Mist", slug: "mist", archivedDaysAgo: 10},
		},
		plants: append(livingPlants(), archivedPlants()...),
		invites: []invite{
			{id: seedID(tableInvite, 1), token: sitterInviteToken, role: "sitter", createdBy: &ellie, daysOld: 2, expiresInDays: 5},
		},
		// Both tokens have the full 90-day lifetime auth.MaxTokenLifetime allows.
		tokens: []apiToken{
			{id: seedID(tableAPIToken, 1), name: "The kitchen display", token: kitchenDisplayToken, prefix: "sprg_7c1f", createdBy: &ellie, daysOld: 40, usedDaysAgo: 0, expiresInDays: 50},
			{id: seedID(tableAPIToken, 2), name: "The spare display", token: spareDisplayToken, prefix: "sprg_2ea8", createdBy: &ellie, daysOld: 96, usedDaysAgo: 74, expiresInDays: -6},
		},
	}
}

// livingPlants returns the plants in the Home garden that are not archived.
// Each interval is written as a person would have entered it, not as a number
// of days, because the schedule editor shows the count and unit back. Three
// weeks and 21 days are the same length, but "every 21 days" is not what
// somebody chose.
func livingPlants() []plant {
	return []plant{
		{
			id:            seedID(tablePlant, 1),
			nickname:      "Big Fella",
			commonName:    "Swiss cheese plant",
			botanicalName: "Monstera deliciosa",
			location:      "Living room",
			acquiredYear:  2024,
			acquiredMonth: 3,
			sun:           "Bright indirect. The west window scorches him by August.",
			waterNeeds:    "When the top 5cm are dry. Less from October.",
			feedNeeds:     "Half-strength balanced feed, growing season only.",
			soil:          "Peat-free houseplant mix with a third perlite.",
			climate:       "18–27°C. Happier above 50% humidity.",
			pot:           "30cm terracotta, drainage hole, no saucer of standing water.",
			notes:         "Wipe the leaves when they dust over. The aerial roots can go back into the pot or be left alone, but do not cut them off.",
			schedules: []schedule{
				{id: seedID(tableCareSchedule, 1), slug: "water", count: 10, unit: engine.UnitDay, dueIn: -2},
				{id: seedID(tableCareSchedule, 2), slug: "feed", count: 3, unit: engine.UnitWeek, dueIn: 14, seasonStart: 3, seasonEnd: 9},
				// A one-off with no interval: nobody knows how often this plant
				// wants repotting, and the next is planned for the spring after
				// next. The schedule was set the day after the last repot, so
				// that repot does not complete it.
				{id: seedID(tableCareSchedule, 3), slug: "repot", anchorMonth: time.March, anchorYear: 2, setDaysAgo: 2},
			},
			// The repot that was actually done. A care type whose only schedule
			// is a one-off still has history, and Activity has to show it.
			extraEvents: []extraEvent{{slug: "repot", daysAgo: 3}},
		},
		{
			id:            seedID(tablePlant, 2),
			nickname:      "Gerald",
			commonName:    "Fiddle-leaf fig",
			botanicalName: "Ficus lyrata",
			location:      "Living room",
			schedules: []schedule{
				{id: seedID(tableCareSchedule, 4), slug: "water", count: 1, unit: engine.UnitWeek, dueIn: 5},
				{id: seedID(tableCareSchedule, 5), slug: "feed", count: 4, unit: engine.UnitWeek, dueIn: 0, seasonStart: 3, seasonEnd: 9},
			},
		},
		{
			id:            seedID(tablePlant, 3),
			nickname:      "Doris",
			commonName:    "Snake plant",
			botanicalName: "Dracaena trifasciata",
			location:      "Bedroom",
			schedules: []schedule{
				{id: seedID(tableCareSchedule, 6), slug: "water", count: 3, unit: engine.UnitWeek, dueIn: 0},
			},
		},
		{
			id:            seedID(tablePlant, 4),
			nickname:      "Mother-in-Law",
			commonName:    "Snake plant",
			botanicalName: "Dracaena trifasciata",
			location:      "Bedroom",
			schedules: []schedule{
				{id: seedID(tableCareSchedule, 7), slug: "water", count: 24, unit: engine.UnitDay, dueIn: 16},
			},
		},
		{
			id:            seedID(tablePlant, 5),
			nickname:      "Nigel",
			commonName:    "Boston fern",
			botanicalName: "Nephrolepis exaltata",
			location:      "Bathroom",
			acquiredYear:  2025,
			acquiredMonth: 8,
			sun:           "Indirect, and he will take quite a dim corner.",
			waterNeeds:    "Never let him dry out. The tips brown the same week.",
			climate:       "Likes the steam, which is why he lives in the bathroom.",
			notes:         "Trim the brown fronds at the base rather than cutting across them.",
			schedules: []schedule{
				{id: seedID(tableCareSchedule, 8), slug: "water", count: 4, unit: engine.UnitDay, dueIn: 0},
				{id: seedID(tableCareSchedule, 9), slug: "feed", count: 3, unit: engine.UnitWeek, dueIn: 11, seasonStart: 3, seasonEnd: 9},
			},
			// Misting was tried for a month and given up. The care type was
			// archived afterwards, so these events have no schedule.
			extraEvents: repeatedEvent("mist", 12, 5, 8),
		},
		{
			id:            seedID(tablePlant, 6),
			nickname:      "Ferngully",
			commonName:    "Maidenhair fern",
			botanicalName: "Adiantum raddianum",
			location:      "Bathroom",
			schedules: []schedule{
				{id: seedID(tableCareSchedule, 10), slug: "water", count: 9, unit: engine.UnitDay, dueIn: 8},
			},
		},
		{
			id:            seedID(tablePlant, 7),
			nickname:      "Trail Mix",
			commonName:    "Golden pothos",
			botanicalName: "Epipremnum aureum",
			location:      "Kitchen",
			schedules: []schedule{
				{id: seedID(tableCareSchedule, 11), slug: "water", count: 9, unit: engine.UnitDay, dueIn: 1},
				{id: seedID(tableCareSchedule, 12), slug: "feed", count: 1, unit: engine.UnitMonth, dueIn: 19, seasonStart: 3, seasonEnd: 9},
			},
		},
		{
			// No nickname, so the common name is the display name.
			id:            seedID(tablePlant, 8),
			commonName:    "Golden pothos",
			botanicalName: "Epipremnum aureum",
			location:      "Kitchen",
			schedules: []schedule{
				{id: seedID(tableCareSchedule, 13), slug: "water", count: 12, unit: engine.UnitDay, dueIn: 8},
			},
		},
		{
			id:            seedID(tablePlant, 9),
			nickname:      "Little Fella",
			commonName:    "Swiss cheese vine",
			botanicalName: "Monstera adansonii",
			location:      "Kitchen",
			schedules: []schedule{
				{id: seedID(tableCareSchedule, 14), slug: "water", count: 2, unit: engine.UnitWeek, dueIn: 9},
				{id: seedID(tableCareSchedule, 15), slug: "feed", count: 1, unit: engine.UnitMonth, dueIn: 21, seasonStart: 3, seasonEnd: 9},
			},
		},
		{
			id:            seedID(tablePlant, 10),
			nickname:      "Spike",
			commonName:    "Golden barrel cactus",
			botanicalName: "Echinocactus grusonii",
			location:      "Windowsill",
			acquiredYear:  2023,
			acquiredMonth: 1,
			sun:           "As much direct sun as the windowsill gets.",
			waterNeeds:    "Soak, then leave him completely alone.",
			soil:          "Cactus compost with grit.",
			pot:           "Terracotta. Plastic holds too much water.",
			schedules: []schedule{
				{id: seedID(tableCareSchedule, 16), slug: "water", count: 1, unit: engine.UnitMonth, dueIn: 2},
				// Anchored to the calendar rather than to the last feeding. A
				// cactus is fed once a year in May, and feeding it late in June
				// should not move next year's feed to June.
				{id: seedID(tableCareSchedule, 17), slug: "feed", count: 1, unit: engine.UnitYear, anchorMonth: time.May, anchorDay: 1},
			},
		},
		{
			// Only a botanical name. It becomes the display name and is shown
			// in italics.
			id:            seedID(tablePlant, 11),
			botanicalName: "Opuntia microdasys",
			location:      "Windowsill",
			schedules: []schedule{
				{id: seedID(tableCareSchedule, 18), slug: "water", count: 5, unit: engine.UnitWeek, dueIn: 19},
			},
		},
		{
			// A nickname and nothing else. Every other field is optional, and
			// a plant with none of them still has to render as a page.
			id:       seedID(tablePlant, 12),
			nickname: "Sprout",
			schedules: []schedule{
				{id: seedID(tableCareSchedule, 19), slug: "water", count: 11, unit: engine.UnitDay, dueIn: 10},
			},
		},
	}
}

// archivedPlants returns the three archived plants in the Home garden. They
// have no schedules and no history. Plants, Today and the digest leave them
// out, and the Archived plants page lists them.
func archivedPlants() []plant {
	return []plant{
		{
			id:              seedID(tablePlant, 13),
			nickname:        "Barry",
			commonName:      "Peace lily",
			botanicalName:   "Spathiphyllum wallisii",
			location:        "Living room",
			archivedDaysAgo: 120,
		},
		{
			id:              seedID(tablePlant, 14),
			nickname:        "Kev",
			commonName:      "Prayer plant",
			botanicalName:   "Calathea orbifolia",
			location:        "Bathroom",
			archivedDaysAgo: 210,
		},
		{
			id:              seedID(tablePlant, 15),
			commonName:      "Sweet basil",
			botanicalName:   "Ocimum basilicum",
			location:        "Kitchen",
			archivedDaysAgo: 300,
		},
	}
}

// upstairs is the second garden. It exists so that a query missing its garden
// WHERE clause returns visibly wrong rows, which it would not with one garden
// in the database. Sam is a member of both gardens, so an account with more
// than one membership is exercised by default.
func upstairs() garden {
	return garden{
		id:      seedID(tableGarden, 2),
		name:    "Upstairs",
		daysOld: 365,
		members: []membership{
			{id: seedID(tableMembership, 3), person: &robin, role: "owner", daysOld: 365, digestOff: true},
			// A sitter for a fortnight, exercising the nullable expires_at.
			{id: seedID(tableMembership, 4), person: &sam, role: "sitter", invitedBy: &robin, daysOld: 60, expiresInDays: 12, digestOff: true},
		},
		careTypes: []careType{
			{id: seedID(tableCareType, 5), name: "Water", slug: "water"},
			{id: seedID(tableCareType, 6), name: "Feed", slug: "feed"},
		},
		plants: []plant{
			{
				id:            seedID(tablePlant, 16),
				nickname:      "Nero",
				commonName:    "Rubber plant",
				botanicalName: "Ficus elastica",
				location:      "Hallway",
				schedules: []schedule{
					{id: seedID(tableCareSchedule, 20), slug: "water", count: 2, unit: engine.UnitWeek, dueIn: 3},
				},
			},
			{
				id:            seedID(tablePlant, 17),
				commonName:    "Aloe vera",
				botanicalName: "Aloe barbadensis",
				location:      "Kitchen",
				schedules: []schedule{
					{id: seedID(tableCareSchedule, 21), slug: "water", count: 3, unit: engine.UnitWeek, dueIn: -1},
				},
			},
			{
				id:            seedID(tablePlant, 18),
				nickname:      "Bramble",
				commonName:    "Spider plant",
				botanicalName: "Chlorophytum comosum",
				location:      "Study",
				schedules: []schedule{
					{id: seedID(tableCareSchedule, 22), slug: "water", count: 1, unit: engine.UnitWeek, dueIn: 4},
				},
			},
		},
	}
}

func repeatedEvent(slug string, firstDaysAgo, step, count int) []extraEvent {
	out := make([]extraEvent, 0, count)
	for i := range count {
		out = append(out, extraEvent{slug: slug, daysAgo: firstDaysAgo + i*step})
	}
	return out
}
