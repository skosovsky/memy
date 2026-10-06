package memy_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/store/sqlite"
)

func TestIndependentSQLiteEnginesShareLifecycleIdempotency(t *testing.T) {
	// Arrange: two independently opened handles and Engine instances share a file.
	path := filepath.Join(t.TempDir(), "cross.db")
	first, setupErr := sqlite.Open(t.Context(), path, sqlite.Options{})
	if setupErr != nil {
		t.Fatal(setupErr)
	}
	f := newFixture(t, first)
	second, setupErr := sqlite.Open(t.Context(), path, sqlite.Options{})
	if setupErr != nil {
		t.Fatal(setupErr)
	}
	t.Cleanup(func() { _ = second.Close() })
	config := f.config
	config.Store = second
	peer, setupErr := memy.New(config)
	if setupErr != nil {
		t.Fatal(setupErr)
	}
	p := f.propose(t, "remember", "private", memy.Interval{})
	request := f.acceptedRequest(t, "commit", "record", 0, p, memy.Append)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	assertConcurrentCommit(ctx, t, f, peer, request)
	// Act: repeat shared idempotent Forget concurrently across those same handles.
	deletion := memy.ForgetRequest{
		OperationID:   "delete",
		Selector:      memy.Selector{Kind: memy.SelectRecord, ID: "record"},
		Reason:        "delete",
		PolicyVersion: "delete/v1",
		Limit:         1,
		MaxBytes:      1 << 20,
	}
	assertConcurrentForget(ctx, t, f, peer, deletion)
	// Assert: one canonical commit revision, one tombstone, no resurrection.
	viewErr := first.View(t.Context(), f.scope, func(b memy.Bucket) error {
		heads, err := listEntries(b, "head/")
		if err != nil {
			return err
		}
		if len(heads) != 1 {
			t.Fatal("duplicate heads")
		}
		var head struct {
			Data struct {
				Revision memy.Version     `json:"revision"`
				State    memy.RecordState `json:"state"`
			} `json:"data"`
		}
		if decodeErr := json.Unmarshal(heads[0].Value.Data, &head); decodeErr != nil {
			return decodeErr
		}
		if head.Data.Revision != 2 || head.Data.State != memy.Revoked {
			t.Fatalf("extra canonical revision: %+v", head.Data)
		}
		history, err := listEntries(b, "record/")
		if err != nil {
			return err
		}
		if len(history) != 0 {
			t.Fatal("retained history")
		}
		return nil
	})
	if viewErr != nil {
		t.Fatal(viewErr)
	}
	for _, engine := range []*memy.Engine[preference, sourceRef, principal]{f.engine, peer} {
		if _, getErr := engine.Get(
			t.Context(),
			f.actor,
			f.scope,
			"record",
			memy.ReadOptions{},
		); !errors.Is(
			getErr,
			memy.ErrNotFound,
		) {
			t.Fatalf("resurrection: %v", getErr)
		}
	}
}

func assertConcurrentCommit(
	ctx context.Context,
	t *testing.T,
	f fixture,
	peer *memy.Engine[preference, sourceRef, principal],
	request memy.CommitRequest,
) {
	t.Helper()
	start := make(chan struct{})
	commits := make(chan memy.CommitReceipt, 2)
	failures := make(chan error, 2)
	// Act: start the exact same commit from both handles together.
	for _, engine := range []*memy.Engine[preference, sourceRef, principal]{f.engine, peer} {
		go func() {
			<-start
			r, err := engine.Commit(ctx, f.actor, f.scope, "assist", request)
			commits <- r
			failures <- err
		}()
	}
	close(start)
	for range 2 {
		if failure := <-failures; failure != nil {
			t.Fatal(failure)
		}
		r := <-commits
		if !r.CanonicalCommitted || r.Revision != 1 || r.RecordID != "record" {
			t.Fatalf("commit=%+v", r)
		}
	}
}

func assertConcurrentForget(
	ctx context.Context,
	t *testing.T,
	f fixture,
	peer *memy.Engine[preference, sourceRef, principal],
	deletion memy.ForgetRequest,
) {
	t.Helper()
	start := make(chan struct{})
	purges := make(chan memy.PurgeReceipt, 2)
	failures := make(chan error, 2)
	for _, engine := range []*memy.Engine[preference, sourceRef, principal]{f.engine, peer} {
		go func() {
			<-start
			r, err := fullForget(ctx, engine, f.actor, f.scope, "assist", deletion)
			purges <- r
			failures <- err
		}()
	}
	close(start)
	var previous memy.PurgeReceipt
	for i := range 2 {
		if failure := <-failures; failure != nil {
			t.Fatal(failure)
		}
		r := <-purges
		if r.State != memy.PurgeComplete {
			t.Fatalf("purge=%+v", r)
		}
		if i > 0 &&
			(r.Batch.Epoch != previous.Batch.Epoch || r.Batch.Chunk != previous.Batch.Chunk) {
			t.Fatal("two independent revoke identities")
		}
		previous = r
	}
}
