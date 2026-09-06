-- name: ListPushSubscriptions :many
SELECT * FROM push_subscription
WHERE user_id = @user_id
ORDER BY created_at, id;

-- name: DeletePushSubscription :execrows
DELETE FROM push_subscription
WHERE user_id = @user_id AND id = @subscription_id;
