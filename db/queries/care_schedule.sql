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
