package memy_test

import (
	"testing"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/internal/workcost"
)

func TestPurgeFanInDoesNotRedecodeChildPerParent(t *testing.T) {
	for _, adapter := range []string{"memory", "sqlite"} {
		t.Run(adapter, func(t *testing.T) {
			var base int64
			for _, n := range []int{100, 200, 400} {
				// Arrange: two children have n parents; every resume spends one entry.
				store, _ := costAdapter(t, adapter)
				f := newFixture(t, store)
				seedCostLineage(t, f, n)
				c := &workcost.Counters{}
				workcost.Start(c)
				// Act: the actual durable pipeline validates each edge across different calls.
				receipt, err := fullForget(f.engine, t.Context(), f.actor, f.scope, "assist", memy.ForgetRequest{OperationID: "fanin", Selector: memy.Selector{Kind: memy.SelectScope}, Reason: "delete", PolicyVersion: "delete/v1", Limit: 1, MaxBytes: 1 << 20})
				workcost.Stop(c)
				if err != nil {
					t.Fatal(err)
				}
				// Assert: quadrupling the graph does not multiply large child decode by parents.
				bytes := c.DecodedBytes.Load()
				t.Logf("parents=%d decoded bytes=%d docs=%d", n, bytes, c.DecodedDocs.Load())
				if receipt.State != memy.PurgeComplete || len(receipt.Batch.Records) != n+2 {
					t.Fatal("incomplete fan-in purge")
				}
				if n == 100 {
					base = bytes
				} else if bytes > base*int64(n)*3/200 {
					t.Fatalf("superlinear repeated child decode: base=%d n=%d actual=%d", base, n, bytes)
				}
				if err = store.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
