package quality

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

func (f *procedureFixture) retrieval(
	ctx context.Context,
	c Corpus,
	r *ScenarioReport,
	search memy.Search[ProcedureQuery],
) error {
	recall, err := f.recall(
		ctx,
		search,
		ProcedureQuery{Service: procedureService, MinimumSeverity: procedureMinimumSeverity},
	)
	if err != nil {
		return err
	}
	ok := len(recall.Records) == 2 && recall.Progress.CanonicalFiltered >= 1 && recall.Progress.CandidatesTruncated
	for _, item := range recall.Records {
		ok = ok && f.recordMatches(item.Record) && item.Record.ID != procedureDistractor && len(item.Signals) == 2
	}
	r.Effective.Checks = append(
		r.Effective.Checks,
		procedureCheck("effective-exact", ok, "two-relevant-exact-revisions", len(recall.Records)),
	)
	all, err := f.projectBody(ctx, search, nil)
	if err != nil {
		return err
	}
	if len(all.Projections) != 2 {
		return memy.ErrInvalid
	}
	target := all
	target.Projections = all.Projections[:1]
	target.Omissions = []memy.BudgetOmission{
		{
			Ref:    memy.RevisionRef{RecordID: all.Projections[1].RecordID, Revision: all.Projections[1].Revision},
			Reason: memy.OmittedBudget,
		},
	}
	packing := reference.JSONPacking[ProcedureObservation, DocumentRef]{RejectOversized: false}
	limit, err := packing.Measure(ctx, target)
	if err != nil {
		return err
	}
	if c.Budgets.ContextBytes < limit {
		limit = c.Budgets.ContextBytes
	}
	packed, err := f.project(
		ctx,
		search,
		&memy.ProjectionBudget[ProcedureObservation, DocumentRef]{
			Max:    limit,
			Codec:  memy.JSONCodec[ProcedureObservation]{},
			Policy: packing,
		},
	)
	if err != nil {
		return err
	}
	bytes, err := procedureBodyBytes(packed)
	if err != nil {
		return err
	}
	ok = f.packedMatches(packed, all, bytes, limit)
	grade, gradeErr := f.gradeProjections(ctx, packed.Projections)
	if gradeErr != nil {
		return gradeErr
	}
	ok = ok && grade
	r.Rendered.Checks = append(
		r.Rendered.Checks,
		procedureCheck("rendered-context", ok, "one-packed-exact-data-projection", len(packed.Projections)),
	)
	var payloadBytes uint64
	for _, projection := range packed.Projections {
		raw, marshalErr := json.Marshal(projection.Output)
		if marshalErr != nil {
			return marshalErr
		}
		payloadBytes += uint64(len(raw))
	}
	r.Metrics.PayloadBytes = KnownMeasurement("payload-json-bytes", payloadBytes)
	r.Metrics.ContextJSONBytes = KnownMeasurement("final-body-json-bytes", bytes)
	return f.checkCurrentRetention(ctx, r)
}

func (f *procedureFixture) abstention(
	ctx context.Context,
	r *ScenarioReport,
	search memy.Search[ProcedureQuery],
) error {
	empty, err := f.recall(ctx, search, ProcedureQuery{Service: "no-match", MinimumSeverity: procedureMinimumSeverity})
	if err != nil {
		return err
	}
	outage, err := f.recall(
		ctx,
		procedureSearch{"procedure-outage/v1", f.scope, nil, true},
		ProcedureQuery{Service: procedureService, MinimumSeverity: procedureMinimumSeverity},
	)
	outageOK := errors.Is(err, memy.ErrUnavailable) && len(outage.Records) == 0
	original := f.receipts[procedureRelevantA]
	p := procedurePayload(procedureConflictID)
	if err = f.commit(
		ctx,
		procedureConflictID,
		p,
		memy.Conflict,
		[]memy.RevisionRef{{RecordID: original.RecordID, Revision: original.Revision}},
	); err != nil {
		return err
	}
	conflicted, conflictErr := f.engine.Get(
		ctx,
		"operator",
		f.scope,
		procedureRelevantA,
		memy.ReadOptions{
			Purpose:          qualityPurpose,
			IncludeConflicts: true,
			ValidAsOf:        time.Time{},
			RecordedAsOf:     time.Time{},
			IncludeUnknown:   false,
		},
	)
	r.Effective.Checks = append(
		r.Effective.Checks,
		procedureCheck(
			"effective-exact",
			len(empty.Records) == 0 && outageOK && conflictErr == nil && conflicted.State == memy.Conflicted,
			"healthy-empty-conflict-unavailable-distinct",
			procedureOriginalCount,
		),
	)
	// Projected bodies are separate observations, not conclusions inferred from Recall.
	emptyBody, emptyBodyErr := memy.RecallProjected(
		ctx,
		f.engine,
		"operator",
		f.scope,
		ProcedureQuery{Service: "no-match", MinimumSeverity: procedureMinimumSeverity},
		search,
		memy.ScoreRanker[ProcedureObservation, DocumentRef]{},
		f.options(),
		procedureProjector(),
		&memy.ProjectionBudget[ProcedureObservation, DocumentRef]{
			Max:    f.contextLimit,
			Codec:  memy.JSONCodec[ProcedureObservation]{},
			Policy: reference.JSONPacking[ProcedureObservation, DocumentRef]{RejectOversized: false},
		},
	)
	if emptyBodyErr != nil {
		return emptyBodyErr
	}
	emptyBytes, measureErr := procedureBodyBytes(emptyBody)
	if measureErr != nil {
		return measureErr
	}
	f.contextBodies++
	f.contextMaximum = max(f.contextMaximum, emptyBytes)

	conflictSearch := procedureSearch{
		"procedure-conflict/v1",
		f.scope,
		[]memy.Candidate{
			{RecordID: procedureRelevantA, Revision: 1, Score: memy.ScoreOf(1), Signals: nil},
			{RecordID: procedureConflictID, Revision: 1, Score: memy.ScoreOf(procedureMissingScore), Signals: nil},
		},
		false,
	}
	conflictBody, conflictBodyErr := f.project(ctx, conflictSearch, nil)
	if conflictBodyErr != nil {
		return conflictBodyErr
	}
	unavailableBody, unavailableBodyErr := f.project(
		ctx,
		procedureSearch{"procedure-outage/v1", f.scope, nil, true},
		nil,
	)
	renderedOK := len(emptyBody.Projections) == 0 && len(emptyBody.Coverage) > 0 &&
		emptyBody.Coverage[0].Status != procedureUnavailable &&
		len(conflictBody.Projections) == 0 &&
		conflictBody.Progress.CanonicalFiltered == 2 &&
		errors.Is(unavailableBodyErr, memy.ErrUnavailable) &&
		len(unavailableBody.Projections) == 0
	f.describeAbstention(r, renderedOK, emptyBody, conflictBody, unavailableBody, emptyBodyErr, unavailableBodyErr)
	return f.canonical(ctx, r, procedureConflictCount)
}

func (f *procedureFixture) describeAbstention(
	r *ScenarioReport,
	renderedOK bool,
	emptyBody, conflictBody, unavailableBody memy.ProjectedRecallResult[ProcedureObservation, DocumentRef],
	emptyBodyErr, unavailableBodyErr error,
) {
	r.Rendered.Checks = append(
		r.Rendered.Checks,
		procedureCheck(
			"rendered-context",
			renderedOK,
			"projected-healthy-empty-conflict-unavailable-distinct",
			procedureOriginalCount,
		),
		procedureCheck(
			"projected-healthy-empty",
			len(emptyBody.Projections) == 0 && emptyBodyErr == nil,
			"healthy-empty-body",
			0,
		),
		procedureCheck(
			"projected-conflict",
			len(conflictBody.Projections) == 0 && conflictBody.Progress.CanonicalFiltered == 2,
			"conflict-suppressed-body",
			0,
		),
		procedureCheck(
			"projected-unavailable",
			errors.Is(unavailableBodyErr, memy.ErrUnavailable) && len(unavailableBody.Projections) == 0,
			"unavailable-fail-closed",
			0,
		),
	)
}
