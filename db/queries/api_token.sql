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

-- Returns revoked and expired rows too, so the caller decides whether the
-- token still works. It binds no garden_id because this row is what tells a
-- request which garden it is on.
-- name: GetAPITokenByHash :one
SELECT * FROM api_token WHERE token_hash = @token_hash;

-- name: TouchAPIToken :exec
UPDATE api_token SET last_used_at = @now::timestamptz WHERE token_hash = @token_hash;

-- Every unrevoked token expiring after @since, paired with each member of its
-- garden whose role grants @capability and whose membership has not ended at
-- @now. A member with no browser subscribed is left out, so the job never
-- claims a ledger row and then sends nothing. owner_name is the display name
-- of the garden's owner, empty when the garden has no owner. recipient_owns is
-- true when the member is that owner. There is no @garden_id because the job
-- runs across every garden.
-- name: ListTokenDeadlines :many
SELECT sqlc.embed(api_token), membership.id AS membership_id, membership.user_id,
    app_user.handle, app_user.timezone, garden.name AS garden_name,
    coalesce(owner.display_name, '')::text AS owner_name,
    coalesce(owner.id = membership.user_id, false)::boolean AS recipient_owns
FROM api_token
JOIN garden ON garden.id = api_token.garden_id
JOIN membership ON membership.garden_id = api_token.garden_id
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
ORDER BY api_token.expires_at, api_token.id, membership.created_at, membership.id;
