package store

import (
	"errors"
	"testing"

	"uuid"
)

func TestInTx_AStatementThatFailsTakesTheOnesBeforeItWithIt(t *testing.T) {
	queries, _ := seedTwoGardens(t)
	before := plantCount(t, queries)

	err := queries.InTx(t.Context(), func(q *Queries) error {
		plant, err := q.CreatePlant(t.Context(), CreatePlantParams{GardenID: testGardenID, Nickname: ptr("Ada")})
		if err != nil {
			return err
		}
		// Another garden's care type, which the composite foreign key refuses.
		_, err = q.CreateCareSchedule(t.Context(), CreateCareScheduleParams{
			GardenID:      testGardenID,
			PlantID:       plant.ID,
			CareTypeID:    otherWaterID,
			IntervalCount: ptr(int32(7)),
			IntervalUnit:  ptr("day"),
		})
		return err
	})

	if err == nil {
		t.Fatal("a schedule pointing at another garden's care type was written")
	}
	if got := plantCount(t, queries); got != before {
		t.Errorf("the garden holds %d plants, want the %d it held before the failure", got, before)
	}
}

func TestInTx_WhatItCommitsIsReadableAfterwards(t *testing.T) {
	queries, _ := seedTwoGardens(t)

	var id uuid.UUID
	err := queries.InTx(t.Context(), func(q *Queries) error {
		plant, err := q.CreatePlant(t.Context(), CreatePlantParams{GardenID: testGardenID, Nickname: ptr("Ada")})
		id = plant.ID
		return err
	})
	if err != nil {
		t.Fatalf("InTx: %v", err)
	}

	plant, err := queries.GetPlant(t.Context(), testGardenID, id)
	if err != nil {
		t.Fatalf("reading the committed plant: %v", err)
	}
	if plant.DisplayName() != "Ada" {
		t.Errorf("the plant reads %q, want Ada", plant.DisplayName())
	}
}

// The tests run inside a transaction of their own, which pgx makes a
// savepoint. A rollback of the inner one has to leave the outer one usable, or
// every handler test that writes would fail on the read after it.
func TestInTx_ARollbackLeavesTheSurroundingTransactionOpen(t *testing.T) {
	queries, _ := seedTwoGardens(t)

	failed := errors.New("nothing to do with the database")
	if err := queries.InTx(t.Context(), func(*Queries) error { return failed }); !errors.Is(err, failed) {
		t.Fatalf("InTx returned %v, want the caller's own error", err)
	}

	if _, err := queries.GetPlant(t.Context(), testGardenID, montyID); err != nil {
		t.Errorf("the surrounding transaction is unusable: %v", err)
	}
}

func plantCount(t *testing.T, queries *Queries) int64 {
	t.Helper()

	n, err := queries.CountPlants(t.Context(), testGardenID)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func ptr[T any](v T) *T {
	return &v
}
