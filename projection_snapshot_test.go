package memy_test

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

type snapshotCallResult struct {
	projection memy.Projection[string, sourceRef]
	body       memy.ProjectedRecallResult[string, sourceRef]
	err        error
}

func projectionSnapshotCall(
	ctx context.Context,
	f fixture,
	path string,
	ref memy.RevisionRef,
	read memy.ReadOptions,
	projector memy.Projector[preference, sourceRef, string],
) snapshotCallResult {
	if path == "project" {
		projection, err := memy.Project(ctx, f.engine, f.actor, f.scope, ref.RecordID, read, projector)
		return snapshotCallResult{projection: projection, err: err}
	}
	var budget *memy.ProjectionBudget[string, sourceRef]
	if path == "packing" {
		budget = &memy.ProjectionBudget[string, sourceRef]{
			Max:    10000,
			Codec:  memy.JSONCodec[string]{},
			Policy: reference.JSONPacking[string, sourceRef]{},
		}
	}
	body, err := memy.RecallProjected(
		ctx,
		f.engine,
		f.actor,
		f.scope,
		"query",
		projectedSearch{refs: []memy.RevisionRef{ref}},
		memy.ScoreRanker[preference, sourceRef]{},
		memy.RecallOptions{Read: read, Limit: 10, Search: memy.SearchOptions{MaxCandidates: 10}},
		projector,
		budget,
	)
	return snapshotCallResult{body: body, err: err}
}

func TestProjectionSnapshotConcurrentConflict(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		for _, path := range []string{"project", "recall", "packing"} {
			for _, historical := range []bool{false, true} {
				t.Run(backend+"/"+path+"/historical="+fmtBool(historical), func(t *testing.T) {
					testProjectionConcurrentConflict(t, backend, path, historical)
				})
			}
		}
	}
}

func fmtBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func testProjectionConcurrentConflict(t *testing.T, backend, path string, historical bool) {
	t.Helper()
	// Arrange: prepare a legal conflict, then block after the projector has read Active.
	f := newFixture(t, raceStore(t, backend))
	ref := projectedSeed(t, f, "fact", "old")
	proposal := f.propose(t, "conflict-proposal", "new", memy.Interval{})
	request := f.acceptedRequest(t, "conflict-commit", "new-fact", 0, proposal, memy.Conflict, ref)
	read := memy.ReadOptions{Purpose: "assist", IncludeConflicts: true}
	if historical {
		read.RecordedAsOf = f.clock.Now()
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	projector := reference.ProjectorFunc[preference, sourceRef, string]{PolicyVersion: "state-projector/v1",
		Apply: func(ctx context.Context, record memy.Record[preference, sourceRef]) (string, error) {
			calls.Add(1)
			close(entered)
			select {
			case <-release:
				return string(record.State), nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
	}
	finished := make(chan snapshotCallResult, 1)
	// Act: commit independently while the callback is blocked.
	go func() { finished <- projectionSnapshotCall(t.Context(), f, path, ref, read, projector) }()
	select {
	case <-entered:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	f.clock.Set(f.clock.Now().Add(time.Second))
	f.commit(t, request)
	close(release)
	result := <-finished
	// Assert: exactly one callback; changed effective input has no delivered output.
	if calls.Load() != 1 {
		t.Fatalf("projector retried %d times", calls.Load())
	}
	if !historical {
		if !errors.Is(result.err, memy.ErrStaleInput) ||
			!reflect.DeepEqual(result.projection, memy.Projection[string, sourceRef]{}) ||
			!reflect.DeepEqual(result.body, memy.ProjectedRecallResult[string, sourceRef]{}) {
			t.Fatalf("stale output: %+v", result)
		}
		return
	}
	if result.err != nil {
		t.Fatal(result.err)
	}
	projection := result.projection
	if path != "project" {
		if len(result.body.Projections) != 1 {
			t.Fatalf("historical body: %+v", result.body)
		}
		projection = result.body.Projections[0]
	}
	if projection.Output != string(memy.Active) || projection.State != memy.Active {
		t.Fatalf("historical state changed: %+v", projection)
	}
}

func TestProjectionSnapshotCacheIdentity(t *testing.T) {
	// Arrange: deterministic state projector, identical authority/read bindings.
	f := newFixture(t, nil)
	ref := projectedSeed(t, f, "fact", "old")
	projector := reference.ProjectorFunc[preference, sourceRef, string]{
		PolicyVersion: "state/v1",
		Apply: func(_ context.Context, r memy.Record[preference, sourceRef]) (string, error) {
			return string(r.State), nil
		},
	}
	read := memy.ReadOptions{Purpose: "assist", IncludeConflicts: true}
	historical := read
	historical.RecordedAsOf = f.clock.Now()
	before := projectSnapshotFixture(t, f, ref, read, projector)
	unchanged := projectSnapshotFixture(t, f, ref, read, projector)
	old := projectSnapshotFixture(t, f, ref, historical, projector)
	// Act: lawful conflict preserves the same content revision but changes effective state.
	f.clock.Set(f.clock.Now().Add(time.Second))
	proposal := f.propose(t, "conflict-proposal", "new", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "conflict-commit", "new-fact", 0, proposal, memy.Conflict, ref))
	after := projectSnapshotFixture(t, f, ref, read, projector)
	stableHistorical := projectSnapshotFixture(t, f, ref, historical, projector)
	// Assert.
	if before.CacheKey != unchanged.CacheKey || before.CacheKey == after.CacheKey || before.Output == after.Output ||
		old.CacheKey != stableHistorical.CacheKey || old.Output != stableHistorical.Output || after.State != memy.Conflicted {
		t.Fatalf("cache identity: before=%+v after=%+v historical=%+v", before, after, stableHistorical)
	}
}

func projectSnapshotFixture(
	t *testing.T,
	f fixture,
	ref memy.RevisionRef,
	read memy.ReadOptions,
	projector memy.Projector[preference, sourceRef, string],
) memy.Projection[string, sourceRef] {
	t.Helper()
	result, err := memy.Project(t.Context(), f.engine, f.actor, f.scope, ref.RecordID, read, projector)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
