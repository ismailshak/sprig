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
