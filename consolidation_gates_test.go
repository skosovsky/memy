package memy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

func TestConsolidationQualityGatesPreserveOriginalState(t *testing.T) {
	for _, gate := range []string{"utility", "empty_output", "missing_evidence", "input_bytes", "output_bytes"} {
		t.Run(gate, func(t *testing.T) { consolidationGateCase(t, gate) })
	}
}

func consolidationGateCase(t *testing.T, gate string) {
	t.Helper()
	// Arrange: a real scoped corpus and a provider that can fail one declared gate.
	f := newFixture(t, nil)
	refs := seedCorpus(t, f, readCorpus(t))
	request := consolidationRequest(refs, memy.DomainMerge)
	if gate == "input_bytes" {
		request.Budget.InputBytes = 1
	}
	if gate == "output_bytes" {
		request.Budget.OutputBytes = 1
	}
	called := false
	provider := reference.MergeFunc[preference, sourceRef](
		func(_ context.Context, inputs []memy.Record[preference, sourceRef], _ memy.Budget) (memy.MergeResult[preference, sourceRef], error) {
			called = true
			return gatedMerge(inputs[0], gate), nil
		},
	)
	// Act.
	proposals, consolidationErr := memy.Consolidate(context.Background(), f.engine, f.actor, f.scope, request, provider)
	state, stateErr := f.engine.Snapshot(context.Background(), f.actor, f.scope, memy.ReadOptions{})
	// Assert: failed utility/evidence/budget cannot publish a derived record or erase originals.
	if !errors.Is(consolidationErr, gateError(gate)) || len(proposals) != 0 || stateErr != nil ||
		len(state) != len(refs) {
		t.Fatalf("gate=%s proposals=%+v err=%v state=%+v err=%v", gate, proposals, consolidationErr, state, stateErr)
	}
	if gate == "input_bytes" && called {
		t.Fatal("input byte budget did not protect provider invocation")
	}
	for _, ref := range refs {
		record, readErr := f.engine.Get(context.Background(), f.actor, f.scope, ref.RecordID, memy.ReadOptions{})
		if readErr != nil || record.Revision != ref.Revision || record.State != memy.Active {
			t.Fatalf("original=%+v err=%v", record, readErr)
		}
	}
}

func gatedMerge(input memy.Record[preference, sourceRef], gate string) memy.MergeResult[preference, sourceRef] {
	result := memy.MergeResult[preference, sourceRef]{
		Utility: 1,
		Suggestions: []memy.Suggestion[preference, sourceRef]{
			{Payload: input.Payload, Valid: input.Valid, Evidence: "typed merge"},
		},
	}
	switch gate {
	case "utility":
		result.Utility = 0
	case "empty_output":
		result.Suggestions = nil
	case "missing_evidence":
		result.Suggestions[0].Evidence = ""
	}
	return result
}

func gateError(gate string) error {
	switch gate {
	case "utility":
		return memy.ErrPolicyDenied
	case "empty_output", "missing_evidence":
		return memy.ErrMissingEvidence
	case "input_bytes", "output_bytes":
		return memy.ErrBudget
	}
	return memy.ErrInvalid
}

func TestSourceOutageCannotBecomeSuccessfulCanonicalCommit(t *testing.T) {
	// Arrange: review succeeds before the mandatory evidence registry fails.
	f := newFixture(t, nil)
	proposal := f.propose(t, "remember", "private fact", memy.Interval{})
	request := f.acceptedRequest(t, "commit", "timezone", 0, proposal, memy.Append)
	failure := errors.New("source registry unavailable")
	f.sources.Fail(failure)
	// Act.
	_, commitErr := f.engine.Commit(context.Background(), f.actor, f.scope, "assist", request)
	_, readErr := f.engine.Get(context.Background(), f.actor, f.scope, "timezone", memy.ReadOptions{})
	// Assert: an unknown source is an explicit failure, not accepted evidence or an empty success.
	if !errors.Is(commitErr, memy.ErrSourceUnavailable) || !errors.Is(commitErr, failure) ||
		!errors.Is(readErr, memy.ErrNotFound) {
		t.Fatalf("commit=%v read=%v", commitErr, readErr)
	}
}
