-- Due dates are computed in Go from these rows, never here. The order matches
-- the plant list: by room, then display name, with id last so two plants with
-- the same room and name keep a stable order.
-- name: ListCareSchedules :many
SELECT sqlc.embed(care_schedule), sqlc.embed(plant), sqlc.embed(care_type)
FROM care_schedule
JOIN plant ON plant.id = care_schedule.plant_id AND plant.garden_id = care_schedule.garden_id
JOIN care_type ON care_type.id = care_schedule.care_type_id AND care_type.garden_id = care_schedule.garden_id
WHERE care_schedule.garden_id = @garden_id
  AND plant.archived_at IS NULL
  AND care_type.archived_at IS NULL
ORDER BY plant.location NULLS LAST, coalesce(plant.nickname, plant.common_name, plant.botanical_name), plant.id, care_type.created_at, care_type.id;

-- set_at defaults to now(), so a plant added today with a ten-day cadence is
-- due in ten days rather than overdue immediately.
-- name: CreateCareSchedule :one
INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit,
                           anchor_date, anchor_precision, season_start_month, season_end_month)
VALUES (@garden_id, @plant_id, @care_type_id, @interval_count, @interval_unit,
        @anchor_date, @anchor_precision, @season_start_month, @season_end_month)
RETURNING *;

-- A plant has at most one schedule per care type, so saving the editor inserts
-- or replaces. set_at is reset on every save, so a schedule with no history is
-- due a full interval from now, and a completed one-off becomes due again.
-- name: UpsertCareSchedule :one
INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit,
                           anchor_date, anchor_precision, season_start_month, season_end_month)
VALUES (@garden_id, @plant_id, @care_type_id, @interval_count, @interval_unit,
        @anchor_date, @anchor_precision, @season_start_month, @season_end_month)
ON CONFLICT (plant_id, care_type_id) DO UPDATE
SET interval_count     = excluded.interval_count,
    interval_unit      = excluded.interval_unit,
    anchor_date        = excluded.anchor_date,
    anchor_precision   = excluded.anchor_precision,
    season_start_month = excluded.season_start_month,
    season_end_month   = excluded.season_end_month,
    set_at             = now()
RETURNING *;

-- The schedule's events are kept. The plant simply has no schedule for the
-- care type any more, and its history stays readable.
-- name: DeleteCareSchedule :one
DELETE FROM care_schedule
WHERE garden_id = @garden_id AND plant_id = @plant_id AND care_type_id = @care_type_id
RETURNING id;
