package reference_test

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

type fixtureSearch struct {
	result       memy.SearchResult
	err          error
	options      []memy.SearchOptions
	order        *[]string
	id           string
	unsupported  bool
	beforeReturn func()
}

func (f *fixtureSearch) Capabilities() memy.SearchCapabilities {
	return memy.SearchCapabilities{Scoped: true, Visibility: true, BoundedCandidates: !f.unsupported}
}

func (f *fixtureSearch) Search(
	_ context.Context,
	_ memy.Scope,
	_ int,
	options memy.SearchOptions,
) (memy.SearchResult, error) {
	f.options = append(f.options, options)
	if f.order != nil {
		*f.order = append(*f.order, f.id)
	}
	if f.beforeReturn != nil {
		f.beforeReturn()
	}
	return f.result, f.err
}
func backend(id string, candidates ...memy.Candidate) reference.Backend[int] {
	for n := range candidates {
		if len(candidates[n].Signals) == 0 {
			candidates[n].Signals = []memy.SearchSignal{{Backend: id, Rank: n + 1, Score: candidates[n].Score}}
		}
	}
	return reference.Backend[int]{
		ID: id,
		Search: &fixtureSearch{
			id: id,
			result: memy.SearchResult{
				Candidates: candidates,
				Coverage:   []memy.Coverage{{Backend: id, Status: "ready", MinimumSatisfied: true}},
			},
		},
	}
}
func candidate(id string, score float64) memy.Candidate {
	return memy.Candidate{RecordID: id, Revision: 1, Score: memy.ScoreOf(score)}
}

func TestCompositeRRFPermutationDuplicatesAndScales(t *testing.T) {
	// Arrange: duplicate a must neither gain weight nor move b's distinct rank.
	sparse := backend("sparse", candidate("a", 10000), candidate("a", -10000), candidate("b", 9000))
	dense := backend("dense", candidate("b", .01), candidate("a", .001))
	order := []string{}
	sparse.Search.(*fixtureSearch).order = &order
	dense.Search.(*fixtureSearch).order = &order
	c := reference.Composite[int]{Backends: []reference.Backend[int]{sparse, dense}, RRF: reference.RRFConfig{K: 60}}
	options := memy.SearchOptions{MaxCandidates: 3}
	// Act.
	first, err := c.Search(context.Background(), portScope(), 7, options)
	c.Backends = []reference.Backend[int]{dense, sparse}
	second, secondErr := c.Search(context.Background(), portScope(), 7, options)
	// Assert.
	if err != nil || secondErr != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("permutation: %+v %+v %v %v", first, second, err, secondErr)
	}
	if !reflect.DeepEqual(order, []string{"dense", "sparse", "dense", "sparse"}) {
		t.Fatalf("order %v", order)
	}
	if len(first.Candidates) != 2 || first.Candidates[0].RecordID != "a" {
		t.Fatalf("stable tie %+v", first)
	}
	expected := 1.0/61 + 1.0/62
	for _, value := range first.Candidates {
		if math.Abs(value.Score.Value-expected) > 1e-15 || len(value.Signals) != 2 {
			t.Fatalf("fusion %+v", value)
		}
	}
	if first.Candidates[1].Signals[1].Rank != 3 || first.Candidates[0].Signals[1].Score.Value != 10000 {
		t.Fatalf("raw signals %+v", first.Candidates)
	}
	for _, b := range c.Backends {
		for _, o := range b.Search.(*fixtureSearch).options {
			if o.MaxCandidates != 3 {
				t.Fatal("missing child bound")
			}
		}
	}
}

func TestCompositeWeightsAndGlobalBound(t *testing.T) {
	// Arrange.
	a := backend("a", candidate("x", 1), candidate("y", 2))
	b := backend("b", candidate("z", 1), candidate("y", 2))
	c := reference.Composite[int]{
		Backends: []reference.Backend[int]{b, a},
		RRF:      reference.RRFConfig{K: 1, Weights: map[string]float64{"b": 2}},
	}
	// Act.
	result, err := c.Search(context.Background(), portScope(), 0, memy.SearchOptions{MaxCandidates: 2})
	// Assert: y's second ranks fuse before global truncation.
	if err != nil || !result.CandidatesTruncated || len(result.Candidates) != 2 ||
		result.Candidates[0].RecordID != "y" ||
		result.Candidates[1].RecordID != "z" {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestCompositeRejectsMalformedBeforeFusion(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*reference.Composite[int])
		want   error
	}{
		{"zero K", func(c *reference.Composite[int]) { c.RRF.K = 0 }, memy.ErrInvalid},
		{"infinite K", func(c *reference.Composite[int]) { c.RRF.K = math.Inf(1) }, memy.ErrInvalid},
		{
			"unknown weight",
			func(c *reference.Composite[int]) { c.RRF.Weights = map[string]float64{"unknown": 1} },
			memy.ErrInvalid,
		},
		{
			"negative weight",
			func(c *reference.Composite[int]) { c.RRF.Weights = map[string]float64{"a": -1} },
			memy.ErrInvalid,
		},
		{
			"nan weight",
			func(c *reference.Composite[int]) { c.RRF.Weights = map[string]float64{"a": math.NaN()} },
			memy.ErrInvalid,
		},
		{
			"duplicate names",
			func(c *reference.Composite[int]) { c.Backends = append(c.Backends, c.Backends[0]) },
			memy.ErrInvalid,
		},
		{"oversized", func(c *reference.Composite[int]) {
			c.Backends[0].Search.(*fixtureSearch).result.Candidates = append(
				c.Backends[0].Search.(*fixtureSearch).result.Candidates,
				candidate("b", 1),
			)
		}, memy.ErrBudget},
		{"malformed ref", func(c *reference.Composite[int]) {
			c.Backends[0].Search.(*fixtureSearch).result.Candidates[0].RecordID = "bad\x00id"
		}, memy.ErrInvalid},
		{"nan score", func(c *reference.Composite[int]) {
			c.Backends[0].Search.(*fixtureSearch).result.Candidates[0].Score = memy.ScoreOf(math.NaN())
		}, memy.ErrInvalid},
		{"foreign coverage", func(c *reference.Composite[int]) {
			c.Backends[0].Search.(*fixtureSearch).result.Coverage[0].Backend = "foreign"
		}, memy.ErrInvalid},
		{"foreign signal", func(c *reference.Composite[int]) {
			c.Backends[0].Search.(*fixtureSearch).result.Candidates[0].Signals = []memy.SearchSignal{
				{Backend: "foreign", Rank: 1, Score: memy.ScoreOf(1)},
			}
		}, memy.ErrInvalid},
		{"invalid signal rank", func(c *reference.Composite[int]) {
			c.Backends[0].Search.(*fixtureSearch).result.Candidates[0].Signals = []memy.SearchSignal{
				{Backend: "a", Rank: 0, Score: memy.ScoreOf(1)},
			}
		}, memy.ErrInvalid},
		{"infinite signal", func(c *reference.Composite[int]) {
			c.Backends[0].Search.(*fixtureSearch).result.Candidates[0].Signals = []memy.SearchSignal{
				{Backend: "a", Rank: 1, Score: memy.ScoreOf(math.Inf(-1))},
			}
		}, memy.ErrInvalid},
		{
			"unsupported bound",
			func(c *reference.Composite[int]) { c.Backends[0].Search.(*fixtureSearch).unsupported = true },
			memy.ErrUnsupported,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange.
			c := reference.Composite[int]{
				Backends: []reference.Backend[int]{backend("a", candidate("x", 1))},
				RRF:      reference.RRFConfig{K: 60},
			}
			tc.mutate(&c)
			// Act.
			_, err := c.Search(context.Background(), portScope(), 0, memy.SearchOptions{MaxCandidates: 1})
			// Assert.
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}

func TestCompositeDegradedMinimumAndCancellation(t *testing.T) {
	for _, scenario := range []string{"partial", "all failed", "minimum", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			// Arrange.
			a := backend("a", candidate("x", 1))
			b := backend("b")
			failed := b.Search.(*fixtureSearch)
			failed.err = memy.ErrUnavailable
			failed.result.Coverage[0].Status = "unavailable"
			c := reference.Composite[int]{
				Backends:      []reference.Backend[int]{a, b},
				RRF:           reference.RRFConfig{K: 60},
				AllowDegraded: true,
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			options := memy.SearchOptions{MaxCandidates: 2}
			var want error
			switch scenario {
			case "all failed":
				c.Backends = []reference.Backend[int]{b}
				want = memy.ErrUnavailable
			case "minimum":
				options.Minimum = &memy.VisibilityToken{Scope: portScope(), RecordID: "x", Revision: 1}
				want = memy.ErrUnavailable
			case "canceled":
				failed.err = context.Canceled
				want = context.Canceled
			}
			// Act.
			result, err := c.Search(ctx, portScope(), 0, options)
			// Assert.
			if !errors.Is(err, want) {
				t.Fatalf("%+v %v want %v", result, err, want)
			}
			if scenario == "partial" &&
				(len(result.Candidates) != 1 || len(result.Coverage) != 2 || result.Coverage[1].Status != "unavailable") {
				t.Fatalf("%+v", result)
			}
		})
	}
}

func TestIndexBoundAndDetachedSignals(t *testing.T) {
	// Arrange.
	index := reference.NewIndex[int]("index", nil)
	ctx := context.Background()
	signal := []memy.SearchSignal{{Backend: "fake", Rank: 99, Score: memy.ScoreOf(100)}}
	for _, value := range []memy.Candidate{candidate("b", 10), candidate("a", 10), candidate("c", 20)} {
		value.Signals = signal
		if err := index.Stage(ctx, portScope(), value); err != nil {
			t.Fatal(err)
		}
		if err := index.Acknowledge(
			ctx,
			memy.VisibilityToken{Scope: portScope(), RecordID: value.RecordID, Revision: 1},
		); err != nil {
			t.Fatal(err)
		}
	}
	signal[0].Score = memy.ScoreOf(math.NaN())
	// Act.
	first, err := index.Search(ctx, portScope(), 0, memy.SearchOptions{MaxCandidates: 2})
	first.Candidates[0].Signals[0].Score = memy.ScoreOf(-1)
	second, secondErr := index.Search(ctx, portScope(), 0, memy.SearchOptions{MaxCandidates: 2})
	// Assert.
	if err != nil || secondErr != nil || !second.CandidatesTruncated || len(second.Candidates) != 2 ||
		second.Candidates[0].RecordID != "c" ||
		second.Candidates[1].RecordID != "a" ||
		second.Candidates[0].Signals[0].Score.Value != 20 ||
		second.Candidates[1].Signals[0].Rank != 2 {
		t.Fatalf("%+v %v %v", second, err, secondErr)
	}
}
func TestIndexValidationAndEventualMinimum(t *testing.T) {
	// Arrange.
	index := reference.NewIndex[int]("index", nil)
	ctx := context.Background()
	// Act and assert independent invalid requests.
	for _, name := range []string{"", " ", "x\x00y", strings.Repeat("a", 1025), string([]byte{255})} {
		if reference.NewIndex[int](name, nil) != nil {
			t.Fatalf("invalid constructor %q", name)
		}
	}
	for _, value := range []memy.Candidate{candidate(" ", 1), candidate("x", math.Inf(1)), {RecordID: "x", Revision: 0}} {
		if !errors.Is(index.Stage(ctx, portScope(), value), memy.ErrInvalid) {
			t.Fatalf("stage %+v", value)
		}
	}
	for _, maximum := range []int{0, -1, memy.MaxSearchCandidates + 1} {
		if _, err := index.Search(
			ctx,
			portScope(),
			0,
			memy.SearchOptions{MaxCandidates: maximum},
		); !errors.Is(
			err,
			memy.ErrInvalid,
		) {
			t.Fatalf("bound %d %v", maximum, err)
		}
	}
	token := &memy.VisibilityToken{Scope: portScope(), RecordID: "x", Revision: 1}
	if _, err := index.Search(
		ctx,
		portScope(),
		0,
		memy.SearchOptions{Minimum: token, MaxCandidates: 1},
	); !errors.Is(
		err,
		memy.ErrInvalid,
	) {
		t.Fatal(err)
	}
	eventual := reference.Eventual[int]{Index: index}
	if !eventual.Capabilities().BoundedCandidates || eventual.Capabilities().Visibility {
		t.Fatal("eventual profile")
	}
	if _, err := eventual.Search(
		ctx,
		portScope(),
		0,
		memy.SearchOptions{Minimum: token, MaxCandidates: 1},
	); !errors.Is(
		err,
		memy.ErrUnsupported,
	) {
		t.Fatal(err)
	}
}

func TestCompositeMinimumSatisfiedWithOtherPendingEntries(t *testing.T) {
	// Arrange: coverage pending can coexist with the exact token's visibility.
	b := backend("a", candidate("x", 1))
	b.Search.(*fixtureSearch).result.Coverage[0].Status = "pending"
	b.Search.(*fixtureSearch).result.CandidatesTruncated = true
	c := reference.Composite[int]{Backends: []reference.Backend[int]{b}, RRF: reference.RRFConfig{K: 60}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	options := memy.SearchOptions{
		MaxCandidates: 1,
		Minimum:       &memy.VisibilityToken{Scope: portScope(), RecordID: "x", Revision: 1},
	}
	// Act.
	result, err := c.Search(ctx, portScope(), 0, options)
	// Assert.
	if err != nil || !result.CandidatesTruncated || !result.Coverage[0].MinimumSatisfied ||
		len(result.Candidates) != 1 {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestCompositeBackendCountAndPreCallCancellation(t *testing.T) {
	// Arrange.
	c := reference.Composite[int]{RRF: reference.RRFConfig{K: 60}}
	for n := 0; n <= memy.MaxSearchBackends; n++ {
		c.Backends = append(c.Backends, backend(strings.Repeat("a", n+1)))
	}
	// Act.
	_, err := c.Search(context.Background(), portScope(), 0, memy.SearchOptions{MaxCandidates: 1})
	// Assert.
	if !errors.Is(err, memy.ErrInvalid) {
		t.Fatal(err)
	}
	for _, b := range c.Backends {
		if len(b.Search.(*fixtureSearch).options) != 0 {
			t.Fatal("invalid config called backend")
		}
	}
	// Arrange.
	c.Backends = c.Backends[:1]
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Act.
	_, err = c.Search(ctx, portScope(), 0, memy.SearchOptions{MaxCandidates: 1})
	// Assert.
	if !errors.Is(err, context.Canceled) || len(c.Backends[0].Search.(*fixtureSearch).options) != 0 {
		t.Fatal(err)
	}
}

func TestCompositeCancellationPrecedesChildMetadataValidation(t *testing.T) {
	for _, scenario := range []string{"empty canceled", "empty deadline", "canceled context malformed success", "joined visibility deadline"} {
		t.Run(scenario, func(t *testing.T) {
			// Arrange: cancellation errors do not need a successful result envelope.
			b := backend("a")
			child := b.Search.(*fixtureSearch)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := context.Canceled
			switch scenario {
			case "empty canceled":
				child.result = memy.SearchResult{}
				child.err = context.Canceled
			case "empty deadline":
				child.result = memy.SearchResult{}
				child.err = context.DeadlineExceeded
				want = context.DeadlineExceeded
			case "canceled context malformed success":
				child.result = memy.SearchResult{
					Candidates: []memy.Candidate{{RecordID: "\x00", Score: memy.ScoreOf(math.NaN())}},
				}
				child.beforeReturn = cancel
			case "joined visibility deadline":
				child.result.Coverage[0].Status = "pending"
				child.result.Coverage[0].MinimumSatisfied = false
				child.err = errors.Join(memy.ErrVisibilityPending, context.DeadlineExceeded)
				want = context.DeadlineExceeded
			}
			c := reference.Composite[int]{
				Backends:      []reference.Backend[int]{b},
				RRF:           reference.RRFConfig{K: 60},
				AllowDegraded: true,
			}
			// Act.
			result, err := c.Search(ctx, portScope(), 0, memy.SearchOptions{MaxCandidates: 1})
			// Assert: neither degraded mode nor malformed metadata hides cancellation.
			if !errors.Is(err, want) || errors.Is(err, memy.ErrInvalid) || len(result.Candidates) != 0 {
				t.Fatalf("%+v %v want %v", result, err, want)
			}
			if scenario == "joined visibility deadline" &&
				(!errors.Is(err, memy.ErrVisibilityPending) || len(result.Coverage) != 1 || result.Coverage[0].Status != "pending") {
				t.Fatalf("lost pending guarantee: %+v %v", result, err)
			}
		})
	}
}

func TestCompositePreservesProvidedRawScoreWithDistinctRanks(t *testing.T) {
	// Arrange: policy scores and raw evidence differ; supplied ranks do not define
	// list positions after exact-revision duplicates are removed.
	aFirst := candidate("x", 1)
	aFirst.Signals = []memy.SearchSignal{{Backend: "a", Rank: 99, Score: memy.ScoreOf(99)}}
	aDuplicate := candidate("x", 1000)
	aDuplicate.Signals = []memy.SearchSignal{{Backend: "a", Rank: 100, Score: memy.ScoreOf(999)}}
	aSecond := candidate("y", 2)
	aSecond.Signals = []memy.SearchSignal{{Backend: "a", Rank: 200, Score: memy.ScoreOf(-99)}}
	a := backend("a", aFirst, aDuplicate, aSecond)
	b := backend("b", candidate("y", .01), candidate("x", .001))
	c := reference.Composite[int]{Backends: []reference.Backend[int]{a, b}, RRF: reference.RRFConfig{K: 60}}
	options := memy.SearchOptions{MaxCandidates: 3}
	// Act.
	first, err := c.Search(context.Background(), portScope(), 0, options)
	c.Backends = []reference.Backend[int]{b, a}
	second, secondErr := c.Search(context.Background(), portScope(), 0, options)
	// Assert: raw evidence survives while RRF uses distinct list ranks.
	if err != nil || secondErr != nil || !reflect.DeepEqual(first, second) || len(first.Candidates) != 2 {
		t.Fatalf("first=%+v second=%+v errs=%v %v", first, second, err, secondErr)
	}
	if first.Candidates[0].RecordID != "x" || first.Candidates[1].RecordID != "y" {
		t.Fatalf("tie order %+v", first)
	}
	expected := 1.0/61 + 1.0/62
	for n, value := range first.Candidates {
		if math.Abs(value.Score.Value-expected) > 1e-15 || len(value.Signals) != 2 {
			t.Fatalf("fusion %+v", value)
		}
		raw := 99.0
		if n == 1 {
			raw = -99
		}
		if value.Signals[0].Backend != "a" || value.Signals[0].Score.Value != raw ||
			value.Signals[0].Rank != map[int]int{0: 99, 1: 200}[n] {
			t.Fatalf("provided evidence %+v", value.Signals)
		}
	}
	if first.Candidates[0].Signals[1].Score.Value != .001 || first.Candidates[1].Signals[1].Score.Value != .01 {
		t.Fatalf("fallback evidence %+v", first.Candidates)
	}
}

func TestCompositeNeverInventsNativeScore(t *testing.T) {
	// Arrange: a child gives only a host ranking score, with no native signal.
	child := backend("rank-only", candidate("record", 100))
	child.Search.(*fixtureSearch).result.Candidates[0].Signals = nil
	c := reference.Composite[int]{Backends: []reference.Backend[int]{child}, RRF: reference.RRFConfig{K: 60}}
	// Act.
	result, err := c.Search(t.Context(), portScope(), 0, memy.SearchOptions{MaxCandidates: 1})
	// Assert: RRF has a computed score; native score remains absent.
	if err != nil || len(result.Candidates) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	got := result.Candidates[0]
	if !got.Score.Present || got.Signals[0].Score.Present || got.Signals[0].Score.Value != 0 ||
		got.Signals[0].Rank != 1 {
		t.Fatal(got)
	}
}
