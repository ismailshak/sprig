-- name: ListPasskeys :many
SELECT * FROM passkey_credential
WHERE user_id = @user_id
ORDER BY created_at, id;

-- The subselect refuses a delete that names the only credential the account
-- has, since there is no password behind it and no self-service way back in.
-- Two removals sent at the same moment can still empty the list: each one
-- counts on its own snapshot and neither blocks the other.
-- name: DeletePasskey :execrows
DELETE FROM passkey_credential AS k
WHERE k.user_id = @user_id AND k.id = @passkey_id
  AND 1 < (SELECT count(*) FROM passkey_credential AS held WHERE held.user_id = k.user_id);

-- name: CreatePasskey :one
INSERT INTO passkey_credential (user_id, credential_id, name, public_key, sign_count, flags, transports)
VALUES (@user_id, @credential_id, @name, @public_key, @sign_count, @flags, @transports)
RETURNING *;

-- Sign-in has no username field, so the credential the browser returns is the
-- only thing naming the account. The query returns that account row too.
-- name: GetPasskeyByCredentialID :one
SELECT sqlc.embed(k), sqlc.embed(u)
FROM passkey_credential AS k
JOIN app_user AS u ON u.id = k.user_id
WHERE k.credential_id = @credential_id;

-- The counter is compared in the statement that writes it, so two sign-ins
-- sending the same counter cannot both pass: the second finds the row already
-- advanced and updates nothing. Zero stored and zero returned is an
-- authenticator that keeps no counter, and is the one equal pair allowed.
-- name: RecordPasskeyUse :execrows
UPDATE passkey_credential
SET sign_count = @sign_count, last_used_at = @used_at
WHERE id = @passkey_id
  AND (sign_count < @sign_count OR (sign_count = 0 AND @sign_count = 0));
