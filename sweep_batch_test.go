package memy_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
	"github.com/skosovsky/memy/store/memory"
	"github.com/skosovsky/memy/store/sqlite"
)

func TestSweepBoundedPassResumesAfterReopen(t *testing.T) {
	for _, adapter := range []string{"memory", "sqlite"} {
		t.Run(adapter, func(t *testing.T) {
			// Arrange: expire canonical records and a draft under a current host policy.
			path := filepath.Join(t.TempDir(), "sweep.db")
			var raw memy.Store = memory.New()
			if adapter == "sqlite" {
				var err error
				raw, err = sqlite.Open(t.Context(), path, sqlite.Options{})
				checkLifecycleFailuref(t, err != nil, "%v", err)
			}
			counted := &costStore{Store: raw}
			f := newFixture(t, counted)
			f.config.Retention = reference.Retain[preference]{
				Version:   "retention/v1",
				ExpiresAt: f.clock.Now().Add(time.Hour),
			}
			var err error
			f.engine, err = memy.New(f.config)
			checkLifecycleFailuref(t, err != nil, "%v", err)
			for i := range 3 {
				id := fmt.Sprintf("record-%d", i)
				p := f.propose(t, "remember-"+id, "private", memy.Interval{})
				f.commit(t, f.acceptedRequest(t, "commit-"+id, id, 0, p, memy.Append))
			}
			f.propose(t, "draft", "draft private", memy.Interval{})
			f.clock.Advance(2 * time.Hour)
			req := memy.SweepRequest{OperationID: "pass", Limit: 1, MaxBytes: 1 << 20}
			complete, proposals, seen, raw, counted := runBoundedSweep(t, &f, raw, counted, adapter, path, req)
			t.Cleanup(func() { _ = raw.Close() })
			checkLifecycleFailuref(
				t,
				!complete || len(seen) != 3 || proposals != 1,
				"complete=%t records=%v proposals=%d",
				complete,
				seen,
				proposals,
			)
			counted.entries = 0
			replay, err := f.engine.Sweep(t.Context(), f.actor, f.scope, "assist", req)
			checkLifecycleFailuref(
				t,
				err != nil || !replay.Complete || counted.entries != 0,
				"replay=%+v entries=%d err=%v",
				replay,
				counted.entries,
				err,
			)
		})
	}
}

func TestSweepIdentityAndCancellationDoNotRetireActiveFence(t *testing.T) {
	// Arrange: begin a bounded pass that still has proposal enumeration to do.
	f := newFixture(t, nil)
	req := memy.SweepRequest{OperationID: "pass", Limit: 1, MaxBytes: 1 << 20}
	first, err := f.engine.Sweep(t.Context(), f.actor, f.scope, "assist", req)
	checkLifecycleFailuref(t, err != nil || first.Complete, "first=%+v err=%v", first, err)
	// Act: try another purpose, operation and a cancelled continuation.
	_, purposeErr := f.engine.Sweep(t.Context(), f.actor, f.scope, "changed", req)
	other := req
	other.OperationID = "other"
	_, otherErr := f.engine.Sweep(t.Context(), f.actor, f.scope, "assist", other)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, cancelErr := f.engine.Sweep(ctx, f.actor, f.scope, "assist", req)
	// Assert: none can accept/replace the existing maintenance ownership.
	checkLifecycleFailuref(t, !errors.Is(purposeErr, memy.ErrConflict) || !errors.Is(otherErr, memy.ErrRevoked) ||
		!errors.Is(cancelErr, context.Canceled), "purpose=%v other=%v cancel=%v", purposeErr, otherErr, cancelErr)
	done, err := f.engine.Sweep(t.Context(), f.actor, f.scope, "assist", req)
	checkLifecycleFailuref(t, err != nil || !done.Complete, "resume=%+v err=%v", done, err)
}

func TestSweepCanResumeUnderCurrentAuthorityPolicy(t *testing.T) {
	// Arrange: a durable pass was authorized under the old host policy.
	f := newFixture(t, nil)
	req := memy.SweepRequest{OperationID: "authority-pass", Limit: 1, MaxBytes: 1 << 20}
	{
		_, guardErr := f.engine.Sweep(t.Context(), f.actor, f.scope, "assist", req)
		checkLifecycleFailuref(t, guardErr != nil, "%v", guardErr)
	}
	f.policy.Grant(f.actor.actor, f.scope, "authority/v2", memy.ActionForget)
	// Act: the same actor still has current permission to finish maintenance.
	page, err := f.engine.Sweep(t.Context(), f.actor, f.scope, "assist", req)
	// Assert: a policy rollout must not leave a permanent maintenance fence.
	checkLifecycleFailuref(
		t,
		err != nil || !page.Complete,
		"current authorized policy cannot resume: page=%+v err=%v",
		page,
		err,
	)
}

func TestSweepRejectsFalseCompletedJobWithLiveFence(t *testing.T) {
	// Arrange: corrupt durable progress through privileged access, keeping the fence.
	f := newFixture(t, nil)
	req := memy.SweepRequest{OperationID: "pass", Limit: 1, MaxBytes: 1 << 20}
	{
		_, guardErr := f.engine.Sweep(t.Context(), f.actor, f.scope, "assist", req)
		checkLifecycleFailuref(t, guardErr != nil, "%v", guardErr)
	}
	err := f.config.Store.Update(t.Context(), f.scope, func(b memy.Bucket) error {
		entries, bucketErr := listEntries(b, "sweep_job/")
		if bucketErr != nil {
			return bucketErr
		}
		entry := entries[0]
		var doc map[string]json.RawMessage
		if bucketErr = json.Unmarshal(entry.Value.Data, &doc); bucketErr != nil {
			return bucketErr
		}
		var data map[string]any
		if bucketErr = json.Unmarshal(doc["data"], &data); bucketErr != nil {
			return bucketErr
		}
		data["phase"] = "done"
		doc["data"], _ = json.Marshal(data)
		raw, _ := json.Marshal(doc)
		_, bucketErr = b.Put(entry.Key, entry.Value.Version, raw)
		return bucketErr
	})
	checkLifecycleFailuref(t, err != nil, "%v", err)
	// Act and assert: a live fence contradicts completion of its own pass.
	page, err := f.engine.Sweep(t.Context(), f.actor, f.scope, "assist", req)
	checkLifecycleFailuref(
		t,
		!errors.Is(err, memy.ErrSchema) || page.Complete,
		"false completion: %+v err=%v",
		page,
		err,
	)
}

func TestSweepRejectsForeignCanonicalRevisionKey(t *testing.T) {
	// Arrange: a valid expired envelope has been placed under the wrong raw key.
	f := newFixture(t, nil)
	p := f.propose(t, "remember", "private", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit", "record", 0, p, memy.Append))
	f.config.Retention = reference.Retain[preference]{Version: "retention/v2", ExpiresAt: f.clock.Now().Add(-time.Hour)}
	var err error
	f.engine, err = memy.New(f.config)
	checkLifecycleFailuref(t, err != nil, "%v", err)
	var original []byte
	err = f.config.Store.Update(t.Context(), f.scope, func(b memy.Bucket) error {
		entries, bucketErr := listEntries(b, "record/")
		if bucketErr != nil {
			return bucketErr
		}
		original = entries[0].Value.Data
		_, bucketErr = b.Put("record/000-foreign/00000000000000000001", 0, original)
		return bucketErr
	})
	checkLifecycleFailuref(t, err != nil, "%v", err)
	// Act: the malformed key sorts before every genuine hashed canonical record.
	page, err := f.engine.Sweep(
		t.Context(),
		f.actor,
		f.scope,
		"assist",
		memy.SweepRequest{OperationID: "foreign", Limit: 1, MaxBytes: 1 << 20},
	)
	// Assert: no false completion/deletion and no newly installed maintenance fence.
	checkLifecycleFailuref(
		t,
		!errors.Is(err, memy.ErrSchema) || page.Complete,
		"foreign revision accepted: %+v err=%v",
		page,
		err,
	)
	err = f.config.Store.View(t.Context(), f.scope, func(b memy.Bucket) error {
		entries, bucketErr := listEntries(b, "record/")
		if bucketErr != nil {
			return bucketErr
		}
		checkLifecycleFailuref(t, len(entries) != 2, "%v", "unexpected history deletion")
		active, bucketErr := b.Get("active_sweep")
		if bucketErr != nil {
			return bucketErr
		}
		checkLifecycleFailuref(t, active.Data != nil, "%v", "failed page installed fence")
		return nil
	})
	checkLifecycleFailuref(t, err != nil, "%v", err)
}

func TestSweepRejectsCompletedReceiptForUnfinishedPurge(t *testing.T) {
	// Arrange: preserve an unfinished native purge and forge only its receipt state.
	f := newFixture(t, nil)
	p := f.propose(t, "remember", "private", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit", "record", 0, p, memy.Append))
	req := memy.ForgetRequest{
		OperationID:   "delete",
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
	err := f.config.Store.Update(t.Context(), f.scope, func(b memy.Bucket) error {
		entries, bucketErr := listEntries(b, "purge/")
		if bucketErr != nil {
			return bucketErr
		}
		entry := entries[0]
		var envelope map[string]json.RawMessage
		if bucketErr = json.Unmarshal(entry.Value.Data, &envelope); bucketErr != nil {
			return bucketErr
		}
		var receipt memy.PurgeReceipt
		if bucketErr = json.Unmarshal(envelope["data"], &receipt); bucketErr != nil {
			return bucketErr
		}
		receipt.State = memy.PurgeComplete
		receipt.CanonicalComplete = true
		receipt.Batch.Chunk = 1
		envelope["data"], _ = json.Marshal(receipt)
		raw, _ := json.Marshal(envelope)
		_, bucketErr = b.Put(entry.Key, entry.Value.Version, raw)
		return bucketErr
	})
	checkLifecycleFailuref(t, err != nil, "%v", err)
	// Act: Sweep must not adopt the malformed completion and skip cleanup.
	page, err := f.engine.Sweep(
		t.Context(),
		f.actor,
		f.scope,
		"assist",
		memy.SweepRequest{OperationID: "pass", Limit: 1, MaxBytes: 1 << 20},
	)
	// Assert: cross-document state is checked before a new maintenance effect.
	checkLifecycleFailuref(
		t,
		!errors.Is(err, memy.ErrSchema) || page.Complete,
		"false adopted completion: %+v err=%v",
		page,
		err,
	)
}

func interruptBoundedSweep(
	t *testing.T,
	f *fixture,
	raw memy.Store,
	counted *costStore,
	adapter string,
	path string,
) (memy.Store, *costStore) {
	t.Helper()

	r, err := f.engine.Get(t.Context(), f.actor, f.scope, "record-2", memy.ReadOptions{})
	checkLifecycleFailuref(
		t,
		!errors.Is(err, memy.ErrRevoked) || r.Payload.Value != "",
		"maintenance served payload: %+v err=%v",
		r,
		err,
	)
	if adapter == "sqlite" {
		{
			guardErr := raw.Close()
			checkLifecycleFailuref(t, guardErr != nil, "%v", guardErr)
		}
		raw, err = sqlite.Open(context.Background(), path, sqlite.Options{})
		checkLifecycleFailuref(t, err != nil, "%v", err)
		counted = &costStore{Store: raw}
		f.config.Store = counted
		f.engine, err = memy.New(f.config)
		checkLifecycleFailuref(t, err != nil, "%v", err)
	}
	return raw, counted
}

func runBoundedSweep(
	t *testing.T,
	f *fixture,
	raw memy.Store,
	counted *costStore,
	adapter string,
	path string,
	req memy.SweepRequest,
) (bool, int, map[string]bool, memy.Store, *costStore) {
	t.Helper()
	complete := false
	proposals := 0
	seen := map[string]bool{}
	// Act: each invocation spends a single unit; reopen with the active pass.
	for call := range 500 {
		counted.entries = 0
		page, err := f.engine.Sweep(t.Context(), f.actor, f.scope, "assist", req)
		checkLifecycleFailuref(t, err != nil, "%v", err)
		// Assert: scan work, output and reported budget never grow with the corpus.
		checkLifecycleFailuref(
			t,
			counted.entries > 1 || page.Work > 1 || len(page.Records) > 1,
			"unbounded: entries=%d page=%+v",
			counted.entries,
			page,
		)
		for _, receipt := range page.Records {
			for _, id := range receipt.Batch.Records {
				seen[id] = true
			}
		}
		proposals += page.ExpiredProposals
		if call == 2 {
			raw, counted = interruptBoundedSweep(t, f, raw, counted, adapter, path)
		}
		if page.Complete {
			complete = true
			break
		}
	}
	return complete, proposals, seen, raw, counted
}
