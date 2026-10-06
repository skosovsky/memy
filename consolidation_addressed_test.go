package memy_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/store/memory"
	"github.com/skosovsky/memy/store/sqlite"
)

func TestConsolidationReadsOnlySelectedEvidence(t *testing.T) {
	for _, adapter := range []string{"memory", "sqlite"} {
		t.Run(adapter, func(t *testing.T) {
			// Arrange: two selected independent records and unavailable unrelated evidence.
			var store memy.Store = memory.New()
			if adapter == "sqlite" {
				var err error
				store, err = sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "addressed.db"), sqlite.Options{})
				if err != nil {
					t.Fatal(err)
				}
			}
			counted := &costStore{Store: store}
			f := newFixture(t, counted)
			var inputs []memy.RevisionRef
			for _, id := range []string{"selected-a", "selected-b"} {
				p := f.propose(t, "remember-"+id, "same", memy.Interval{Known: true})
				r := f.commit(t, f.acceptedRequest(t, "commit-"+id, id, 0, p, memy.Append))
				inputs = append(inputs, memy.RevisionRef{RecordID: id, Revision: r.Revision})
			}
			other := f
			other.source = memy.Source[sourceRef]{ID: "unrelated", Revision: "r1", Reference: sourceRef{URI: "host://unrelated"}}
			if err := f.sources.Put(f.scope, other.source); err != nil {
				t.Fatal(err)
			}
			p := other.propose(t, "remember-unrelated", "unrelated", memy.Interval{Known: true})
			other.commit(t, other.acceptedRequest(t, "commit-unrelated", "unrelated", 0, p, memy.Append))
			f.sources.Remove(f.scope, other.source.ID)
			counted.gets, counted.entries, counted.bytes = 0, 0, 0

			// Act: exact dedup validates only selected revisions and their lineage.
			proposals, err := memy.Consolidate(context.Background(), f.engine, f.actor, f.scope,
				consolidationRequest(inputs, memy.ExactDedup), nil)

			// Assert: no history listing and no failure from unrelated source removal.
			if err != nil || len(proposals) != 1 || counted.entries != 0 {
				t.Fatalf("proposals=%d listed=%d err=%v", len(proposals), counted.entries, err)
			}
		})
	}
}
