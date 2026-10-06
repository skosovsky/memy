package memy_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
	"github.com/skosovsky/memy/store/memory"
	"github.com/skosovsky/memy/store/sqlite"
)

type genericCodecFixture struct {
	base   fixture
	engine *memy.Engine[any, any, principal]
	config memy.Config[any, any, principal]
	source memy.Source[any]
}

func newGenericCodecFixture(t *testing.T, store memy.Store) genericCodecFixture {
	t.Helper()
	base := newFixture(t, store)
	codec := memy.JSONCodec[any]{}
	sources := reference.NewRegistry[any](codec)
	source := memy.Source[any]{ID: "numeric-source", Revision: "r1", Reference: map[string]any{
		"number": json.Number("9007199254740993"), "decimal": json.Number("1.2300"), "exponent": json.Number("1e99"),
	}}
	if err := sources.Put(base.scope, source); err != nil {
		t.Fatal(err)
	}
	config := memy.Config[any, any, principal]{
		Store: base.config.Store, Authority: base.policy, Sources: sources, Clock: base.clock,
		Retention: reference.Retain[any]{Version: "retention/v1"}, PayloadCodec: codec, ReferenceCodec: codec,
	}
	engine, err := memy.New(config)
	if err != nil {
		t.Fatal(err)
	}
	return genericCodecFixture{base: base, engine: engine, config: config, source: source}
}

func (f genericCodecFixture) suggestion(payload any) memy.Suggestion[any, any] {
	return memy.Suggestion[any, any]{Payload: payload, Sources: []memy.Source[any]{f.source},
		Evidence: "exact host numbers", Extractor: "manual/v1", ObservedAt: f.base.clock.Now()}
}

func (f genericCodecFixture) commit(t *testing.T, payload any) memy.Proposal[any, any] {
	t.Helper()
	proposal, err := f.engine.Remember(
		t.Context(),
		f.base.actor,
		f.base.scope,
		"remember-number",
		"assist",
		f.suggestion(payload),
	)
	if err != nil {
		t.Fatal(err)
	}
	acceptance, err := f.engine.Accept(
		t.Context(),
		f.base.actor,
		f.base.scope,
		proposal.ID,
		proposal.Digest,
		proposal.Revision,
		"assist",
	)
	if err != nil {
		t.Fatal(err)
	}
	if acceptance.Digest != proposal.Digest {
		t.Fatal("acceptance changed digest")
	}
	_, err = f.engine.Commit(t.Context(), f.base.actor, f.base.scope, "assist", memy.CommitRequest{
		OperationID: "commit-number", ProposalID: proposal.ID, Acceptance: acceptance, RecordID: "number",
		Reconcile: memy.Reconciliation{Mode: memy.Append, PolicyVersion: "resolver/v1", Basis: "explicit append"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return proposal
}

func TestJSONCodecExactLifecycle(t *testing.T) {
	for _, profile := range []string{"memory", "sqlite"} {
		t.Run(profile, func(t *testing.T) {
			// Arrange.
			store := codecLifecycleStore(t, profile)
			f := newGenericCodecFixture(t, store)
			payload := map[string]any{"numbers": []any{
				json.Number(
					"9007199254740993",
				),
				json.Number("9223372036854775807"),
				json.Number("-9223372036854775808"),
				map[string]any{"decimal": json.Number("1.2300"), "exponent": json.Number("1E-99"), "text": "�😀"},
			}}
			expected := encodeJSONFixture(t, payload)
			expectedSource := encodeJSONFixture(t, f.source.Reference)
			// Act.
			proposal := f.commit(t, payload)
			record, err := f.engine.Get(
				t.Context(),
				f.base.actor,
				f.base.scope,
				"number",
				memy.ReadOptions{Purpose: "assist"},
			)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := f.engine.Proposal(t.Context(), f.base.actor, f.base.scope, proposal.ID, "assist")
			if err != nil {
				t.Fatal(err)
			}
			actual := encodeJSONFixture(t, record.Payload)
			actualSource := encodeJSONFixture(t, record.Provenance.Sources[0].Reference)
			proposed := encodeJSONFixture(t, loaded.Suggestion.Payload)
			// Assert: acceptance, persisted proposal and delivered record bind the same bytes.
			if !bytes.Equal(actual, expected) || !bytes.Equal(proposed, expected) ||
				!bytes.Equal(actualSource, expectedSource) || loaded.Digest != proposal.Digest {
				t.Fatalf("changed accepted bytes/digest: payload=%s source=%s", actual, actualSource)
			}
		})
	}
}

func TestJSONCodecUnicodeAdmissionLeavesNoLedger(t *testing.T) {
	for _, target := range []string{"payload", "reference"} {
		t.Run(target, func(t *testing.T) {
			// Arrange: proposal and operation keys must remain absent on invalid input.
			f := newGenericCodecFixture(t, nil)
			suggestion := f.suggestion("valid")
			if target == "payload" {
				suggestion.Payload = map[string]any{"invalid": string([]byte{0xff})}
			} else {
				suggestion.Sources[0].Reference = json.RawMessage(`"\ud800"`)
			}
			// Act.
			proposal, err := f.engine.Remember(
				t.Context(),
				f.base.actor,
				f.base.scope,
				"bad-unicode",
				"assist",
				suggestion,
			)
			// Assert.
			if !errors.Is(err, memy.ErrInvalid) || proposal.ID != "" {
				t.Fatalf("admitted: %+v %v", proposal, err)
			}
			assertEmptyCodecLedger(t, f)
		})
	}
}

type legacyJSONCodec struct{ memy.JSONCodec[any] }

func (legacyJSONCodec) Version() string { return "json/v1" }

func TestJSONCodecOldIdentityRejected(t *testing.T) {
	// Arrange: retain valid legacy-labelled bytes, without changing or relabeling them.
	f := newGenericCodecFixture(t, nil)
	oldConfig := f.config
	oldConfig.PayloadCodec = legacyJSONCodec{}
	oldConfig.ReferenceCodec = legacyJSONCodec{}
	oldEngine, err := memy.New(oldConfig)
	if err != nil {
		t.Fatal(err)
	}
	legacy := f
	legacy.engine = oldEngine
	proposal := legacy.commit(t, json.Number("9007199254740993"))
	// Act / Assert: both canonical and proposal reads reject mismatched identities.
	_, err = f.engine.Get(t.Context(), f.base.actor, f.base.scope, "number", memy.ReadOptions{Purpose: "assist"})
	if !errors.Is(err, memy.ErrSchema) {
		t.Fatalf("legacy record accepted: %v", err)
	}
	_, err = f.engine.Proposal(t.Context(), f.base.actor, f.base.scope, proposal.ID, "assist")
	if !errors.Is(err, memy.ErrSchema) {
		t.Fatalf("legacy proposal accepted: %v", err)
	}
}

func codecLifecycleStore(t *testing.T, profile string) memy.Store {
	t.Helper()
	if profile == "memory" {
		return memory.New()
	}
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "numbers.db"), sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func encodeJSONFixture(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := (memy.JSONCodec[any]{}).Encode(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertEmptyCodecLedger(t *testing.T, f genericCodecFixture) {
	t.Helper()
	err := f.config.Store.View(t.Context(), f.base.scope, func(bucket memy.Bucket) error {
		page, scanErr := bucket.Scan(memy.ScanOptions{Limit: 1024, MaxBytes: 1 << 20})
		if scanErr != nil {
			return scanErr
		}
		if len(page.Entries) != 0 || !page.Complete {
			t.Fatalf("invalid admission wrote keys: %+v", page)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
