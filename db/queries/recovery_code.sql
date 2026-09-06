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
