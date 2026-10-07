package quality

import (
	"context"
	"errors"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

func (f *procedureFixture) failures(ctx context.Context, r *ScenarioReport) error {
	original := f.ports.Extractor
	f.ports.Extractor = reference.ExtractorFunc[ProcedureInput, ProcedureObservation, DocumentRef](
		func(context.Context, ProcedureInput) ([]memy.Suggestion[ProcedureObservation, DocumentRef], error) {
			return nil, memy.ErrUnavailable
		},
	)
	providerErr := f.commit(ctx, "provider-failure", procedurePayload("failure"), memy.Append, nil)
	f.ports.Extractor = original
	_, searchErr := f.recall(
		ctx,
		procedureSearch{"procedure-failing/v1", f.scope, nil, true},
		ProcedureQuery{Service: procedureService, MinimumSeverity: procedureMinimumSeverity},
	)
	f.authority.Fail(memy.ErrUnavailable)
	_, policyErr := f.engine.Get(
		ctx,
		"operator",
		f.scope,
		procedureRelevantA,
		procedureReadOptions(),
	)
	f.authority.Fail(nil)
	search := f.ports.Search(
		f.scope,
		[]memy.Candidate{{RecordID: procedureRelevantA, Revision: 1, Score: memy.ScoreOf(1), Signals: nil}},
	)
	body, err := f.project(ctx, search, nil)
	if err != nil {
		return err
	}
	failingGrader := procedureControlledGrader{}
	_, graderErr := failingGrader.Grade(ctx, body.Projections)
	r.Execution.Checks = append(
		r.Execution.Checks,
		procedureCheck(
			"controlled-grader-executed",
			r.Versions.Grader == failingGrader.Version() && errors.Is(graderErr, memy.ErrUnavailable),
			"controlled-grader-unavailable",
			1,
		),
	)
	known := errors.Is(providerErr, memy.ErrUnavailable) && errors.Is(searchErr, memy.ErrUnavailable) &&
		errors.Is(policyErr, memy.ErrUnavailable) &&
		errors.Is(graderErr, memy.ErrUnavailable)
	r.Execution.Checks = append(
		r.Execution.Checks,
		procedureCheck(
			"known-controlled-port-errors",
			known,
			"four-controlled-unavailable-errors",
			procedureConflictCount,
		),
	)
	r.HostReview = StageResult{
		Version: procedureGuardedHostVersion,
		Checks: []Check{
			procedureCheck("error-not-rejection", known, "errors-are-unknown-not-rejection", procedureConflictCount),
		},
		Status: Unknown,
	}
	recall, err := f.recall(
		ctx,
		search,
		ProcedureQuery{Service: procedureService, MinimumSeverity: procedureMinimumSeverity},
	)
	if err != nil {
		return err
	}
	ok := len(recall.Records) == 1 && f.recordMatches(recall.Records[0].Record)
	r.Effective.Checks = append(
		r.Effective.Checks,
		procedureCheck("effective-exact", ok, "baseline-retained-after-errors", len(recall.Records)),
	)
	r.Rendered.Checks = append(
		r.Rendered.Checks,
		procedureCheck(
			"rendered-context",
			len(body.Projections) == 1 && f.projectionMatches(body.Projections[0]),
			"baseline-rendered-after-errors",
			len(body.Projections),
		),
	)
	return f.canonical(ctx, r, procedureOriginalCount)
}

type procedureControlledGrader struct{}

func (procedureControlledGrader) Version() string { return "controlled-grader/v1" }

func (procedureControlledGrader) Grade(
	ctx context.Context,
	_ []memy.Projection[ProcedureObservation, DocumentRef],
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return false, memy.ErrUnavailable
}
