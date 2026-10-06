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
				if err != nil {
					t.Fatal(err)
				}
			}
			counted := &costStore{Store: raw}
			f := newFixture(t, counted)
			var previous memy.RevisionRef
			for i := 0; i < 30; i++ {
				id := fmt.Sprintf("record-%02d", i)
				suggestion := f.suggestion("private", memy.Interval{Known: true})
				if i > 0 {
					suggestion.Lineage = []memy.RevisionRef{previous}
				}
				p, err := f.engine.Remember(context.Background(), f.actor, f.scope, "remember-"+id, "assist", suggestion)
				if err != nil {
					t.Fatal(err)
				}
				r := f.commit(t, f.acceptedRequest(t, "commit-"+id, id, 0, p, memy.Append))
				previous = memy.RevisionRef{RecordID: id, Revision: r.Revision}
			}
			before, err := f.engine.Fence(context.Background(), f.actor, f.scope, "assist")
			if err != nil {
				t.Fatal(err)
			}
			request := memy.ForgetRequest{OperationID: "bounded", Selector: memy.Selector{Kind: memy.SelectSource, ID: f.source.ID}, Reason: "delete", PolicyVersion: "delete/v1", Limit: 1, MaxBytes: 1 << 20}
			seen := make(map[string]bool)
			var lastChunk uint64
			var complete memy.PurgeReceipt

			// Act: interruption/reopen happens before canonical cleanup is complete.
			for call := 0; call < 1000; call++ {
				counted.entries = 0
				receipt, err := f.engine.Forget(context.Background(), f.actor, f.scope, "assist", request)
				if err != nil {
					t.Fatal(err)
				}
				// Assert: each call's traversal/output remains bounded.
				if counted.entries > 1 || len(receipt.Batch.Records) > 1 {
					t.Fatalf("unbounded: entries=%d receipt=%+v", counted.entries, receipt)
				}
				if receipt.Batch.Chunk < lastChunk {
					t.Fatal("chunk regressed")
				}
				lastChunk = receipt.Batch.Chunk
				for _, id := range receipt.Batch.Records {
					seen[id] = true
				}
				if call == 9 {
					if receipt.CanonicalComplete || receipt.State == memy.PurgeComplete {
						t.Fatal("premature completion")
					}
					if record, err := f.engine.Get(context.Background(), f.actor, f.scope, "record-29", memy.ReadOptions{}); !errors.Is(err, memy.ErrRevoked) || record.Payload.Value != "" {
						t.Fatalf("pending payload: %+v err=%v", record, err)
					}
					other := f.scope
					other.Subject = "independent"
					if err := raw.Update(context.Background(), other, func(b memy.Bucket) error { _, err := b.Put("key", 0, []byte("live")); return err }); err != nil {
						t.Fatal(err)
					}
					if adapter == "sqlite" {
						if err := raw.Close(); err != nil {
							t.Fatal(err)
						}
						reopened, err := sqlite.Open(context.Background(), path, sqlite.Options{})
						if err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() { _ = reopened.Close() })
						raw = reopened
						counted = &costStore{Store: raw}
						f.config.Store = counted
						f.engine, err = memy.New(f.config)
						if err != nil {
							t.Fatal(err)
						}
					}
				}
				if receipt.State == memy.PurgeComplete {
					complete = receipt
					break
				}
			}
			if complete.State != memy.PurgeComplete || !complete.CanonicalComplete || len(seen) != 30 {
				t.Fatalf("completion=%+v observed=%d", complete, len(seen))
			}
			called := false
			if err := f.engine.WithDerivedWrite(context.Background(), f.actor, before, "assist", []memy.RevisionRef{previous}, func(context.Context) error { called = true; return nil }); !errors.Is(err, memy.ErrStaleInput) || called {
				t.Fatalf("late write: called=%t err=%v", called, err)
			}
			replay, err := f.engine.Forget(context.Background(), f.actor, f.scope, "assist", request)
			if err != nil || replay.Batch.Epoch != complete.Batch.Epoch || replay.Batch.Chunk != complete.Batch.Chunk || replay.State != memy.PurgeComplete {
				t.Fatalf("replay=%+v err=%v", replay, err)
			}
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
			req := memy.ForgetRequest{OperationID: "bounded", Selector: memy.Selector{Kind: memy.SelectRecord, ID: "record"}, Reason: "delete", PolicyVersion: "delete/v1", Limit: 1, MaxBytes: 1 << 20}
			if _, err := f.engine.Forget(t.Context(), f.actor, f.scope, "assist", req); err != nil {
				t.Fatal(err)
			}
			var before []memy.Entry
			err := f.config.Store.Update(t.Context(), f.scope, func(b memy.Bucket) error {
				key := "active_purge"
				if corruption == "next_past_queue" || corruption == "unexpected_resume" || corruption == "premature_emit" || corruption == "premature_done" {
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
			})
			if err != nil {
				t.Fatal(err)
			}
			if err = f.config.Store.View(t.Context(), f.scope, func(b memy.Bucket) error { var err error; before, err = listEntries(b, "record/"); return err }); err != nil {
				t.Fatal(err)
			}
			// Act: resuming must reject the broken durable ownership/progress contract.
			_, err = f.engine.Forget(t.Context(), f.actor, f.scope, "assist", req)
			// Assert: the rejected continuation has not deleted canonical history.
			if !errors.Is(err, memy.ErrSchema) {
				t.Fatalf("accepted corrupt continuation: %v", err)
			}
			if err = f.config.Store.View(t.Context(), f.scope, func(b memy.Bucket) error {
				after, err := listEntries(b, "record/")
				if err != nil {
					return err
				}
				if len(after) != len(before) {
					t.Fatal("history deleted")
				}
				for i := range before {
					if before[i].Value.Version != after[i].Value.Version {
						t.Fatal("history mutated")
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
