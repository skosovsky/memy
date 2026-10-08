//go:build integration

package compatibility_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/skosovsky/contexty"
	"github.com/skosovsky/ragy/retrieval"
	memorytool "github.com/skosovsky/toolsy/toolkits/memory"
	ragtool "github.com/skosovsky/toolsy/toolkits/rag"

	"github.com/skosovsky/memy"
)

func TestIntegrationRagyToMemyScopedBoundedCancellation(t *testing.T) {
	// Arrange.
	s := memy.Scope{Tenant: "t", Namespace: "n", Subject: "s"}
	a := adapter{
		[]retrieval.Document[meta]{
			{ID: "b", Rank: 2, Meta: meta{s, 2}},
			{ID: "a", Rank: 1, Meta: meta{s, 7}},
			{ID: "foreign", Meta: meta{memy.Scope{Tenant: "other"}, 1}},
		},
	}
	// Act.
	out, err := a.Search(t.Context(), s, "q", memy.SearchOptions{MaxCandidates: 1})
	// Assert.
	if err != nil || len(out.Candidates) != 1 || out.Candidates[0].RecordID != "b" || out.Candidates[0].Revision != 2 ||
		out.Candidates[0].Signals[0].Score.Present ||
		!out.CandidatesTruncated {
		t.Fatalf("%+v %v", out, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = a.Search(ctx, s, "q", memy.SearchOptions{MaxCandidates: 1})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestIntegrationRagyArtifactToContexty(t *testing.T) {
	// Arrange.
	rs := retrieval.NewResultSet(
		[]retrieval.Document[meta]{{ID: "doc", Content: "quoted data", Rank: 3}},
		retrieval.DocumentIDResolver[meta]{},
	)
	// Act.
	r, err := (retrieval.DefaultArtifactRenderer[meta]{}).Render(
		t.Context(),
		retrieval.UnrestrictedRead(),
		rs,
		retrieval.ArtifactRenderOptions[meta]{
			Resource:  retrieval.RuneResource(1000),
			CloneMeta: func(m meta) (meta, error) { return m, nil },
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	a := contexty.NewRetrievalDocument("tenant/doc@1", contexty.TextPayload(r.RenderedText)).WithTurn("turn")
	a.SourceRefs = []contexty.SourceRef{{Namespace: "tenant", ID: "doc", CheckpointID: "1"}}
	n, _, err := (contexty.CompileRequest{TurnID: "turn", Artifacts: []contexty.ContextArtifact{a}}).Normalize()
	// Assert.
	if err != nil || len(n.Artifacts) != 1 || n.Artifacts[0].Payload.PlainText() != r.RenderedText ||
		r.Snippets[0].Rank != 3 {
		t.Fatalf("%+v %v", n, err)
	}
	n.Artifacts[0].SourceRefs[0].ID = "changed"
	if a.SourceRefs[0].ID != "doc" {
		t.Fatal("alias")
	}
}
func TestIntegrationMemyProjectionEnvelopeToContexty(t *testing.T) {
	// Arrange: already authorized DTO fixture, not a grant.
	p := memy.Projection[string, string]{
		Output:                 "knowledge",
		Scope:                  memy.Scope{Tenant: "t", Namespace: "n", Subject: "s"},
		RecordID:               "same",
		Revision:               9,
		Trust:                  "untrusted",
		AuthorityPolicyVersion: "v1",
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	a := contexty.NewMemoryBlock(p.Scope.Key()+"/same@9", contexty.JSONPayload(string(raw))).
		WithPersistence(contexty.ArtifactPersistenceSkip)
	a.Lifecycle = contexty.ArtifactLifecycleEphemeral
	state := (contexty.ConversationState{}).WithArtifact(a)
	detached := state.Artifacts()
	detached[0].Payload.Data[0] = 'x'
	var actual memy.Projection[string, string]
	err = json.Unmarshal([]byte(state.Artifacts()[0].Payload.PlainText()), &actual)
	// Assert.
	if err != nil || actual.RecordID != p.RecordID || actual.Revision != 9 || actual.Scope != p.Scope ||
		actual.Trust != "untrusted" {
		t.Fatalf("%+v %v", actual, err)
	}
}
func TestIntegrationToolkitsCompileSameGraph(t *testing.T) {
	// Arrange.
	a := toolAdapter{}
	// Act.
	s, err := ragtool.AsSearchTool(a)
	if err != nil {
		t.Fatal(err)
	}
	m, err := memorytool.NewScratchpad()
	if err != nil {
		t.Fatal(err)
	}
	tools, err := m.AsTools()
	// Assert.
	if err != nil || s == nil || len(tools) != 3 {
		t.Fatalf("%v %d", err, len(tools))
	}
}
func TestIntegrationContextyCompileHostMaterialization(t *testing.T) {
	// Arrange: role/trust are explicit host policy, not inferred from knowledge.
	a := contexty.NewRetrievalDocument("scoped-id", contexty.TextPayload("untrusted quote")).WithTurn("t1")
	e := contexty.NewEngine(
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
	// Act.
	out, err := e.CompileSnapshot(
		t.Context(),
		contexty.CompileRequest{CompilationID: "c1", TurnID: "t1", Artifacts: []contexty.ContextArtifact{a}},
	)
	// Assert.
	if err != nil || len(out.Artifacts) != 1 {
		t.Fatalf("artifacts=%d err=%v", len(out.Artifacts), err)
	}
}
func TestIntegrationContextyRemoveHostMappedMemoryArtifact(t *testing.T) {
	// Arrange: host maps memy deletion handle to owned context artifacts.
	s := memy.Scope{Tenant: "t", Namespace: "n", Subject: "s"}
	batch := memy.PurgeBatch{Scope: s, Records: []string{"record"}}
	id := batch.Scope.Key() + "/record@9"
	state := contexty.EmptyState().
		WithArtifact(contexty.NewMemoryBlock(id, contexty.TextPayload("secret")).ContextArtifact)
	// Act.
	next, err := contexty.ApplyDelta(
		state,
		contexty.ConversationDelta{Operation: contexty.DeltaRemoveArtifact, ArtifactIDs: []string{id}},
	)
	// Assert: this is local deletion only, not a durable multi-store purge receipt.
	if err != nil || len(next.Artifacts()) != 0 || len(state.Artifacts()) != 1 {
		t.Fatalf("%v", err)
	}
}
