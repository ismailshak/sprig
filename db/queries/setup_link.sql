-- name: CreateSetupLink :one
INSERT INTO setup_link (token_hash, expires_at)
VALUES (@token_hash, @expires_at)
RETURNING *;

-- Reports whether the link with this token hash is unused and has not expired.
-- A used, expired and unknown token all return false.
-- name: SetupLinkOpen :one
SELECT EXISTS (
    SELECT 1 FROM setup_link
    WHERE token_hash = @token_hash AND used_at IS NULL AND expires_at > @now::timestamptz
);

-- Sets used_at on a link that is unused and has not expired. It updates no row
-- for a used, expired or unknown token.
-- name: UseSetupLink :execrows
UPDATE setup_link SET used_at = @now::timestamptz
WHERE token_hash = @token_hash AND used_at IS NULL AND expires_at > @now;

-- Deletes a used link whose used_at is at or before @before, and an unused
-- link whose expires_at is.
-- name: DeleteUsedAndExpiredSetupLinks :execrows
DELETE FROM setup_link
WHERE (used_at IS NOT NULL AND used_at <= @before::timestamptz)
   OR (used_at IS NULL AND expires_at <= @before::timestamptz);
