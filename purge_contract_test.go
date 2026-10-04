package memy_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/store/sqlite"
)

type purgeAdapter struct {
	failure  error
	wrongAck bool
}

func (*purgeAdapter) Name() string { return "required-sink" }

func (s *purgeAdapter) Purge(_ context.Context, batch memy.PurgeBatch) (memy.PurgeAck, error) {
	ack := memy.PurgeAck{Sink: s.Name(), OperationID: batch.OperationID, Epoch: batch.Epoch}
	if s.wrongAck {
		ack.OperationID = "another-operation"
	}
	return ack, s.failure
}

func TestFailedPurgeRemainsFencedAndCanRecoverAcrossReopen(t *testing.T) {
	for _, failure := range []string{"missing", "unsupported", "invalid_ack"} {
		t.Run(failure, func(t *testing.T) { failedPurgeCase(t, failure) })
	}
}

func failedPurgeCase(t *testing.T, failure string) {
	t.Helper()
	// Arrange: the durable receipt records its mandatory participant during outage.
	path := filepath.Join(t.TempDir(), "purge.db")
	store, openErr := sqlite.Open(context.Background(), path, sqlite.Options{})
	if openErr != nil {
		t.Fatal(openErr)
	}
	f := newFixture(t, store)
	sink := &purgeAdapter{failure: errors.New("temporary outage")}
	f.config.Sinks = []memy.Sink{sink}
	f.engine, openErr = memy.New(f.config)
	if openErr != nil {
		t.Fatal(openErr)
	}
	proposal := f.propose(t, "remember", "private content", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit", "private", 0, proposal, memy.Append))
	request := memy.ForgetRequest{
		OperationID:   "forget",
		Selector:      memy.Selector{Kind: memy.SelectRecord, ID: "private"},
		Reason:        "user requested deletion",
		PolicyVersion: "deletion/v1",
	}
	pending, pendingErr := f.engine.Forget(context.Background(), f.actor, f.scope, "assist", request)
	if pendingErr != nil || pending.State != memy.PurgePending {
		t.Fatalf("pending=%+v err=%v", pending, pendingErr)
	}
	configurePurgeFailure(&f, sink, failure)
	f = reopenFixture(t, f, path)
	// Act: unsupported/invalid/missing acknowledgement must be a durable failure.
	failed, failedErr := f.engine.Forget(context.Background(), f.actor, f.scope, "assist", request)
	f = reopenFixture(t, f, path)
	replayed, replayErr := f.engine.Forget(context.Background(), f.actor, f.scope, "assist", request)
	_, deniedRead := f.engine.Get(context.Background(), f.actor, f.scope, "private", memy.ReadOptions{})
	// Assert: neither failure nor reopen permits reads or falsely completes deletion.
	if failedErr != nil || replayErr != nil || failed.State != memy.PurgeFailed || replayed.State != memy.PurgeFailed ||
		failed.Sinks[0].Acknowledged || failed.Batch.Epoch != pending.Batch.Epoch || !errors.Is(deniedRead, memy.ErrNotFound) {
		t.Fatalf("failed=%+v err=%v replay=%+v err=%v read=%v", failed, failedErr, replayed, replayErr, deniedRead)
	}
	// Repair the actual participant and retry the same operation; no new revoke.
	sink.failure, sink.wrongAck = nil, false
	f.config.Sinks = []memy.Sink{sink}
	f = reopenFixture(t, f, path)
	complete, completionErr := f.engine.Forget(context.Background(), f.actor, f.scope, "assist", request)
	if completionErr != nil || complete.State != memy.PurgeComplete || complete.Batch.Epoch != pending.Batch.Epoch {
		t.Fatalf("complete=%+v err=%v", complete, completionErr)
	}
}

func configurePurgeFailure(f *fixture, sink *purgeAdapter, failure string) {
	switch failure {
	case "missing":
		f.config.Sinks = nil
	case "unsupported":
		sink.failure = memy.ErrUnsupported
	case "invalid_ack":
		sink.failure, sink.wrongAck = nil, true
	}
}

func TestLifecycleCapabilitiesMatchTemporalAndFieldBehavior(t *testing.T) {
	// Arrange: an admitted store gives the engine full canonical revision history.
	f := newFixture(t, nil)
	// Act.
	capabilities := f.engine.Capabilities()
	// Assert: temporal filtering is implemented by the engine; masks are unsupported.
	if !capabilities.ValidTime || !capabilities.RecordedTime || capabilities.FieldProjection ||
		capabilities.Store.Durable || !capabilities.Store.Atomic || !capabilities.Store.ConditionalWrite {
		t.Fatalf("capabilities=%+v", capabilities)
	}
}
