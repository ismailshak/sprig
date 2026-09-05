-- Care types are listed in creation order.
-- name: ListCareTypes :many
SELECT * FROM care_type
WHERE garden_id = @garden_id AND archived_at IS NULL
ORDER BY created_at;
