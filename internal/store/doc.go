// Package store is the query layer, built on the pgx pool and the code sqlc generates from db/queries.
package store

// The generated files are checked in, and the lint task runs sqlc diff, so a
// query edited without regenerating fails there.
//go:generate sqlc generate --file ../../sqlc.yaml
