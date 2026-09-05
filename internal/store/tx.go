package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// InTx runs fn with a Queries bound to a transaction. It commits when fn
// returns nil and rolls back otherwise. Use it for writes that must land
// together, such as a new plant and its schedules.
//
// The transaction begins on whatever the Queries was built over: the pool in
// the running server, or the test's own transaction in a handler test, where
// pgx makes it a savepoint. Anything else cannot begin one, and the error for
// that is a wiring mistake rather than a failed request.
func (q *Queries) InTx(ctx context.Context, fn func(*Queries) error) error {
	beginner, ok := q.db.(interface {
		Begin(context.Context) (pgx.Tx, error)
	})
	if !ok {
		return fmt.Errorf("a %T cannot begin a transaction", q.db)
	}

	tx, err := beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	// Rollback after a commit returns ErrTxClosed, which is harmless.
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
