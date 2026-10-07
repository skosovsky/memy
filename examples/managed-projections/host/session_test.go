package host_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/examples/managed-projections/checkpoint"
	"github.com/skosovsky/memy/examples/managed-projections/host"
	"github.com/skosovsky/memy/reference"
)

func openSession(t *testing.T, dir string) *host.Session {
	t.Helper()
	s, err := host.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}
func scope() memy.Scope { return memy.Scope{Tenant: "a", Namespace: "knowledge", Subject: "user"} }
func seed(t *testing.T, s *host.Session, sc memy.Scope, id string) memy.RevisionRef {
	t.Helper()
	if err := s.Provision(sc); err != nil {
		t.Fatal(err)
	}
	ref, err := s.Seed(t.Context(), sc, id, "canonical "+id)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func persist(
	t *testing.T,
	s *host.Session,
	sc memy.Scope,
	handle string,
	refs ...memy.RevisionRef,
) (string, memy.EpochFence, memy.ProjectedRecallResult[string, string]) {
	t.Helper()
	fence, result, err := s.Prepare(t.Context(), sc, refs)
	if err != nil {
		t.Fatal(err)
	}
	key, err := s.Persist(t.Context(), fence, handle, result)
	if err != nil {
		t.Fatal(err)
	}
	return key, fence, result
}

//nolint:gocognit // Sequential restart/restore assertions exercise one durable failure history.
func TestDurableMultiLineagePendingRetryRestore(t *testing.T) {
	// Arrange: two checkpoints depend on both canonical inputs.
	dir := t.TempDir()
	s := openSession(t, dir)
	sc := scope()
	a := seed(t, s, sc, "a")
	b := seed(t, s, sc, "b")
	first, fence, result := persist(t, s, sc, "first", a, b)
	second, _, _ := persist(t, s, sc, "second", a, b)
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	s.Checkpoints.FailPurge(errors.New("offline"))
	// Act: revoke succeeds, sink cannot acknowledge; then recover both DBs.
	pending, err := s.Forget(t.Context(), sc, a)
	// Assert.
	if err != nil || pending.State == memy.PurgeComplete {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openSession(t, dir)
	if err = reopened.Provision(sc); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{first, second} {
		data, readErr := checkpoint.ReadValidated(
			t.Context(),
			reopened.Checkpoints,
			reopened.Engine,
			"reader",
			sc,
			host.Purpose,
			key,
		)
		if readErr == nil || len(data) != 0 {
			t.Fatalf("revoked checkpoint returned %q err=%v", data, readErr)
		}
	}
	complete, err := reopened.Forget(t.Context(), sc, a)
	if err != nil || complete.State != memy.PurgeComplete {
		t.Fatalf("retry=%+v err=%v", complete, err)
	}
	// A late remote retry cannot recreate the artifact even through privileged storage.
	_, err = reopened.Checkpoints.Put(
		t.Context(),
		fence,
		checkpoint.Artifact{Scope: sc, Handle: "late", Lineage: []memy.RevisionRef{a, b}, Data: raw},
	)
	if !errors.Is(err, memy.ErrStaleInput) {
		t.Fatalf("late retry err=%v", err)
	}
	if err = reopened.Close(); err != nil {
		t.Fatal(err)
	}
	final := openSession(t, dir)
	if err = final.Provision(sc); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{first, second} {
		if _, err = final.Checkpoints.Load(t.Context(), sc, key); !errors.Is(err, memy.ErrNotFound) {
			t.Fatalf("durable deletion err=%v", err)
		}
	}
	// Restore an old checkpoint DB without restoring old canonical authority.
	restored, err := checkpoint.Open(t.Context(), filepath.Join(t.TempDir(), "restored.db"), "restored")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restored.Close() })
	oldKey, err := restored.Put(
		t.Context(),
		fence,
		checkpoint.Artifact{Scope: sc, Handle: "old-backup", Lineage: []memy.RevisionRef{a, b}, Data: raw},
	)
	if err != nil {
		t.Fatal(err)
	}
	data, err := checkpoint.ReadValidated(t.Context(), restored, final.Engine, "reader", sc, host.Purpose, oldKey)
	if err == nil || len(data) != 0 {
		t.Fatalf("restored revoked data=%q err=%v", data, err)
	}
	_, got, err := final.Prepare(t.Context(), sc, []memy.RevisionRef{a, b})
	if err != nil || len(got.Projections) != 1 || got.Projections[0].RecordID != "b" {
		t.Fatalf("late index body=%+v err=%v", got, err)
	}
}

func TestCheckpointScopeIsolationAndStructuralKeys(t *testing.T) {
	// Arrange: identical IDs/handles in tenant, namespace and subject variants.
	s := openSession(t, t.TempDir())
	scopes := []memy.Scope{
		scope(),
		{Tenant: "b", Namespace: "knowledge", Subject: "user"},
		{Tenant: "a", Namespace: "other", Subject: "user"},
		{Tenant: "a", Namespace: "knowledge", Subject: "other"},
		{Tenant: "a/b", Namespace: "c", Subject: "d"},
		{Tenant: "a", Namespace: "b/c", Subject: "d"},
	}
	keys := make([]string, len(scopes))
	refs := make([]memy.RevisionRef, len(scopes))
	seen := map[string]bool{}
	for i, sc := range scopes {
		refs[i] = seed(t, s, sc, "same")
		keys[i], _, _ = persist(t, s, sc, "same/handle", refs[i])
		if seen[keys[i]] {
			t.Fatal("structural key collision")
		}
		seen[keys[i]] = true
	}
	// Act: forget only the first exact scope.
	receipt, err := s.Forget(t.Context(), scopes[0], refs[0])
	// Assert.
	if err != nil || receipt.State != memy.PurgeComplete {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	for i := 1; i < len(scopes); i++ {
		if _, err = checkpoint.ReadValidated(
			t.Context(),
			s.Checkpoints,
			s.Engine,
			"reader",
			scopes[i],
			host.Purpose,
			keys[i],
		); err != nil {
			t.Fatal(err)
		}
		if _, err = s.Checkpoints.Load(t.Context(), scopes[0], keys[i]); !errors.Is(err, memy.ErrNotFound) {
			t.Fatalf("foreign key err=%v", err)
		}
	}
}

func TestForgetBetweenProjectionAndPersistence(t *testing.T) {
	// Arrange.
	s := openSession(t, t.TempDir())
	sc := scope()
	ref := seed(t, s, sc, "fact")
	fence, result, err := s.Prepare(t.Context(), sc, []memy.RevisionRef{ref})
	if err != nil {
		t.Fatal(err)
	}
	prepared := make(chan struct{})
	revoked := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(prepared)
		<-revoked
		_, writeErr := s.Persist(context.Background(), fence, "late", result)
		done <- writeErr
	}()
	// Act: use explicit barriers instead of sleeps.
	<-prepared
	receipt, err := s.Forget(t.Context(), sc, ref)
	close(revoked)
	writeErr := <-done
	// Assert.
	if err != nil || receipt.State != memy.PurgeComplete || !errors.Is(writeErr, memy.ErrStaleInput) {
		t.Fatalf("purge=%+v err=%v write=%v", receipt, err, writeErr)
	}
}

func TestWriteBeforeForgetAndUnknownCallbackOutcome(t *testing.T) {
	// Arrange: an external effect commits before the callback reports a failure.
	s := openSession(t, t.TempDir())
	sc := scope()
	ref := seed(t, s, sc, "fact")
	fence, result, err := s.Prepare(t.Context(), sc, []memy.RevisionRef{ref})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	writeDone := make(chan error, 1)
	purgeDone := make(chan error, 1)
	var key string
	var callbacks atomic.Int32
	go func() {
		writeDone <- s.Engine.WithDerivedWrite(context.Background(), "reader", fence, host.Purpose, []memy.RevisionRef{ref}, func(ctx context.Context) error {
			callbacks.Add(1)
			var putErr error
			key, putErr = s.Checkpoints.Put(ctx, fence, checkpoint.Artifact{Scope: sc, Handle: "ambiguous", Lineage: []memy.RevisionRef{ref}, Data: raw})
			close(entered)
			<-release
			if putErr != nil {
				return putErr
			}
			return memy.ErrUnknownOutcome
		})
	}()
	<-entered
	go func() { _, purgeErr := s.Forget(context.Background(), sc, ref); purgeDone <- purgeErr }()
	// Act.
	close(release)
	writeErr := <-writeDone
	purgeErr := <-purgeDone
	// Assert: the failed callback had a real effect, which subsequent purge removed.
	if !errors.Is(writeErr, memy.ErrUnknownOutcome) || purgeErr != nil || callbacks.Load() != 1 {
		t.Fatalf("write=%v purge=%v calls=%d", writeErr, purgeErr, callbacks.Load())
	}
	if _, err = s.Checkpoints.Load(t.Context(), sc, key); !errors.Is(err, memy.ErrNotFound) {
		t.Fatalf("artifact survived purge: %v", err)
	}
}

func TestCheckpointSourceChangeAndCancellationFailClosed(t *testing.T) {
	// Arrange.
	s := openSession(t, t.TempDir())
	sc := scope()
	ref := seed(t, s, sc, "fact")
	key, _, _ := persist(t, s, sc, "cached", ref)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// Act and Assert: cancellation is preserved without delivering bytes.
	data, err := checkpoint.ReadValidated(ctx, s.Checkpoints, s.Engine, "reader", sc, host.Purpose, key)
	if !errors.Is(err, context.Canceled) || len(data) != 0 {
		t.Fatalf("cancel data=%q err=%v", data, err)
	}
	if err = s.Sources.Put(
		sc,
		memy.Source[string]{ID: "source", Revision: "changed", Reference: "host://source"},
	); err != nil {
		t.Fatal(err)
	}
	data, err = checkpoint.ReadValidated(t.Context(), s.Checkpoints, s.Engine, "reader", sc, host.Purpose, key)
	if err == nil || len(data) != 0 {
		t.Fatalf("stale source data=%q err=%v", data, err)
	}
}

func TestCheckpointMissingAndUnavailableCanonicalLineage(t *testing.T) {
	t.Run("missing exact revision", func(t *testing.T) {
		// Arrange: a restored checkpoint references canonical data that is missing.
		s := openSession(t, t.TempDir())
		sc := scope()
		if err := s.Provision(sc); err != nil {
			t.Fatal(err)
		}
		key, err := s.Checkpoints.Put(
			t.Context(),
			memy.EpochFence{Scope: sc},
			checkpoint.Artifact{
				Scope:   sc,
				Handle:  "orphan",
				Lineage: []memy.RevisionRef{{RecordID: "missing", Revision: 7}},
				Data:    []byte("old checkpoint"),
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		// Act.
		data, err := checkpoint.ReadValidated(t.Context(), s.Checkpoints, s.Engine, "reader", sc, host.Purpose, key)
		// Assert.
		if err == nil || len(data) != 0 {
			t.Fatalf("orphan data=%q err=%v", data, err)
		}
		if _, err = s.Checkpoints.Load(t.Context(), sc, key); !errors.Is(err, memy.ErrNotFound) {
			t.Fatalf("orphan not invalidated: %v", err)
		}
	})
	t.Run("verification unavailable", func(t *testing.T) {
		// Arrange: the checkpoint is durable but canonical verification is unavailable.
		s := openSession(t, t.TempDir())
		sc := scope()
		ref := seed(t, s, sc, "fact")
		key, _, _ := persist(t, s, sc, "checkpoint", ref)
		if err := s.Canonical.Close(); err != nil {
			t.Fatal(err)
		}
		// Act.
		data, err := checkpoint.ReadValidated(t.Context(), s.Checkpoints, s.Engine, "reader", sc, host.Purpose, key)
		// Assert: there is no cache fallback when eligibility cannot be proven.
		if err == nil || len(data) != 0 {
			t.Fatalf("unverified data=%q err=%v", data, err)
		}
	})
}

type observedAuthority struct {
	policy *reference.Policy[string]
	checks int
}

func (a *observedAuthority) Check(ctx context.Context, identity string, access memy.Access) (memy.Decision, error) {
	a.checks++
	return a.policy.Check(ctx, identity, access)
}

type changingCheckpointReader struct {
	store     *checkpoint.Store
	authority *observedAuthority
	change    func()
	changed   bool
}

func (r *changingCheckpointReader) Load(ctx context.Context, sc memy.Scope, key string) (checkpoint.Artifact, error) {
	a, err := r.store.Load(ctx, sc, key)
	// Emulate host state changing during storage I/O performed after eligibility.
	if r.authority.checks > 0 {
		r.change()
		r.changed = true
	}
	return a, err
}
func (r *changingCheckpointReader) Invalidate(ctx context.Context, sc memy.Scope, key string) error {
	return r.store.Invalidate(ctx, sc, key)
}

func TestCheckpointDeliveryRevalidatesAfterAllStorageIO(t *testing.T) {
	for _, fault := range []string{"authority revoked", "source removed"} {
		t.Run(fault, func(t *testing.T) {
			// Arrange: a real SQLite reader can invalidate host state during its Load.
			s := openSession(t, t.TempDir())
			sc := scope()
			ref := seed(t, s, sc, "fact")
			key, _, _ := persist(t, s, sc, "checkpoint", ref)
			authority := &observedAuthority{policy: s.Policy}
			engine, err := memy.New(
				memy.Config[string, string, string]{
					Store:          s.Canonical,
					Authority:      authority,
					Sources:        s.Sources,
					Clock:          s.Clock,
					Retention:      reference.Retain[string]{Version: "retain"},
					PayloadCodec:   memy.JSONCodec[string]{},
					ReferenceCodec: memy.JSONCodec[string]{},
					Sinks:          []memy.Sink{s.Checkpoints},
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			reader := &changingCheckpointReader{store: s.Checkpoints, authority: authority, change: func() {
				if fault == "authority revoked" {
					s.Policy.Grant("reader", sc, "revoked")
				} else {
					s.Sources.Remove(sc, "source")
				}
			}}
			// Act.
			data, err := checkpoint.ReadValidated(t.Context(), reader, engine, "reader", sc, host.Purpose, key)
			// Assert: either I/O precedes the final gate or changed authority/source blocks delivery.
			if reader.changed && (err == nil || len(data) != 0) {
				t.Fatalf("host changed during I/O but data=%q err=%v", data, err)
			}
			if !reader.changed && (err != nil || len(data) == 0) {
				t.Fatalf("valid delivery data=%q err=%v", data, err)
			}
		})
	}
}
