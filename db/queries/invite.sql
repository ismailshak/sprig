-- An expired invite is not pending, so More's People row counts only the ones
-- that can still be redeemed. A re-enrolment link is not counted either: it
-- adds a device to somebody already in the garden, and their row is in the
-- members list rather than in the Pending invites section.
-- name: CountPendingInvites :one
SELECT count(*) FROM invite
WHERE garden_id = @garden_id AND user_id IS NULL AND redeemed_at IS NULL AND expires_at > @now;

-- The Pending invites section on People. It holds invites to people who are
-- not in the garden yet, the ones that have run out included, because an
-- expired link is still a row somebody wants to clear away.
-- name: ListPendingInvites :many
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
-- name: DeletePendingInvite :execrows
DELETE FROM invite
WHERE garden_id = @garden_id AND id = @invite_id AND redeemed_at IS NULL;

-- Every unredeemed re-enrolment link for one person. Issuing a new one deletes
-- the old, because these rows are not in the Pending invites section and a
-- link nobody can see is a link nobody can revoke.
-- name: DeleteReenrolmentInvites :execrows
DELETE FROM invite
WHERE garden_id = @garden_id AND user_id = @user_id AND redeemed_at IS NULL;

-- The invite for a token hash, with its garden and the account that created
-- it. The page shows the garden's name and that account's display name. There
-- is no garden_id parameter, because the token is what says which garden the
-- link is for.
-- name: GetInviteByTokenHash :one
SELECT sqlc.embed(invite), sqlc.embed(garden), sqlc.embed(app_user)
FROM invite
JOIN garden ON garden.id = invite.garden_id
JOIN app_user ON app_user.id = invite.created_by
WHERE invite.token_hash = @token_hash;

-- Sets redeemed_at on an invite that has not been redeemed and has not
-- expired. The row count is zero when it had already been redeemed or had
-- expired, so the caller reads the count to find out whether it was usable.
-- name: RedeemInvite :execrows
UPDATE invite SET redeemed_at = @now::timestamptz
WHERE garden_id = @garden_id AND id = @invite_id AND redeemed_at IS NULL AND expires_at > @now;

-- Deletes the unredeemed re-enrolment invites made for this account, in every
-- garden. A redeemed invite is kept as a record of the enrolment.
-- name: DeleteUserReenrolmentInvites :exec
DELETE FROM invite
WHERE user_id = @user_id::uuid AND redeemed_at IS NULL;

-- DeleteSpentInvites deletes the invites no page lists once they are @before
-- or older: a redeemed invite of either kind, and an expired re-enrolment
-- link. An expired join invite is kept, because People lists it under Pending
-- invites with a Revoke button.
-- name: DeleteSpentInvites :execrows
DELETE FROM invite
WHERE (redeemed_at IS NOT NULL AND redeemed_at <= @before::timestamptz)
   OR (user_id IS NOT NULL AND redeemed_at IS NULL AND expires_at <= @before::timestamptz);
