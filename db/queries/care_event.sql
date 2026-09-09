-- A skip counts as the latest event, so this does not filter on done. Events
-- of archived plants and care types are included, because the caller matches
-- these rows against ListCareSchedules, which already excludes both.
-- name: ListLatestCareEvents :many
SELECT DISTINCT ON (plant_id, care_type_id) *
FROM care_event
WHERE garden_id = @garden_id
ORDER BY plant_id, care_type_id, performed_at DESC;

-- recorded_at comes from the handler's clock rather than now(), so the row and
-- the handler agree on the time.
-- name: CreateCareEvent :one
INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done, note, override_interval_days)
VALUES (@garden_id, @plant_id, @care_type_id, @performed_by, @performed_at, @recorded_at, @done, @note, @override_interval_days)
RETURNING *;

-- Matches on performed_by unless may_delete_any is set, so the ownership check
-- is in the query and cannot be forgotten by a caller. There is no age limit
-- here. The undo window decides whether an Undo button is rendered, not
-- whether a delete is accepted.
-- name: DeleteCareEvent :one
DELETE FROM care_event
WHERE id = @id AND garden_id = @garden_id AND plant_id = @plant_id
  AND (@may_delete_any::boolean OR performed_by = @performed_by)
RETURNING *;

-- Ordered by recorded_at rather than performed_at, so a backdated care still
-- appears at the top of the feed.
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
-- care_type_id is null for every care type, or a care type id to show only
-- that care. since and until are the ends of the date range the page is
-- filtered to, as instants: the start of the first day and the start of the
-- day after the last, in the reader's timezone. Either is null when that end
-- is open.
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
  AND (sqlc.narg('care_type_id')::uuid IS NULL OR care_event.care_type_id = sqlc.narg('care_type_id'))
  AND (sqlc.narg('since')::timestamptz IS NULL OR care_event.performed_at >= sqlc.narg('since'))
  AND (sqlc.narg('until')::timestamptz IS NULL OR care_event.performed_at < sqlc.narg('until'))
  AND (sqlc.narg('before_at')::timestamptz IS NULL
       OR (care_event.performed_at, care_event.id) < (sqlc.narg('before_at'), sqlc.narg('before_id')::uuid))
ORDER BY care_event.performed_at DESC, care_event.id DESC
LIMIT @count;

-- Ordered by performed_at because a plant's Recent section is a history of the
-- plant, not of data entry. No plant join, because the caller already has the
-- plant.
-- name: ListPlantCareEvents :many
SELECT sqlc.embed(care_event), sqlc.embed(care_type), app_user.display_name AS performed_by_name
FROM care_event
JOIN care_type ON care_type.id = care_event.care_type_id AND care_type.garden_id = care_event.garden_id
JOIN app_user ON app_user.id = care_event.performed_by
WHERE care_event.garden_id = @garden_id AND care_event.plant_id = @plant_id
ORDER BY care_event.performed_at DESC, care_event.id DESC
LIMIT @count;

-- GetCareEvent reads one event with everything the correcting sheet shows: the
-- plant it belongs to, the care type it records, and the name of whoever
-- recorded it.
-- name: GetCareEvent :one
SELECT sqlc.embed(care_event), sqlc.embed(plant), sqlc.embed(care_type), app_user.display_name AS performed_by_name
FROM care_event
JOIN plant ON plant.id = care_event.plant_id AND plant.garden_id = care_event.garden_id
JOIN care_type ON care_type.id = care_event.care_type_id AND care_type.garden_id = care_event.garden_id
JOIN app_user ON app_user.id = care_event.performed_by
WHERE care_event.garden_id = @garden_id AND care_event.plant_id = @plant_id AND care_event.id = @id;

-- Matches on performed_by unless may_edit_any is set, so the ownership check is
-- in the query and cannot be forgotten by a caller. recorded_at and performed_by
-- are left alone: a correction changes what was done and when it was done, not
-- who wrote it down or when they wrote it.
-- name: UpdateCareEvent :one
UPDATE care_event
SET care_type_id = @care_type_id,
    performed_at = @performed_at,
    done = @done,
    note = @note,
    override_interval_days = @override_interval_days
WHERE id = @id AND garden_id = @garden_id AND plant_id = @plant_id
  AND (@may_edit_any::boolean OR performed_by = @performed_by)
RETURNING *;

-- RestoreCareEvent puts a deleted event back under the id it had, for the Undo
-- button on the row a delete leaves behind. The row is gone from the table, so
-- the values arrive with the request rather than from the database.
--
-- The WHERE clause is what stops the performer being anybody the sender likes:
-- it has to be the person making the request unless they may delete anyone's
-- care. That is the same test DeleteCareEvent applies, so nobody can restore an
-- event they could not have deleted. The join is there so that an id naming no
-- user returns no rows rather than failing a foreign key.
--
-- The performer is not required to still be a member of the garden. The event's
-- foreign key is to app_user rather than to membership, so that a departed
-- member's care still says who gave it, and a restore has to be able to say the
-- same.
--
-- An id already in the table inserts nothing and returns no rows, so restoring
-- the same event twice is a 404 rather than a duplicate key error.
-- name: RestoreCareEvent :one
INSERT INTO care_event (id, garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done, note, override_interval_days)
SELECT @id::uuid, @garden_id::uuid, @plant_id::uuid, @care_type_id::uuid, app_user.id,
       @performed_at::timestamptz, @recorded_at::timestamptz, @done::boolean,
       sqlc.narg('note')::text, sqlc.narg('override_interval_days')::integer
FROM app_user
WHERE app_user.id = @performed_by::uuid
  AND (@may_delete_any::boolean OR app_user.id = @restored_by::uuid)
ON CONFLICT (id) DO NOTHING
RETURNING *;
