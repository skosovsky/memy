package memy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

func TestMixedSearchPreservesCoverageAndRejectsStrongerProfile(t *testing.T) {
	// Arrange.
	f, index, summary := withSinks(t)
	receipt, _ := staged(t, f, index, summary)
	if err := index.Acknowledge(context.Background(), receipt.Visibility); err != nil {
		t.Fatal(err)
	}
	eventual := reference.NewIndex[string]("eventual", nil)
	mixed := reference.Composite[string]{
		Backends: []reference.Backend[string]{
			{ID: "index/v1", Search: index},
			{ID: "eventual", Search: reference.Eventual[string]{Index: eventual}},
		},
		AllowDegraded: true,
		RRF:           reference.RRFConfig{K: 60},
	}
	// Act.
	recalled, operationErr := memy.Recall(
		context.Background(),
		f.engine,
		f.actor,
		f.scope,
		"timezone",
		mixed,
		memy.ScoreRanker[preference, sourceRef]{},
		memy.RecallOptions{Search: memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates}, Limit: 5},
	)
	_, strongErr := memy.Recall(
		context.Background(),
		f.engine,
		f.actor,
		f.scope,
		"timezone",
		mixed,
		memy.ScoreRanker[preference, sourceRef]{},
		memy.RecallOptions{
			Limit:  5,
			Search: memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates, Minimum: &receipt.Visibility},
		},
	)
	// Assert: eventual coverage remains explicit and rejects stronger visibility.
	if operationErr != nil || len(recalled.Records) != 1 || len(recalled.Coverage) != 2 ||
		recalled.Coverage[0].Backend != "eventual" || recalled.Coverage[0].Status != "eventual" ||
		recalled.Coverage[1].Backend != "index/v1" || recalled.Coverage[1].Status != "eventual" ||
		!errors.Is(strongErr, memy.ErrUnsupported) {
		t.Fatalf("recall=%+v err=%v strong=%v", recalled, operationErr, strongErr)
	}
}

func TestCompositePartialFailureRequiresExplicitDegradedMode(t *testing.T) {
	// Arrange.
	f, index, summary := withSinks(t)
	receipt, _ := staged(t, f, index, summary)
	if err := index.Acknowledge(context.Background(), receipt.Visibility); err != nil {
		t.Fatal(err)
	}
	failed := reference.NewIndex[string]("failed", nil)
	failed.Fail(errors.New("outage"))
	mixed := reference.Composite[string]{
		Backends:      []reference.Backend[string]{{ID: "index/v1", Search: index}, {ID: "failed", Search: failed}},
		AllowDegraded: false,
		RRF:           reference.RRFConfig{K: 60},
	}
	// Act.
	_, strictErr := memy.Recall(
		context.Background(),
		f.engine,
		f.actor,
		f.scope,
		"timezone",
		mixed,
		memy.ScoreRanker[preference, sourceRef]{},
		memy.RecallOptions{Search: memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates}, Limit: 1},
	)
	mixed.AllowDegraded = true
	partial, partialErr := memy.Recall(
		context.Background(),
		f.engine,
		f.actor,
		f.scope,
		"timezone",
		mixed,
		memy.ScoreRanker[preference, sourceRef]{},
		memy.RecallOptions{Search: memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates}, Limit: 1},
	)
	index.Fail(errors.New("another outage"))
	_, allErr := memy.Recall(
		context.Background(),
		f.engine,
		f.actor,
		f.scope,
		"timezone",
		mixed,
		memy.ScoreRanker[preference, sourceRef]{},
		memy.RecallOptions{Search: memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates}, Limit: 1},
	)
	// Assert.
	if !errors.Is(strictErr, memy.ErrUnavailable) || partialErr != nil ||
		len(partial.Records) != 1 ||
		len(partial.Coverage) != 2 ||
		partial.Coverage[0].Backend != "failed" || partial.Coverage[0].Status != "unavailable" ||
		partial.Coverage[1].Backend != "index/v1" || partial.Coverage[1].Status != "eventual" ||
		!errors.Is(allErr, memy.ErrUnavailable) {
		t.Fatalf("strict=%v partial=%+v %v all=%v", strictErr, partial, partialErr, allErr)
	}
}

func TestCompositeRejectsTypedNilBackend(t *testing.T) {
	// Arrange: the interface itself is nonnil but carries a missing adapter.
	f := newFixture(t, nil)
	var absent *reference.Index[string]
	composite := reference.Composite[string]{
		Backends: []reference.Backend[string]{{ID: "absent", Search: absent}},
		RRF:      reference.RRFConfig{K: 60},
	}
	// Act.
	coverage := composite.Capabilities()
	result, err := memy.Recall(
		context.Background(),
		f.engine,
		f.actor,
		f.scope,
		"timezone",
		composite,
		memy.ScoreRanker[preference, sourceRef]{},
		memy.RecallOptions{Search: memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates}, Limit: 1},
	)
	// Assert: missing backend is unsupported, never a panic or successful absence.
	if coverage.Scoped || !errors.Is(err, memy.ErrUnsupported) || len(result.Records) != 0 {
		t.Fatalf("capabilities=%+v result=%+v err=%v", coverage, result, err)
	}
}
