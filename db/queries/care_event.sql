-- A skipped event is a plant's latest one too, so nothing here filters on done.
-- An event whose plant or care type was archived stays in, because the caller
-- pairs these rows against ListCareSchedules, which already excludes both.
-- name: ListLatestCareEvents :many
SELECT DISTINCT ON (plant_id, care_type_id) *
FROM care_event
WHERE garden_id = @garden_id
ORDER BY plant_id, care_type_id, performed_at DESC;

-- The handler passes recorded_at because its clock and the row have to agree.
-- name: CreateCareEvent :one
INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done, note, override_interval_days)
VALUES (@garden_id, @plant_id, @care_type_id, @performed_by, @performed_at, @recorded_at, @done, @note, @override_interval_days)
RETURNING *;

-- DeleteCareEvent matches on performed_by unless may_delete_any is set,
-- because a check standing beside the query can be forgotten. Nothing bounds
-- how old the event may be because the undo window decides what the feed
-- draws rather than what the delete accepts.
-- name: DeleteCareEvent :one
DELETE FROM care_event
WHERE id = @id AND garden_id = @garden_id AND plant_id = @plant_id
  AND (@may_delete_any::boolean OR performed_by = @performed_by)
RETURNING *;

-- The order is recorded_at rather than performed_at so a backdated care still
-- lands at the top of the feed.
-- name: ListRecentCareEvents :many
SELECT sqlc.embed(care_event), sqlc.embed(plant), sqlc.embed(care_type), app_user.display_name AS performed_by_name
FROM care_event
JOIN plant ON plant.id = care_event.plant_id AND plant.garden_id = care_event.garden_id
JOIN care_type ON care_type.id = care_event.care_type_id AND care_type.garden_id = care_event.garden_id
JOIN app_user ON app_user.id = care_event.performed_by
WHERE care_event.garden_id = @garden_id
ORDER BY care_event.recorded_at DESC, care_event.id DESC
LIMIT @count;

-- ListCareEventLog reads one page of Activity. It orders by performed_at rather
-- than recorded_at because the page dates an event by when the care happened,
-- not by when it was entered.
--
-- plant_id is null for the whole garden, or a plant id to show only that plant.
--
-- before_at and before_id are null for the newest page, and otherwise hold the
-- performed_at and id of the last event on the previous page. Both columns are
-- compared because backdated events can share a performed_at to the minute.
-- name: ListCareEventLog :many
SELECT sqlc.embed(care_event), sqlc.embed(plant), sqlc.embed(care_type), app_user.display_name AS performed_by_name
FROM care_event
JOIN plant ON plant.id = care_event.plant_id AND plant.garden_id = care_event.garden_id
JOIN care_type ON care_type.id = care_event.care_type_id AND care_type.garden_id = care_event.garden_id
JOIN app_user ON app_user.id = care_event.performed_by
WHERE care_event.garden_id = @garden_id
  AND (sqlc.narg('plant_id')::uuid IS NULL OR care_event.plant_id = sqlc.narg('plant_id'))
  AND (sqlc.narg('before_at')::timestamptz IS NULL
       OR (care_event.performed_at, care_event.id) < (sqlc.narg('before_at'), sqlc.narg('before_id')::uuid))
ORDER BY care_event.performed_at DESC, care_event.id DESC
LIMIT @count;

-- The order is performed_at because a plant's Recent is a history of the plant
-- rather than of what was typed. Nothing here joins plant, because the page is
-- the plant.
-- name: ListPlantCareEvents :many
SELECT sqlc.embed(care_event), sqlc.embed(care_type), app_user.display_name AS performed_by_name
FROM care_event
JOIN care_type ON care_type.id = care_event.care_type_id AND care_type.garden_id = care_event.garden_id
JOIN app_user ON app_user.id = care_event.performed_by
WHERE care_event.garden_id = @garden_id AND care_event.plant_id = @plant_id
ORDER BY care_event.performed_at DESC, care_event.id DESC
LIMIT @count;
