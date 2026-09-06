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
-- Nothing checks that the handle is free before this runs. The unique index on
-- app_user.handle rejects a duplicate. A check made first would let two
-- accounts saving the same handle at the same moment both pass it.
UPDATE app_user
SET display_name = @display_name, handle = @handle, timezone = @timezone
WHERE id = @user_id;

-- CreateUser inserts an account and returns the row. There is no default
-- timezone, so every caller chooses one.
-- name: CreateUser :one
INSERT INTO app_user (display_name, handle, timezone)
VALUES (@display_name, @handle, @timezone)
RETURNING *;
