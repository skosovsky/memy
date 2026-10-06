package memy_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
	"github.com/skosovsky/memy/store/sqlite"
)

func FuzzScopeIdentity(f *testing.F) {
	for _, seed := range []string{"кириллица", "👩", `a/"b`, "\ufffd", "e\u0301", "é", string([]byte{255})} {
		f.Add(seed, "namespace", "subject", seed+"other", "namespace", "subject")
	}
	f.Fuzz(func(t *testing.T, at, an, as, bt, bn, bs string) {
		// Arrange.
		a := memy.Scope{Tenant: at, Namespace: an, Subject: as}
		b := memy.Scope{Tenant: bt, Namespace: bn, Subject: bs}
		if a.Validate() != nil || b.Validate() != nil {
			return
		}
		// Act.
		var roundtrip memy.Scope
		err := json.Unmarshal([]byte(a.Key()), &roundtrip)
		// Assert: exact identity survives wire and keys distinguish tuples.
		if err != nil || roundtrip != a || (a != b && a.Key() == b.Key()) {
			t.Fatal("scope identity lost")
		}
	})
}

func TestEngineRejectsMalformedIdentityBeforeMutation(t *testing.T) {
	for _, field := range []string{"scope", "operation", "source-id", "source-revision", "extractor", "evidence", "purpose", "lineage"} {
		t.Run(field, func(t *testing.T) {
			// Arrange.
			f := newFixture(t, nil)
			bad := string([]byte{255})
			scope, operation, purpose := f.scope, "remember", "assist"
			suggestion := f.suggestion("value", memy.Interval{})
			switch field {
			case "scope":
				scope.Tenant = bad
			case "operation":
				operation = bad
			case "source-id":
				suggestion.Sources[0].ID = bad
			case "source-revision":
				suggestion.Sources[0].Revision = bad
			case "extractor":
				suggestion.Extractor = bad
			case "evidence":
				suggestion.Evidence = bad
			case "purpose":
				purpose = bad
			case "lineage":
				suggestion.Lineage = []memy.RevisionRef{{RecordID: bad, Revision: 1}}
			}
			// Act.
			_, err := f.engine.Remember(t.Context(), f.actor, scope, operation, purpose, suggestion)
			// Assert: no partially usable proposal/operation is persisted.
			if err == nil {
				t.Fatal("malformed identity admitted")
			}
			if err := f.config.Store.View(t.Context(), f.scope, func(b memy.Bucket) error {
				entries, err := listEntries(b, "")
				if len(entries) != 0 {
					t.Fatal("invalid input changed state")
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestUnicodeIdentityCompletesLifecycle(t *testing.T) {
	// Arrange: literal replacement rune and Unicode spelling are exact identities.
	f := newFixture(t, nil)
	f.scope = memy.Scope{Tenant: "\ufffd", Namespace: "предпочтения", Subject: "👩"}
	f.policy.Grant(f.actor.actor, f.scope, "authority/v1", memy.ActionRead, memy.ActionPropose, memy.ActionAccept, memy.ActionCommit)
	f.source.ID, f.source.Revision = "источник/\ufffd", "версия/👩"
	if err := f.sources.Put(f.scope, f.source); err != nil {
		t.Fatal(err)
	}
	// Act.
	p := f.propose(t, `операция/"👩`, "UTC+7", memy.Interval{})
	request := f.acceptedRequest(t, "коммит", "запись/\ufffd", 0, p, memy.Append)
	f.commit(t, request)
	record, err := f.engine.Get(t.Context(), f.actor, f.scope, request.RecordID, memy.ReadOptions{})
	// Assert.
	if err != nil || record.ID != request.RecordID || record.Scope != f.scope || record.Provenance.Sources[0].ID != f.source.ID {
		t.Fatalf("record=%+v err=%v", record, err)
	}
}

func TestReconciliationDecisionSurvivesReopenAndReplay(t *testing.T) {
	for _, mode := range []memy.ReconcileMode{memy.Append, memy.Duplicate, memy.Supersede, memy.Conflict} {
		t.Run(string(mode), func(t *testing.T) {
			// Arrange.
			path := filepath.Join(t.TempDir(), "knowledge.db")
			store, err := sqlite.Open(t.Context(), path, sqlite.Options{})
			if err != nil {
				t.Fatal(err)
			}
			f := newFixture(t, store)
			base := f.propose(t, "base-proposal", "original", memy.Interval{})
			f.commit(t, f.acceptedRequest(t, "base-commit", "base", 0, base, memy.Append))
			p := f.propose(t, "proposal", "new", memy.Interval{})
			var refs []memy.RevisionRef
			if mode != memy.Append {
				refs = []memy.RevisionRef{{RecordID: "base", Revision: 1}}
			}
			request := f.acceptedRequest(t, "commit", "new", 0, p, mode, refs...)
			request.Reconcile.PolicyVersion = "resolver/проверка"
			request.Reconcile.Basis = "sensitive initial basis 👩"
			original := f.commit(t, request)
			// Act: reopen without transcript and replay exact request.
			f = reopenFixture(t, f, path)
			replayed := f.commit(t, request)
			record, err := f.engine.Get(t.Context(), f.actor, f.scope, "new", memy.ReadOptions{IncludeConflicts: true})
			changed := request
			changed.Reconcile.Basis = "different basis"
			_, conflict := f.engine.Commit(t.Context(), f.actor, f.scope, "assist", changed)
			// Assert: one initial decision, independent authority and resolver policies.
			if err != nil || original != replayed || record.Revision != 1 || !reflect.DeepEqual(record.Reconciliation, &request.Reconcile) || record.AuthorityPolicyVersion != "authority/v1" || !errors.Is(conflict, memy.ErrConflict) {
				t.Fatalf("record=%+v err=%v replay=%+v conflict=%v", record, err, replayed, conflict)
			}
		})
	}
}

func TestHistoricalDecisionDoesNotExposeFutureResolution(t *testing.T) {
	// Arrange: an old revision later gets superseded under a different resolver.
	f := newFixture(t, nil)
	p := f.propose(t, "p1", "old", memy.Interval{})
	first := f.acceptedRequest(t, "c1", "fact", 0, p, memy.Append)
	first.Reconcile.Basis = "initial basis"
	f.commit(t, first)
	asof := f.clock.Now()
	f.clock.Advance(time.Second)
	p2 := f.propose(t, "p2", "new", memy.Interval{})
	second := f.acceptedRequest(t, "c2", "fact", 1, p2, memy.Supersede, memy.RevisionRef{RecordID: "fact", Revision: 1})
	second.Reconcile.Basis = "future sensitive basis"
	f.commit(t, second)
	// Act.
	old, err := f.engine.Get(t.Context(), f.actor, f.scope, "fact", memy.ReadOptions{RecordedAsOf: asof})
	_, denied := f.engine.Get(t.Context(), principal{actor: "other"}, f.scope, "fact", memy.ReadOptions{RecordedAsOf: asof})
	// Assert.
	if err != nil || old.Revision != 1 || old.State != memy.Active || old.Reconciliation.Basis != first.Reconcile.Basis || !errors.Is(denied, memy.ErrUnauthorized) {
		t.Fatalf("old=%+v err=%v denied=%v", old, err, denied)
	}
	encoded, _ := json.Marshal(old)
	if bytes.Contains(encoded, []byte(second.Reconcile.Basis)) {
		t.Fatal("future basis leaked into historical record")
	}
}

func TestForgetPurgesDecisionAndManagedProjection(t *testing.T) {
	for _, mode := range []memy.ReconcileMode{memy.Append, memy.Duplicate, memy.Supersede, memy.Conflict} {
		t.Run(string(mode), func(t *testing.T) {
			// Arrange: basis is copied into a managed projection, not into permanent receipts.
			f, _, summary := withSinks(t)
			var related []memy.RevisionRef
			if mode != memy.Append {
				base := f.propose(t, "base-proposal", "base", memy.Interval{})
				f.commit(t, f.acceptedRequest(t, "base-commit", "base", 0, base, memy.Append))
				related = []memy.RevisionRef{{RecordID: "base", Revision: 1}}
			}
			p := f.propose(t, "p", "value", memy.Interval{})
			request := f.acceptedRequest(t, "c", "fact", 0, p, mode, related...)
			request.Reconcile.Basis = "sensitive-decision-basis"
			receipt := f.commit(t, request)
			projection, err := memy.Project(t.Context(), f.engine, f.actor, f.scope, "fact", memy.ReadOptions{IncludeConflicts: true}, reference.ProjectorFunc[preference, sourceRef, string]{PolicyVersion: "projection/v1", Apply: func(_ context.Context, r memy.Record[preference, sourceRef]) (string, error) {
				return r.Reconciliation.Basis, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(projection)
			if err != nil {
				t.Fatal(err)
			}
			fence, err := f.engine.Fence(t.Context(), f.actor, f.scope, "assist")
			if err != nil {
				t.Fatal(err)
			}
			lineage := []memy.RevisionRef{{RecordID: receipt.RecordID, Revision: receipt.Revision}}
			writeErr := f.engine.WithDerivedWrite(t.Context(), f.actor, fence, "assist", lineage, func(ctx context.Context) error { return summary.Put(ctx, "decision", f.scope, lineage, raw) })
			if mode == memy.Conflict {
				if !errors.Is(writeErr, memy.ErrStaleInput) {
					t.Fatalf("conflicted dependency admitted: %v", writeErr)
				}
			} else if writeErr != nil {
				t.Fatal(writeErr)
			}
			// Act.
			purged, err := fullForget(f.engine, t.Context(), f.actor, f.scope, "assist", memy.ForgetRequest{OperationID: "forget", Selector: memy.Selector{Kind: memy.SelectRecord, ID: "fact"}, Reason: "withdraw", PolicyVersion: "deletion/v1"})
			_, getErr := f.engine.Get(t.Context(), f.actor, f.scope, "fact", memy.ReadOptions{})
			// Assert: history, tombstones, receipts and managed sink contain no basis.
			if err != nil || purged.State != memy.PurgeComplete || summary.Contains("decision") || !errors.Is(getErr, memy.ErrNotFound) {
				t.Fatalf("purge=%+v err=%v get=%v", purged, err, getErr)
			}
			if err := f.config.Store.View(t.Context(), f.scope, func(b memy.Bucket) error {
				entries, err := listEntries(b, "")
				for _, entry := range entries {
					if bytes.Contains(entry.Value.Data, []byte(request.Reconcile.Basis)) {
						t.Fatal("basis survived forget")
					}
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPersistedIdentityRejectsNormalization(t *testing.T) {
	for _, replacement := range []string{`"\ud800"`, `"\udfff"`, "\"" + string([]byte{255}) + "\""} {
		t.Run(strings.ReplaceAll(replacement, "/", "_"), func(t *testing.T) {
			// Arrange: corrupt the raw wire before JSON decoding can repair it.
			f := newFixture(t, nil)
			p := f.propose(t, "p", "value", memy.Interval{})
			f.commit(t, f.acceptedRequest(t, "c", "fact", 0, p, memy.Append))
			if err := f.config.Store.Update(t.Context(), f.scope, func(b memy.Bucket) error {
				entries, err := listEntries(b, "head/")
				if err != nil {
					return err
				}
				entry := entries[0]
				raw := bytes.Replace(entry.Value.Data, []byte(`"id":"fact"`), []byte(`"id":`+replacement), 1)
				_, err = b.Put(entry.Key, entry.Value.Version, raw)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			// Act.
			_, err := f.engine.Get(t.Context(), f.actor, f.scope, "fact", memy.ReadOptions{})
			// Assert.
			if !errors.Is(err, memy.ErrSchema) {
				t.Fatalf("malformed identity: %v", err)
			}
		})
	}
}

func TestPurgeReceiptRejectsMalformedRecordIdentity(t *testing.T) {
	for _, id := range []string{"", "a\x00b", strings.Repeat("x", 1025), strings.Repeat("я", 513), "\u00a0"} {
		t.Run(string([]rune(id)[:min(8, len([]rune(id)))]), func(t *testing.T) {
			// Arrange: corrupt a durable receipt after successful revocation.
			f := newFixture(t, nil)
			p := f.propose(t, "p", "value", memy.Interval{})
			f.commit(t, f.acceptedRequest(t, "c", "fact", 0, p, memy.Append))
			request := memy.ForgetRequest{OperationID: "forget", Selector: memy.Selector{Kind: memy.SelectRecord, ID: "fact"}, Reason: "withdraw", PolicyVersion: "deletion/v1"}
			if _, err := fullForget(f.engine, t.Context(), f.actor, f.scope, "assist", request); err != nil {
				t.Fatal(err)
			}
			if err := f.config.Store.Update(t.Context(), f.scope, func(b memy.Bucket) error {
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
				receipt.Batch.Records = []string{id}
				envelope["data"], err = json.Marshal(receipt)
				if err != nil {
					return err
				}
				raw, err := json.Marshal(envelope)
				if err != nil {
					return err
				}
				_, err = b.Put(entry.Key, entry.Value.Version, raw)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			// Act.
			_, err := fullForget(f.engine, t.Context(), f.actor, f.scope, "assist", request)
			// Assert: replay never certifies malformed receipts as complete.
			if !errors.Is(err, memy.ErrSchema) {
				t.Fatalf("malformed receipt: %v", err)
			}
		})
	}
}

func TestExecutableSchemaIdentityAssertions(t *testing.T) {
	// Arrange: independent boundary cases include Unicode byte count and whitespace.
	schemas := compileSchemas(t)
	for _, id := range []string{"", "a\x00b", strings.Repeat("x", 1025), strings.Repeat("я", 513), "\u00a0"} {
		// Act / Assert: scope and revision identity reject the same forbidden text.
		scope := map[string]any{"tenant": id, "namespace": "n", "subject": "s"}
		for _, instance := range []map[string]any{
			{"schema": memy.SchemaVersion, "kind": "acceptance", "data": map[string]any{"proposal_id": "p", "proposal_revision": 1, "digest": strings.Repeat("a", 64), "actor": "actor", "scope": scope, "policy_version": "v2", "expires_at": "0001-01-01T00:00:00Z"}},
			{"schema": memy.SchemaVersion, "kind": "acceptance", "data": map[string]any{"proposal_id": id, "proposal_revision": 1, "digest": strings.Repeat("a", 64), "actor": "actor", "scope": map[string]any{"tenant": "t", "namespace": "n", "subject": "s"}, "policy_version": "v2", "expires_at": "0001-01-01T00:00:00Z"}},
		} {
			if err := schemas["document"].Validate(instance); err == nil {
				t.Fatalf("schema accepted invalid identity of %d bytes", len(id))
			}
		}
	}
}
