package reference

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/internal/workcost"
)

func performanceIndex(t testing.TB, own, unrelated int) (*Index[string], memy.Scope) {
	t.Helper()
	scope := memy.Scope{Tenant: "t", Namespace: "n", Subject: "s"}
	idx := NewIndex[string]("audit", nil)
	for j := range own + unrelated {
		s := scope
		if j >= own {
			s.Subject = "unrelated"
		}
		candidate := memy.Candidate{
			RecordID: fmt.Sprintf("record-%08d", j),
			Revision: 1,
			Score:    memy.ScoreOf(float64(j)),
		}
		performancePublish(t, idx, s, candidate)
	}
	return idx, scope
}

func performancePublish(t testing.TB, idx *Index[string], scope memy.Scope, candidate memy.Candidate) {
	t.Helper()
	if err := idx.Stage(context.Background(), scope, candidate); err != nil {
		t.Fatal(err)
	}
	if err := idx.Acknowledge(
		context.Background(),
		memy.VisibilityToken{Scope: scope, RecordID: candidate.RecordID, Revision: candidate.Revision},
	); err != nil {
		t.Fatal(err)
	}
}

func TestPerformanceIndexBoundedOwnership(t *testing.T) {
	// Arrange: the original audit fixture of 10,000 matches and K=10.
	idx, scope := performanceIndex(t, 10000, 0)
	// Act: preserve the result while releasing every candidate in the index.
	result, err := idx.Search(t.Context(), scope, "", memy.SearchOptions{MaxCandidates: 10})
	if err != nil {
		t.Fatal(err)
	}
	_, err = idx.Purge(t.Context(), memy.PurgeBatch{Scope: scope, Selector: memy.Selector{Kind: memy.SelectScope}})
	// Assert: owned storage is bounded and contains no omitted metadata.
	if err != nil || len(result.Candidates) != 10 || !result.CandidatesTruncated || cap(result.Candidates) > 20 {
		t.Fatalf(
			"len=%d cap=%d truncated=%v err=%v",
			len(result.Candidates),
			cap(result.Candidates),
			result.CandidatesTruncated,
			err,
		)
	}
	if len(idx.visible) != 0 || len(idx.pending) != 0 {
		t.Fatal("purge retained empty scope maps")
	}
	for _, candidate := range result.Candidates[len(result.Candidates):cap(result.Candidates)] {
		if candidate.RecordID != "" || candidate.Signals != nil {
			t.Fatalf("omitted metadata retained: %+v", candidate)
		}
	}
	if result.Candidates[0].RecordID != "record-00009999" {
		t.Fatal("purge changed owned result")
	}
}

// performanceLongResult keeps fixture allocation out of the measuring frame;
// after return only the issued result can retain the candidate metadata.
func performanceLongResult(t *testing.T) memy.SearchResult {
	t.Helper()
	scope := memy.Scope{Tenant: "t", Namespace: "n", Subject: "s"}
	idx := NewIndex[string]("heap", nil)
	for n := range 10000 {
		id := fmt.Sprintf("%08d", n) + strings.Repeat("x", 1016)
		performancePublish(t, idx, scope, memy.Candidate{RecordID: id, Revision: 1, Score: memy.ScoreOf(float64(n))})
	}
	result, err := idx.Search(t.Context(), scope, "", memy.SearchOptions{MaxCandidates: 10})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = idx.Purge(
		t.Context(),
		memy.PurgeBatch{Scope: scope, Selector: memy.Selector{Kind: memy.SelectScope}},
	); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPerformanceIndexRetainedHeapAfterGC(t *testing.T) {
	// Arrange: establish live heap after GC, with no index or candidate strings.
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	// Act: keep a result alive after purging 10,000 independently allocated 1-KiB IDs.
	result := performanceLongResult(t)
	runtime.GC()
	runtime.ReadMemStats(&after)
	// Assert: a generous noise allowance is still well below omitted ID storage
	// (~10 MiB). This measures live heap, separately from benchmark B/op.
	retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	if retained > 2*1024*1024 {
		t.Fatalf("retained heap delta=%d; bounded result budget=2 MiB", retained)
	}
	if len(result.Candidates) != 10 || len(result.Candidates[0].RecordID) != 1024 {
		t.Fatal("invalid retained fixture")
	}
	t.Logf(
		"live heap delta=%d bytes; returned=%d capacity=%d",
		retained,
		len(result.Candidates),
		cap(result.Candidates),
	)
	runtime.KeepAlive(result)
}

//nolint:gocognit // One table-driven regression checks ordering, limits and evidence together.
func TestPerformanceIndexRankingBoundaries(t *testing.T) {
	for _, maximum := range []int{1, 5, 10} {
		t.Run(fmt.Sprintf("K=%d", maximum), func(t *testing.T) {
			// Arrange: score ties, multiple revisions and absent scores.
			idx := NewIndex[string]("ranking", nil)
			scope := memy.Scope{Tenant: "t", Namespace: "n", Subject: "s"}
			values := []memy.Candidate{
				{RecordID: "z", Revision: 1, Score: memy.ScoreOf(20)},
				{RecordID: "a", Revision: 2, Score: memy.ScoreOf(10)},
				{RecordID: "a", Revision: 1, Score: memy.ScoreOf(10)},
				{RecordID: "b", Revision: 1, Score: memy.ScoreOf(10)},
				{RecordID: "missing", Revision: 1},
			}
			for _, value := range values {
				performancePublish(t, idx, scope, value)
			}
			slices.SortFunc(values, candidateOrder)
			count := min(maximum, len(values))
			// Act: repeated map iteration must not change order or evidence.
			for range 20 {
				result, err := idx.Search(t.Context(), scope, "", memy.SearchOptions{MaxCandidates: maximum})
				// Assert.
				if err != nil || len(result.Candidates) != count ||
					result.CandidatesTruncated != (maximum < len(values)) {
					t.Fatalf("%+v %v", result, err)
				}
				for n, value := range result.Candidates {
					if value.RecordID != values[n].RecordID || value.Revision != values[n].Revision ||
						value.Score != values[n].Score ||
						!reflect.DeepEqual(
							value.Signals,
							[]memy.SearchSignal{{Backend: "ranking", Rank: n + 1, Score: value.Score}},
						) {
						t.Fatalf("rank %d: %+v want %+v", n, value, values[n])
					}
				}
			}
		})
	}
}

func TestPerformanceIndexExactScopeTraversal(t *testing.T) {
	// Arrange: 10 selected and 100,000 unrelated candidates, plus unrelated pending.
	idx, scope := performanceIndex(t, 10, 100000)
	other := scope
	other.Subject = "unrelated"
	if err := idx.Stage(t.Context(), other, memy.Candidate{RecordID: "pending-other", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if len(idx.visible[scope]) != 10 || len(idx.visible[other]) != 100000 || len(idx.pending[other]) != 1 {
		t.Fatal("invalid exact-scope partitions")
	}
	visits := 0
	idx.match = func(_ string, _ memy.Candidate) bool { visits++; return true }
	// Act: count every examined entry before scope/match filtering.
	var costs workcost.Counters
	workcost.Start(&costs)
	result, err := idx.Search(t.Context(), scope, "", memy.SearchOptions{MaxCandidates: 10})
	workcost.Stop(&costs)
	// Assert: deterministic work counter, independent of wall time.
	if err != nil || costs.SearchCandidates.Load() != 10 || visits != 10 || len(result.Candidates) != 10 ||
		result.Coverage[0].Status != coverageEventual {
		t.Fatalf("examined=%d visits=%d result=%+v err=%v", costs.SearchCandidates.Load(), visits, result, err)
	}
}

func TestPerformanceIndexPendingPurgeAndCancellation(t *testing.T) {
	// Arrange: same revision is pending in one scope, visible in another.
	scope := memy.Scope{Tenant: "t", Namespace: "n", Subject: "s"}
	other := scope
	other.Subject = "other"
	idx := NewIndex[string]("pending", func(q string, c memy.Candidate) bool { return q == "" || q == c.RecordID })
	candidate := memy.Candidate{RecordID: "record", Revision: 1, Score: memy.ScoreOf(1)}
	performancePublish(t, idx, other, candidate)
	if err := idx.Stage(t.Context(), scope, candidate); err != nil {
		t.Fatal(err)
	}
	token := memy.VisibilityToken{Scope: scope, RecordID: candidate.RecordID, Revision: 1}
	// Act.
	pending, err := idx.Search(t.Context(), scope, "", memy.SearchOptions{MaxCandidates: 1})
	zero, zeroErr := idx.Search(t.Context(), scope, "no-match", memy.SearchOptions{MaxCandidates: 1})
	_, purgeErr := idx.Purge(
		t.Context(),
		memy.PurgeBatch{Scope: scope, Selector: memy.Selector{Kind: memy.SelectScope}},
	)
	ackErr := idx.Acknowledge(t.Context(), token)
	preserved, preservedErr := idx.Search(t.Context(), other, "", memy.SearchOptions{MaxCandidates: 1})
	deadline, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	waiting, waitingErr := idx.Search(deadline, scope, "", memy.SearchOptions{MaxCandidates: 1, Minimum: &token})
	canceled, cancelNow := context.WithCancel(t.Context())
	cancelNow()
	_, cancelErr := idx.Search(canceled, scope, "", memy.SearchOptions{MaxCandidates: 1})
	// Assert.
	if err != nil || len(pending.Candidates) != 0 || pending.Coverage[0].Status != coveragePending ||
		!pending.Coverage[0].MinimumSatisfied {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	if zeroErr != nil || len(zero.Candidates) != 0 || zero.CandidatesTruncated ||
		zero.Coverage[0].Status != coverageEventual {
		t.Fatalf("zero=%+v err=%v", zero, zeroErr)
	}
	if purgeErr != nil || !errors.Is(ackErr, memy.ErrNotFound) || preservedErr != nil ||
		len(preserved.Candidates) != 1 ||
		len(idx.pending) != 0 ||
		len(idx.visible) != 1 {
		t.Fatalf("purge=%v ack=%v preserved=%+v %v", purgeErr, ackErr, preserved, preservedErr)
	}
	if !errors.Is(waitingErr, context.DeadlineExceeded) || !errors.Is(waitingErr, memy.ErrVisibilityPending) ||
		waiting.Coverage[0].MinimumSatisfied ||
		!errors.Is(cancelErr, context.Canceled) {
		t.Fatalf("wait=%+v %v cancel=%v", waiting, waitingErr, cancelErr)
	}
}

func BenchmarkPerformanceSearch(b *testing.B) {
	for _, f := range []struct{ own, other int }{{10, 0}, {1000, 0}, {10000, 0}, {10, 10000}, {10, 100000}} {
		b.Run(fmt.Sprintf("own=%d/other=%d", f.own, f.other), func(b *testing.B) {
			idx, scope := performanceIndex(b, f.own, f.other)
			ctx := context.Background()
			opts := memy.SearchOptions{MaxCandidates: 10}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				r, err := idx.Search(ctx, scope, "", opts)
				if err != nil || len(r.Candidates) != 10 {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestPerformanceCompositeDetachedTruncation(t *testing.T) {
	// Arrange: two individually bounded backends produce four distinct records;
	// fusion must return only two while preserving their independent signals.
	scope := memy.Scope{Tenant: "t", Namespace: "n", Subject: "s"}
	a, b := NewIndex[string]("a", nil), NewIndex[string]("b", nil)
	for n, idx := range []*Index[string]{a, b} {
		for rank := range 2 {
			performancePublish(
				t,
				idx,
				scope,
				memy.Candidate{
					RecordID: fmt.Sprintf("backend-%d-record-%d", n, rank),
					Revision: 1,
					Score:    memy.ScoreOf(float64(2 - rank)),
				},
			)
		}
	}
	composite := Composite[string]{
		Backends: []Backend[string]{{ID: "a", Search: a}, {ID: "b", Search: b}},
		RRF:      RRFConfig{K: 60},
	}
	// Act.
	result, err := composite.Search(t.Context(), scope, "", memy.SearchOptions{MaxCandidates: 2})
	for _, idx := range []*Index[string]{a, b} {
		if _, purgeErr := idx.Purge(
			t.Context(),
			memy.PurgeBatch{Scope: scope, Selector: memy.Selector{Kind: memy.SelectScope}},
		); purgeErr != nil {
			t.Fatal(purgeErr)
		}
	}
	// Assert: no reachable hidden tail, including omitted ID or signal storage.
	if err != nil || !result.CandidatesTruncated || len(result.Candidates) != 2 || cap(result.Candidates) != 2 {
		t.Fatalf(
			"len=%d cap=%d truncated=%v err=%v",
			len(result.Candidates),
			cap(result.Candidates),
			result.CandidatesTruncated,
			err,
		)
	}
	if result.Candidates[0].RecordID != "backend-0-record-0" || result.Candidates[1].RecordID != "backend-1-record-0" ||
		len(result.Candidates[0].Signals) != 1 ||
		result.Candidates[0].Signals[0].Backend != "a" {
		t.Fatalf("fusion changed: %+v", result)
	}
}

// performanceObservedContext signals each blocked visibility select after Search
// has captured the notification channel and released the index mutex.
type performanceObservedContext struct {
	context.Context

	observed chan struct{}
}

func (c performanceObservedContext) Done() <-chan struct{} {
	select {
	case c.observed <- struct{}{}:
	default:
	}
	return c.Context.Done()
}

type performanceSearchOutcome struct {
	result memy.SearchResult
	err    error
}

func performanceAwaitBlocked(t *testing.T, ctx performanceObservedContext) {
	t.Helper()
	select {
	case <-ctx.observed:
	case <-ctx.Context.Done():
		t.Fatal("Search did not enter visibility wait before deadline")
	}
}

//nolint:gocognit // Related notification cases share the same deterministic blocked-search fixture.
func TestPerformanceIndexBlockedVisibilityNotifications(t *testing.T) {
	for _, scenario := range []string{"acknowledge", "failure", "purge then failure"} {
		t.Run(scenario, func(t *testing.T) {
			// Arrange: one pending exact revision and a deadline-bearing Search. The
			// observed Done access synchronizes on the actual blocked select, not sleep.
			scope := memy.Scope{Tenant: "t", Namespace: "n", Subject: "s"}
			idx := NewIndex[string]("notification", nil)
			value := memy.Candidate{RecordID: "record", Revision: 1, Score: memy.ScoreOf(1)}
			if err := idx.Stage(t.Context(), scope, value); err != nil {
				t.Fatal(err)
			}
			token := memy.VisibilityToken{Scope: scope, RecordID: value.RecordID, Revision: value.Revision}
			deadline, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			ctx := performanceObservedContext{Context: deadline, observed: make(chan struct{}, 1)}
			outcomes := make(chan performanceSearchOutcome, 1)
			go func() {
				result, err := idx.Search(ctx, scope, "", memy.SearchOptions{MaxCandidates: 1, Minimum: &token})
				outcomes <- performanceSearchOutcome{result: result, err: err}
			}()
			performanceAwaitBlocked(t, ctx)
			failure := errors.New("visibility backend failed while waiting")
			// Act: notify an already waiting Search; purge must wake and re-enter wait
			// without making an exact token visible or permitting delayed publication.
			if scenario == "purge then failure" {
				if _, err := idx.Purge(
					t.Context(),
					memy.PurgeBatch{Scope: scope, Selector: memy.Selector{Kind: memy.SelectScope}},
				); err != nil {
					t.Fatal(err)
				}
				performanceAwaitBlocked(t, ctx)
				if err := idx.Acknowledge(t.Context(), token); !errors.Is(err, memy.ErrNotFound) {
					t.Fatalf("delayed ack: %v", err)
				}
			}
			if scenario == "acknowledge" {
				if err := idx.Acknowledge(t.Context(), token); err != nil {
					t.Fatal(err)
				}
			} else {
				idx.Fail(failure)
			}
			var outcome performanceSearchOutcome
			select {
			case outcome = <-outcomes:
			case <-deadline.Done():
				t.Fatal("Search did not wake after notification")
			}
			// Assert: wakeups preserve exact visibility and failure metadata.
			if scenario == "acknowledge" {
				if outcome.err != nil || len(outcome.result.Candidates) != 1 ||
					outcome.result.Candidates[0].RecordID != value.RecordID ||
					len(outcome.result.Coverage) != 1 ||
					outcome.result.Coverage[0].Status != coverageReady ||
					!outcome.result.Coverage[0].MinimumSatisfied {
					t.Fatalf("acknowledged wait: %+v %v", outcome.result, outcome.err)
				}
			} else {
				if !errors.Is(outcome.err, memy.ErrUnavailable) || !errors.Is(outcome.err, failure) ||
					len(outcome.result.Candidates) != 0 ||
					len(outcome.result.Coverage) != 1 ||
					outcome.result.Coverage[0].Status != coverageUnavailable ||
					outcome.result.Coverage[0].MinimumSatisfied {
					t.Fatalf("failed wait: %+v %v", outcome.result, outcome.err)
				}
			}
		})
	}
}
