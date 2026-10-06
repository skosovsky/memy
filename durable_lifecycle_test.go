package memy_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
	"github.com/skosovsky/memy/store/sqlite"
)

func reopenFixture(t *testing.T, f fixture, path string) fixture {
	t.Helper()
	if err := f.config.Store.Close(); err != nil {
		t.Fatal(err)
	}
	store, operationErr := sqlite.Open(context.Background(), path, sqlite.Options{})
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	f.config.Store = store
	f.engine, operationErr = memy.New(f.config)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	return f
}

func TestDurableCommitFaultAndOperationRecovery(t *testing.T) {
	for _, stage := range []sqlite.Stage{sqlite.BeforeCommit, sqlite.AfterCommit} {
		t.Run(string(stage), func(t *testing.T) { durableCommitStage(t, stage) })
	}
}

func durableCommitStage(t *testing.T, stage sqlite.Stage) {
	t.Helper()

	// Arrange: inject the fault only for the canonical commit transaction.
	path := filepath.Join(t.TempDir(), "knowledge.db")
	injected := errors.New("commit fault")
	var armed atomic.Bool
	store, transactionErr := sqlite.Open(
		context.Background(),
		path,
		sqlite.Options{Fault: func(_ context.Context, at sqlite.Stage) error {
			if at == stage && armed.CompareAndSwap(true, false) {
				return injected
			}
			return nil
		}},
	)
	if transactionErr != nil {
		t.Fatal(transactionErr)
	}
	f := newFixture(t, store)
	p := f.propose(t, "remember", "UTC+7", memy.Interval{})
	request := f.acceptedRequest(t, "commit", "timezone", 0, p, memy.Append)
	armed.Store(true)
	// Act.
	_, faultErr := f.engine.Commit(context.Background(), f.actor, f.scope, "assist", request)
	f = reopenFixture(t, f, path)
	before, beforeErr := f.engine.Get(context.Background(), f.actor, f.scope, "timezone", memy.ReadOptions{})
	recovered := f.commit(t, request)
	repeated := f.commit(t, request)
	state, transactionErr := fullSnapshot(f.engine, context.Background(), f.actor, f.scope, memy.ReadOptions{})
	if transactionErr != nil {
		t.Fatal(transactionErr)
	}
	// Assert: retry recovers one canonical revision and the same durable receipt.
	if !errors.Is(faultErr, injected) || recovered != repeated || recovered.Revision != 1 || len(state) != 1 ||
		state[0].Payload.Value != "UTC+7" {
		t.Fatalf("fault=%v recovered=%+v state=%+v", faultErr, recovered, state)
	}
	if stage == sqlite.BeforeCommit && !errors.Is(beforeErr, memy.ErrNotFound) {
		t.Fatalf("failed commit survived: %+v %v", before, beforeErr)
	}
	if stage == sqlite.AfterCommit &&
		(!errors.Is(faultErr, memy.ErrUnknownOutcome) || beforeErr != nil || before.Revision != 1) {
		t.Fatalf("committed outcome lost: %+v %v fault=%v", before, beforeErr, faultErr)
	}
}

func TestDurableForgetFaultPendingReceiptAndReopen(t *testing.T) {
	for _, stage := range []sqlite.Stage{sqlite.BeforeCommit, sqlite.AfterCommit} {
		t.Run(string(stage), func(t *testing.T) { durableForgetStage(t, stage) })
	}
}

func durableForgetStage(t *testing.T, stage sqlite.Stage) {
	t.Helper()

	// Arrange.
	path := filepath.Join(t.TempDir(), "knowledge.db")
	injected := errors.New("revoke fault")
	var armed atomic.Bool
	store, transactionErr := sqlite.Open(
		context.Background(),
		path,
		sqlite.Options{Fault: func(_ context.Context, at sqlite.Stage) error {
			if at == stage && armed.CompareAndSwap(true, false) {
				return injected
			}
			return nil
		}},
	)
	if transactionErr != nil {
		t.Fatal(transactionErr)
	}
	f := newFixture(t, store)
	index := reference.NewIndex[string]("index", nil)
	summary := reference.NewProjectionSink("summary")
	f.config.Sinks = []memy.Sink{index, summary}
	f.engine, transactionErr = memy.New(f.config)
	if transactionErr != nil {
		t.Fatal(transactionErr)
	}
	committed, fence := staged(t, f, index, summary)
	if err := index.Acknowledge(context.Background(), committed.Visibility); err != nil {
		t.Fatal(err)
	}
	request := memy.ForgetRequest{
		OperationID: "forget",
		Selector:    memy.Selector{Kind: memy.SelectRecord, ID: "timezone"},
		Expected: []memy.RevisionRef{
			{RecordID: "timezone", Revision: 1},
		},
		Reason:        "user request",
		PolicyVersion: "deletion/v1",
	}
	armed.Store(true)
	// Act.
	_, faultErr := fullForget(f.engine, context.Background(), f.actor, f.scope, "assist", request)
	f = reopenFixture(t, f, path)
	_, beforeErr := f.engine.Get(context.Background(), f.actor, f.scope, "timezone", memy.ReadOptions{})
	summary.FailPurge(errors.New("summary unavailable"))
	pending, transactionErr := fullForget(f.engine, context.Background(), f.actor, f.scope, "assist", request)
	if transactionErr != nil {
		t.Fatal(transactionErr)
	}
	f = reopenFixture(t, f, path)
	lateErr := f.engine.WithDerivedWrite(
		context.Background(),
		f.actor,
		fence,
		"assist",
		[]memy.RevisionRef{{RecordID: "timezone", Revision: 1}},
		func(context.Context) error { return errors.New("must not enter") },
	)
	summary.FailPurge(nil)
	complete, transactionErr := fullForget(f.engine, context.Background(), f.actor, f.scope, "assist", request)
	if transactionErr != nil {
		t.Fatal(transactionErr)
	}
	f = reopenFixture(t, f, path)
	replayed, transactionErr := fullForget(f.engine, context.Background(), f.actor, f.scope, "assist", request)
	if transactionErr != nil {
		t.Fatal(transactionErr)
	}
	_, afterErr := f.engine.Get(context.Background(), f.actor, f.scope, "timezone", memy.ReadOptions{})
	// Assert: durable fence, per-sink ack, idempotency and late-job rejection
	// survive reopen even when external deletion initially failed.
	if !errors.Is(faultErr, injected) || pending.State != memy.PurgePending || !pending.Sinks[0].Acknowledged ||
		pending.Sinks[1].Acknowledged ||
		complete.State != memy.PurgeComplete ||
		replayed.Batch.Epoch != complete.Batch.Epoch ||
		!errors.Is(lateErr, memy.ErrStaleInput) ||
		!errors.Is(afterErr, memy.ErrNotFound) {
		t.Fatalf("fault=%v pending=%+v complete=%+v late=%v", faultErr, pending, complete, lateErr)
	}
	if stage == sqlite.BeforeCommit && beforeErr != nil {
		t.Fatalf("failed revoke closed recall: %v", beforeErr)
	}
	if stage == sqlite.AfterCommit &&
		(!errors.Is(faultErr, memy.ErrUnknownOutcome) || !errors.Is(beforeErr, memy.ErrNotFound)) {
		t.Fatalf("durable revoke was not recovered: %v %v", faultErr, beforeErr)
	}
}
