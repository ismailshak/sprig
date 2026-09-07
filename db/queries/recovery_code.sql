-- name: GetRecoveryBatch :one
-- Counts an account's recovery codes and reports when they were made, for the
-- Recovery codes page. Codes are made and replaced as a whole batch, so every
-- row a user has belongs to the same batch and shares its generated_at. An
-- account with no codes gets no row back, rather than a row of zeroes with a
-- null date. An account whose codes have all been used still gets a row, and
-- the page reads "0 of 10 left".
SELECT count(*)                                AS size,
       count(*) FILTER (WHERE used_at IS NULL) AS unused,
       max(generated_at)::timestamptz          AS made_at
FROM recovery_code
WHERE user_id = @user_id
HAVING count(*) > 0;

-- Writes one batch of codes for an account, all with the same generated_at.
-- The caller has deleted the previous batch in the same transaction.
-- name: CreateRecoveryBatch :exec
INSERT INTO recovery_code (user_id, code_hash, generated_at)
SELECT @user_id, unnest(@code_hashes::text[]), @generated_at::timestamptz;

-- name: DeleteRecoveryCodes :exec
DELETE FROM recovery_code WHERE user_id = @user_id;

-- Finds the account an unused code belongs to. The lookup is by hash across
-- every account, so the Recover an account page never asks whose code it is.
-- A used code and a code nobody made both return no row.
-- name: GetLiveRecoveryCode :one
SELECT user_id FROM recovery_code
WHERE code_hash = @code_hash AND used_at IS NULL;

-- Marks an unused code used and returns the account it belongs to. The query
-- returns no row when the code is already used or was never made.
-- name: RedeemRecoveryCode :one
UPDATE recovery_code SET used_at = @now::timestamptz
WHERE code_hash = @code_hash AND used_at IS NULL
RETURNING user_id;
