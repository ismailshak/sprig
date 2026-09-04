-- Read on every request rather than cached, so a row added to the table is a
-- capability the next request already has.
-- name: ListRoleCapabilities :many
SELECT capability FROM role_capability
WHERE role = @role
ORDER BY capability;
