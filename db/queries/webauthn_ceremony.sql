-- name: CreateCeremony :one
INSERT INTO webauthn_ceremony (token_hash, user_id, session, expires_at)
VALUES (@token_hash, @user_id, @session, @expires_at)
RETURNING *;

-- Deleting and returning in one statement is what makes a challenge answerable
-- once. Two requests sending the same cookie race for the row and only one of
-- them gets it.
-- name: TakeCeremony :one
DELETE FROM webauthn_ceremony
WHERE token_hash = @token_hash AND expires_at > @now
RETURNING *;

-- A prompt nobody answers keeps its row until it expires, so this runs whenever
-- a ceremony starts.
-- name: DeleteExpiredCeremonies :exec
DELETE FROM webauthn_ceremony WHERE expires_at <= @now;
