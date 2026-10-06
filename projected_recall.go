package memy

import (
	"context"
	"slices"
)

// BudgetOmission explains the removal of an otherwise authorized projection.
type BudgetOmission struct {
	Ref    RevisionRef `json:"ref"`
	Reason string      `json:"reason"`
}

const (
	OmittedBudget    = "budget"
	OmittedOversized = "oversized"
)

// OutputSelection contains identities only, never replacement content.
type OutputSelection struct {
	Refs      []RevisionRef
	Omissions []BudgetOmission
}

// BudgetUsage is out-of-band metadata, not part of the measured JSON body.
type BudgetUsage struct {
	Unit  string
	Used  uint64
	Limit uint64
	Exact bool
}

// ProjectedRecallResult preserves the whole measured consumer body.
type ProjectedRecallResult[O, R any] struct {
	Projections []Projection[O, R] `json:"projections"`
	Coverage    []Coverage         `json:"coverage"`
	Progress    RecallProgress     `json:"progress"`
	Omissions   []BudgetOmission   `json:"omissions"`
	Budget      BudgetUsage        `json:"-"`
}

// OutputPolicy owns meaningful units and selection. Measure is a deterministic,
// bounded, cancellation-aware cost of the actual final body, not payload bytes.
// Exact describes the host representation; core cannot verify a tokenizer/model.
type OutputPolicy[O, R any] interface {
	Version() string
	Unit() string
	Exact() bool
	Select(context.Context, ProjectedRecallResult[O, R], uint64) (OutputSelection, error)
	Measure(context.Context, ProjectedRecallResult[O, R]) (uint64, error)
}

// ProjectionBudget supplies a detached output codec and one host packing policy.
type ProjectionBudget[O, R any] struct {
	Max    uint64
	Codec  Codec[O]
	Policy OutputPolicy[O, R]
}

// RecallProjected is the single ranked/projected budget flow. A nil budget
// selects every ranked projection. Errors never certify a partial output body.
func RecallProjected[P, R, Q, A, O any](
	ctx context.Context, e *Engine[P, R, A], authority A, scope Scope, query Q,
	search Search[Q], ranker Ranker[P, R], options RecallOptions,
	projector Projector[P, R, O], budget *ProjectionBudget[O, R],
) (ProjectedRecallResult[O, R], error) {
	if e == nil || nilPort(projector) || !validIdentifier(projector.Version()) {
		return ProjectedRecallResult[O, R]{}, ErrInvalid
	}
	packing, budgetUnit, budgetExact, err := captureProjectionBudget(budget)
	if err != nil {
		return ProjectedRecallResult[O, R]{}, err
	}
	decision, err := e.authorize(ctx, authority, scope, ActionRead, options.Read.Purpose)
	if err != nil {
		return ProjectedRecallResult[O, R]{}, err
	}
	recalled, err := Recall(ctx, e, authority, scope, query, search, ranker, options)
	if err != nil {
		return ProjectedRecallResult[O, R]{}, err
	}
	body := ProjectedRecallResult[O, R]{
		Projections: make([]Projection[O, R], 0, len(recalled.Records)),
		Coverage:    slices.Clone(recalled.Coverage), Progress: recalled.Progress,
		Omissions: []BudgetOmission{}, Budget: BudgetUsage{Unit: "", Used: 0, Limit: 0, Exact: false},
	}
	body, projectedRefs, projectedStates, err := projectRecalled(
		ctx,
		e,
		authority,
		scope,
		decision,
		options.Read,
		projector,
		recalled,
		body,
	)
	if err != nil {
		return ProjectedRecallResult[O, R]{}, err
	}
	if budget != nil {
		body, err = packProjectedBody(ctx, e, body, packing, budgetUnit, budgetExact)
		if err != nil {
			return ProjectedRecallResult[O, R]{}, err
		}
	}
	// Revalidate omitted refs too: their identity/omission metadata is returned.
	err = revalidateProjected(ctx, e, authority, scope, decision, options.Read, projectedRefs, projectedStates)
	if err != nil {
		return ProjectedRecallResult[O, R]{}, err
	}
	return body, nil
}

func applyOutputSelection[O, R any](
	body ProjectedRecallResult[O, R],
	selection OutputSelection,
) (ProjectedRecallResult[O, R], error) {
	if len(selection.Refs) > len(body.Projections) || len(selection.Omissions) > len(body.Projections) {
		return ProjectedRecallResult[O, R]{}, ErrInvalid
	}
	originals := make(map[RevisionRef]Projection[O, R], len(body.Projections))
	for _, p := range body.Projections {
		originals[RevisionRef{p.RecordID, p.Revision}] = p
	}
	body.Projections = make([]Projection[O, R], 0, len(selection.Refs))
	body.Omissions = slices.Clone(selection.Omissions)
	for _, ref := range selection.Refs {
		p, ok := originals[ref]
		if !ok {
			return ProjectedRecallResult[O, R]{}, ErrInvalid
		}
		delete(originals, ref)
		body.Projections = append(body.Projections, p)
	}
	for _, omission := range selection.Omissions {
		if omission.Reason != OmittedBudget && omission.Reason != OmittedOversized {
			return ProjectedRecallResult[O, R]{}, ErrInvalid
		}
		if _, ok := originals[omission.Ref]; !ok {
			return ProjectedRecallResult[O, R]{}, ErrInvalid
		}
		delete(originals, omission.Ref)
	}
	if len(originals) != 0 {
		return ProjectedRecallResult[O, R]{}, ErrInvalid
	}
	return body, nil
}

func cloneProjectedBody[P, R, A, O any](
	e *Engine[P, R, A],
	body ProjectedRecallResult[O, R],
	codec Codec[O],
) (ProjectedRecallResult[O, R], error) {
	body.Coverage = slices.Clone(body.Coverage)
	body.Omissions = slices.Clone(body.Omissions)
	body.Projections = slices.Clone(body.Projections)
	for i := range body.Projections {
		p := &body.Projections[i]
		raw, err := codec.Encode(p.Output)
		if err != nil {
			return ProjectedRecallResult[O, R]{}, err
		}
		p.Output, err = codec.Decode(raw)
		if err != nil {
			return ProjectedRecallResult[O, R]{}, err
		}
		p.Reconciliation = cloneReconciliation(p.Reconciliation)
		p.Provenance.Lineage = slices.Clone(p.Provenance.Lineage)
		p.Provenance.Losses = slices.Clone(p.Provenance.Losses)
		p.Provenance.Uncertainties = slices.Clone(p.Provenance.Uncertainties)
		p.Provenance.Sources = slices.Clone(p.Provenance.Sources)
		for j := range p.Provenance.Sources {
			source := &p.Provenance.Sources[j]
			raw, err := e.config.ReferenceCodec.Encode(source.Reference)
			if err != nil {
				return ProjectedRecallResult[O, R]{}, err
			}
			source.Reference, err = e.config.ReferenceCodec.Decode(raw)
			if err != nil {
				return ProjectedRecallResult[O, R]{}, err
			}
		}
	}
	return body, nil
}

func captureProjectionBudget[O, R any](budget *ProjectionBudget[O, R]) (ProjectionBudget[O, R], string, bool, error) {
	// Capture the requested bound and its representation before host callbacks.
	var packing ProjectionBudget[O, R]
	var budgetUnit string
	var budgetExact bool
	if budget != nil {
		packing = *budget
		if packing.Max == 0 || nilPort(packing.Codec) || nilPort(packing.Policy) ||
			!validIdentifier(packing.Codec.Version()) || !validIdentifier(packing.Policy.Version()) {
			return packing, "", false, ErrInvalid
		}
		budgetUnit, budgetExact = packing.Policy.Unit(), packing.Policy.Exact()
		if !validIdentifier(budgetUnit) {
			return packing, "", false, ErrInvalid
		}
	}
	return packing, budgetUnit, budgetExact, nil
}

func projectRecalled[P, R, A, O any](
	ctx context.Context,
	e *Engine[P, R, A],
	authority A,
	scope Scope,
	decision Decision,
	read ReadOptions,
	projector Projector[P, R, O],
	recalled RecallResult[P, R],
	body ProjectedRecallResult[O, R],
) (ProjectedRecallResult[O, R], []RevisionRef, map[RevisionRef]RecordState, error) {
	projectedRefs := make([]RevisionRef, 0, len(recalled.Records))
	projectedStates := make(map[RevisionRef]RecordState, len(recalled.Records))
	for _, item := range recalled.Records {
		if err := ctx.Err(); err != nil {
			return body, nil, nil, err
		}
		ref := RevisionRef{item.Record.ID, item.Record.Revision}
		var initial Record[P, R]
		err := e.config.Store.FencedView(ctx, scope, func(b Bucket) error {
			_, record, err := e.projectionRecord(ctx, b, scope, ref.RecordID, ref.Revision, read)
			if err != nil {
				return err
			}
			if err := e.reauthorize(ctx, authority, scope, ActionRead, read.Purpose, decision); err != nil {
				return err
			}
			initial = record
			return nil
		})
		if err != nil {
			return body, nil, nil, err
		}
		projection, err := finishProjection(ctx, e, authority, scope, decision, initial, read, projector)
		if err != nil {
			return body, nil, nil, err
		}
		body.Projections = append(body.Projections, projection)
		projectedRefs = append(projectedRefs, ref)
		projectedStates[ref] = projection.State
	}
	return body, projectedRefs, projectedStates, nil
}

func packProjectedBody[P, R, A, O any](
	ctx context.Context,
	e *Engine[P, R, A],
	body ProjectedRecallResult[O, R],
	packing ProjectionBudget[O, R],
	budgetUnit string,
	budgetExact bool,
) (ProjectedRecallResult[O, R], error) {
	var err error
	// Detach projector-owned output before any policy sees it.
	body, err = cloneProjectedBody(e, body, packing.Codec)
	if err != nil {
		return ProjectedRecallResult[O, R]{}, err
	}
	input, err := cloneProjectedBody(e, body, packing.Codec)
	if err != nil {
		return ProjectedRecallResult[O, R]{}, err
	}
	selected, err := packing.Policy.Select(ctx, input, packing.Max)
	if err != nil {
		return ProjectedRecallResult[O, R]{}, err
	}
	if cancellation := ctx.Err(); cancellation != nil {
		return ProjectedRecallResult[O, R]{}, cancellation
	}
	body, err = applyOutputSelection(body, selected)
	if err != nil {
		return ProjectedRecallResult[O, R]{}, err
	}
	measured, err := cloneProjectedBody(e, body, packing.Codec)
	if err != nil {
		return ProjectedRecallResult[O, R]{}, err
	}
	used, err := packing.Policy.Measure(ctx, measured)
	if err != nil {
		return ProjectedRecallResult[O, R]{}, err
	}
	if cancellation := ctx.Err(); cancellation != nil {
		return ProjectedRecallResult[O, R]{}, cancellation
	}
	if used == 0 {
		return ProjectedRecallResult[O, R]{}, ErrInvalid
	}
	if used > packing.Max {
		return ProjectedRecallResult[O, R]{}, ErrBudget
	}
	body.Budget = BudgetUsage{budgetUnit, used, packing.Max, budgetExact}

	return body, nil
}

func revalidateProjected[P, R, A any](
	ctx context.Context,
	e *Engine[P, R, A],
	authority A,
	scope Scope,
	decision Decision,
	read ReadOptions,
	projectedRefs []RevisionRef,
	projectedStates map[RevisionRef]RecordState,
) error {
	return e.config.Store.FencedView(ctx, scope, func(b Bucket) error {
		ranked := make([]Ranked[P, R], 0, len(projectedRefs))
		for _, ref := range projectedRefs {
			disk, record, err := e.projectionRecord(ctx, b, scope, ref.RecordID, ref.Revision, read)
			if err != nil {
				return err
			}
			// Preserve the measured body: a still-readable revision can have
			// transitioned to superseded/conflict during a policy callback.
			if record.State != projectedStates[ref] {
				return ErrStaleInput
			}
			if err := e.validateCurrentRead(ctx, b, scope, disk); err != nil {
				return err
			}
			ranked = append(ranked, Ranked[P, R]{Record: record, Score: 0, Explanation: "", Signals: nil})
		}
		return e.completeRecall(ctx, b, authority, scope, read.Purpose, decision, ranked)
	})
}
