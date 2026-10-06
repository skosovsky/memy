package conformance

import (
	"context"
	"errors"
	"testing"

	"github.com/skosovsky/memy"
)

const selectedRecord = "selected"

const sinkChunk = 7

// SinkFixture provisions artifacts with one-record lineage and probes metadata.
type SinkFixture struct {
	Adapter  memy.Sink
	Seed     func(context.Context, string, memy.Scope, memy.RevisionRef) error
	Contains func(context.Context, string, memy.Scope, memy.RevisionRef) (bool, error)
	Fail     func(error)
}

// SinkSuite verifies scoped, repeatable purge and truthful acknowledgements.
func SinkSuite(t *testing.T, factory func(*testing.T) SinkFixture) {
	t.Helper()
	for _, foreign := range foreignScopes(scope()) {
		t.Run("scoped_repeatable_purge/"+foreign.Key(), func(t *testing.T) {
			// Arrange: selected, unrelated, and foreign artifacts.
			f := factory(t)
			a := scope()
			b := foreign
			selected := memy.RevisionRef{RecordID: selectedRecord, Revision: 1}
			other := memy.RevisionRef{RecordID: conformanceOther, Revision: 1}
			must(t, f.Seed(t.Context(), selectedRecord, a, selected))
			must(t, f.Seed(t.Context(), conformanceOther, a, other))
			must(t, f.Seed(t.Context(), "foreign", b, selected))
			batch := sinkBatch(a)
			// Act: repeat the same durable purge identity.
			for range 2 {
				ack, err := f.Adapter.Purge(t.Context(), batch)
				must(t, err)
				if ack.Sink != f.Adapter.Name() || ack.OperationID != batch.OperationID || ack.Epoch != batch.Epoch ||
					ack.Chunk != batch.Chunk {
					t.Fatalf("unbound acknowledgement: %+v", ack)
				}
			}
			// Assert: only the selected exact scope is removed.
			assertArtifact(t, f, selectedRecord, a, selected, false)
			assertArtifact(t, f, conformanceOther, a, other, true)
			assertArtifact(t, f, "foreign", b, selected, true)
		})
	}
	t.Run("chunks_and_replay", func(t *testing.T) { sinkChunks(t, factory) })
	t.Run("failure_and_cancel", func(t *testing.T) {
		// Arrange.
		f := factory(t)
		a := scope()
		ref := memy.RevisionRef{RecordID: selectedRecord, Revision: 1}
		must(t, f.Seed(t.Context(), selectedRecord, a, ref))
		failure := errors.New("purge unavailable")
		f.Fail(failure)
		// Act and assert: failed deletion must not certify acknowledgement.
		ack, err := f.Adapter.Purge(t.Context(), sinkBatch(a))
		if !errors.Is(err, failure) || ack.Sink != "" {
			t.Fatalf("failed ack=%+v err=%v", ack, err)
		}
		assertArtifact(t, f, selectedRecord, a, ref, true)
		f.Fail(nil)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		ack, err = f.Adapter.Purge(ctx, sinkBatch(a))
		if !errors.Is(err, context.Canceled) || ack.Sink != "" {
			t.Fatalf("cancel ack=%+v err=%v", ack, err)
		}
		assertArtifact(t, f, selectedRecord, a, ref, true)
	})
}

func sinkBatch(sc memy.Scope) memy.PurgeBatch {
	return memy.PurgeBatch{
		OperationID: "purge",
		Scope:       sc,
		Epoch:       1,
		Chunk:       sinkChunk,
		Selector:    memy.Selector{Kind: memy.SelectRecord, ID: selectedRecord},
		Records:     []string{selectedRecord},
	}
}

func assertArtifact(t *testing.T, f SinkFixture, handle string, sc memy.Scope, ref memy.RevisionRef, want bool) {
	t.Helper()
	got, err := f.Contains(t.Context(), handle, sc, ref)
	must(t, err)
	if got != want {
		t.Fatalf("artifact %s present=%t want=%t", handle, got, want)
	}
}

func sinkChunks(t *testing.T, factory func(*testing.T) SinkFixture) {
	// Arrange: independent selected revisions and an unrelated artifact.
	f := factory(t)
	a := scope()
	first := memy.RevisionRef{RecordID: selectedRecord, Revision: 1}
	second := memy.RevisionRef{RecordID: "second", Revision: 2}
	other := memy.RevisionRef{RecordID: conformanceOther, Revision: 1}
	must(t, f.Seed(t.Context(), selectedRecord, a, first))
	must(t, f.Seed(t.Context(), "second", a, second))
	must(t, f.Seed(t.Context(), conformanceOther, a, other))
	batch := sinkBatch(a)
	// Act: acknowledge distinct chunks under one operation, then replay both.
	for round := range 2 {
		for i, ref := range []memy.RevisionRef{first, second} {
			batch.Chunk = sinkChunk + uint64(i)
			batch.Records = []string{ref.RecordID}
			ack, err := f.Adapter.Purge(t.Context(), batch)
			must(t, err)
			if ack.Sink != f.Adapter.Name() || ack.OperationID != batch.OperationID || ack.Epoch != batch.Epoch ||
				ack.Chunk != batch.Chunk {
				t.Fatalf("ack mismatch: %+v", ack)
			}
			assertArtifact(t, f, ref.RecordID, a, ref, false)
			if round == 0 && i == 0 {
				assertArtifact(t, f, "second", a, second, true)
			}
		}
	}
	// Assert: replay never removes an unselected reference.
	assertArtifact(t, f, conformanceOther, a, other, true)
}
