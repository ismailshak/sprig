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
