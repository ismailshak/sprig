-- Both reads exist for the development sign-in, which offers every user and
-- takes a handle as the whole credential. Nothing in a production build calls
-- them, and they go when the development sign-in does.
-- name: ListUsers :many
SELECT * FROM app_user
ORDER BY created_at, id;

-- name: GetUserByHandle :one
SELECT * FROM app_user
WHERE handle = @handle;
