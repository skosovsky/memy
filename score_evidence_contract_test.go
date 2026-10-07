package memy_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
	"unicode/utf8"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

func TestScorePresenceContract(t *testing.T) {
	// Arrange.
	cases := []struct {
		name  string
		score memy.Score
		valid bool
		wire  string
	}{
		{"absent", memy.Score{}, true, `{"present":false,"value":0}`},
		{"zero", memy.ScoreOf(0), true, `{"present":true,"value":0}`},
		{"negative", memy.ScoreOf(-1), true, `{"present":true,"value":-1}`},
		{"ambiguous", memy.Score{Present: false, Value: 1}, false, ""},
		{"nan", memy.ScoreOf(math.NaN()), false, ""},
		{"infinite", memy.ScoreOf(math.Inf(1)), false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act.
			err := tc.score.Validate()
			// Assert.
			if tc.valid != (err == nil) {
				t.Fatalf("score=%+v err=%v", tc.score, err)
			}
			if !tc.valid {
				if !errors.Is(err, memy.ErrInvalid) {
					t.Fatal(err)
				}
				return
			}
			raw, err := json.Marshal(tc.score)
			if err != nil || string(raw) != tc.wire {
				t.Fatalf("json=%s err=%v", raw, err)
			}
			var decoded memy.Score
			if err = json.Unmarshal(raw, &decoded); err != nil || decoded != tc.score {
				t.Fatalf("roundtrip=%+v err=%v", decoded, err)
			}
		})
	}
}

func TestScoreRankerPresenceOrder(t *testing.T) {
	// Arrange: an absent result sorts after an observed negative score.
	input := []memy.Ranked[string, string]{
		{Record: memy.Record[string, string]{ID: "a"}},
		{Record: memy.Record[string, string]{ID: "b"}, Score: memy.ScoreOf(-1)},
		{Record: memy.Record[string, string]{ID: "c"}, Score: memy.ScoreOf(0)},
		{Record: memy.Record[string, string]{ID: "d"}},
	}
	// Act.
	ranked, err := (memy.ScoreRanker[string, string]{}).Rank(context.Background(), input)
	// Assert.
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, r := range ranked {
		ids = append(ids, r.Record.ID)
	}
	if !reflect.DeepEqual(ids, []string{"c", "b", "a", "d"}) {
		t.Fatal(ids)
	}
	input[0].Score = memy.Score{Value: 1}
	if _, err = (memy.ScoreRanker[string, string]{}).Rank(t.Context(), input); !errors.Is(err, memy.ErrInvalid) {
		t.Fatalf("invalid score err=%v", err)
	}
}

func TestProjectedEvidenceCanonicalAndDetached(t *testing.T) {
	// Arrange: selection attempts to rewrite native evidence.
	f := newFixture(t, nil)
	refs := []memy.RevisionRef{projectedSeed(t, f, "fact", "canonical")}
	p := &projectedPolicy{
		selectFn: func(_ context.Context, b memy.ProjectedRecallResult[projectedOutput, sourceRef]) (memy.OutputSelection, error) {
			selected := projectedSelectAll(b)
			b.Projections[0].Retrieval.Signals[0].Backend = "forged"
			b.Projections[0].Retrieval.Score = memy.ScoreOf(-1)
			return selected, nil
		},
	}
	// Act.
	got, err := projectedCall(t.Context(), f, refs, memy.ReadOptions{}, p, 100000)
	// Assert.
	if err != nil || len(got.Projections) != 1 {
		t.Fatalf("body=%+v err=%v", got, err)
	}
	projection := got.Projections[0]
	evidence := projection.Retrieval
	if evidence == nil || evidence.Score != memy.ScoreOf(1) || len(evidence.Signals) != 1 ||
		evidence.Signals[0].Backend != "test" ||
		projection.Trust != "data" {
		t.Fatalf("projection=%+v evidence=%+v", projection, evidence)
	}
	raw, err := json.Marshal(got)
	if err != nil || got.Budget.Used != uint64(len(raw)) {
		t.Fatalf("budget=%+v bytes=%d err=%v", got.Budget, len(raw), err)
	}
	projector := reference.ProjectorFunc[preference, sourceRef, string]{
		PolicyVersion: "plain",
		Apply: func(_ context.Context, r memy.Record[preference, sourceRef]) (string, error) {
			return r.Payload.Value, nil
		},
	}
	plain, err := memy.Project(t.Context(), f.engine, f.actor, f.scope, "fact", memy.ReadOptions{}, projector)
	if err != nil || plain.Retrieval != nil {
		t.Fatalf("plain retrieval=%+v err=%v", plain.Retrieval, err)
	}
}

func TestProjectedUnicodeBytesAndEstimatedUnits(t *testing.T) {
	// Arrange: code points, UTF-8 bytes and a host's estimated cost are separate units.
	f := newFixture(t, nil)
	refs := []memy.RevisionRef{projectedSeed(t, f, "unicode", "Нячанг ☕")}
	exact := &projectedPolicy{}
	// Act.
	got, err := projectedCall(t.Context(), f, refs, memy.ReadOptions{}, exact, 100000)
	// Assert: the exact cost includes both Unicode and evidence envelopes.
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if got.Budget.Used != uint64(len(raw)) || len(raw) == utf8.RuneCount(raw) || got.Projections[0].Retrieval == nil {
		t.Fatalf("budget=%+v bytes=%d runes=%d", got.Budget, len(raw), utf8.RuneCount(raw))
	}
	estimated := &projectedPolicy{
		estimated: true,
		measureFn: func(context.Context, memy.ProjectedRecallResult[projectedOutput, sourceRef]) (uint64, error) {
			return 7, nil
		},
	}
	estimate, err := projectedCall(t.Context(), f, refs, memy.ReadOptions{}, estimated, 100000)
	if err != nil || estimate.Budget.Exact || estimate.Budget.Unit != "test-units" || estimate.Budget.Used != 7 {
		t.Fatalf("estimate=%+v err=%v", estimate.Budget, err)
	}
}
