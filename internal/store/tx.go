package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// InTx runs fn against a Queries bound to a transaction, committing when fn
// returns nil and rolling back otherwise. It is for a write that is one act to
// the person who asked for it, such as a plant and the schedules it arrives
// with.
//
// The transaction is begun on whatever the Queries was built over, which is the
// pool in the running server and the test's own transaction under a handler
// test, where pgx makes it a savepoint. Anything else cannot begin one, and
// that is a wiring mistake rather than a request that failed.
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
	// A rollback after the commit answers ErrTxClosed, which is not a failure.
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
