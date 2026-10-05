-- The Tokens list: the unrevoked tokens @user_id created in the garden, newest
-- first. Expired tokens are included so their creator can remove them.
-- name: ListAPITokens :many
SELECT * FROM api_token
WHERE garden_id = @garden_id AND created_by = @user_id AND revoked_at IS NULL
ORDER BY created_at DESC, id;

-- name: CreateAPIToken :one
INSERT INTO api_token (garden_id, name, token_hash, prefix, created_by, expires_at)
VALUES (@garden_id, @name, @token_hash, @prefix, @created_by, @expires_at)
RETURNING *;

-- Revoke and Remove both set revoked_at. A token another member created
-- updates no row, the same as one that does not exist.
-- name: RevokeAPIToken :execrows
UPDATE api_token SET revoked_at = @now::timestamptz
WHERE garden_id = @garden_id AND created_by = @user_id AND id = @token_id AND revoked_at IS NULL;

-- Returns revoked and expired rows too, so the caller decides whether the
-- token still works. It binds no garden_id because this row is what tells a
-- request which garden it is on. creator_can_manage is true when the account
-- that created the token has a membership of the token's garden that has not
-- ended at @now, with a role that grants @capability.
-- name: GetAPITokenByHash :one
SELECT sqlc.embed(api_token),
    EXISTS (SELECT 1 FROM membership
        JOIN role_capability ON role_capability.role = membership.role AND role_capability.capability = @capability
        WHERE membership.garden_id = api_token.garden_id AND membership.user_id = api_token.created_by
          AND (membership.expires_at IS NULL OR membership.expires_at > @now::timestamptz)) AS creator_can_manage
FROM api_token WHERE token_hash = @token_hash;

-- name: TouchAPIToken :exec
UPDATE api_token SET last_used_at = @now::timestamptz WHERE token_hash = @token_hash;

-- Every unrevoked token expiring after @since, paired with the membership of
-- the person who created it. A token whose creator has no membership of its
-- garden at @now with a role that grants @capability is left out, because it
-- has already stopped working. A creator with no browser subscribed is left
-- out, so the job never claims a ledger row and then sends nothing. owner_name
-- is the display name of the garden's owner, empty when the garden has no
-- owner. recipient_owns is true when the creator is that owner. There is no
-- @garden_id because the job runs across every garden.
-- name: ListTokenDeadlines :many
SELECT sqlc.embed(api_token), membership.id AS membership_id, membership.user_id,
    app_user.handle, app_user.timezone, garden.name AS garden_name,
    coalesce(owner.display_name, '')::text AS owner_name,
    coalesce(owner.id = membership.user_id, false)::boolean AS recipient_owns
FROM api_token
JOIN garden ON garden.id = api_token.garden_id
JOIN membership ON membership.garden_id = api_token.garden_id AND membership.user_id = api_token.created_by
    AND (membership.expires_at IS NULL OR membership.expires_at > @now::timestamptz)
JOIN role_capability ON role_capability.role = membership.role AND role_capability.capability = @capability
JOIN app_user ON app_user.id = membership.user_id
LEFT JOIN LATERAL (
    SELECT app_user.id, app_user.display_name
    FROM membership o
    JOIN app_user ON app_user.id = o.user_id
    WHERE o.garden_id = garden.id AND o.role = 'owner'
    ORDER BY o.created_at, o.id
    LIMIT 1
) AS owner ON true
WHERE api_token.revoked_at IS NULL
  AND api_token.expires_at > @since::timestamptz
  AND EXISTS (SELECT 1 FROM push_subscription WHERE push_subscription.user_id = membership.user_id)
ORDER BY api_token.expires_at, api_token.id;
