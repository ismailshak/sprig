package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/ismailshak/sprig/internal/pgtest"
)

// openTraced opens a pool on the shared test database whose spans are kept in
// the returned recorder once they end.
func openTraced(t *testing.T) (*pgxpool.Pool, *sdktrace.TracerProvider, *tracetest.SpanRecorder) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	pool, err := Open(t.Context(), pgtest.Shared(t, migrateSchema), provider)
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool, provider, recorder
}

func TestOpen_AQuerySpanIsNamedAfterTheSqlcQueryAndHoldsNoArgument(t *testing.T) {
	const hash = "c2Vzc2lvbi10b2tlbi1oYXNo"
	pool, provider, recorder := openTraced(t)

	ctx, request := provider.Tracer("test").Start(t.Context(), "request")
	_, err := New(pool).GetSessionByTokenHash(ctx, hash)
	request.End()
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("GetSessionByTokenHash: %v, want no rows", err)
	}

	var named []sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		if span.Name() == "GetSessionByTokenHash" {
			named = append(named, span)
		}
	}
	if len(named) == 0 {
		t.Fatal("no span is named GetSessionByTokenHash")
	}
	// pgx prepares the statement the first time a connection runs it. The
	// prepare span is a child of the query span and has the same name.
	underRequest := false
	for _, span := range named {
		underRequest = underRequest || span.Parent().SpanID() == request.SpanContext().SpanID()
		text := ""
		for _, kv := range span.Attributes() {
			if kv.Key == "db.query.text" {
				text = kv.Value.AsString()
			}
			if strings.Contains(kv.Value.String(), hash) {
				t.Errorf("%s = %q holds the token hash", kv.Key, kv.Value.String())
			}
		}
		if !strings.HasPrefix(text, "-- name: GetSessionByTokenHash") {
			t.Errorf("db.query.text = %q, want the statement", text)
		}
	}
	if !underRequest {
		t.Error("no GetSessionByTokenHash span is a child of the request's span")
	}
}

func TestOpen_AStatementNotFromSqlcIsNamedByItsFirstKeyword(t *testing.T) {
	pool, provider, recorder := openTraced(t)

	ctx, request := provider.Tracer("test").Start(t.Context(), "migrate")
	_, err := pool.Exec(ctx, "-- a comment that is not a sqlc name\nselect 1")
	request.End()
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	var names []string
	for _, span := range recorder.Ended() {
		if span.Name() != "migrate" {
			names = append(names, span.Name())
		}
	}
	if len(names) == 0 || names[0] != "SELECT" {
		t.Errorf("query span names = %q, want SELECT", names)
	}
}
