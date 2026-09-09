-- ListUsers lists the accounts the development sign-in offers. Closed accounts
-- are left out because nothing may sign in as them. Only a build tagged dev
-- calls it.
-- name: ListUsers :many
SELECT * FROM app_user
WHERE closed_at IS NULL
ORDER BY created_at, id;

-- GetUserByHandle returns the account with that handle. The development
-- sign-in and sprig admin invite both name an account by it. A closed account
-- is left out, because nothing may sign in as one and no link may be issued
-- for one.
-- name: GetUserByHandle :one
SELECT * FROM app_user
WHERE handle = @handle AND closed_at IS NULL;

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

-- name: GetUser :one
SELECT * FROM app_user
WHERE id = @id;

-- Switching gardens records the garden on the account, so the next session
-- starts there.
-- name: SetLastGarden :exec
UPDATE app_user SET last_garden_id = @garden_id
WHERE id = @user_id;

-- Lists the gardens where this account is the only owner, ordered by name. An
-- expired membership counts on neither side, because an owner whose access has
-- ended can no longer delete the garden or hand it on. It takes a user id and
-- no garden id, because it searches every garden.
-- name: ListGardensOnlyThisUserOwns :many
SELECT garden.* FROM garden
JOIN membership AS mine ON mine.garden_id = garden.id
WHERE mine.user_id = @user_id AND mine.role = 'owner'
  AND (mine.expires_at IS NULL OR mine.expires_at > @now::timestamptz)
  AND NOT EXISTS (
    SELECT 1 FROM membership AS other
    WHERE other.garden_id = garden.id AND other.role = 'owner' AND other.user_id <> mine.user_id
      AND (other.expires_at IS NULL OR other.expires_at > @now::timestamptz))
ORDER BY garden.name, garden.id;

-- Marks the account closed. The row is kept because care events and photos
-- still show the person's name. The caller deletes the passkeys, sessions and
-- memberships in the same transaction.
-- name: CloseAccount :execrows
UPDATE app_user SET closed_at = @now::timestamptz, last_garden_id = NULL
WHERE id = @user_id AND closed_at IS NULL;
