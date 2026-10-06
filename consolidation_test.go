package memy_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

type corpusRecord struct {
	ID     string    `json:"id"`
	Tenant string    `json:"tenant"`
	Key    string    `json:"key"`
	Value  string    `json:"value"`
	Known  bool      `json:"known"`
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
}
type corpus struct {
	Version     string         `json:"version"`
	Provider    string         `json:"provider"`
	Resolver    string         `json:"resolver"`
	Projection  string         `json:"projection"`
	Records     []corpusRecord `json:"records"`
	Expected    []string       `json:"expected_unique_A"`
	BadSemantic string         `json:"scripted_bad_semantic"`
	AutoApply   bool           `json:"auto_apply"`
}

func readCorpus(t *testing.T) corpus {
	t.Helper()
	raw, operationErr := os.ReadFile("testdata/consolidation-v1.json")
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	var c corpus
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func seedCorpus(t *testing.T, f fixture, c corpus) []memy.RevisionRef {
	t.Helper()
	refs := make([]memy.RevisionRef, 0)
	for _, record := range c.Records {
		if record.Tenant != "A" {
			continue
		}
		suggestion := f.suggestion(record.Value, memy.Interval{Known: record.Known, From: record.From, To: record.To})
		suggestion.Payload.Key = record.Key
		p, err := f.engine.Remember(context.Background(), f.actor, f.scope, "remember-"+record.ID, "assist", suggestion)
		if err != nil {
			t.Fatal(err)
		}
		r := f.commit(t, f.acceptedRequest(t, "commit-"+record.ID, record.ID, 0, p, memy.Append))
		refs = append(refs, memy.RevisionRef{RecordID: r.RecordID, Revision: r.Revision})
	}
	return refs
}

func consolidationRequest(refs []memy.RevisionRef, mode memy.ConsolidationMode) memy.ConsolidationRequest {
	return memy.ConsolidationRequest{
		OperationID:    "consolidate-" + string(mode),
		Purpose:        "assist",
		PolicyVersion:  "consolidation/v1",
		Mode:           mode,
		Inputs:         refs,
		Budget:         memy.Budget{InputBytes: 10000, OutputBytes: 10000, CostUnits: 5},
		MinimumUtility: 0.1,
	}
}

func TestExactDedupPreservesNegationValidityLineageAndOriginals(t *testing.T) {
	// Arrange: duplicate, negative exception, and another scope in versioned corpus.
	f := newFixture(t, nil)
	c := readCorpus(t)
	refs := seedCorpus(t, f, c)
	before, operationErr := fullSnapshot(context.Background(), f.engine, f.actor, f.scope, memy.ReadOptions{})
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	// Act.
	proposals, operationErr := memy.Consolidate(
		context.Background(),
		f.engine,
		f.actor,
		f.scope,
		consolidationRequest(refs, memy.ExactDedup),
		nil,
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	after, operationErr := fullSnapshot(context.Background(), f.engine, f.actor, f.scope, memy.ReadOptions{})
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	// Assert: one dedup proposal leaves the independent negative claim untouched.
	if len(proposals) != 1 || len(before) != 3 || len(after) != 3 || c.AutoApply {
		t.Fatalf("proposals=%d before=%d after=%d", len(proposals), len(before), len(after))
	}
	p := proposals[0]
	if p.State != memy.Proposed || p.Scope != f.scope || p.Suggestion.Payload.Value != "sometimes prefers tea" ||
		len(p.Suggestion.Lineage) != 2 ||
		p.Suggestion.Valid.To != c.Records[0].To {
		t.Fatalf("dedup lost constraints: %+v", p)
	}
	negative, operationErr := f.engine.Get(context.Background(), f.actor, f.scope, "tea-evening", memy.ReadOptions{})
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	actual := []string{p.Suggestion.Payload.Value, negative.Payload.Value}
	slices.Sort(actual)
	expected := slices.Clone(c.Expected)
	slices.Sort(expected)
	if !slices.Equal(actual, expected) {
		t.Fatalf("mandatory records lost: %v want %v", actual, expected)
	}
	// Foreign-scope input cannot be absorbed into a broader summary.
	foreign := append(slices.Clone(refs), memy.RevisionRef{RecordID: "tea-other-scope", Revision: 1})
	foreignRequest := consolidationRequest(foreign, memy.ExactDedup)
	foreignRequest.OperationID = "foreign-input"
	_, operationErr = memy.Consolidate(context.Background(), f.engine, f.actor, f.scope, foreignRequest, nil)
	if !errors.Is(operationErr, memy.ErrStaleInput) {
		t.Fatalf("foreign input accepted: %v", operationErr)
	}
}

func TestSemanticProposalReviewAndSourceRevocation(t *testing.T) {
	// Arrange: a scripted bad semantic provider overstates a preference.
	f := newFixture(t, nil)
	c := readCorpus(t)
	refs := seedCorpus(t, f, c)
	provider := reference.MergeFunc[preference, sourceRef](
		func(_ context.Context, _ []memy.Record[preference, sourceRef], _ memy.Budget) (memy.MergeResult[preference, sourceRef], error) {
			return memy.MergeResult[preference, sourceRef]{
				Suggestions: []memy.Suggestion[preference, sourceRef]{
					{
						Payload:       preference{Key: "drink", Value: c.BadSemantic},
						Evidence:      "scripted unsupported generalization",
						Losses:        []string{"frequency qualifier", "evening negation"},
						Uncertainties: []string{"overgeneralization"},
					},
				},
				CostUnits: 1,
				Utility:   0.5,
			}, nil
		},
	)
	// Act.
	proposals, operationErr := memy.Consolidate(
		context.Background(),
		f.engine,
		f.actor,
		f.scope,
		consolidationRequest(refs, memy.SemanticMerge),
		provider,
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	state, operationErr := fullSnapshot(context.Background(), f.engine, f.actor, f.scope, memy.ReadOptions{})
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	_, operationErr = fullForget(context.Background(),
		f.engine,
		f.actor,
		f.scope,
		"assist",
		memy.ForgetRequest{
			OperationID:   "forget-source",
			Selector:      memy.Selector{Kind: memy.SelectRecord, ID: refs[0].RecordID},
			Reason:        "source withdrawn",
			PolicyVersion: "deletion/v1",
		},
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	_, acceptErr := f.engine.Accept(
		context.Background(),
		f.actor,
		f.scope,
		proposals[0].ID,
		proposals[0].Digest,
		proposals[0].Revision,
		"assist",
	)
	// Assert: inaccurate output is proposed, originals remain, and withdrawn input
	// cannot be reviewed into fresh canonical knowledge.
	if len(state) != 3 || len(proposals) != 1 || proposals[0].State != memy.Proposed ||
		len(proposals[0].Suggestion.Losses) != 2 {
		t.Fatalf("semantic bypassed review: %+v", proposals)
	}
	if acceptErr == nil {
		t.Fatal("withdrawn source accepted")
	}
}

func TestDomainMergeAndBudgetFailureAreNonDestructive(t *testing.T) {
	// Arrange.
	f := newFixture(t, nil)
	refs := seedCorpus(t, f, readCorpus(t))
	provider := reference.MergeFunc[preference, sourceRef](
		func(_ context.Context, inputs []memy.Record[preference, sourceRef], _ memy.Budget) (memy.MergeResult[preference, sourceRef], error) {
			return memy.MergeResult[preference, sourceRef]{
				Suggestions: []memy.Suggestion[preference, sourceRef]{
					{Payload: inputs[0].Payload, Valid: inputs[0].Valid, Evidence: "typed domain merge"},
				},
				CostUnits: 1,
				Utility:   1,
			}, nil
		},
	)
	request := consolidationRequest(refs, memy.DomainMerge)
	// Act.
	proposals, operationErr := memy.Consolidate(context.Background(), f.engine, f.actor, f.scope, request, provider)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	request.OperationID = "too-expensive"
	request.Budget.CostUnits = 0
	_, budgetErr := memy.Consolidate(context.Background(), f.engine, f.actor, f.scope, request, provider)
	state, operationErr := fullSnapshot(context.Background(), f.engine, f.actor, f.scope, memy.ReadOptions{})
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	// Assert.
	if len(proposals) != 1 || len(proposals[0].Suggestion.Lineage) != 3 || !errors.Is(budgetErr, memy.ErrBudget) ||
		len(state) != 3 {
		t.Fatalf("proposals=%+v budget=%v state=%d", proposals, budgetErr, len(state))
	}
}

func TestRetentionUsesManagedPurgeAndResumesPending(t *testing.T) {
	// Arrange.
	f, index, summary := withSinks(t)
	f.config.Retention = reference.Retain[preference]{Version: "retention/v1", ExpiresAt: f.clock.Now().Add(time.Hour)}
	var operationErr error
	f.engine, operationErr = memy.New(f.config)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	_, _ = staged(t, f, index, summary)
	summary.FailPurge(errors.New("summary down"))
	f.clock.Advance(2 * time.Hour)
	// Act.
	first, operationErr := fullSweep(context.Background(), f.engine, f.actor, f.scope, "assist", "expiry-run")
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	_, readErr := f.engine.Get(context.Background(), f.actor, f.scope, "timezone", memy.ReadOptions{})
	summary.FailPurge(nil)
	retried, operationErr := fullSweep(context.Background(), f.engine, f.actor, f.scope, "assist", "expiry-run")
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	// Assert.
	if len(first.Records) != 1 || first.Records[0].State != memy.PurgePending ||
		!errors.Is(readErr, memy.ErrNotFound) ||
		len(retried.Records) != 1 ||
		retried.Records[0].State != memy.PurgeComplete ||
		summary.Contains("summary-1") {
		t.Fatalf("first=%+v retry=%+v read=%v", first, retried, readErr)
	}
}

func TestReintroductionNeedsExplicitHostPolicy(t *testing.T) {
	// Arrange.
	f := newFixture(t, nil)
	_, operationErr := fullForget(context.Background(),
		f.engine,
		f.actor,
		f.scope,
		"assist",
		memy.ForgetRequest{
			OperationID:   "forget",
			Selector:      memy.Selector{Kind: memy.SelectScope},
			Reason:        "reset",
			PolicyVersion: "deletion/v1",
		},
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	request := memy.ReintroductionRequest{
		OperationID:   "reintroduce",
		Selector:      memy.Selector{Kind: memy.SelectScope},
		ExpectedEpoch: 1,
		PolicyVersion: "reintroduction/v1",
	}
	// Act.
	_, denied := f.engine.Remember(
		context.Background(),
		f.actor,
		f.scope,
		"new",
		"assist",
		f.suggestion("new fact", memy.Interval{}),
	)
	_, unsupported := f.engine.Reintroduce(context.Background(), f.actor, f.scope, "assist", request)
	f.config.Reintroduction = reference.ReintroductionFunc[principal](
		func(_ context.Context, _ principal, _ memy.Scope, request memy.ReintroductionRequest) error {
			if request.PolicyVersion != "reintroduction/v1" {
				return memy.ErrPolicyDenied
			}
			return nil
		},
	)
	f.engine, operationErr = memy.New(f.config)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	fence, operationErr := f.engine.Reintroduce(context.Background(), f.actor, f.scope, "assist", request)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	retry, operationErr := f.engine.Reintroduce(context.Background(), f.actor, f.scope, "assist", request)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	newFact := f.propose(t, "new", "new fact", memy.Interval{})
	// Assert.
	if !errors.Is(denied, memy.ErrRevoked) || !errors.Is(unsupported, memy.ErrUnsupported) || fence.Epoch != 2 ||
		retry != fence ||
		newFact.Epoch != 2 {
		t.Fatalf("denied=%v unsupported=%v fence=%+v", denied, unsupported, fence)
	}
}

func TestConsolidationCannotExtendFiniteEvidenceExpiry(t *testing.T) {
	// Arrange: host retention is unlimited, but both source proposals expire.
	f := newFixture(t, nil)
	deadline := f.clock.Now().Add(time.Hour)
	var refs []memy.RevisionRef
	for _, id := range []string{"first", "second"} {
		suggestion := f.suggestion("same fact", memy.Interval{})
		suggestion.ExpiresAt = deadline
		proposal, rememberErr := f.engine.Remember(
			context.Background(),
			f.actor,
			f.scope,
			"remember-"+id,
			"assist",
			suggestion,
		)
		if rememberErr != nil {
			t.Fatal(rememberErr)
		}
		receipt := f.commit(t, f.acceptedRequest(t, "commit-"+id, id, 0, proposal, memy.Append))
		refs = append(refs, memy.RevisionRef{RecordID: receipt.RecordID, Revision: receipt.Revision})
	}
	// Act: exact dedup must carry the original finite deadline into its proposal.
	proposals, consolidationErr := memy.Consolidate(context.Background(), f.engine, f.actor, f.scope,
		consolidationRequest(refs, memy.ExactDedup), nil)
	// Assert: annotations cannot promise data beyond its evidence lifetime.
	if consolidationErr != nil || len(proposals) != 1 || !proposals[0].Suggestion.ExpiresAt.Equal(deadline) {
		t.Fatalf("proposals=%+v err=%v", proposals, consolidationErr)
	}
}

func TestReviewedSemanticSummaryCommitsAndRetainsOriginals(t *testing.T) {
	// Arrange: a consumer supplies a bounded semantic summary and its evaluation.
	f := newFixture(t, nil)
	corpus := readCorpus(t)
	refs := seedCorpus(t, f, corpus)
	const summary = "sometimes prefers tea; never drinks tea in the evening"
	provider := reference.MergeFunc[preference, sourceRef](
		func(_ context.Context, inputs []memy.Record[preference, sourceRef], _ memy.Budget) (memy.MergeResult[preference, sourceRef], error) {
			return memy.MergeResult[preference, sourceRef]{
				Suggestions: []memy.Suggestion[preference, sourceRef]{
					{Payload: preference{Key: "drink-summary", Value: summary}, Valid: inputs[0].Valid,
						Evidence:      "host evaluated preservation of frequency, evening negation and interval",
						Uncertainties: []string{"observations do not establish a universal preference"}},
				},
				CostUnits: 1, Utility: 1,
			}, nil
		},
	)
	// Act: semantic output remains a proposal until an independent host decision.
	proposals, consolidationErr := memy.Consolidate(context.Background(), f.engine, f.actor, f.scope,
		consolidationRequest(refs, memy.SemanticMerge), provider)
	if consolidationErr != nil || len(proposals) != 1 {
		t.Fatalf("proposals=%+v err=%v", proposals, consolidationErr)
	}
	before, beforeErr := fullSnapshot(context.Background(), f.engine, f.actor, f.scope, memy.ReadOptions{})
	if beforeErr != nil || len(before) != len(refs) {
		t.Fatalf("semantic provider changed canonical state: %v %v", before, beforeErr)
	}
	receipt := f.commit(t, f.acceptedRequest(t, "commit-summary", "drink-summary", 0, proposals[0], memy.Append))
	record, readErr := f.engine.Get(context.Background(), f.actor, f.scope, receipt.RecordID, memy.ReadOptions{})
	after, afterErr := fullSnapshot(context.Background(), f.engine, f.actor, f.scope, memy.ReadOptions{})
	// Assert: review applies a new derived record; source identities survive intact.
	if readErr != nil || afterErr != nil || record.Payload.Value != summary || len(after) != len(refs)+1 ||
		!slices.Equal(record.Provenance.Lineage, refs) || record.Valid.To != corpus.Records[0].To ||
		len(record.Provenance.Uncertainties) != 1 {
		t.Fatalf("record=%+v err=%v state=%v err=%v", record, readErr, after, afterErr)
	}
	for _, ref := range refs {
		original, originalErr := f.engine.Get(context.Background(), f.actor, f.scope, ref.RecordID, memy.ReadOptions{})
		if originalErr != nil || original.Revision != ref.Revision || original.State != memy.Active {
			t.Fatalf("original lost: %+v %v", original, originalErr)
		}
	}
}
