-- The Tokens list, newest first. A revoked token leaves the list. One that has
-- expired stays in it and goes grey, because it is still the row somebody came
-- looking for.
-- name: ListAPITokens :many
SELECT * FROM api_token
WHERE garden_id = @garden_id AND revoked_at IS NULL
ORDER BY created_at DESC, id;

-- name: CreateAPIToken :one
INSERT INTO api_token (garden_id, name, token_hash, prefix, created_by, expires_at)
VALUES (@garden_id, @name, @token_hash, @prefix, @created_by, @expires_at)
RETURNING *;

-- Revoke and Remove are one write. The row leaves the list either way, and the
-- two words differ because one stops a credential that still works and the
-- other clears away one that has already run out.
-- name: RevokeAPIToken :execrows
UPDATE api_token SET revoked_at = @now::timestamptz
WHERE garden_id = @garden_id AND id = @token_id AND revoked_at IS NULL;
