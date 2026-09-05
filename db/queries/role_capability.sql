-- Read on every request rather than cached, so a row added to the table takes
-- effect on the next request.
-- name: ListRoleCapabilities :many
SELECT capability FROM role_capability
WHERE role = @role
ORDER BY capability;
