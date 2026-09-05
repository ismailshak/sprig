-- +goose Up

CREATE TABLE plant (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    garden_id      uuid NOT NULL REFERENCES garden (id) ON DELETE CASCADE,
    -- At least one of the three names is required. The handler enforces that,
    -- because the error message belongs under the name fields.
    nickname       text,
    common_name    text,
    botanical_name text,
    location       text,
    sun            text,
    water_needs    text,
    feed_needs     text,
    soil           text,
    climate        text,
    pot            text,
    notes          text,
    -- Both NULL for a plant with no known acquisition date. The form, not the
    -- schema, requires a year with a month.
    acquired_year  smallint,
    acquired_month smallint,
    created_at     timestamptz NOT NULL DEFAULT now(),
    -- Plants are archived, never deleted, so their history stays readable.
    archived_at    timestamptz,

    -- Target of the composite foreign keys on care_schedule and care_event.
    UNIQUE (id, garden_id)
);

CREATE INDEX plant_garden_id_idx ON plant (garden_id);

-- A garden's default care types are inserted when the garden is created, not
-- here, because a migration has no garden to attach them to.
CREATE TABLE care_type (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    garden_id   uuid NOT NULL REFERENCES garden (id) ON DELETE CASCADE,
    name        text NOT NULL,
    -- Code refers to a care type by slug, so the name can be changed freely.
    slug        text COLLATE "C" NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    -- Set when a type with events is turned off. It cannot be deleted.
    archived_at timestamptz,
    UNIQUE (garden_id, slug),
    -- Target of the composite foreign keys on care_schedule and care_event.
    UNIQUE (id, garden_id)
);

-- The due-date engine tells the three schedule shapes apart by which columns
-- are NULL, so the CHECK constraints below make any other combination
-- impossible to store.
--
--   cadence   interval, no anchor      every 10 days, counted from the last one
--   anchored  interval and anchor      every year, on 1 August
--   one-off   anchor, no interval      in March 2028, and then we will see
CREATE TABLE care_schedule (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    garden_id          uuid NOT NULL,
    plant_id           uuid NOT NULL,
    care_type_id       uuid NOT NULL,
    -- A count and a unit rather than a number of days, so "every month" falls
    -- on the same day of each month instead of drifting.
    interval_count     integer,
    interval_unit      text COLLATE "C",
    anchor_date        date,
    anchor_precision   text COLLATE "C",
    season_start_month smallint,
    season_end_month   smallint,
    -- When the schedule was last set. Every edit resets it. With no events yet
    -- the due date counts from here, so a plant added today with a ten-day
    -- cadence is due in ten days rather than overdue immediately. Only care
    -- performed after set_at completes a one-off, so re-saving a completed
    -- one-off makes it due again.
    set_at             timestamptz NOT NULL DEFAULT now(),

    -- Both foreign keys include garden_id, so a schedule cannot join a plant in
    -- one garden to a care type in another.
    FOREIGN KEY (plant_id, garden_id) REFERENCES plant (id, garden_id) ON DELETE CASCADE,
    FOREIGN KEY (care_type_id, garden_id) REFERENCES care_type (id, garden_id) ON DELETE CASCADE,

    UNIQUE (plant_id, care_type_id),

    CONSTRAINT care_schedule_interval_is_both_or_neither CHECK ((interval_count IS NULL) = (interval_unit IS NULL)),
    CONSTRAINT care_schedule_anchor_is_both_or_neither CHECK ((anchor_date IS NULL) = (anchor_precision IS NULL)),
    CONSTRAINT care_schedule_season_is_both_or_neither CHECK ((season_start_month IS NULL) = (season_end_month IS NULL)),
    CONSTRAINT care_schedule_has_a_rule CHECK (interval_count IS NOT NULL OR anchor_date IS NOT NULL),
    -- An anchored schedule already names its month, so a season on it would be
    -- meaningless.
    CONSTRAINT care_schedule_season_bounds_a_cadence CHECK (season_start_month IS NULL OR anchor_date IS NULL),
    CONSTRAINT care_schedule_interval_is_positive CHECK (interval_count > 0),
    CONSTRAINT care_schedule_interval_unit_is_known CHECK (interval_unit IN ('day', 'week', 'month', 'year')),
    -- Month precision means "sometime in March". Storing a day would invent a
    -- precision nobody gave and then report the plant as days late.
    CONSTRAINT care_schedule_anchor_precision_is_known CHECK (anchor_precision IN ('day', 'month'))
);

CREATE TABLE care_event (
    id                     uuid PRIMARY KEY DEFAULT uuidv7(),
    garden_id              uuid NOT NULL,
    plant_id               uuid NOT NULL,
    care_type_id           uuid NOT NULL,
    -- References the user, not the membership, so a removed member's events
    -- still show who performed them.
    performed_by           uuid NOT NULL REFERENCES app_user (id) ON DELETE RESTRICT,
    -- performed_at is when the care happened, recorded_at when it was entered.
    -- Due dates count from performed_at. The pair distinguishes a backdated
    -- entry from one entered at the time.
    performed_at           timestamptz NOT NULL,
    recorded_at            timestamptz NOT NULL DEFAULT now(),
    -- false is a skip, which pushes the due date back without recording that
    -- care was given.
    done                   boolean NOT NULL,
    note                   text,
    -- The "ask again in 2 days" interval on a skip. Stored as an interval, not
    -- a date, so it still works when the event is backdated.
    override_interval_days integer,

    FOREIGN KEY (plant_id, garden_id) REFERENCES plant (id, garden_id) ON DELETE CASCADE,
    -- RESTRICT, so a care type with events can only be archived, not deleted.
    FOREIGN KEY (care_type_id, garden_id) REFERENCES care_type (id, garden_id) ON DELETE RESTRICT
);

-- Due dates need the latest event per plant and care type. A plant's history
-- uses the same index without the care type.
CREATE INDEX care_event_plant_id_care_type_id_performed_at_idx
    ON care_event (plant_id, care_type_id, performed_at DESC);

-- The Activity page lists a garden's events newest first.
CREATE INDEX care_event_garden_id_performed_at_idx ON care_event (garden_id, performed_at DESC);

-- Checking whether a care type has events, and so can only be archived.
CREATE INDEX care_event_care_type_id_idx ON care_event (care_type_id);

-- +goose Down

DROP TABLE care_event;
DROP TABLE care_schedule;
DROP TABLE care_type;
DROP TABLE plant;
