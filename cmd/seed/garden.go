package main

import (
	"fmt"
	"time"
	"uuid"

	engine "github.com/ismailshak/sprig/internal/schedule"
)

// The tables the seed writes identifiers into. A seeded identifier is written
// rather than generated, so that two runs produce the same rows and a test can
// name one. The shape stays a well-formed version 7 UUID, with the timestamp
// replaced by a table number and a counter that sorts in the order it was
// written.
const (
	tableGarden = iota + 1
	tableUser
	tableMembership
	tableCareType
	tablePlant
	tableCareSchedule
	tableCareEvent
)

func seedID(table, n int) uuid.UUID {
	return uuid.MustParse(fmt.Sprintf("00000000-0000-7000-8000-%02d%010d", table, n))
}

type person struct {
	id       uuid.UUID
	name     string
	handle   string
	timezone string
	// Days before the reference the account was created.
	daysOld int
}

// Robin is on a different timezone from the other two, so a query or a digest
// that applied one zone to everybody has somewhere to be wrong.
var (
	ellie = person{id: seedID(tableUser, 1), name: "Ellie", handle: "ellie", timezone: "Europe/London", daysOld: 731}
	sam   = person{id: seedID(tableUser, 2), name: "Sam", handle: "sam", timezone: "Europe/London", daysOld: 700}
	robin = person{id: seedID(tableUser, 3), name: "Robin", handle: "robin", timezone: "Europe/Lisbon", daysOld: 366}
)

type membership struct {
	id     uuid.UUID
	person *person
	role   string
	// Nobody invited the person who set the garden up.
	invitedBy *person
	daysOld   int
	// Days after the reference the arrangement runs out. Zero is the ordinary
	// permanent membership.
	expiresInDays int
}

type careType struct {
	id   uuid.UUID
	name string
	slug string
	// Set on a type that was tried and turned off. A type with events against it
	// can only be archived, which takes it out of the scheduler and leaves its
	// history readable.
	archivedDaysAgo int
}

// A schedule is one of the three shapes the schema allows, told apart here the
// same way the schema tells them apart: by which fields are set.
//
//	cadence   an interval and no anchor      every ten days, counted from the last one
//	anchored  an interval and an anchor      every year, on the first of May
//	one-off   an anchor and no interval      in March, and then we will see
type schedule struct {
	id   uuid.UUID
	slug string

	// A cadence and an anchored schedule repeat, and a one-off leaves these zero.
	count int
	unit  string

	// Days from the reference at which a cadence next falls due, and what the
	// history is worked back from. Negative is overdue.
	dueIn int

	// Inclusive, and it wraps: March to September is 3 and 9, and November to
	// February would be 11 and 2. Zero is a schedule that runs all year.
	seasonStart, seasonEnd int

	// Where an anchored series starts, placed relative to the reference year so
	// the fixture keeps its position in the calendar whenever the seed runs. A
	// zero day is the month precision the schema allows, because writing
	// "sometime in March" as the first would invent an accuracy nobody gave.
	anchorMonth time.Month
	anchorDay   int
	anchorYear  int

	// Days before the reference at which the schedule was set. Zero places it a
	// day after the plant arrived. A one-off sets a value, because care
	// performed after set_at completes it.
	setDaysAgo int
}

// An event no cadence produced, because the care it records repeats on no
// schedule.
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

	// Set on a plant that is no longer in the garden. A dead plant's history is
	// the record of what happened to it, so a plant is archived and never
	// deleted.
	archivedDaysAgo int

	schedules   []schedule
	extraEvents []extraEvent
}

// The interface leads with whichever of the three names is present first.
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

	// The owner first. The log attributes an event to one of the first two, so a
	// garden's history is not entirely its owner's.
	members   []membership
	careTypes []careType
	plants    []plant
}

// home is the garden the prototype draws, carrying its plants, rooms, names,
// people, grades and schedules.
//
// A plant's schedules are the whole of its state. The history behind them is
// derived rather than written out here, because the app computes a next
// occurrence as the last event plus the interval, and a log written by hand
// would let Activity show a watering the roster's due dates disagreed with.
func home() garden {
	return garden{
		id:      seedID(tableGarden, 1),
		name:    "Home",
		daysOld: 730,
		members: []membership{
			{id: seedID(tableMembership, 1), person: &ellie, role: "owner", daysOld: 730},
			{id: seedID(tableMembership, 2), person: &sam, role: "member", invitedBy: &ellie, daysOld: 700},
		},
		careTypes: []careType{
			{id: seedID(tableCareType, 1), name: "Water", slug: "water"},
			{id: seedID(tableCareType, 2), name: "Feed", slug: "feed"},
			{id: seedID(tableCareType, 3), name: "Repot", slug: "repot"},
			// Tried, turned off, and still carrying the events that make it
			// impossible to delete.
			{id: seedID(tableCareType, 4), name: "Mist", slug: "mist", archivedDaysAgo: 10},
		},
		plants: append(livingPlants(), archivedPlants()...),
	}
}

// The interval a schedule is written with is the one a person would have
// entered rather than the number of days it comes to, because the schedule
// editor shows the count and the unit back. Three weeks and twenty-one days are
// the same length, and "every 21 days" is not what somebody chose.
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
				// Nobody knows how often this plant wants repotting. The last
				// one was recent and the next is a job for the spring after
				// next, which is why an interval is not required. The schedule
				// was set the day after that repot, so the repot leaves it
				// standing.
				{id: seedID(tableCareSchedule, 3), slug: "repot", anchorMonth: time.March, anchorYear: 2, setDaysAgo: 2},
			},
			// The repot that was done rather than the one pencilled in. A care
			// type whose only schedule is a one-off still has history behind it,
			// and Activity has to show it.
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
			// The misting somebody tried for a month and gave up on. The type
			// was archived afterwards, so these are events with no schedule
			// above them.
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
			// No nickname, so the common name leads the interface.
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
				// cactus is fed once, in May, and feeding it late in June should
				// not drag next year to June.
				{id: seedID(tableCareSchedule, 17), slug: "feed", count: 1, unit: engine.UnitYear, anchorMonth: time.May, anchorDay: 1},
			},
		},
		{
			// Only a botanical name, which leads the interface and is set in
			// italic wherever it does.
			id:            seedID(tablePlant, 11),
			botanicalName: "Opuntia microdasys",
			location:      "Windowsill",
			schedules: []schedule{
				{id: seedID(tableCareSchedule, 18), slug: "water", count: 5, unit: engine.UnitWeek, dueIn: 19},
			},
		},
		{
			// A nickname and nothing else. Every other field is optional, and a
			// plant missing all of them still has to render as a page.
			id:       seedID(tablePlant, 12),
			nickname: "Sprout",
			schedules: []schedule{
				{id: seedID(tableCareSchedule, 19), slug: "water", count: 11, unit: engine.UnitDay, dueIn: 10},
			},
		},
	}
}

// The three plants the roster counts as archived. They carry no schedules and
// no history, because an archived plant is here to be a row every list leaves
// out, and care invented for a plant the prototype never described is fixture
// nobody can check.
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

// upstairs is the second garden, and it is not there to be looked at. With one
// garden in the database a query that lost its WHERE returns the right answer
// anyway, so this is the cheap way to make that failure show up in a test. Sam
// holds a membership in both, so an account with more than one is the ordinary
// case rather than one nothing exercises.
func upstairs() garden {
	return garden{
		id:      seedID(tableGarden, 2),
		name:    "Upstairs",
		daysOld: 365,
		members: []membership{
			{id: seedID(tableMembership, 3), person: &robin, role: "owner", daysOld: 365},
			// The sitter who came for a fortnight, which is what a nullable
			// expires_at is for.
			{id: seedID(tableMembership, 4), person: &sam, role: "sitter", invitedBy: &robin, daysOld: 60, expiresInDays: 12},
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
