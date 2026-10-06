package memy_test

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/internal/workcost"
)

type retrievalBudgetSearch struct {
	result memy.SearchResult
	caps   memy.SearchCapabilities
	calls  int
}

func (s *retrievalBudgetSearch) Capabilities() memy.SearchCapabilities { return s.caps }

func (s *retrievalBudgetSearch) Search(
	context.Context,
	memy.Scope,
	string,
	memy.SearchOptions,
) (memy.SearchResult, error) {
	s.calls++
	return s.result, nil
}

func retrievalBudgetCandidate(id string, revision memy.Version, rank int) memy.Candidate {
	return memy.Candidate{
		RecordID: id,
		Revision: revision,
		Score:    10,
		Signals:  []memy.SearchSignal{{Backend: "search/v1", Rank: rank, Score: 100}},
	}
}

func retrievalBudgetPort(candidates ...memy.Candidate) *retrievalBudgetSearch {
	return &retrievalBudgetSearch{
		caps: memy.SearchCapabilities{Scoped: true, BoundedCandidates: true},
		result: memy.SearchResult{
			Candidates: candidates,
			Coverage:   []memy.Coverage{{Backend: "search/v1", Status: "ready"}},
		},
	}
}

func TestRetrievalBudgetRejectsMetadataBeforeCanonicalDecode(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*retrievalBudgetSearch)
		max    int
		want   error
	}{
		{"oversized result", func(s *retrievalBudgetSearch) {
			s.result.Candidates = append(s.result.Candidates, retrievalBudgetCandidate("second", 1, 1))
		}, 1, memy.ErrBudget},
		{
			"blank identity",
			func(s *retrievalBudgetSearch) { s.result.Candidates[0].RecordID = " " },
			2,
			memy.ErrInvalid,
		},
		{
			"nul identity",
			func(s *retrievalBudgetSearch) { s.result.Candidates[0].RecordID = "first\x00" },
			2,
			memy.ErrInvalid,
		},
		{
			"invalid utf8 identity",
			func(s *retrievalBudgetSearch) { s.result.Candidates[0].RecordID = string([]byte{0xff}) },
			2,
			memy.ErrInvalid,
		},
		{
			"overlong identity",
			func(s *retrievalBudgetSearch) { s.result.Candidates[0].RecordID = strings.Repeat("a", 1025) },
			2,
			memy.ErrInvalid,
		},
		{"zero revision", func(s *retrievalBudgetSearch) { s.result.Candidates[0].Revision = 0 }, 2, memy.ErrInvalid},
		{
			"revision overflow",
			func(s *retrievalBudgetSearch) { s.result.Candidates[0].Revision = memy.MaxVersion + 1 },
			2,
			memy.ErrInvalid,
		},
		{
			"nan candidate score",
			func(s *retrievalBudgetSearch) { s.result.Candidates[0].Score = math.NaN() },
			2,
			memy.ErrInvalid,
		},
		{
			"infinite candidate score",
			func(s *retrievalBudgetSearch) { s.result.Candidates[0].Score = math.Inf(1) },
			2,
			memy.ErrInvalid,
		},
		{
			"missing signals",
			func(s *retrievalBudgetSearch) { s.result.Candidates[0].Signals = nil },
			2,
			memy.ErrInvalid,
		},
		{"duplicate signals", func(s *retrievalBudgetSearch) {
			s.result.Candidates[0].Signals = append(s.result.Candidates[0].Signals, s.result.Candidates[0].Signals[0])
		}, 2, memy.ErrInvalid},
		{
			"unknown backend signal",
			func(s *retrievalBudgetSearch) { s.result.Candidates[0].Signals[0].Backend = "unknown/v1" },
			2,
			memy.ErrInvalid,
		},
		{
			"unavailable backend signal",
			func(s *retrievalBudgetSearch) { s.result.Coverage[0].Status = "unavailable" },
			2,
			memy.ErrInvalid,
		},
		{
			"zero rank",
			func(s *retrievalBudgetSearch) { s.result.Candidates[0].Signals[0].Rank = 0 },
			2,
			memy.ErrInvalid,
		},
		{
			"rank exceeds bound",
			func(s *retrievalBudgetSearch) { s.result.Candidates[0].Signals[0].Rank = 3 },
			2,
			memy.ErrInvalid,
		},
		{
			"nan signal score",
			func(s *retrievalBudgetSearch) { s.result.Candidates[0].Signals[0].Score = math.NaN() },
			2,
			memy.ErrInvalid,
		},
		{
			"infinite signal score",
			func(s *retrievalBudgetSearch) { s.result.Candidates[0].Signals[0].Score = math.Inf(-1) },
			2,
			memy.ErrInvalid,
		},
		{"duplicate final reference", func(s *retrievalBudgetSearch) {
			s.result.Candidates = append(s.result.Candidates, s.result.Candidates[0])
		}, 2, memy.ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange: a real canonical record makes accidental decoding observable.
			f := newFixture(t, nil)
			p := f.propose(t, "seed", "retained", memy.Interval{})
			f.commit(t, f.acceptedRequest(t, "seed-commit", "first", 0, p, memy.Append))
			search := retrievalBudgetPort(retrievalBudgetCandidate("first", 1, 1))
			tc.mutate(search)
			metrics := &workcost.Counters{}
			workcost.Start(metrics)
			// Act.
			result, err := memy.Recall(
				t.Context(),
				f.engine,
				f.actor,
				f.scope,
				"query",
				search,
				memy.ScoreRanker[preference, sourceRef]{},
				memy.RecallOptions{Search: memy.SearchOptions{MaxCandidates: tc.max}, Limit: 1},
			)
			workcost.Stop(metrics)
			// Assert: rejection precedes all canonical envelope decoding.
			if !errors.Is(err, tc.want) || metrics.DecodedDocs.Load() != 0 || len(result.Records) != 0 ||
				search.calls != 1 {
				t.Fatalf(
					"result=%+v error=%v decoded=%d search calls=%d",
					result,
					err,
					metrics.DecodedDocs.Load(),
					search.calls,
				)
			}
		})
	}
}

func TestRetrievalBudgetRequiresBoundedPortBeforeSearch(t *testing.T) {
	// Arrange.
	f := newFixture(t, nil)
	search := retrievalBudgetPort()
	search.caps.BoundedCandidates = false
	// Act.
	result, err := memy.Recall(
		t.Context(),
		f.engine,
		f.actor,
		f.scope,
		"query",
		search,
		memy.ScoreRanker[preference, sourceRef]{},
		memy.RecallOptions{Search: memy.SearchOptions{MaxCandidates: 1}, Limit: 1},
	)
	// Assert.
	if !errors.Is(err, memy.ErrUnsupported) || search.calls != 0 || len(result.Records) != 0 {
		t.Fatalf("result=%+v error=%v search calls=%d", result, err, search.calls)
	}
}

func TestRetrievalBudgetUnavailableCoverageCannotReportAbsence(t *testing.T) {
	for _, requireMinimum := range []bool{false, true} {
		name := "without minimum"
		if requireMinimum {
			name = "with minimum"
		}
		t.Run(name, func(t *testing.T) {
			// Arrange: the port returns no error but every declared backend is unavailable.
			f := newFixture(t, nil)
			search := retrievalBudgetPort()
			search.caps.Visibility = true
			search.result.Coverage = []memy.Coverage{
				{Backend: "search/v1", Status: "unavailable"},
				{Backend: "second/v1", Status: "unavailable"},
			}
			options := memy.RecallOptions{Search: memy.SearchOptions{MaxCandidates: 1}, Limit: 1}
			if requireMinimum {
				options.Search.Minimum = &memy.VisibilityToken{Scope: f.scope, RecordID: "first", Revision: 1}
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			// Act.
			result, err := memy.Recall(
				ctx,
				f.engine,
				f.actor,
				f.scope,
				"query",
				search,
				memy.ScoreRanker[preference, sourceRef]{},
				options,
			)
			// Assert: minimum visibility cannot downgrade an outage into pending indexing.
			if !errors.Is(err, memy.ErrUnavailable) || errors.Is(err, memy.ErrVisibilityPending) ||
				len(result.Records) != 0 ||
				!reflect.DeepEqual(result.Coverage, search.result.Coverage) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}

func TestRetrievalBudgetProgressSeparatesCandidateStages(t *testing.T) {
	// Arrange: two eligible records, one missing revision, and one absent identity.
	f := newFixture(t, nil)
	for _, id := range []string{"first", "second"} {
		p := f.propose(t, "seed-"+id, id, memy.Interval{})
		f.commit(t, f.acceptedRequest(t, "commit-"+id, id, 0, p, memy.Append))
	}
	search := retrievalBudgetPort(
		retrievalBudgetCandidate("first", 1, 1),
		retrievalBudgetCandidate("second", 1, 2),
		retrievalBudgetCandidate("first", 2, 3),
		retrievalBudgetCandidate("absent", 1, 4),
	)
	search.result.CandidatesTruncated = true
	// Act.
	result, err := memy.Recall(
		t.Context(),
		f.engine,
		f.actor,
		f.scope,
		"query",
		search,
		memy.ScoreRanker[preference, sourceRef]{},
		memy.RecallOptions{Search: memy.SearchOptions{MaxCandidates: 4}, Limit: 1},
	)
	// Assert: canonical filtering, ranking omission, and index truncation differ.
	want := memy.RecallProgress{
		ReturnedCandidates:  4,
		CanonicalChecked:    4,
		CanonicalFiltered:   2,
		RankingOmitted:      1,
		CandidatesTruncated: true,
	}
	if err != nil || result.Progress != want || len(result.Records) != 1 || result.Records[0].Record.ID != "first" {
		t.Fatalf("result=%+v error=%v want progress=%+v", result, err, want)
	}
}

func TestRetrievalBudgetRankerCannotReplaceSignalsOrAuthorizedReferences(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		name := "preserve typed evidence"
		if foreign {
			name = "reject foreign reference"
		}
		t.Run(name, func(t *testing.T) {
			// Arrange.
			f := newFixture(t, nil)
			p := f.propose(t, "seed", "original", memy.Interval{})
			f.commit(t, f.acceptedRequest(t, "seed-commit", "first", 0, p, memy.Append))
			search := retrievalBudgetPort(retrievalBudgetCandidate("first", 1, 1))
			original := search.result.Candidates[0].Signals[0]
			ranker := rankerFunc[preference, sourceRef](
				func(_ context.Context, input []memy.Ranked[preference, sourceRef]) ([]memy.Ranked[preference, sourceRef], error) {
					input[0].Signals[0] = memy.SearchSignal{Backend: "forged", Rank: 100, Score: math.Inf(1)}
					input[0].Record.Payload.Value = "forged payload"
					if foreign {
						input[0].Record.ID = "not-authorized-candidate"
					}
					return input, nil
				},
			)
			// Act.
			result, err := memy.Recall(
				t.Context(),
				f.engine,
				f.actor,
				f.scope,
				"query",
				search,
				ranker,
				memy.RecallOptions{Search: memy.SearchOptions{MaxCandidates: 1}, Limit: 1},
			)
			// Assert.
			if foreign {
				if !errors.Is(err, memy.ErrInvalid) || len(result.Records) != 0 {
					t.Fatalf("foreign reference result=%+v error=%v", result, err)
				}
				return
			}
			if err != nil || len(result.Records) != 1 ||
				!reflect.DeepEqual(result.Records[0].Signals, []memy.SearchSignal{original}) ||
				result.Records[0].Record.Payload.Value != "original" ||
				search.result.Candidates[0].Signals[0] != original {
				t.Fatalf("ranker rewrote original data: result=%+v search=%+v error=%v", result, search.result, err)
			}
		})
	}
}
