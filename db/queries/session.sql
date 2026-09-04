-- The instants are written from the caller's clock rather than the database's,
-- so the expiry decision in Go compares two readings of one clock.
-- name: CreateSession :one
INSERT INTO session (token_hash, user_id, garden_id, user_agent, created_at, last_seen_at)
VALUES (@token_hash, @user_id, @garden_id, @user_agent, @now, @now)
RETURNING *;

-- The session row is what tells a request which garden it is on, so this query
-- cannot take a garden as an input and reads by the hash of the token instead.
-- name: GetSessionByTokenHash :one
SELECT * FROM session
WHERE token_hash = @token_hash;

-- The expiry check runs in Go before this query, so an expired row is deleted
-- rather than revived.
-- name: TouchSession :one
UPDATE session
SET last_seen_at = @last_seen_at
WHERE token_hash = @token_hash
RETURNING *;

-- name: DeleteSession :exec
DELETE FROM session
WHERE token_hash = @token_hash;
