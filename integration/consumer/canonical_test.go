package compatibility_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/skosovsky/contexty"
	"github.com/skosovsky/ragy/retrieval"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/examples/managed-projections/checkpoint"
	"github.com/skosovsky/memy/examples/managed-projections/host"
	"github.com/skosovsky/memy/reference"
	"github.com/skosovsky/memy/store/memory"
)

func TestCanonicalRecallProjectedRagyContextyForget(t *testing.T) {
	// Arrange: real canonical engine with offline host ports.
	ctx := t.Context()
	scope := memy.Scope{Tenant: "tenant", Namespace: "knowledge", Subject: "user"}
	clock := reference.NewClock(time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))
	authority := reference.NewPolicy(func(a string) string { return a })
	authority.Grant(
		"user",
		scope,
		"auth/v1",
		memy.ActionPropose,
		memy.ActionAccept,
		memy.ActionCommit,
		memy.ActionRead,
		memy.ActionForget,
	)
	sources := reference.NewRegistry[string](memy.JSONCodec[string]{})
	source := memy.Source[string]{ID: "source", Revision: "r1", Reference: "document/section"}
	if err := sources.Put(scope, source); err != nil {
		t.Fatal(err)
	}
	engine, err := memy.New(
		memy.Config[string, string, string]{
			Store:          memory.New(),
			Authority:      authority,
			Sources:        sources,
			Clock:          clock,
			Retention:      reference.Retain[string]{Version: "retention/v1", ExpiresAt: clock.Now().Add(time.Hour)},
			PayloadCodec:   memy.JSONCodec[string]{},
			ReferenceCodec: memy.JSONCodec[string]{},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := engine.Remember(
		ctx,
		"user",
		scope,
		"proposal",
		"assist",
		memy.Suggestion[string, string]{
			Payload:    "canonical knowledge",
			Sources:    []memy.Source[string]{source},
			Extractor:  "host/v1",
			Evidence:   "source excerpt",
			ObservedAt: clock.Now(),
			Valid:      memy.Interval{Known: true, From: clock.Now()},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	acceptance, err := engine.Accept(ctx, "user", scope, proposal.ID, proposal.Digest, proposal.Revision, "assist")
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := engine.Commit(
		ctx,
		"user",
		scope,
		"assist",
		memy.CommitRequest{
			OperationID: "commit",
			RecordID:    "record",
			ProposalID:  proposal.ID,
			Acceptance:  acceptance,
			Reconcile:   memy.Reconciliation{Mode: memy.Append, PolicyVersion: "resolver/v1", Basis: "fixture"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	// Fake retrieval intentionally returns hostile/stale index payload; only IDs/revisions enter memy.
	search := adapter{
		docs: []retrieval.Document[meta]{
			{ID: "record", Content: "INDEX PAYLOAD MUST NOT LEAK", Rank: 1, Meta: meta{scope, receipt.Revision}},
			{ID: "record", Content: "STALE", Rank: 2, Meta: meta{scope, receipt.Revision + 100}},
		},
	}
	options := memy.RecallOptions{
		Read:   memy.ReadOptions{Purpose: "assist"},
		Search: memy.SearchOptions{MaxCandidates: 10},
		Limit:  10,
	}
	projector := reference.ProjectorFunc[string, string, string]{
		PolicyVersion: "projection/v1",
		Apply:         func(ctx context.Context, r memy.Record[string, string]) (string, error) { return r.Payload, ctx.Err() },
	}
	recall := func() (memy.ProjectedRecallResult[string, string], error) {
		return memy.RecallProjected(
			ctx,
			engine,
			"user",
			scope,
			"query",
			search,
			memy.ScoreRanker[string, string]{},
			options,
			projector,
			nil,
		)
	}
	// Act: real canonical checks and projection followed by real context compilation.
	out, err := recall()
	if err != nil {
		t.Fatal(err)
	}
	// Assert: stale revision rejected and canonical payload used instead of index content.
	if len(out.Projections) != 1 || out.Projections[0].Output != "canonical knowledge" ||
		out.Progress.CanonicalFiltered != 1 ||
		out.Projections[0].Retrieval == nil ||
		out.Projections[0].Retrieval.Signals[0].Score.Present {
		t.Fatalf("recall=%+v", out)
	}
	a := contexty.NewMemoryBlock(scope.Key()+"/record", contexty.TextPayload(out.Projections[0].Output)).
		WithPersistence(contexty.ArtifactPersistenceSkip)
	a.Lifecycle = contexty.ArtifactLifecycleEphemeral
	contextEngine := contexty.NewEngine(
		contexty.WithArtifactMaterialization(
			contexty.ArtifactMaterializationPolicy{
				Identity: contexty.Descriptor{ID: "consumer", Revision: "1"},
				Materialize: func(ctx context.Context, a contexty.ContextArtifact) (contexty.ArtifactRepresentation, error) {
					return contexty.ArtifactRepresentation{
						Role:  contexty.RoleUser,
						Parts: []contexty.ContentPart{contexty.TextPart{Text: a.Payload.PlainText()}},
					}, ctx.Err()
				},
			},
		),
	)
	compiled, err := contextEngine.CompileSnapshot(
		ctx,
		contexty.CompileRequest{CompilationID: "canonical", TurnID: "turn", Artifacts: []contexty.ContextArtifact{a}},
	)
	if err != nil || len(compiled.Artifacts) != 1 ||
		compiled.Artifacts[0].Payload.PlainText() != "canonical knowledge" {
		t.Fatalf("compiled=%+v err=%v", compiled, err)
	}
	// Act: source becomes stale while index remains unchanged.
	source.Revision = "r2"
	if err = sources.Put(scope, source); err != nil {
		t.Fatal(err)
	}
	stale, err := recall()
	// Assert: stale source must not publish; error is allowed/expected fail-closed behavior.
	if err == nil && len(stale.Projections) != 0 {
		t.Fatalf("stale source published: %+v", stale)
	}
	if err != nil && !errors.Is(err, memy.ErrStaleInput) && !errors.Is(err, memy.ErrSourceUnavailable) {
		t.Fatalf("unexpected source failure: %v", err)
	}
	t.Logf("stale source: projections=%d err=%v", len(stale.Projections), err)
	source.Revision = "r1"
	if err = sources.Put(scope, source); err != nil {
		t.Fatal(err)
	}
	// Act: forget canonical state, leaving fake search candidates deliberately stale.
	purge, err := engine.Forget(
		ctx,
		"user",
		scope,
		"assist",
		memy.ForgetRequest{
			OperationID:   "forget",
			Selector:      memy.Selector{Kind: memy.SelectRecord, ID: "record"},
			Expected:      []memy.RevisionRef{{RecordID: "record", Revision: receipt.Revision}},
			Reason:        "fixture",
			PolicyVersion: "delete/v1",
			Limit:         100,
			MaxBytes:      1048576,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	after, err := recall()
	// Assert: revoked canonical record never restored by stale index candidate.
	if err != nil || len(after.Projections) != 0 || after.Progress.CanonicalFiltered != 2 || !purge.CanonicalComplete {
		t.Fatalf("after=%+v purge=%+v err=%v", after, purge, err)
	}
}

func TestPersistentCompiledContextManagedForget(t *testing.T) {
	// Arrange: the real consumer stores a compiled artifact whose text is the full
	// canonical projection envelope, rather than an untracked payload-only copy.
	ctx := t.Context()
	session, err := host.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	scope := memy.Scope{Tenant: "tenant", Namespace: "knowledge", Subject: "user"}
	if err = session.Provision(scope); err != nil {
		t.Fatal(err)
	}
	ref, err := session.Seed(ctx, scope, "record", "canonical knowledge")
	if err != nil {
		t.Fatal(err)
	}
	fence, err := session.Engine.Fence(ctx, "reader", scope, host.Purpose)
	if err != nil {
		t.Fatal(err)
	}
	search := adapter{
		docs: []retrieval.Document[meta]{
			{
				ID:      ref.RecordID,
				Content: "hostile index payload",
				Rank:    5,
				Meta:    meta{Scope: scope, Revision: ref.Revision},
			},
		},
	}
	result, err := memy.RecallProjected(
		ctx,
		session.Engine,
		"reader",
		scope,
		"query",
		search,
		memy.ScoreRanker[string, string]{},
		memy.RecallOptions{
			Read:   memy.ReadOptions{Purpose: host.Purpose},
			Search: memy.SearchOptions{MaxCandidates: 10},
			Limit:  10,
		},
		reference.ProjectorFunc[string, string, string]{
			PolicyVersion: "projection",
			Apply:         func(ctx context.Context, r memy.Record[string, string]) (string, error) { return r.Payload, ctx.Err() },
		},
		nil,
	)
	if err != nil || len(result.Projections) != 1 {
		t.Fatalf("recall=%+v err=%v", result, err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	artifact := contexty.NewMemoryBlock(scope.Key()+"/record/checkpoint", contexty.TextPayload(string(raw)))
	materializer := contexty.NewEngine(
		contexty.WithArtifactMaterialization(
			contexty.ArtifactMaterializationPolicy{
				Identity: contexty.Descriptor{ID: "host-data", Revision: "1"},
				Materialize: func(ctx context.Context, a contexty.ContextArtifact) (contexty.ArtifactRepresentation, error) {
					return contexty.ArtifactRepresentation{
						Role:  contexty.RoleUser,
						Parts: []contexty.ContentPart{contexty.TextPart{Text: a.Payload.PlainText()}},
					}, ctx.Err()
				},
			},
		),
	)
	compiled, err := materializer.CompileSnapshot(
		ctx,
		contexty.CompileRequest{
			CompilationID: "managed",
			TurnID:        "turn",
			Artifacts:     []contexty.ContextArtifact{artifact.ContextArtifact},
		},
	)
	if err != nil || len(compiled.Artifacts) != 1 {
		t.Fatalf("compile=%+v err=%v", compiled, err)
	}
	// Explicit host serialization retains evidence in payload, not json-ignored extensions.
	codec := contexty.ConversationCodec{}
	snapshot, err := codec.Encode(contexty.EmptyState().WithArtifact(compiled.Artifacts[0]))
	if err != nil {
		t.Fatal(err)
	}
	var key string
	err = session.Engine.WithDerivedWrite(
		ctx,
		"reader",
		fence,
		host.Purpose,
		[]memy.RevisionRef{ref},
		func(ctx context.Context) error {
			var writeErr error
			key, writeErr = session.Checkpoints.Put(
				ctx,
				fence,
				checkpoint.Artifact{
					Scope:   scope,
					Handle:  "context-checkpoint",
					Lineage: []memy.RevisionRef{ref},
					Data:    snapshot,
				},
			)
			return writeErr
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	// Act: restore through canonical validation, then revoke with a failing sink.
	restored, err := checkpoint.ReadValidated(
		ctx,
		session.Checkpoints,
		session.Engine,
		"reader",
		scope,
		host.Purpose,
		key,
	)
	if err != nil {
		t.Fatal(err)
	}
	assertRestoredProjection(t, codec, restored)
	session.Checkpoints.FailPurge(errors.New("host storage offline"))
	receipt, err := session.Forget(ctx, scope, ref)
	// Assert: persistent compiled output cannot bypass revoke; sink retry is durable.
	if err != nil || receipt.State == memy.PurgeComplete {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	if data, readErr := checkpoint.ReadValidated(
		ctx,
		session.Checkpoints,
		session.Engine,
		"reader",
		scope,
		host.Purpose,
		key,
	); readErr == nil ||
		len(data) != 0 {
		t.Fatalf("revoked restored data=%q err=%v", data, readErr)
	}
	session.Checkpoints.FailPurge(nil)
	receipt, err = session.Forget(ctx, scope, ref)
	if err != nil || receipt.State != memy.PurgeComplete {
		t.Fatalf("retry=%+v err=%v", receipt, err)
	}
}

func assertRestoredProjection(t *testing.T, codec contexty.ConversationCodec, restored []byte) {
	t.Helper()
	state, err := codec.Decode(restored)
	if err != nil || len(state.Artifacts()) != 1 {
		t.Fatalf("restored state=%+v err=%v", state, err)
	}
	loaded := state.Artifacts()[0]
	var envelope memy.ProjectedRecallResult[string, string]
	if err = json.Unmarshal([]byte(loaded.Payload.PlainText()), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Projections[0].Output != "canonical knowledge" ||
		envelope.Projections[0].Retrieval.Signals[0].Score.Present ||
		envelope.Projections[0].Retrieval.Signals[0].Rank != 5 ||
		envelope.Projections[0].Trust != "data" {
		t.Fatalf("restored evidence=%+v", envelope)
	}
}
