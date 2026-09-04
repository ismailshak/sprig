// Command seed writes the prototype's garden into the development database.
//
// Every timestamp it writes is an offset from one reference instant, the start
// of today in the garden's own timezone, computed once per run. Two runs on the
// same day write identical rows, and the plants fall into the same three
// sections of Today whatever day the seed runs on, which the e2e suite needs
// because it seeds from empty every time.
//
// A seasonal schedule moves with the calendar instead. Feeding is shut between
// October and February, so a garden seeded in winter has fewer plants due than
// the prototype shows.
//
// Running it again replaces what it wrote last time and leaves everything else
// in the database alone.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"time"
	"uuid"

	// The reference instant is a local midnight, so zone lookup has to work on
	// a machine carrying no zoneinfo database.
	_ "time/tzdata"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ismailshak/sprig/db"
	"github.com/ismailshak/sprig/internal/store"
)

func main() {
	if err := run(context.Background(), os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	databaseURL := getenv("SPRIG_DATABASE_URL")
	if databaseURL == "" {
		return errors.New("SPRIG_DATABASE_URL is required")
	}
	if err := checkIsLocal(databaseURL); err != nil {
		return err
	}

	ref, err := reference(time.Now())
	if err != nil {
		return err
	}

	pool, err := store.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	logger := slog.New(slog.NewTextHandler(stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	// The seed runs against a database the server may never have opened, so it
	// migrates the schema itself.
	if err := store.Migrate(ctx, pool, db.Migrations, logger); err != nil {
		return err
	}

	written, err := seed(ctx, pool, ref)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(stdout, "seeded %d gardens, %d plants and %d care events as of %s\n",
		written.gardens, written.plants, written.events, ref.Format(time.DateOnly))
	return err
}

// checkIsLocal refuses a database that is neither on this machine nor in the
// compose stack, because the seed deletes rows before it writes them.
func checkIsLocal(databaseURL string) error {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return errors.New("SPRIG_DATABASE_URL is not a URL")
	}
	switch parsed.Hostname() {
	case "localhost", "127.0.0.1", "::1", "db":
		return nil
	default:
		return fmt.Errorf("refusing to seed %q: the seed only writes to loopback or the compose database", parsed.Hostname())
	}
}

// reference is the instant every seeded timestamp is an offset from. The
// owner's zone stands for the garden's, because a household reads its due dates
// in one zone.
func reference(now time.Time) (time.Time, error) {
	loc, err := time.LoadLocation(ellie.timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("load the timezone %s: %w", ellie.timezone, err)
	}
	local := now.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc), nil
}

type counts struct {
	gardens int
	plants  int
	events  int
}

func seed(ctx context.Context, pool *pgxpool.Pool, ref time.Time) (counts, error) {
	gardens := []garden{home(), upstairs()}
	people := []*person{&ellie, &sam, &robin}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return counts{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if err := clear(ctx, tx, gardens, people); err != nil {
		return counts{}, err
	}
	if err := writePeople(ctx, tx, people, ref); err != nil {
		return counts{}, err
	}

	written := counts{gardens: len(gardens)}
	// One counter across both gardens, so an event's identifier is the position
	// it was written at and two runs agree on it.
	events := 0
	for i := range gardens {
		n, err := writeGarden(ctx, tx, &gardens[i], ref, &events)
		if err != nil {
			return counts{}, err
		}
		written.plants += n
	}
	written.events = events

	if err := tx.Commit(ctx); err != nil {
		return counts{}, fmt.Errorf("commit: %w", err)
	}
	return written, nil
}

// clear removes what a previous run wrote. care_event references care_type with
// ON DELETE RESTRICT, so cascading from the garden would depend on which of the
// two foreign keys on an event fired first.
func clear(ctx context.Context, tx pgx.Tx, gardens []garden, people []*person) error {
	gardenIDs := make([]uuid.UUID, 0, len(gardens))
	for i := range gardens {
		gardenIDs = append(gardenIDs, gardens[i].id)
	}
	userIDs := make([]uuid.UUID, 0, len(people))
	for _, p := range people {
		userIDs = append(userIDs, p.id)
	}

	byGarden := []string{
		"DELETE FROM care_event WHERE garden_id = ANY($1)",
		"DELETE FROM care_schedule WHERE garden_id = ANY($1)",
		"DELETE FROM plant WHERE garden_id = ANY($1)",
		"DELETE FROM care_type WHERE garden_id = ANY($1)",
		// The cascade from garden takes the memberships, sessions and invites.
		"DELETE FROM garden WHERE id = ANY($1)",
	}
	for _, sql := range byGarden {
		if _, err := tx.Exec(ctx, sql, gardenIDs); err != nil {
			return fmt.Errorf("clearing: %w\n%s", err, sql)
		}
	}
	if _, err := tx.Exec(ctx, "DELETE FROM app_user WHERE id = ANY($1)", userIDs); err != nil {
		return fmt.Errorf("clearing app_user: %w", err)
	}
	return nil
}

func writePeople(ctx context.Context, tx pgx.Tx, people []*person, ref time.Time) error {
	const sql = `INSERT INTO app_user (id, display_name, handle, timezone, created_at)
		VALUES ($1, $2, $3, $4, $5)`
	for _, p := range people {
		if _, err := tx.Exec(ctx, sql, p.id, p.name, p.handle, p.timezone, ref.AddDate(0, 0, -p.daysOld)); err != nil {
			return fmt.Errorf("writing %s: %w", p.handle, err)
		}
	}
	return nil
}

// writeGarden returns how many plants the garden holds. events is the running
// count of care events, which is where the next event identifier comes from.
func writeGarden(ctx context.Context, tx pgx.Tx, g *garden, ref time.Time, events *int) (int, error) {
	created := ref.AddDate(0, 0, -g.daysOld)

	if _, err := tx.Exec(ctx,
		"INSERT INTO garden (id, name, created_at) VALUES ($1, $2, $3)",
		g.id, g.name, created,
	); err != nil {
		return 0, fmt.Errorf("writing the garden %s: %w", g.name, err)
	}

	for _, m := range g.members {
		var invitedBy *uuid.UUID
		if m.invitedBy != nil {
			invitedBy = &m.invitedBy.id
		}
		var expires *time.Time
		if m.expiresInDays != 0 {
			at := ref.AddDate(0, 0, m.expiresInDays)
			expires = &at
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO membership (id, garden_id, user_id, role, invited_by, created_at, expires_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			m.id, g.id, m.person.id, m.role, invitedBy, ref.AddDate(0, 0, -m.daysOld), expires,
		); err != nil {
			return 0, fmt.Errorf("writing %s's membership of %s: %w", m.person.handle, g.name, err)
		}
	}

	// A garden's care types are shown in the order it created them, so they are
	// written a second apart rather than all on the same instant.
	careTypeID := map[string]uuid.UUID{}
	for i, ct := range g.careTypes {
		careTypeID[ct.slug] = ct.id
		var archived *time.Time
		if ct.archivedDaysAgo != 0 {
			at := ref.AddDate(0, 0, -ct.archivedDaysAgo)
			archived = &at
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO care_type (id, garden_id, name, slug, created_at, archived_at)
				VALUES ($1, $2, $3, $4, $5, $6)`,
			ct.id, g.id, ct.name, ct.slug, created.Add(time.Duration(i)*time.Second), archived,
		); err != nil {
			return 0, fmt.Errorf("writing the care type %s: %w", ct.slug, err)
		}
	}

	var log []logEntry
	for i := range g.plants {
		p := &g.plants[i]
		if err := writePlant(ctx, tx, g, p, careTypeID, ref, created.AddDate(0, 0, i)); err != nil {
			return 0, err
		}
		log = append(log, history(g, p, ref)...)
	}

	if err := writeEvents(ctx, tx, g, log, careTypeID, events); err != nil {
		return 0, err
	}
	return len(g.plants), nil
}

func writePlant(ctx context.Context, tx pgx.Tx, g *garden, p *plant, careTypeID map[string]uuid.UUID, ref, created time.Time) error {
	var archived *time.Time
	if p.archivedDaysAgo != 0 {
		at := ref.AddDate(0, 0, -p.archivedDaysAgo)
		archived = &at
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO plant (
			id, garden_id, nickname, common_name, botanical_name, location,
			sun, water_needs, feed_needs, soil, climate, pot, notes,
			acquired_year, acquired_month, created_at, archived_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`,
		p.id, g.id, text(p.nickname), text(p.commonName), text(p.botanicalName), text(p.location),
		text(p.sun), text(p.waterNeeds), text(p.feedNeeds), text(p.soil), text(p.climate), text(p.pot), text(p.notes),
		number(p.acquiredYear), number(p.acquiredMonth), created, archived,
	); err != nil {
		return fmt.Errorf("writing the plant %s: %w", p.displayName(), err)
	}

	for i := range p.schedules {
		s := &p.schedules[i]
		var count *int
		var unit *string
		if s.repeats() {
			count, unit = &s.count, &s.unit
		}
		var anchorDate *time.Time
		var anchorPrecision *string
		if s.anchored() {
			at := s.anchor(ref)
			anchorDate = &at
			precision := "day"
			if s.anchorDay == 0 {
				precision = "month"
			}
			anchorPrecision = &precision
		}

		if _, err := tx.Exec(ctx,
			`INSERT INTO care_schedule (
				id, garden_id, plant_id, care_type_id,
				interval_count, interval_unit, anchor_date, anchor_precision,
				season_start_month, season_end_month, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			s.id, g.id, p.id, careTypeID[s.slug],
			count, unit, anchorDate, anchorPrecision,
			number(s.seasonStart), number(s.seasonEnd),
			// The creation timestamp stands in for the missing event on a
			// schedule that has none, so it sits before every event the seed
			// writes.
			created.AddDate(0, 0, 1),
		); err != nil {
			return fmt.Errorf("writing the %s schedule for %s: %w", s.slug, p.displayName(), err)
		}
	}
	return nil
}

func writeEvents(ctx context.Context, tx pgx.Tx, g *garden, log []logEntry, careTypeID map[string]uuid.UUID, events *int) error {
	rows := make([][]any, 0, len(log))
	for _, e := range log {
		*events++
		rows = append(rows, []any{
			seedID(tableCareEvent, *events), g.id, e.plant.id, careTypeID[e.careSlug],
			e.performedBy.id, e.performedAt, e.recordedAt, e.done,
			text(e.note), number(e.override),
		})
	}

	_, err := tx.CopyFrom(ctx, pgx.Identifier{"care_event"},
		[]string{
			"id", "garden_id", "plant_id", "care_type_id",
			"performed_by", "performed_at", "recorded_at", "done",
			"note", "override_interval_days",
		},
		pgx.CopyFromRows(rows))
	if err != nil {
		return fmt.Errorf("writing %s's care events: %w", g.name, err)
	}
	return nil
}

// A column holding NULL and one holding the empty string are different answers,
// and the fixture only means the first.
func text(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func number(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}
