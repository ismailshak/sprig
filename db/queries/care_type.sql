-- Care types are listed in creation order.
-- name: ListCareTypes :many
SELECT * FROM care_type
WHERE garden_id = @garden_id AND archived_at IS NULL
ORDER BY created_at;

-- GetCareType reads one care type whether or not it is archived, so a restore
-- can check that the id it was sent is the garden's before inserting a row that
-- references it.
-- name: GetCareType :one
SELECT * FROM care_type
WHERE garden_id = @garden_id AND id = @care_type_id;
