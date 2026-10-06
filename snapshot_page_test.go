package memy_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/store/memory"
	"github.com/skosovsky/memy/store/sqlite"
)

func TestSnapshotPagesBoundScannedWorkAndBindReadPolicy(t *testing.T) {
	for _, adapter := range []string{"memory", "sqlite"} {
		t.Run(adapter, func(t *testing.T) {
			// Arrange: three eligible revisions, with host-owned bounded page options.
			var store memy.Store = memory.New()
			if adapter == "sqlite" {
				var err error
				store, err = sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "snapshot.db"), sqlite.Options{})
				if err != nil {
					t.Fatal(err)
				}
			}
			f := newFixture(t, store)
			for _, id := range []string{"one", "two", "three"} {
				p := f.propose(t, "remember-"+id, "value", memy.Interval{Known: true})
				f.commit(t, f.acceptedRequest(t, "commit-"+id, id, 0, p, memy.Append))
			}
			options := memy.SnapshotOptions{Read: memy.ReadOptions{RecordedAsOf: f.clock.Now().Add(-time.Second)}, Limit: 1, MaxBytes: 10000}
			count := 0

			// Act: pages with no eligible records must still advance bounded traversal.
			for {
				page, err := f.engine.Snapshot(context.Background(), f.actor, f.scope, options)
				if err != nil {
					t.Fatal(err)
				}
				// Assert: scan budget applies to canonical work, not eligible output count.
				if page.Scanned != 1 || len(page.Records) != 0 || page.ScannedBytes > options.MaxBytes {
					t.Fatalf("page=%+v", page)
				}
				count += page.Scanned
				if page.Complete {
					if page.Cursor != "" {
						t.Fatal("completed cursor")
					}
					break
				}
				if page.Cursor == "" {
					t.Fatal("missing progress")
				}
				options.Cursor = page.Cursor
			}
			if count != 3 {
				t.Fatalf("scanned=%d", count)
			}
			options = memy.SnapshotOptions{Limit: 1, MaxBytes: 10000}
			page, err := f.engine.Snapshot(context.Background(), f.actor, f.scope, options)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Records) != 1 || page.Complete || page.Cursor == "" {
				t.Fatalf("initial page=%+v", page)
			}
			options.Cursor = page.Cursor
			changed := options
			changed.Read.IncludeConflicts = true
			if result, err := f.engine.Snapshot(context.Background(), f.actor, f.scope, changed); !errors.Is(err, memy.ErrStaleCursor) || len(result.Records) != 0 {
				t.Fatalf("changed read: %+v err=%v", result, err)
			}
			f.policy.Grant("alice", f.scope, "authority/v2", memy.ActionRead)
			if result, err := f.engine.Snapshot(context.Background(), f.actor, f.scope, options); !errors.Is(err, memy.ErrStaleCursor) || len(result.Records) != 0 {
				t.Fatalf("changed authority: %+v err=%v", result, err)
			}
		})
	}
}
