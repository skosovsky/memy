package memy_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/skosovsky/memy"
)

type receiptCorruptingSink struct {
	store memy.Store
	scope memy.Scope
}

func (*receiptCorruptingSink) Name() string { return "corrupting" }
func (s *receiptCorruptingSink) Purge(ctx context.Context, batch memy.PurgeBatch) (memy.PurgeAck, error) {
	e := s.store.Update(ctx, s.scope, func(b memy.Bucket) error {
		xs, e := listEntries(b, "purge/")
		if e != nil {
			return e
		}
		v := xs[0]
		var x map[string]any
		if e = json.Unmarshal(v.Value.Data, &x); e != nil {
			return e
		}
		data := x["data"].(map[string]any)
		data["state"] = "complete"
		for _, s := range data["sinks"].([]any) {
			s.(map[string]any)["acknowledged"] = true
			s.(map[string]any)["error_code"] = ""
		}
		raw, _ := json.Marshal(x)
		_, e = b.Put(v.Key, v.Value.Version, raw)
		return e
	})
	return memy.PurgeAck{Sink: s.Name(), OperationID: batch.OperationID, Epoch: batch.Epoch, Chunk: batch.Chunk}, e
}
func TestPurgeRejectsCompletedReceiptDuringSinkCallback(t *testing.T) {
	// Arrange: the privileged store corrupts the receipt during sink I/O.
	f := newFixture(t, nil)
	f.config.Sinks = []memy.Sink{&receiptCorruptingSink{store: f.config.Store, scope: f.scope}}
	var e error
	f.engine, e = memy.New(f.config)
	checkLifecycleFailuref(t, e != nil, "%v", e)
	seedCostCorpus(t, f, 1)
	q := memy.ForgetRequest{
		OperationID:   "round3",
		Selector:      memy.Selector{Kind: memy.SelectScope},
		Reason:        "review",
		PolicyVersion: "v1",
		Limit:         1,
		MaxBytes:      1 << 20,
	}
	var r memy.PurgeReceipt
	for range 100 {
		r, e = f.engine.Forget(t.Context(), f.actor, f.scope, "assist", q)
		checkLifecycleFailuref(t, e != nil, "%v", e)
		if r.State == memy.PurgePending {
			break
		}
	}
	// Act and assert: reject the inconsistent completion and retain the fence.
	r, e = f.engine.Forget(t.Context(), f.actor, f.scope, "assist", q)
	checkLifecycleFailuref(t, !errors.Is(e, memy.ErrSchema) || r.State == memy.PurgeComplete, "%v %v", r, e)
	_, e = f.engine.Fence(t.Context(), f.actor, f.scope, "assist")
	checkLifecycleFailuref(t, !errors.Is(e, memy.ErrRevoked), "%v", e)
}
