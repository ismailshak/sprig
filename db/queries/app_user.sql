-- Both queries exist for the development sign-in, which lists every user and
-- signs one in by handle alone. Nothing in a production build calls them, and
-- they will be removed with the development sign-in.
-- name: ListUsers :many
SELECT * FROM app_user
ORDER BY created_at, id;

-- name: GetUserByHandle :one
SELECT * FROM app_user
WHERE handle = @handle;

-- name: UpdateAccount :exec
UPDATE app_user
SET display_name = @display_name, timezone = @timezone
WHERE id = @user_id;
