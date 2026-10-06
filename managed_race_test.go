package memy_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/store/memory"
	"github.com/skosovsky/memy/store/sqlite"
)

const revokeProbeDeadline = 20 * time.Millisecond

func TestManagedWriteCannotRacePurgeToResurrectArtifacts(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		t.Run(backend, func(t *testing.T) { managedRaceCase(t, backend) })
	}
}

func managedRaceCase(t *testing.T, backend string) {
	t.Helper()
	// Arrange: hold the real canonical read boundary while a derived job is in flight.
	store := raceStore(t, backend)
	f, index, summary := withSinksStore(t, store)
	receipt, fence := staged(t, f, index, summary)
	lineage := []memy.RevisionRef{{RecordID: receipt.RecordID, Revision: receipt.Revision}}
	entered, release := make(chan struct{}), make(chan struct{})
	writeDone := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		writeDone <- f.engine.WithDerivedWrite(ctx, f.actor, fence, "assist", lineage, func(ctx context.Context) error {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			if stageErr := index.Stage(ctx, f.scope, memy.Candidate{RecordID: receipt.RecordID, Revision: receipt.Revision}); stageErr != nil {
				return stageErr
			}
			return summary.Put(ctx, "in-flight-summary", f.scope, lineage, []byte("private derived artifact"))
		})
	}()
	<-entered
	request := memy.ForgetRequest{
		OperationID:   "forget",
		Selector:      memy.Selector{Kind: memy.SelectRecord, ID: receipt.RecordID},
		Reason:        "user request",
		PolicyVersion: "deletion/v1",
	}
	// Act: revoke cannot commit while the managed writer holds canonical exclusion.
	probe, probeCancel := context.WithTimeout(ctx, revokeProbeDeadline)
	_, blockedErr := fullForget(probe, f.engine, f.actor, f.scope, "assist", request)
	probeCancel()
	close(release)
	writeErr := <-writeDone
	purged, purgeErr := fullForget(ctx, f.engine, f.actor, f.scope, "assist", request)
	lateAckErr := index.Acknowledge(ctx, receipt.Visibility)
	lateWriteErr := f.engine.WithDerivedWrite(ctx, f.actor, fence, "assist", lineage,
		func(context.Context) error { return errors.New("stale callback must not run") })
	// Assert: completed in-flight writes are purged; post-revoke writes stay fenced.
	if !errors.Is(blockedErr, context.DeadlineExceeded) || writeErr != nil || purgeErr != nil ||
		purged.State != memy.PurgeComplete ||
		summary.Contains("in-flight-summary") ||
		!errors.Is(lateAckErr, memy.ErrNotFound) ||
		!errors.Is(lateWriteErr, memy.ErrStaleInput) {
		t.Fatalf(
			"blocked=%v write=%v purge=%+v err=%v ack=%v late=%v",
			blockedErr,
			writeErr,
			purged,
			purgeErr,
			lateAckErr,
			lateWriteErr,
		)
	}
}

func raceStore(t *testing.T, backend string) memy.Store {
	t.Helper()
	if backend == "memory" {
		return memory.New()
	}
	store, openErr := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "race.db"), sqlite.Options{})
	if openErr != nil {
		t.Fatal(openErr)
	}
	return store
}
