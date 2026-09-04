-- A garden's care types are shown in the order the garden created them, not
-- alphabetically.
-- name: ListCareTypes :many
SELECT * FROM care_type
WHERE garden_id = @garden_id AND archived_at IS NULL
ORDER BY created_at;
