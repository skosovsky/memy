package conformance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/skosovsky/memy"
)

// SearchFixture provisions isolated adapters and query-matching metadata.
// Seed must stage a revision; Publish makes its visibility token observable.
type SearchFixture[Q any] struct {
	Adapter memy.Search[Q]
	Query   Q
	Seed    func(context.Context, memy.Scope, memy.Candidate) error
	Publish func(context.Context, memy.VisibilityToken) error
	Fail    func(error)
}

// SearchSuite tests the declared scoped and minimum-visibility guarantees.
func SearchSuite[Q any](t *testing.T, factory func(*testing.T) SearchFixture[Q]) {
	t.Helper()
	t.Run("scope_coverage", func(t *testing.T) {
		// Arrange: matching metadata in two scopes; B is already published.
		f := factory(t)
		a := scope()
		b := a
		b.Tenant = "B"
		candidate := memy.Candidate{RecordID: "record", Revision: 1, Score: 1}
		must(t, f.Seed(t.Context(), a, candidate))
		must(t, f.Seed(t.Context(), b, memy.Candidate{RecordID: "private", Revision: 1, Score: 1}))
		must(t, f.Publish(t.Context(), memy.VisibilityToken{Scope: b, RecordID: "private", Revision: 1}))
		if !f.Adapter.Capabilities().Scoped {
			t.Fatal("fixture requires scoped search")
		}
		// Act: known staged data must not certify empty completeness.
		staged, err := f.Adapter.Search(t.Context(), a, f.Query, memy.SearchOptions{Minimum: nil})
		must(t, err)
		checkCoverage(t, staged, false)
		must(t, f.Publish(t.Context(), memy.VisibilityToken{Scope: a, RecordID: candidate.RecordID, Revision: 1}))
		visible, err := f.Adapter.Search(t.Context(), a, f.Query, memy.SearchOptions{Minimum: nil})
		// Assert: foreign metadata never enters A results.
		must(t, err)
		if len(visible.Candidates) != 1 || visible.Candidates[0].RecordID != candidate.RecordID {
			t.Fatalf("scope result: %+v", visible)
		}
	})
	t.Run("minimum_and_cancel", func(t *testing.T) {
		// Arrange.
		f := factory(t)
		a := scope()
		candidate := memy.Candidate{RecordID: "record", Revision: 1, Score: 1}
		must(t, f.Seed(t.Context(), a, candidate))
		token := memy.VisibilityToken{Scope: a, RecordID: candidate.RecordID, Revision: 1}
		ctx, cancel := context.WithTimeout(t.Context(), lockWaitDeadline)
		defer cancel()
		// Act: a minimum cannot be silently satisfied by staged metadata.
		result, err := f.Adapter.Search(ctx, a, f.Query, memy.SearchOptions{Minimum: &token})
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
			result, err = f.Adapter.Search(ready, a, f.Query, memy.SearchOptions{Minimum: &token})
			must(t, err)
			checkCoverage(t, result, true)
		}
		canceled, stop := context.WithCancel(t.Context())
		stop()
		_, err = f.Adapter.Search(canceled, a, f.Query, memy.SearchOptions{Minimum: nil})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel: %v", err)
		}
	})
	t.Run("failure", func(t *testing.T) {
		// Arrange.
		f := factory(t)
		failure := errors.New("search unavailable")
		f.Fail(failure)
		// Act.
		result, err := f.Adapter.Search(t.Context(), scope(), f.Query, memy.SearchOptions{Minimum: nil})
		// Assert: unavailable is an error rather than certified empty coverage.
		if err == nil || !errors.Is(err, failure) {
			t.Fatalf("failure: %+v %v", result, err)
		}
	})
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
		} else if coverage.Status == "complete" {
			t.Fatalf("known staged lag certified complete: %+v", coverage)
		}
	}
}
