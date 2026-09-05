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
