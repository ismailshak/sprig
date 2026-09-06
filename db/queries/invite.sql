-- An invite that has expired is not waiting for anybody, so More's People row
-- counts only the ones that can still be redeemed.
-- name: CountWaitingInvites :one
SELECT count(*) FROM invite
WHERE garden_id = @garden_id AND redeemed_at IS NULL AND expires_at > @now;
