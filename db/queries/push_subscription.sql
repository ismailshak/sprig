-- name: ListPushSubscriptions :many
SELECT * FROM push_subscription
WHERE user_id = @user_id
ORDER BY created_at, id;

-- name: DeletePushSubscription :execrows
DELETE FROM push_subscription
WHERE user_id = @user_id AND id = @subscription_id;

-- name: UpsertPushSubscription :exec
-- The endpoint identifies the browser, so a browser subscribing again updates
-- its row instead of adding one. A row that belonged to another account moves
-- to the account signed in now. Its created_at and last_sent_at are reset,
-- because those dates were the other account's.
INSERT INTO push_subscription (user_id, endpoint, p256dh_key, auth_key, user_agent)
VALUES (@user_id, @endpoint, @p256dh_key, @auth_key, @user_agent)
ON CONFLICT (endpoint) DO UPDATE SET
    user_id = EXCLUDED.user_id,
    p256dh_key = EXCLUDED.p256dh_key,
    auth_key = EXCLUDED.auth_key,
    user_agent = EXCLUDED.user_agent,
    created_at = CASE WHEN push_subscription.user_id = EXCLUDED.user_id THEN push_subscription.created_at ELSE now() END,
    last_sent_at = CASE WHEN push_subscription.user_id = EXCLUDED.user_id THEN push_subscription.last_sent_at END;

-- name: SetPushSubscriptionSent :exec
UPDATE push_subscription SET last_sent_at = @sent_at
WHERE user_id = @user_id AND id = @subscription_id;

-- The browsers to notify when @actor_id logs care in the garden: every push
-- subscription of every other member whose membership has not ended and who
-- has the activity notification on.
-- name: ListActivitySubscriptions :many
SELECT sqlc.embed(push_subscription), app_user.handle, garden.name AS garden_name
FROM membership
JOIN app_user ON app_user.id = membership.user_id
JOIN garden ON garden.id = membership.garden_id
JOIN notification_preference ON notification_preference.membership_id = membership.id
    AND notification_preference.kind = 'activity' AND notification_preference.enabled
JOIN push_subscription ON push_subscription.user_id = membership.user_id
WHERE membership.garden_id = @garden_id
  AND membership.user_id <> @actor_id
  AND (membership.expires_at IS NULL OR membership.expires_at > @now::timestamptz)
ORDER BY membership.created_at, membership.id, push_subscription.created_at, push_subscription.id;

-- name: GetPushSubscriptionByEndpoint :one
-- Send a test looks the row up by endpoint, because the browser's push API
-- reads its own endpoint and not the row's id.
SELECT * FROM push_subscription
WHERE user_id = @user_id AND endpoint = @endpoint;

-- name: DeleteUserPushSubscriptions :exec
DELETE FROM push_subscription WHERE user_id = @user_id;
