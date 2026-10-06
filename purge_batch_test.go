package memy_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/store/memory"
	"github.com/skosovsky/memy/store/sqlite"
)

func TestBoundedPurgeStaysFencedAndResumesAcrossReopen(t *testing.T) {
	for _, adapter := range []string{"memory", "sqlite"} {
		t.Run(adapter, func(t *testing.T) {
			// Arrange: a real historical/lineage corpus and one-entry native work calls.
			path := filepath.Join(t.TempDir(), "batch.db")
			var raw memy.Store = memory.New()
			if adapter == "sqlite" {
				var err error
				raw, err = sqlite.Open(context.Background(), path, sqlite.Options{})
				checkLifecycleFailuref(t, err != nil, "%v", err)
			}
			counted := &costStore{Store: raw}
			f := newFixture(t, counted)
			var previous memy.RevisionRef
			for i := range 30 {
				id := fmt.Sprintf("record-%02d", i)
				suggestion := f.suggestion("private", memy.Interval{Known: true})
				if i > 0 {
					suggestion.Lineage = []memy.RevisionRef{previous}
				}
				p, err := f.engine.Remember(
					context.Background(),
					f.actor,
					f.scope,
					"remember-"+id,
					"assist",
					suggestion,
				)
				checkLifecycleFailuref(t, err != nil, "%v", err)
				r := f.commit(t, f.acceptedRequest(t, "commit-"+id, id, 0, p, memy.Append))
				previous = memy.RevisionRef{RecordID: id, Revision: r.Revision}
			}
			before, fenceErr := f.engine.Fence(context.Background(), f.actor, f.scope, "assist")
			checkLifecycleFailuref(t, fenceErr != nil, "%v", fenceErr)
			request := memy.ForgetRequest{
				OperationID:   "bounded",
				Selector:      memy.Selector{Kind: memy.SelectSource, ID: f.source.ID},
				Reason:        "delete",
				PolicyVersion: "delete/v1",
				Limit:         1,
				MaxBytes:      1 << 20,
			}
			complete, seen := runBoundedPurge(t, &f, raw, counted, adapter, path, request)
			checkLifecycleFailuref(
				t,
				complete.State != memy.PurgeComplete || !complete.CanonicalComplete || len(seen) != 30,
				"completion=%+v observed=%d",
				complete,
				len(seen),
			)
			called := false
			if err := f.engine.WithDerivedWrite(
				context.Background(),
				f.actor,
				before,
				"assist",
				[]memy.RevisionRef{previous},
				func(context.Context) error { called = true; return nil },
			); !errors.Is(err, memy.ErrStaleInput) ||
				called {
				t.Fatalf("late write: called=%t err=%v", called, err)
			}
			replay, err := f.engine.Forget(context.Background(), f.actor, f.scope, "assist", request)
			checkLifecycleFailuref(
				t,
				err != nil || replay.Batch.Epoch != complete.Batch.Epoch || replay.Batch.Chunk != complete.Batch.Chunk ||
					replay.State != memy.PurgeComplete,
				"replay=%+v err=%v",
				replay,
				err,
			)
		})
	}
}

func TestPurgeContinuationRejectsCorruptFenceAndProgress(t *testing.T) {
	for _, corruption := range []string{"foreign_fence", "missing_fence", "next_past_queue", "unexpected_resume", "premature_emit", "premature_done"} {
		t.Run(corruption, func(t *testing.T) {
			// Arrange: stop immediately after installing the fence and enqueuing one head.
			f := newFixture(t, nil)
			p := f.propose(t, "remember", "private", memy.Interval{})
			f.commit(t, f.acceptedRequest(t, "commit", "record", 0, p, memy.Append))
			req := memy.ForgetRequest{
				OperationID:   "bounded",
				Selector:      memy.Selector{Kind: memy.SelectRecord, ID: "record"},
				Reason:        "delete",
				PolicyVersion: "delete/v1",
				Limit:         1,
				MaxBytes:      1 << 20,
			}
			{
				_, guardErr := f.engine.Forget(t.Context(), f.actor, f.scope, "assist", req)
				checkLifecycleFailuref(t, guardErr != nil, "%v", guardErr)
			}
			var before []memy.Entry
			err := f.config.Store.Update(
				t.Context(),
				f.scope,
				func(b memy.Bucket) error { return corruptPurgeProgress(b, corruption) },
			)
			checkLifecycleFailuref(t, err != nil, "%v", err)
			if err = f.config.Store.View(
				t.Context(),
				f.scope,
				func(b memy.Bucket) error {
					var bucketErr error
					before, bucketErr = listEntries(b, "record/")
					return bucketErr
				},
			); err != nil {
				t.Fatal(err)
			}
			// Act: resuming must reject the broken durable ownership/progress contract.
			_, err = f.engine.Forget(t.Context(), f.actor, f.scope, "assist", req)
			// Assert: the rejected continuation has not deleted canonical history.
			checkLifecycleFailuref(t, !errors.Is(err, memy.ErrSchema), "accepted corrupt continuation: %v", err)
			if err = f.config.Store.View(
				t.Context(),
				f.scope,
				func(b memy.Bucket) error { return assertPurgeHistoryUnchanged(t, b, before) },
			); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// checkLifecycleFailuref stops the current phase on an invariant violation.
func checkLifecycleFailuref(t *testing.T, failed bool, format string, values ...any) {
	t.Helper()
	if failed {
		t.Fatalf(format, values...)
	}
}

func interruptBoundedPurge(
	t *testing.T,
	f *fixture,
	raw memy.Store,
	counted *costStore,
	adapter string,
	path string,
	receipt memy.PurgeReceipt,
) (memy.Store, *costStore) {
	t.Helper()

	checkLifecycleFailuref(
		t,
		receipt.CanonicalComplete || receipt.State == memy.PurgeComplete,
		"%v",
		"premature completion",
	)
	if record, err := f.engine.Get(
		context.Background(),
		f.actor,
		f.scope,
		"record-29",
		memy.ReadOptions{},
	); !errors.Is(err, memy.ErrRevoked) ||
		record.Payload.Value != "" {
		t.Fatalf("pending payload: %+v err=%v", record, err)
	}
	other := f.scope
	other.Subject = "independent"
	if err := raw.Update(
		context.Background(),
		other,
		func(b memy.Bucket) error { _, bucketErr := b.Put("key", 0, []byte("live")); return bucketErr },
	); err != nil {
		t.Fatal(err)
	}
	if adapter == "sqlite" {
		{
			guardErr := raw.Close()
			checkLifecycleFailuref(t, guardErr != nil, "%v", guardErr)
		}
		reopened, err := sqlite.Open(context.Background(), path, sqlite.Options{})
		checkLifecycleFailuref(t, err != nil, "%v", err)
		t.Cleanup(func() { _ = reopened.Close() })
		raw = reopened
		counted = &costStore{Store: raw}
		f.config.Store = counted
		f.engine, err = memy.New(f.config)
		checkLifecycleFailuref(t, err != nil, "%v", err)
	}
	return raw, counted
}

func runBoundedPurge(
	t *testing.T,
	f *fixture,
	raw memy.Store,
	counted *costStore,
	adapter string,
	path string,
	request memy.ForgetRequest,
) (memy.PurgeReceipt, map[string]bool) {
	t.Helper()
	seen := make(map[string]bool)
	var lastChunk uint64
	var complete memy.PurgeReceipt

	// Act: interruption/reopen happens before canonical cleanup is complete.
	for call := range 1000 {
		counted.entries = 0
		receipt, err := f.engine.Forget(context.Background(), f.actor, f.scope, "assist", request)
		checkLifecycleFailuref(t, err != nil, "%v", err)
		// Assert: each call's traversal/output remains bounded.
		checkLifecycleFailuref(
			t,
			counted.entries > 1 || len(receipt.Batch.Records) > 1,
			"unbounded: entries=%d receipt=%+v",
			counted.entries,
			receipt,
		)
		checkLifecycleFailuref(t, receipt.Batch.Chunk < lastChunk, "%v", "chunk regressed")
		lastChunk = receipt.Batch.Chunk
		for _, id := range receipt.Batch.Records {
			seen[id] = true
		}
		if call == 9 {
			raw, counted = interruptBoundedPurge(t, f, raw, counted, adapter, path, receipt)
		}
		if receipt.State == memy.PurgeComplete {
			complete = receipt
			break
		}
	}
	return complete, seen
}

func corruptPurgeProgress(b memy.Bucket, corruption string) error {
	key := "active_purge"
	if corruption == "next_past_queue" || corruption == "unexpected_resume" ||
		corruption == "premature_emit" ||
		corruption == "premature_done" {
		entries, err := listEntries(b, "purge_job/")
		if err != nil {
			return err
		}
		key = entries[0].Key
	}
	value, err := b.Get(key)
	if err != nil {
		return err
	}
	if corruption == "missing_fence" {
		_, err = b.Delete(key, value.Version)
		return err
	}
	var document map[string]json.RawMessage
	if err = json.Unmarshal(value.Data, &document); err != nil {
		return err
	}
	var data map[string]any
	if err = json.Unmarshal(document["data"], &data); err != nil {
		return err
	}
	switch corruption {
	case "foreign_fence":
		data["operation_id"] = "foreign"
	case "next_past_queue":
		data["next"] = 100
	case "unexpected_resume":
		data["resume"] = "done"
	case "premature_emit":
		data["phase"] = "emit"
	case "premature_done":
		data["phase"] = "done"
	}
	document["data"], err = json.Marshal(data)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return err
	}
	_, err = b.Put(key, value.Version, encoded)
	return err
}

func assertPurgeHistoryUnchanged(t *testing.T, b memy.Bucket, before []memy.Entry) error {
	t.Helper()
	after, err := listEntries(b, "record/")
	if err != nil {
		return err
	}
	checkLifecycleFailuref(t, len(after) != len(before), "%v", "history deleted")
	for i := range before {
		checkLifecycleFailuref(t, before[i].Value.Version != after[i].Value.Version, "%v", "history mutated")
	}
	return nil
}
