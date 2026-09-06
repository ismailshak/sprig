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

// HandleTaken reports whether err is Postgres refusing a write because another
// account already holds the handle.
func HandleTaken(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == handleIndex
}
