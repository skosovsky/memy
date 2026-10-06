package reference_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

func packingBody(values ...string) memy.ProjectedRecallResult[string, string] {
	body := memy.ProjectedRecallResult[string, string]{
		Projections: []memy.Projection[string, string]{},
		Coverage:    []memy.Coverage{{Backend: "offline", Status: "ready"}},
		Omissions:   []memy.BudgetOmission{},
	}
	for i, value := range values {
		body.Projections = append(
			body.Projections,
			memy.Projection[string, string]{
				RecordID: string(rune('a' + i)),
				Revision: 1,
				Output:   value,
				Trust:    "data",
				Provenance: memy.Provenance[string]{
					Evidence:      "source evidence",
					Sources:       []memy.Source[string]{{ID: "source", Revision: "v1", Reference: "host://source"}},
					Losses:        []string{"compressed"},
					Uncertainties: []string{"unverified"},
				},
			},
		)
	}
	return body
}

func applyPacking(
	body memy.ProjectedRecallResult[string, string],
	selection memy.OutputSelection,
) memy.ProjectedRecallResult[string, string] {
	original := body.Projections
	body.Projections = []memy.Projection[string, string]{}
	body.Omissions = selection.Omissions
	for _, ref := range selection.Refs {
		for _, projection := range original {
			if projection.RecordID == ref.RecordID && projection.Revision == ref.Revision {
				body.Projections = append(body.Projections, projection)
			}
		}
	}
	return body
}

func TestJSONPackingWholeBody(t *testing.T) {
	// Arrange: two equal payloads keep the caller's ranked order; metadata dominates.
	ctx := context.Background()
	policy := reference.JSONPacking[string, string]{}
	body := packingBody("tiny", "tiny", strings.Repeat("x", 20000))
	expected := applyPacking(
		body,
		memy.OutputSelection{
			Refs: []memy.RevisionRef{{RecordID: "a", Revision: 1}},
			Omissions: []memy.BudgetOmission{
				{Ref: memy.RevisionRef{RecordID: "b", Revision: 1}, Reason: memy.OmittedBudget},
				{Ref: memy.RevisionRef{RecordID: "c", Revision: 1}, Reason: memy.OmittedOversized},
			},
		},
	)
	limit, err := policy.Measure(ctx, expected)
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	selection, err := policy.Select(ctx, body, limit)
	actual := applyPacking(body, selection)
	measured, measureErr := policy.Measure(ctx, actual)
	raw, marshalErr := json.Marshal(actual)
	// Assert.
	if err != nil || measureErr != nil || marshalErr != nil {
		t.Fatalf("select=%v measure=%v marshal=%v", err, measureErr, marshalErr)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("unexpected selection: %+v", selection)
	}
	if measured != uint64(len(raw)) || measured > limit || measured <= uint64(len(actual.Projections[0].Output)) {
		t.Fatalf("whole body cost %d, raw=%d, limit=%d", measured, len(raw), limit)
	}
	actual.Budget = memy.BudgetUsage{Used: math.MaxUint64, Limit: math.MaxUint64, Unit: "bytes/json", Exact: true}
	receiptRaw, err := json.Marshal(actual)
	if err != nil || string(receiptRaw) != string(raw) {
		t.Fatal("out-of-band budget changed body")
	}
	if !policy.Exact() || policy.Unit() != "bytes/json" || policy.Version() == "" {
		t.Fatal("invalid policy metadata")
	}
}

func TestJSONPackingLimits(t *testing.T) {
	ctx := context.Background()
	body := packingBody(strings.Repeat("x", 20000))
	for _, tc := range []struct {
		name   string
		limit  uint64
		reject bool
		want   error
	}{
		{"zero", 0, false, memy.ErrInvalid},
		{"metadata cannot fit", 1, false, memy.ErrBudget},
		{"explicit oversized rejection", 1000, true, memy.ErrBudget},
		{"max uint does not wrap", math.MaxUint64, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange.
			policy := reference.JSONPacking[string, string]{RejectOversized: tc.reject}
			// Act.
			selection, err := policy.Select(ctx, body, tc.limit)
			// Assert.
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if tc.want == nil && len(selection.Refs) != 1 {
				t.Fatalf("max limit lost record: %+v", selection)
			}
		})
	}
}

func TestJSONPackingCancellationAndInvalidInput(t *testing.T) {
	// Arrange.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	policy := reference.JSONPacking[string, string]{}
	body := packingBody("value")
	// Act.
	_, selectErr := policy.Select(ctx, body, math.MaxUint64)
	_, measureErr := policy.Measure(ctx, body)
	// Assert.
	if !errors.Is(selectErr, context.Canceled) || !errors.Is(measureErr, context.Canceled) {
		t.Fatalf("select=%v measure=%v", selectErr, measureErr)
	}
	// Arrange.
	body.Projections = append(body.Projections, body.Projections[0])
	// Act.
	_, err := policy.Select(context.Background(), body, math.MaxUint64)
	// Assert.
	if !errors.Is(err, memy.ErrInvalid) {
		t.Fatalf("duplicate identity accepted: %v", err)
	}
}

func TestJSONPackingAllOmittedAndEmpty(t *testing.T) {
	for _, body := range []memy.ProjectedRecallResult[string, string]{packingBody(), packingBody(strings.Repeat("x", 20000))} {
		// Arrange.
		policy := reference.JSONPacking[string, string]{}
		// Act.
		selection, err := policy.Select(context.Background(), body, 1000)
		final := applyPacking(body, selection)
		cost, measureErr := policy.Measure(context.Background(), final)
		// Assert.
		if err != nil || measureErr != nil || len(selection.Refs) != 0 || cost > 1000 {
			t.Fatalf("selection=%+v cost=%d error=%v/%v", selection, cost, err, measureErr)
		}
		if len(body.Projections) > 0 &&
			(len(selection.Omissions) != 1 || selection.Omissions[0].Reason != memy.OmittedOversized) {
			t.Fatalf("oversized omission lost: %+v", selection)
		}
	}
}

func TestJSONPackingPreservesRankedOrder(t *testing.T) {
	// Arrange: packing receives the ranker's order, which need not be lexical.
	ctx := context.Background()
	policy := reference.JSONPacking[string, string]{}
	body := packingBody("tie", "tie")
	body.Projections[0], body.Projections[1] = body.Projections[1], body.Projections[0]
	expected := applyPacking(
		body,
		memy.OutputSelection{
			Refs: []memy.RevisionRef{{RecordID: "b", Revision: 1}},
			Omissions: []memy.BudgetOmission{
				{Ref: memy.RevisionRef{RecordID: "a", Revision: 1}, Reason: memy.OmittedBudget},
			},
		},
	)
	limit, err := policy.Measure(ctx, expected)
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	selection, err := policy.Select(ctx, body, limit)
	// Assert.
	if err != nil || !reflect.DeepEqual(selection.Refs, []memy.RevisionRef{{RecordID: "b", Revision: 1}}) {
		t.Fatalf("ranked order lost: %+v %v", selection, err)
	}
}

type cancelPackingJSON struct{ Cancel context.CancelFunc }

func (v cancelPackingJSON) MarshalJSON() ([]byte, error) { v.Cancel(); return []byte(`"value"`), nil }

func TestJSONPackingMarshalFailureAndCancellation(t *testing.T) {
	// Arrange: consumer-defined marshalers remain subject to cancellation checks.
	ctx, cancel := context.WithCancel(context.Background())
	body := memy.ProjectedRecallResult[cancelPackingJSON, string]{
		Projections: []memy.Projection[cancelPackingJSON, string]{
			{RecordID: "a", Revision: 1, Output: cancelPackingJSON{Cancel: cancel}},
		},
	}
	policy := reference.JSONPacking[cancelPackingJSON, string]{}
	// Act.
	_, err := policy.Select(ctx, body, math.MaxUint64)
	// Assert.
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation during marshal ignored: %v", err)
	}
	// Arrange: invalid consumer JSON must not be represented as a successful cost.
	invalid := memy.ProjectedRecallResult[func(), string]{
		Projections: []memy.Projection[func(), string]{{RecordID: "a", Revision: 1, Output: func() {}}},
	}
	// Act.
	_, measureErr := (reference.JSONPacking[func(), string]{}).Measure(context.Background(), invalid)
	_, selectErr := (reference.JSONPacking[func(), string]{}).Select(context.Background(), invalid, math.MaxUint64)
	// Assert.
	var unsupported *json.UnsupportedTypeError
	if !errors.As(measureErr, &unsupported) || !errors.As(selectErr, &unsupported) {
		t.Fatalf("marshal failure lost: %v / %v", measureErr, selectErr)
	}
}
