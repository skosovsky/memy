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
				if err != nil {
					t.Fatal(err)
				}
			}
			counted := &costStore{Store: raw}
			f := newFixture(t, counted)
			f.config.Retention = reference.Retain[preference]{Version: "retention/v1", ExpiresAt: f.clock.Now().Add(time.Hour)}
			var err error
			f.engine, err = memy.New(f.config)
			if err != nil {
				t.Fatal(err)
			}
			for i := range 3 {
				id := fmt.Sprintf("record-%d", i)
				p := f.propose(t, "remember-"+id, "private", memy.Interval{})
				f.commit(t, f.acceptedRequest(t, "commit-"+id, id, 0, p, memy.Append))
			}
			f.propose(t, "draft", "draft private", memy.Interval{})
			f.clock.Advance(2 * time.Hour)
			req := memy.SweepRequest{OperationID: "pass", Limit: 1, MaxBytes: 1 << 20}
			complete := false
			proposals := 0
			seen := map[string]bool{}
			// Act: each invocation spends a single unit; reopen with the active pass.
			for call := 0; call < 500; call++ {
				counted.entries = 0
				page, err := f.engine.Sweep(t.Context(), f.actor, f.scope, "assist", req)
				if err != nil {
					t.Fatal(err)
				}
				// Assert: scan work, output and reported budget never grow with the corpus.
				if counted.entries > 1 || page.Work > 1 || len(page.Records) > 1 {
					t.Fatalf("unbounded: entries=%d page=%+v", counted.entries, page)
				}
				for _, receipt := range page.Records {
					for _, id := range receipt.Batch.Records {
						seen[id] = true
					}
				}
				proposals += page.ExpiredProposals
				if call == 2 {
					r, err := f.engine.Get(t.Context(), f.actor, f.scope, "record-2", memy.ReadOptions{})
					if !errors.Is(err, memy.ErrRevoked) || r.Payload.Value != "" {
						t.Fatalf("maintenance served payload: %+v err=%v", r, err)
					}
					if adapter == "sqlite" {
						if err = raw.Close(); err != nil {
							t.Fatal(err)
						}
						raw, err = sqlite.Open(context.Background(), path, sqlite.Options{})
						if err != nil {
							t.Fatal(err)
						}
						counted = &costStore{Store: raw}
						f.config.Store = counted
						f.engine, err = memy.New(f.config)
						if err != nil {
							t.Fatal(err)
						}
					}
				}
				if page.Complete {
					complete = true
					break
				}
			}
			t.Cleanup(func() { _ = raw.Close() })
			if !complete || len(seen) != 3 || proposals != 1 {
				t.Fatalf("complete=%t records=%v proposals=%d", complete, seen, proposals)
			}
			counted.entries = 0
			replay, err := f.engine.Sweep(t.Context(), f.actor, f.scope, "assist", req)
			if err != nil || !replay.Complete || counted.entries != 0 {
				t.Fatalf("replay=%+v entries=%d err=%v", replay, counted.entries, err)
			}
		})
	}
}

func TestSweepIdentityAndCancellationDoNotRetireActiveFence(t *testing.T) {
	// Arrange: begin a bounded pass that still has proposal enumeration to do.
	f := newFixture(t, nil)
	req := memy.SweepRequest{OperationID: "pass", Limit: 1, MaxBytes: 1 << 20}
	first, err := f.engine.Sweep(t.Context(), f.actor, f.scope, "assist", req)
	if err != nil || first.Complete {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	// Act: try another purpose, operation and a cancelled continuation.
	_, purposeErr := f.engine.Sweep(t.Context(), f.actor, f.scope, "changed", req)
	other := req
	other.OperationID = "other"
	_, otherErr := f.engine.Sweep(t.Context(), f.actor, f.scope, "assist", other)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, cancelErr := f.engine.Sweep(ctx, f.actor, f.scope, "assist", req)
	// Assert: none can accept/replace the existing maintenance ownership.
	if !errors.Is(purposeErr, memy.ErrConflict) || !errors.Is(otherErr, memy.ErrRevoked) || !errors.Is(cancelErr, context.Canceled) {
		t.Fatalf("purpose=%v other=%v cancel=%v", purposeErr, otherErr, cancelErr)
	}
	done, err := f.engine.Sweep(t.Context(), f.actor, f.scope, "assist", req)
	if err != nil || !done.Complete {
		t.Fatalf("resume=%+v err=%v", done, err)
	}
}

func TestSweepCanResumeUnderCurrentAuthorityPolicy(t *testing.T) {
	// Arrange: a durable pass was authorized under the old host policy.
	f := newFixture(t, nil)
	req := memy.SweepRequest{OperationID: "authority-pass", Limit: 1, MaxBytes: 1 << 20}
	if _, err := f.engine.Sweep(t.Context(), f.actor, f.scope, "assist", req); err != nil {
		t.Fatal(err)
	}
	f.policy.Grant(f.actor.actor, f.scope, "authority/v2", memy.ActionForget)
	// Act: the same actor still has current permission to finish maintenance.
	page, err := f.engine.Sweep(t.Context(), f.actor, f.scope, "assist", req)
	// Assert: a policy rollout must not leave a permanent maintenance fence.
	if err != nil || !page.Complete {
		t.Fatalf("current authorized policy cannot resume: page=%+v err=%v", page, err)
	}
}

func TestSweepRejectsFalseCompletedJobWithLiveFence(t *testing.T) {
	// Arrange: corrupt durable progress through privileged access, keeping the fence.
	f := newFixture(t, nil)
	req := memy.SweepRequest{OperationID: "pass", Limit: 1, MaxBytes: 1 << 20}
	if _, err := f.engine.Sweep(t.Context(), f.actor, f.scope, "assist", req); err != nil {
		t.Fatal(err)
	}
	err := f.config.Store.Update(t.Context(), f.scope, func(b memy.Bucket) error {
		entries, err := listEntries(b, "sweep_job/")
		if err != nil {
			return err
		}
		entry := entries[0]
		var doc map[string]json.RawMessage
		if err = json.Unmarshal(entry.Value.Data, &doc); err != nil {
			return err
		}
		var data map[string]any
		if err = json.Unmarshal(doc["data"], &data); err != nil {
			return err
		}
		data["phase"] = "done"
		doc["data"], _ = json.Marshal(data)
		raw, _ := json.Marshal(doc)
		_, err = b.Put(entry.Key, entry.Value.Version, raw)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// Act and assert: a live fence contradicts completion of its own pass.
	page, err := f.engine.Sweep(t.Context(), f.actor, f.scope, "assist", req)
	if !errors.Is(err, memy.ErrSchema) || page.Complete {
		t.Fatalf("false completion: %+v err=%v", page, err)
	}
}

func TestSweepRejectsForeignCanonicalRevisionKey(t *testing.T) {
	// Arrange: a valid expired envelope has been placed under the wrong raw key.
	f := newFixture(t, nil)
	p := f.propose(t, "remember", "private", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit", "record", 0, p, memy.Append))
	f.config.Retention = reference.Retain[preference]{Version: "retention/v2", ExpiresAt: f.clock.Now().Add(-time.Hour)}
	var err error
	f.engine, err = memy.New(f.config)
	if err != nil {
		t.Fatal(err)
	}
	var original []byte
	err = f.config.Store.Update(t.Context(), f.scope, func(b memy.Bucket) error {
		entries, err := listEntries(b, "record/")
		if err != nil {
			return err
		}
		original = entries[0].Value.Data
		_, err = b.Put("record/000-foreign/00000000000000000001", 0, original)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// Act: the malformed key sorts before every genuine hashed canonical record.
	page, err := f.engine.Sweep(t.Context(), f.actor, f.scope, "assist", memy.SweepRequest{OperationID: "foreign", Limit: 1, MaxBytes: 1 << 20})
	// Assert: no false completion/deletion and no newly installed maintenance fence.
	if !errors.Is(err, memy.ErrSchema) || page.Complete {
		t.Fatalf("foreign revision accepted: %+v err=%v", page, err)
	}
	err = f.config.Store.View(t.Context(), f.scope, func(b memy.Bucket) error {
		entries, err := listEntries(b, "record/")
		if err != nil {
			return err
		}
		if len(entries) != 2 {
			t.Fatal("unexpected history deletion")
		}
		active, err := b.Get("active_sweep")
		if err != nil {
			return err
		}
		if active.Data != nil {
			t.Fatal("failed page installed fence")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSweepRejectsCompletedReceiptForUnfinishedPurge(t *testing.T) {
	// Arrange: preserve an unfinished native purge and forge only its receipt state.
	f := newFixture(t, nil)
	p := f.propose(t, "remember", "private", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit", "record", 0, p, memy.Append))
	req := memy.ForgetRequest{OperationID: "delete", Selector: memy.Selector{Kind: memy.SelectRecord, ID: "record"}, Reason: "delete", PolicyVersion: "delete/v1", Limit: 1, MaxBytes: 1 << 20}
	if _, err := f.engine.Forget(t.Context(), f.actor, f.scope, "assist", req); err != nil {
		t.Fatal(err)
	}
	err := f.config.Store.Update(t.Context(), f.scope, func(b memy.Bucket) error {
		entries, err := listEntries(b, "purge/")
		if err != nil {
			return err
		}
		entry := entries[0]
		var envelope map[string]json.RawMessage
		if err = json.Unmarshal(entry.Value.Data, &envelope); err != nil {
			return err
		}
		var receipt memy.PurgeReceipt
		if err = json.Unmarshal(envelope["data"], &receipt); err != nil {
			return err
		}
		receipt.State = memy.PurgeComplete
		receipt.CanonicalComplete = true
		receipt.Batch.Chunk = 1
		envelope["data"], _ = json.Marshal(receipt)
		raw, _ := json.Marshal(envelope)
		_, err = b.Put(entry.Key, entry.Value.Version, raw)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// Act: Sweep must not adopt the malformed completion and skip cleanup.
	page, err := f.engine.Sweep(t.Context(), f.actor, f.scope, "assist", memy.SweepRequest{OperationID: "pass", Limit: 1, MaxBytes: 1 << 20})
	// Assert: cross-document state is checked before a new maintenance effect.
	if !errors.Is(err, memy.ErrSchema) || page.Complete {
		t.Fatalf("false adopted completion: %+v err=%v", page, err)
	}
}
