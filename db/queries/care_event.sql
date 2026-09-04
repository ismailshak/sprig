-- A skipped event is a plant's latest one too, so nothing here filters on done.
-- An event whose plant or care type was archived stays in, because the caller
-- pairs these rows against ListCareSchedules, which already excludes both.
-- name: ListLatestCareEvents :many
SELECT DISTINCT ON (plant_id, care_type_id) *
FROM care_event
WHERE garden_id = @garden_id
ORDER BY plant_id, care_type_id, performed_at DESC;
