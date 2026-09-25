-- +goose Up

-- A line of text on the calendar over a run of days, such as the owner away
-- or a heatwave. It changes no due date.
CREATE TABLE calendar_note (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    garden_id  uuid NOT NULL REFERENCES garden (id) ON DELETE CASCADE,
    -- Dates rather than timestamps, because a note names days and every
    -- member sees the same days whatever their timezone. ends_on equals
    -- starts_on for a note on one day. The handler refuses an end before the
    -- start.
    starts_on  date NOT NULL,
    ends_on    date NOT NULL,
    text       text NOT NULL,
    -- The member who added the note. The calendar does not show it.
    created_by uuid NOT NULL REFERENCES app_user (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- A month's notes are the garden's rows that end on or after the month's first
-- day.
CREATE INDEX calendar_note_garden_id_ends_on_idx ON calendar_note (garden_id, ends_on);

-- +goose Down
DROP TABLE calendar_note;
