package store

import (
	"errors"
	"testing"

	"uuid"
)

func TestInTx_AFailedStatementRollsBackTheEarlierOnes(t *testing.T) {
	queries, _ := seedTwoGardens(t)
	before := plantCount(t, queries)

	err := queries.InTx(t.Context(), func(q *Queries) error {
		plant, err := q.CreatePlant(t.Context(), CreatePlantParams{GardenID: testGardenID, Nickname: ptr("Ada")})
		if err != nil {
			return err
		}
		// A care type from another garden. The composite foreign key rejects it.
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

func TestInTx_CommittedRowsAreReadableAfterwards(t *testing.T) {
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

// Tests run inside their own transaction, so InTx's transaction becomes a
// savepoint. Rolling back the savepoint must leave the outer transaction
// usable, or every handler test that writes would fail on the following read.
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
