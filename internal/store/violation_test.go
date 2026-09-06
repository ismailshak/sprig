package store

import (
	"errors"
	"testing"
)

func TestHandleTaken_TheSecondAccountToTakeAHandleIsRefused(t *testing.T) {
	pool := migratedPool(t)
	ctx := t.Context()

	if _, err := pool.Exec(ctx, "INSERT INTO app_user (display_name, handle, timezone) VALUES ('Emma', 'emma', 'Europe/London')"); err != nil {
		t.Fatalf("inserting the first account: %v", err)
	}
	_, err := pool.Exec(ctx, "INSERT INTO app_user (display_name, handle, timezone) VALUES ('Emma Fletcher', 'emma', 'Europe/London')")

	if err == nil {
		t.Fatal("two accounts hold the handle emma, want the second refused")
	}
	if !HandleTaken(err) {
		t.Errorf("HandleTaken says no about %v", err)
	}
}

func TestHandleTaken_AnErrorFromSomethingElseIsNotAHandleCollision(t *testing.T) {
	pool := migratedPool(t)

	_, err := pool.Exec(t.Context(), "INSERT INTO app_user (display_name, timezone) VALUES ('Emma', 'Europe/London')")

	if err == nil {
		t.Fatal("an account with no handle was inserted, want the not-null constraint to refuse it")
	}
	if HandleTaken(err) {
		t.Errorf("HandleTaken says yes about %v", err)
	}
	if HandleTaken(errors.New("the pool is closed")) {
		t.Error("HandleTaken says yes about an error that did not come from Postgres")
	}
}
