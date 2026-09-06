-- An invite that has expired is not waiting for anybody, so More's People row
-- counts only the ones that can still be redeemed. A re-enrolment link is not
-- counted either: it adds a device to somebody already in the garden, and
-- their row is in the members list rather than in the Invited section.
-- name: CountWaitingInvites :one
SELECT count(*) FROM invite
WHERE garden_id = @garden_id AND user_id IS NULL AND redeemed_at IS NULL AND expires_at > @now;

-- The Invited section on People. It holds invites to people who are not in the
-- garden yet, the ones that have run out included, because an expired link is
-- still a row somebody wants to clear away.
-- name: ListWaitingInvites :many
SELECT * FROM invite
WHERE garden_id = @garden_id AND user_id IS NULL AND redeemed_at IS NULL
ORDER BY created_at DESC, id;

-- name: CreateInvite :one
INSERT INTO invite (garden_id, token_hash, role, user_id, created_by, expires_at, membership_expires_at)
VALUES (@garden_id, @token_hash, @role, @user_id, @created_by, @expires_at, @membership_expires_at)
RETURNING *;

-- Revoking deletes the row. That is what makes the link stop working. A
-- redeemed invite is not deleted here, because Revoke is only offered on rows
-- nobody has opened.
-- name: DeleteWaitingInvite :execrows
DELETE FROM invite
WHERE garden_id = @garden_id AND id = @invite_id AND redeemed_at IS NULL;

-- Every unredeemed re-enrolment link for one person. Issuing a new one deletes
-- the old, because these rows are not in the Invited section and a link nobody
-- can see is a link nobody can revoke.
-- name: DeleteReenrolmentInvites :execrows
DELETE FROM invite
WHERE garden_id = @garden_id AND user_id = @user_id AND redeemed_at IS NULL;
