package memy_test

import (
	"errors"
	"testing"

	"github.com/skosovsky/memy"
)

func TestMaintenanceErrorDistinguishesRetiredIdentity(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		for _, operation := range []string{"sweep", "forget"} {
			t.Run(backend+"/"+operation, func(t *testing.T) { maintenanceErrorCase(t, backend, operation) })
		}
	}
}

func maintenanceErrorCase(t *testing.T, backend, operation string) {
	t.Helper()
	// Arrange: one canonical record and a deliberately interrupted durable pass.
	f := newFixture(t, raceStore(t, backend))
	proposal := f.propose(t, "remember", "private", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit", "timezone", 0, proposal, memy.Append))
	request := memy.ForgetRequest{
		OperationID:   "forget",
		Selector:      memy.Selector{Kind: memy.SelectRecord, ID: "timezone"},
		Reason:        "remove",
		PolicyVersion: "forget/v1",
		Limit:         1,
		MaxBytes:      1 << 20,
	}
	if operation == "sweep" {
		_, err := f.engine.Sweep(
			t.Context(),
			f.actor,
			f.scope,
			"assist",
			memy.SweepRequest{OperationID: "pass", Limit: 1, MaxBytes: 1 << 20},
		)
		if err != nil {
			t.Fatal(err)
		}
	} else {
		if _, err := f.engine.Forget(t.Context(), f.actor, f.scope, "assist", request); err != nil {
			t.Fatal(err)
		}
	}
	// Act: the read is excluded by active maintenance.
	_, readErr := f.engine.Get(t.Context(), f.actor, f.scope, "timezone", memy.ReadOptions{})
	// Assert: existing revoke inspection survives with an explicit transient discriminator.
	if !errors.Is(readErr, memy.ErrRevoked) || !errors.Is(readErr, memy.ErrMaintenance) {
		t.Fatalf("maintenance=%v", readErr)
	}
	if operation == "sweep" {
		if _, err := fullSweep(t.Context(), f.engine, f.actor, f.scope, "assist", "pass"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fullForget(t.Context(), f.engine, f.actor, f.scope, "assist", request); err != nil {
		t.Fatal(err)
	}
	replacement := f.propose(t, "replacement", "private", memy.Interval{})
	_, commitErr := f.engine.Commit(
		t.Context(),
		f.actor,
		f.scope,
		"assist",
		f.acceptedRequest(t, "replacement-commit", "timezone", 0, replacement, memy.Append),
	)
	if !errors.Is(commitErr, memy.ErrRevoked) || errors.Is(commitErr, memy.ErrMaintenance) {
		t.Fatalf("retired=%v", commitErr)
	}
}
