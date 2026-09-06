package store

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// uniqueViolation is the SQLSTATE Postgres returns for a write a unique index
// refused.
const uniqueViolation = "23505"

// handleIndex is the unique index on app_user.handle. Postgres names an index
// declared with UNIQUE on the column after the table and the column.
const handleIndex = "app_user_handle_key"

// passkeyIndex is the unique index on passkey_credential.credential_id.
const passkeyIndex = "passkey_credential_credential_id_key" //nolint:gosec // the name of a unique index, not a secret

// HandleTaken reports whether err is Postgres refusing a write because another
// account already holds the handle.
func HandleTaken(err error) bool {
	return violates(err, handleIndex)
}

// CredentialTaken reports whether err is Postgres refusing a write because a
// passkey with the same credential id is already registered.
func CredentialTaken(err error) bool {
	return violates(err, passkeyIndex)
}

// violates reports whether err is Postgres refusing a write because of the
// unique index named.
func violates(err error, index string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == index
}
