-- Timestamps come from the caller's clock rather than now(), so the expiry
-- check in Go compares two readings of the same clock. garden_id is NULL for
-- a session that starts without one.
-- name: CreateSession :one
INSERT INTO session (token_hash, user_id, garden_id, passkey_credential_id, user_agent, created_at, last_seen_at)
VALUES (@token_hash, @user_id, sqlc.narg('garden_id'), @passkey_credential_id, @user_agent, @now, @now)
RETURNING *;

-- The session row is how a request learns its garden, so this query cannot
-- take a garden_id. It reads by token hash instead.
-- name: GetSessionByTokenHash :one
SELECT * FROM session
WHERE token_hash = @token_hash;

-- The caller checks expiry in Go before calling this, so an expired row is
-- deleted rather than refreshed.
-- name: TouchSession :one
UPDATE session
SET last_seen_at = @last_seen_at
WHERE token_hash = @token_hash
RETURNING *;

-- name: DeleteSession :exec
DELETE FROM session
WHERE token_hash = @token_hash;

-- Switching gardens updates the session row rather than creating a new one, so
-- the cookie stays as it is. The foreign key on (garden_id, user_id) rejects a
-- garden the session's user is not a member of. NULL leaves the session with
-- no garden.
-- name: SetSessionGarden :execrows
UPDATE session
SET garden_id = sqlc.narg('garden_id')
WHERE token_hash = @token_hash;

-- name: DeleteUserSessions :exec
DELETE FROM session WHERE user_id = @user_id;

-- DeleteExpiredSessions deletes the sessions last seen at @before or earlier,
-- where @before is now minus the session TTL. Looking a session up deletes an
-- expired row when its token is next presented, so what is left for this are
-- the sessions no browser will present again.
-- name: DeleteExpiredSessions :execrows
DELETE FROM session
WHERE last_seen_at <= @before::timestamptz;
