package memy_test

import (
	"path/filepath"
	"testing"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/internal/workcost"
	"github.com/skosovsky/memy/store/memory"
	"github.com/skosovsky/memy/store/sqlite"
)

func TestAddressedOperationsDoNotDecodeOrCopyUnrelatedHistory(t *testing.T) {
	for _, adapter := range []string{"memory", "sqlite"} {
		for _, operation := range []string{"Get", "Consolidate"} {
			t.Run(adapter+"/"+operation, func(t *testing.T) {
				var baseline [4]int64
				for _, n := range []int{1000, 10000} {
					// Arrange: the exact requested data stays fixed while unrelated scope grows.
					var store memy.Store = memory.New()
					if adapter == "sqlite" {
						var err error
						store, err = sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "cost.db"), sqlite.Options{})
						if err != nil {
							t.Fatal(err)
						}
					}
					f := newFixture(t, store)
					seedCostCorpus(t, f, n)
					c := &workcost.Counters{}
					workcost.Start(c)
					// Act: private counters observe actual canonical decodes and value reads.
					var err error
					if operation == "Get" {
						_, err = f.engine.Get(t.Context(), f.actor, f.scope, "cost-0", memy.ReadOptions{})
					} else {
						_, err = memy.Consolidate(t.Context(), f.engine, f.actor, f.scope, consolidationRequest([]memy.RevisionRef{{RecordID: "cost-0", Revision: 1}, {RecordID: "cost-1", Revision: 1}}, memy.ExactDedup), nil)
					}
					workcost.Stop(c)
					if err != nil {
						t.Fatal(err)
					}
					got := [4]int64{c.DecodedDocs.Load(), c.DecodedBytes.Load(), c.MemoryCopiedBytes.Load(), c.SQLValueRows.Load()}
					// Assert: no hidden unrelated decoding/blob copying; AVL seeks stay logarithmic.
					if n == 1000 {
						baseline = got
					} else if got != baseline {
						t.Fatalf("scope-dependent private work: baseline=%v actual=%v", baseline, got)
					}
					if c.IndexNodes.Load() > 1000 || c.SQLMetadataRows.Load() > 2 {
						t.Fatalf("unrelated traversal: nodes=%d metadata=%d", c.IndexNodes.Load(), c.SQLMetadataRows.Load())
					}
					if err = store.Close(); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}
