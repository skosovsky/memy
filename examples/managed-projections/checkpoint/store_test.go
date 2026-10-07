package checkpoint_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/conformance"
	"github.com/skosovsky/memy/examples/managed-projections/checkpoint"
)

func TestPurgeAllDependentCopiesAndLateOldScopeBatch(t *testing.T) {
	// Arrange: multiple managed copies with full reverse lineage in a real database.
	path := filepath.Join(t.TempDir(), "checkpoints.db")
	s, err := checkpoint.Open(t.Context(), path, "checkpoint")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	sc := memy.Scope{Tenant: "tenant", Namespace: "knowledge", Subject: "user"}
	old := memy.EpochFence{Scope: sc, Epoch: 0}
	keys := []string{}
	for _, handle := range []string{"conversation", "checkpoint", "cache"} {
		key, putErr := s.Put(
			t.Context(),
			old,
			checkpoint.Artifact{
				Scope:   sc,
				Handle:  handle,
				Lineage: []memy.RevisionRef{{RecordID: "a", Revision: 1}, {RecordID: "b", Revision: 1}},
				Data:    []byte(handle),
			},
		)
		if putErr != nil {
			t.Fatal(putErr)
		}
		keys = append(keys, key)
	}
	batch := memy.PurgeBatch{
		OperationID: "revoke-a",
		Scope:       sc,
		Epoch:       1,
		Selector:    memy.Selector{Kind: memy.SelectRecord, ID: "a"},
		Records:     []string{"a"},
		Chunk:       1,
	}
	// Act: purge all copies, then reopen the backend.
	if _, err = s.Purge(t.Context(), batch); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := checkpoint.Open(t.Context(), path, "checkpoint")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	// Assert: neither durable payload nor its reverse-index entries can be served.
	for _, key := range keys {
		if _, err = reopened.Load(t.Context(), sc, key); !errors.Is(err, memy.ErrNotFound) {
			t.Fatalf("copy=%s err=%v", key, err)
		}
	}
	// A late retry of an older scope purge must not delete newly fenced knowledge.
	current := memy.EpochFence{Scope: sc, Epoch: 2}
	key, err := reopened.Put(
		t.Context(),
		current,
		checkpoint.Artifact{
			Scope:   sc,
			Handle:  "new",
			Lineage: []memy.RevisionRef{{RecordID: "new", Revision: 1}},
			Data:    []byte("new"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	batch.Selector = memy.Selector{Kind: memy.SelectScope}
	batch.Records = nil
	if _, err = reopened.Purge(t.Context(), batch); err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.Load(t.Context(), sc, key); err != nil {
		t.Fatalf("old purge deleted newer artifact: %v", err)
	}
	if _, err = reopened.Put(
		t.Context(),
		old,
		checkpoint.Artifact{
			Scope:   sc,
			Handle:  "old",
			Lineage: []memy.RevisionRef{{RecordID: "a", Revision: 1}},
			Data:    []byte("old"),
		},
	); !errors.Is(
		err,
		memy.ErrStaleInput,
	) {
		t.Fatalf("old write err=%v", err)
	}
}

func TestCheckpointSinkConformance(t *testing.T) {
	conformance.SinkSuite(t, func(t *testing.T) conformance.SinkFixture {
		s, err := checkpoint.Open(t.Context(), filepath.Join(t.TempDir(), "sink.db"), "checkpoint")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return conformance.SinkFixture{
			Adapter: s,
			Seed: func(ctx context.Context, handle string, sc memy.Scope, ref memy.RevisionRef) error {
				_, err := s.Put(
					ctx,
					memy.EpochFence{Scope: sc, Epoch: 0},
					checkpoint.Artifact{
						Scope:   sc,
						Handle:  handle,
						Lineage: []memy.RevisionRef{ref},
						Data:    []byte(handle),
					},
				)
				return err
			},
			Contains: func(ctx context.Context, handle string, sc memy.Scope, ref memy.RevisionRef) (bool, error) {
				key, err := checkpoint.Key(
					checkpoint.Artifact{Scope: sc, Handle: handle, Lineage: []memy.RevisionRef{ref}},
				)
				if err != nil {
					return false, err
				}
				_, err = s.Load(ctx, sc, key)
				if errors.Is(err, memy.ErrNotFound) {
					return false, nil
				}
				return err == nil, err
			},
			Fail: s.FailPurge,
		}
	})
}
