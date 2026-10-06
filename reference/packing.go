package reference

import (
	"context"
	"encoding/json"

	"github.com/skosovsky/memy"
)

// JSONPacking greedily preserves the supplied ranked order using exact bytes of
// [encoding/json.Marshal] on the complete ProjectedRecallResult. Budget receipts
// are out of band. Hosts adding a transport envelope must measure it separately.
// RejectOversized fails with ErrBudget instead of reporting oversized omissions.
// Inputs and custom JSON marshalers must have bounded size and execution time.
type JSONPacking[O, R any] struct {
	RejectOversized bool
}

func (JSONPacking[O, R]) Version() string { return "json-packing/v1" }
func (JSONPacking[O, R]) Unit() string    { return "bytes/json" }
func (JSONPacking[O, R]) Exact() bool     { return true }

// Measure includes projection envelopes, provenance, coverage and omissions.
func (JSONPacking[O, R]) Measure(ctx context.Context, body memy.ProjectedRecallResult[O, R]) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return uint64(len(raw)), nil
}

// Select considers each projection once in ranked order. Oversized means that
// its singleton body (with shared coverage/progress and no other omissions)
// cannot fit. Trial bodies include every outstanding omission, so their costs
// cannot be mistaken for the sum of payload costs. The metadata-only body must
// fit even when no projection can be selected.
func (p JSONPacking[O, R]) Select(
	ctx context.Context,
	input memy.ProjectedRecallResult[O, R],
	limit uint64,
) (memy.OutputSelection, error) {
	if err := ctx.Err(); err != nil {
		return memy.OutputSelection{}, err
	}
	if limit == 0 || len(input.Projections) > memy.MaxSearchCandidates || len(input.Omissions) != 0 {
		return memy.OutputSelection{}, memy.ErrInvalid
	}
	omissions, err := p.classifyOmissions(ctx, input, limit)
	if err != nil {
		return memy.OutputSelection{}, err
	}
	selected := make([]bool, len(input.Projections))
	body := func() memy.ProjectedRecallResult[O, R] { return packedBody(input, selected, omissions) }
	baseCost, err := p.Measure(ctx, body())
	if err != nil {
		return memy.OutputSelection{}, err
	}
	if baseCost > limit {
		return memy.OutputSelection{}, memy.ErrBudget
	}
	for i := range selected {
		if omissions[i].Reason == memy.OmittedOversized {
			continue
		}
		selected[i] = true
		cost, err := p.Measure(ctx, body())
		if err != nil {
			return memy.OutputSelection{}, err
		}
		if cost > limit {
			selected[i] = false
		}
	}
	final := body()
	refs := make([]memy.RevisionRef, 0, len(final.Projections))
	for _, projection := range final.Projections {
		refs = append(refs, memy.RevisionRef{RecordID: projection.RecordID, Revision: projection.Revision})
	}
	if err := ctx.Err(); err != nil {
		return memy.OutputSelection{}, err
	}
	return memy.OutputSelection{Refs: refs, Omissions: final.Omissions}, nil
}

func (p JSONPacking[O, R]) classifyOmissions(
	ctx context.Context,
	input memy.ProjectedRecallResult[O, R],
	limit uint64,
) ([]memy.BudgetOmission, error) {
	omissions := make([]memy.BudgetOmission, len(input.Projections))
	seen := make(map[memy.RevisionRef]bool, len(input.Projections))
	for i, projection := range input.Projections {
		ref := memy.RevisionRef{RecordID: projection.RecordID, Revision: projection.Revision}
		if ref.Validate() != nil || seen[ref] {
			return nil, memy.ErrInvalid
		}
		seen[ref] = true
		singleton := input
		singleton.Projections = []memy.Projection[O, R]{projection}
		singleton.Omissions = []memy.BudgetOmission{}
		cost, err := p.Measure(ctx, singleton)
		if err != nil {
			return nil, err
		}
		reason := memy.OmittedBudget
		if cost > limit {
			if p.RejectOversized {
				return nil, memy.ErrBudget
			}
			reason = memy.OmittedOversized
		}
		omissions[i] = memy.BudgetOmission{Ref: ref, Reason: reason}
	}

	return omissions, nil
}

func packedBody[O, R any](
	input memy.ProjectedRecallResult[O, R],
	selected []bool,
	omissions []memy.BudgetOmission,
) memy.ProjectedRecallResult[O, R] {
	result := input
	result.Projections = make([]memy.Projection[O, R], 0, len(selected))
	result.Omissions = make([]memy.BudgetOmission, 0, len(selected))
	for i, projection := range input.Projections {
		if selected[i] {
			result.Projections = append(result.Projections, projection)
		} else {
			result.Omissions = append(result.Omissions, omissions[i])
		}
	}
	return result
}
