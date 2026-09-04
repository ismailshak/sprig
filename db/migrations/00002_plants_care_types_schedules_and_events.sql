-- +goose Up

CREATE TABLE plant (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    garden_id      uuid NOT NULL REFERENCES garden (id) ON DELETE CASCADE,
    -- At least one of the three names is required. The handler holds that rule,
    -- because the message belongs under the field group.
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
    -- Both null for a plant that has always been there. That a month needs a
    -- year is the form's rule, not this table's.
    acquired_year  smallint,
    acquired_month smallint,
    created_at     timestamptz NOT NULL DEFAULT now(),
    -- A dead plant's history is the record of what happened to it, so a plant
    -- is archived and never deleted.
    archived_at    timestamptz,

    -- What the composite foreign keys on care_schedule and care_event point at.
    UNIQUE (id, garden_id)
);

CREATE INDEX plant_garden_id_idx ON plant (garden_id);

-- A garden's three types are inserted when the garden is created, not here,
-- because a migration has no garden to hang them off.
CREATE TABLE care_type (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    garden_id   uuid NOT NULL REFERENCES garden (id) ON DELETE CASCADE,
    name        text NOT NULL,
    -- Code refers to a care type by slug, which is why renaming one is free.
    slug        text COLLATE "C" NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    -- Set once a type has events behind it and somebody turns it off.
    archived_at timestamptz,
    UNIQUE (garden_id, slug),
    -- What the composite foreign keys on care_schedule and care_event point at.
    UNIQUE (id, garden_id)
);

-- The due-date engine tells the three shapes apart by which columns are null, so
-- the checks below make a fourth combination impossible rather than unproduced.
--
--   cadence   interval, no anchor      every 10 days, counted from the last one
--   anchored  interval and anchor      every year, on 1 August
--   one-off   anchor, no interval      in March 2028, and then we will see
CREATE TABLE care_schedule (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    garden_id          uuid NOT NULL,
    plant_id           uuid NOT NULL,
    care_type_id       uuid NOT NULL,
    -- A count and a unit rather than a number of days, so "every month" lands on
    -- the same day of the month instead of drifting.
    interval_count     integer,
    interval_unit      text COLLATE "C",
    anchor_date        date,
    anchor_precision   text COLLATE "C",
    season_start_month smallint,
    season_end_month   smallint,
    -- When the schedule was last set, which an edit rewrites. It stands in for
    -- the missing event, so a plant added today with a ten-day cadence is due in
    -- ten days rather than overdue on arrival. Care performed after it completes a
    -- one-off, so a new date on a completed one-off brings it back.
    set_at             timestamptz NOT NULL DEFAULT now(),

    -- Both foreign keys carry garden_id, so a schedule cannot join a plant in
    -- one garden to a care type in another.
    FOREIGN KEY (plant_id, garden_id) REFERENCES plant (id, garden_id) ON DELETE CASCADE,
    FOREIGN KEY (care_type_id, garden_id) REFERENCES care_type (id, garden_id) ON DELETE CASCADE,

    UNIQUE (plant_id, care_type_id),

    CONSTRAINT care_schedule_interval_is_both_or_neither CHECK ((interval_count IS NULL) = (interval_unit IS NULL)),
    CONSTRAINT care_schedule_anchor_is_both_or_neither CHECK ((anchor_date IS NULL) = (anchor_precision IS NULL)),
    CONSTRAINT care_schedule_season_is_both_or_neither CHECK ((season_start_month IS NULL) = (season_end_month IS NULL)),
    CONSTRAINT care_schedule_has_a_rule CHECK (interval_count IS NOT NULL OR anchor_date IS NOT NULL),
    -- An anchored schedule already names its month, so a season on top of it
    -- would say nothing.
    CONSTRAINT care_schedule_season_bounds_a_cadence CHECK (season_start_month IS NULL OR anchor_date IS NULL),
    CONSTRAINT care_schedule_interval_is_positive CHECK (interval_count > 0),
    CONSTRAINT care_schedule_interval_unit_is_known CHECK (interval_unit IN ('day', 'week', 'month', 'year')),
    -- Month precision is "sometime in March". A day would invent an accuracy
    -- nobody gave, and then report it as four days late.
    CONSTRAINT care_schedule_anchor_precision_is_known CHECK (anchor_precision IN ('day', 'month'))
);

CREATE TABLE care_event (
    id                     uuid PRIMARY KEY DEFAULT uuidv7(),
    garden_id              uuid NOT NULL,
    plant_id               uuid NOT NULL,
    care_type_id           uuid NOT NULL,
    -- The user row and not the membership, because a removed member's history
    -- has to keep reading correctly.
    performed_by           uuid NOT NULL REFERENCES app_user (id) ON DELETE RESTRICT,
    -- A sitter waters at nine and logs it at eleven. The next reminder counts
    -- from nine, and the pair is what tells a backdated entry from a live one.
    performed_at           timestamptz NOT NULL,
    recorded_at            timestamptz NOT NULL DEFAULT now(),
    -- False is a skip, which resets the clock without claiming water was given.
    done                   boolean NOT NULL,
    note                   text,
    -- "Ask again in 2 days". An interval and not a date, so it still moves
    -- correctly when the event is backdated.
    override_interval_days integer,

    FOREIGN KEY (plant_id, garden_id) REFERENCES plant (id, garden_id) ON DELETE CASCADE,
    -- Restrict, so a care type with history behind it can only be archived.
    FOREIGN KEY (care_type_id, garden_id) REFERENCES care_type (id, garden_id) ON DELETE RESTRICT
);

-- The due-date computation asks for the latest event of one type on one plant,
-- and a plant's log reads the same rows without the type.
CREATE INDEX care_event_plant_id_care_type_id_performed_at_idx
    ON care_event (plant_id, care_type_id, performed_at DESC);

-- Activity is the garden's events newest first.
CREATE INDEX care_event_garden_id_performed_at_idx ON care_event (garden_id, performed_at DESC);

-- Deciding whether a care type may be deleted is a lookup on this column.
CREATE INDEX care_event_care_type_id_idx ON care_event (care_type_id);

-- +goose Down

DROP TABLE care_event;
DROP TABLE care_schedule;
DROP TABLE care_type;
DROP TABLE plant;
