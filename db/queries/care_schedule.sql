-- Every caller hands these rows to the due-date engine in Go, so no due date
-- is computed here. The ordering is the roster's, and id ends it so two
-- plants agreeing on room and name keep their order.
-- name: ListCareSchedules :many
SELECT sqlc.embed(care_schedule), sqlc.embed(plant), sqlc.embed(care_type)
FROM care_schedule
JOIN plant ON plant.id = care_schedule.plant_id AND plant.garden_id = care_schedule.garden_id
JOIN care_type ON care_type.id = care_schedule.care_type_id AND care_type.garden_id = care_schedule.garden_id
WHERE care_schedule.garden_id = @garden_id
  AND plant.archived_at IS NULL
  AND care_type.archived_at IS NULL
ORDER BY plant.location NULLS LAST, coalesce(plant.nickname, plant.common_name, plant.botanical_name), plant.id, care_type.created_at, care_type.id;

-- set_at defaults to now, so a plant added today with a ten-day cadence is due
-- in ten days rather than overdue on arrival.
-- name: CreateCareSchedule :one
INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit,
                           anchor_date, anchor_precision, season_start_month, season_end_month)
VALUES (@garden_id, @plant_id, @care_type_id, @interval_count, @interval_unit,
        @anchor_date, @anchor_precision, @season_start_month, @season_end_month)
RETURNING *;

-- A plant is scheduled for a care type at most once, so saving the editor
-- either writes the row or replaces it. set_at moves with the save, which is
-- what leaves a schedule newly set with no history due a full interval from
-- now and what brings a spent one-off back.
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

-- The events the schedule produced stay where they are, so the care type drops
-- back to being one the plant is not on rather than losing its history.
-- name: DeleteCareSchedule :one
DELETE FROM care_schedule
WHERE garden_id = @garden_id AND plant_id = @plant_id AND care_type_id = @care_type_id
RETURNING id;
