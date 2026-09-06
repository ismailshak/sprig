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
-- timezone, so every caller chooses one. The caller chooses the id too,
-- because Set up your garden gives the id to the browser before the row is
-- written and the passkey is registered under it.
--
-- A handle another account already holds inserts nothing and returns no row,
-- rather than raising a unique violation. The violation would end the
-- transaction the caller is in, and the caller tries the next candidate handle
-- in that same transaction.
-- name: CreateUser :one
INSERT INTO app_user (id, display_name, handle, timezone)
VALUES (@id, @display_name, @handle, @timezone)
ON CONFLICT (handle) DO NOTHING
RETURNING *;

-- AnyUsers reports whether an account exists. Set up your garden is open on an
-- install with none, whatever SPRIG_SIGNUP_ENABLED says.
-- name: AnyUsers :one
SELECT EXISTS (SELECT 1 FROM app_user);

-- LockUsers blocks every other insert into app_user until the transaction
-- ends. With sign-up off, Set up your garden checks that no account exists and
-- then inserts one. Two of those running at once would both see an empty
-- table, so the lock makes the second wait and then see the first.
-- name: LockUsers :exec
LOCK TABLE app_user IN SHARE ROW EXCLUSIVE MODE;
