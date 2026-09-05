// Package store is the database layer: the pgx pool, migrations, and the
// queries sqlc generates from db/queries.
package store

// The generated files are checked in. The lint task runs sqlc diff, so a query
// edited without regenerating fails lint.
//go:generate sqlc generate --file ../../sqlc.yaml
