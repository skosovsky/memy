package conformance

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/skosovsky/memy"
)

// SearchFixture provisions isolated adapters and query-matching metadata.
// Seed must stage a revision and its native producer score; composition may
// compute a different ranking score. Publish makes its visibility token observable.
type SearchFixture[Q any] struct {
	Adapter memy.Search[Q]
	Query   Q
	Seed    func(context.Context, memy.Scope, memy.Candidate) error
	Publish func(context.Context, memy.VisibilityToken) error
	Fail    func(error)
}

// SearchSuite tests scoped retrieval, minimum visibility and candidate-return bounds.
func SearchSuite[Q any](t *testing.T, factory func(*testing.T) SearchFixture[Q]) {
	t.Helper()
	for i, foreign := range foreignScopes(scope()) {
		t.Run(fmt.Sprintf("scope_coverage/%d", i), func(t *testing.T) { searchScopeCoverage(t, factory, foreign) })
	}
	t.Run("exact_visibility_bindings", func(t *testing.T) { searchExactVisibility(t, factory) })
	t.Run("minimum_and_cancel", func(t *testing.T) { searchMinimumAndCancel(t, factory) })
	t.Run("candidate_bound", func(t *testing.T) { searchCandidateBound(t, factory) })
	t.Run("score_presence", func(t *testing.T) { searchScorePresence(t, factory) })
	t.Run("failure", func(t *testing.T) { searchFailure(t, factory) })
}

func checkCoverage(t *testing.T, result memy.SearchResult, minimum bool) {
	t.Helper()
	if len(result.Coverage) == 0 {
		t.Fatal("missing coverage")
	}
	for _, coverage := range result.Coverage {
		if coverage.Backend == "" {
			t.Fatal("missing backend identity")
		}
		if minimum {
			if !coverage.MinimumSatisfied {
				t.Fatalf("minimum not satisfied: %+v", coverage)
			}
		}
	}
	for _, candidate := range result.Candidates {
		checkCandidateEvidence(t, candidate, result.Coverage)
	}
}

func searchScopeCoverage[Q any](t *testing.T, factory func(*testing.T) SearchFixture[Q], b memy.Scope) {
	// Arrange: matching metadata in two scopes; B is already published.
	f := factory(t)
	a := scope()
	candidate := memy.Candidate{RecordID: "record", Revision: 1, Score: memy.ScoreOf(1), Signals: nil}
	must(t, f.Seed(t.Context(), a, candidate))
	must(
		t,
		f.Seed(t.Context(), b, memy.Candidate{RecordID: "private", Revision: 1, Score: memy.ScoreOf(1), Signals: nil}),
	)
	must(t, f.Publish(t.Context(), memy.VisibilityToken{Scope: b, RecordID: "private", Revision: 1}))
	if !f.Adapter.Capabilities().Scoped {
		t.Fatal("fixture requires scoped search")
	}
	// Act: staged data exposes index lag separately from candidate results.
	staged, err := f.Adapter.Search(
		t.Context(),
		a,
		f.Query,
		memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates, Minimum: nil},
	)
	must(t, err)
	checkCoverage(t, staged, false)
	for _, coverage := range staged.Coverage {
		if coverage.Status != "pending" && coverage.Status != "eventual" {
			t.Fatalf("staged index lag: %+v", coverage)
		}
	}
	must(t, f.Publish(t.Context(), memy.VisibilityToken{Scope: a, RecordID: candidate.RecordID, Revision: 1}))
	visible, err := f.Adapter.Search(
		t.Context(),
		a,
		f.Query,
		memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates, Minimum: nil},
	)
	// Assert: foreign metadata never enters A results.
	must(t, err)
	checkCoverage(t, visible, false)
	if len(visible.Candidates) != 1 || visible.Candidates[0].RecordID != candidate.RecordID ||
		visible.Candidates[0].Revision != candidate.Revision {
		t.Fatalf("scope result: %+v", visible)
	}
}

func searchMinimumAndCancel[Q any](t *testing.T, factory func(*testing.T) SearchFixture[Q]) {
	// Arrange.
	f := factory(t)
	a := scope()
	candidate := memy.Candidate{RecordID: "record", Revision: 1, Score: memy.ScoreOf(1), Signals: nil}
	must(t, f.Seed(t.Context(), a, candidate))
	token := memy.VisibilityToken{Scope: a, RecordID: candidate.RecordID, Revision: 1}
	ctx, cancel := context.WithTimeout(t.Context(), lockWaitDeadline)
	defer cancel()
	// Act: a minimum cannot be silently satisfied by staged metadata.
	result, err := f.Adapter.Search(
		ctx,
		a,
		f.Query,
		memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates, Minimum: &token},
	)
	// Assert: weaker profiles reject, capable adapters wait then report pending.
	if !f.Adapter.Capabilities().Visibility {
		if !errors.Is(err, memy.ErrUnsupported) {
			t.Fatalf("unsupported minimum: %v", err)
		}
	} else {
		if !errors.Is(err, memy.ErrVisibilityPending) {
			t.Fatalf("pending=%+v err=%v", result, err)
		}
		must(t, f.Publish(t.Context(), token))
		ready, stop := context.WithTimeout(t.Context(), time.Second)
		defer stop()
		result, err = f.Adapter.Search(
			ready,
			a,
			f.Query,
			memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates, Minimum: &token},
		)
		must(t, err)
		checkCoverage(t, result, true)
	}
	canceled, stop := context.WithCancel(t.Context())
	stop()
	_, err = f.Adapter.Search(
		canceled,
		a,
		f.Query,
		memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates, Minimum: nil},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func searchCandidateBound[Q any](t *testing.T, factory func(*testing.T) SearchFixture[Q]) {
	// Arrange: three query-matching, visible exact revisions.
	f := factory(t)
	a := scope()
	for _, id := range []string{"bound-a", "bound-b", "bound-c"} {
		must(t, f.Seed(t.Context(), a, memy.Candidate{RecordID: id, Revision: 1, Score: memy.ScoreOf(1), Signals: nil}))
		must(t, f.Publish(t.Context(), memy.VisibilityToken{Scope: a, RecordID: id, Revision: 1}))
	}
	// Act: request one candidate independently of index availability.
	result, err := f.Adapter.Search(t.Context(), a, f.Query, memy.SearchOptions{MaxCandidates: 1, Minimum: nil})
	// Assert: supporting adapters return a bounded, explicitly truncated set.
	if !f.Adapter.Capabilities().BoundedCandidates {
		if !errors.Is(err, memy.ErrUnsupported) {
			t.Fatalf("unsupported bound: %+v %v", result, err)
		}
		return
	}
	must(t, err)
	checkCoverage(t, result, false)
	if len(result.Candidates) != 1 || !result.CandidatesTruncated {
		t.Fatalf("candidate return bound: %+v", result)
	}
}

func searchFailure[Q any](t *testing.T, factory func(*testing.T) SearchFixture[Q]) {
	// Arrange.
	f := factory(t)
	failure := errors.New("search unavailable")
	f.Fail(failure)
	// Act.
	result, err := f.Adapter.Search(
		t.Context(),
		scope(),
		f.Query,
		memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates, Minimum: nil},
	)
	// Assert: unavailable is an error rather than certified empty coverage.
	if err == nil || !errors.Is(err, failure) {
		t.Fatalf("failure: %+v %v", result, err)
	}
}

func checkCandidateEvidence(t *testing.T, candidate memy.Candidate, coverage []memy.Coverage) {
	t.Helper()
	if len(candidate.Signals) == 0 {
		t.Fatalf("missing search evidence: %+v", candidate)
	}
	for _, signal := range candidate.Signals {
		present := false
		for _, backend := range coverage {
			present = present || backend.Backend == signal.Backend
		}
		if !present || signal.Rank < 1 || signal.Score.Validate() != nil {
			t.Fatalf("invalid search evidence: %+v", signal)
		}
	}
}

func searchExactVisibility[Q any](t *testing.T, factory func(*testing.T) SearchFixture[Q]) {
	// Arrange: publish two exact revisions; revision 3 was never staged.
	f := factory(t)
	if !f.Adapter.Capabilities().Visibility {
		t.Skip("adapter does not support minimum visibility")
	}
	a := scope()
	for _, revision := range []memy.Version{1, 2} {
		must(
			t,
			f.Seed(
				t.Context(),
				a,
				memy.Candidate{
					RecordID: visibilityUpgradeRecord,
					Revision: revision,
					Score:    memy.ScoreOf(1),
					Signals:  nil,
				},
			),
		)
		must(
			t,
			f.Publish(
				t.Context(),
				memy.VisibilityToken{Scope: a, RecordID: visibilityUpgradeRecord, Revision: revision},
			),
		)
	}
	token := memy.VisibilityToken{Scope: a, RecordID: visibilityUpgradeRecord, Revision: 2}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	// Act: exact published revision must accompany its satisfied visibility claim.
	result, err := f.Adapter.Search(
		ctx,
		a,
		f.Query,
		memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates, Minimum: &token},
	)
	must(t, err)
	checkCoverage(t, result, true)
	// Assert.
	requireExactVisibleCandidate(t, result, token)
	// A delayed acknowledgement of revision 1 cannot erase published revision 2.
	stale := token
	stale.Revision = 1
	must(t, f.Publish(t.Context(), stale))
	result, err = f.Adapter.Search(
		ctx,
		a,
		f.Query,
		memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates, Minimum: &token},
	)
	must(t, err)
	checkCoverage(t, result, true)
	requireExactVisibleCandidate(t, result, token)
	for _, foreign := range foreignScopes(a) {
		token.Scope = foreign
		_, err = f.Adapter.Search(
			ctx,
			a,
			f.Query,
			memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates, Minimum: &token},
		)
		if !errors.Is(err, memy.ErrScopeViolation) {
			t.Fatalf("foreign minimum: %v", err)
		}
	}
	token.Scope, token.Revision = a, 3
	if err = f.Publish(t.Context(), token); err == nil {
		t.Fatal("unstaged acknowledgement accepted")
	}
	waiting, stop := context.WithTimeout(t.Context(), lockWaitDeadline)
	defer stop()
	result, err = f.Adapter.Search(
		waiting,
		a,
		f.Query,
		memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates, Minimum: &token},
	)
	if !errors.Is(err, memy.ErrVisibilityPending) {
		t.Fatalf("unstaged minimum fabricated: %+v err=%v", result, err)
	}
}

func requireExactVisibleCandidate(t *testing.T, result memy.SearchResult, token memy.VisibilityToken) {
	t.Helper()
	for _, candidate := range result.Candidates {
		if candidate.RecordID == token.RecordID && candidate.Revision == token.Revision {
			return
		}
	}
	t.Fatalf("exact visible revision missing: %+v", result)
}

func searchScorePresence[Q any](t *testing.T, factory func(*testing.T) SearchFixture[Q]) {
	// Arrange: observed zero and absent scores must remain distinguishable.
	f := factory(t)
	a := scope()
	scores := map[string]memy.Score{
		"absent":   {Present: false, Value: 0},
		"zero":     memy.ScoreOf(0),
		"negative": memy.ScoreOf(-1),
	}
	for id, score := range scores {
		must(t, f.Seed(t.Context(), a, memy.Candidate{RecordID: id, Revision: 1, Score: score, Signals: nil}))
		must(t, f.Publish(t.Context(), memy.VisibilityToken{Scope: a, RecordID: id, Revision: 1}))
	}
	// Act.
	result, err := f.Adapter.Search(
		t.Context(),
		a,
		f.Query,
		memy.SearchOptions{Minimum: nil, MaxCandidates: memy.MaxSearchCandidates},
	)
	// Assert.
	must(t, err)
	checkCoverage(t, result, false)
	if len(result.Candidates) != len(scores) {
		t.Fatalf("score-presence candidates=%+v", result.Candidates)
	}
	for _, candidate := range result.Candidates {
		expected, ok := scores[candidate.RecordID]
		nativePreserved := false
		for _, signal := range candidate.Signals {
			nativePreserved = nativePreserved || signal.Score == expected
		}
		if !ok || candidate.Score.Validate() != nil || !nativePreserved {
			t.Fatalf("score changed: %+v", candidate)
		}
	}
}
