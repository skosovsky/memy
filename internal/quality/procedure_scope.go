package quality

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/skosovsky/memy"
)

func (f *procedureFixture) seedForeign(ctx context.Context) error {
	f.foreignScope = memy.Scope{Tenant: "synthetic-foreign", Namespace: f.scope.Namespace, Subject: f.scope.Subject}
	f.authority.Grant(
		"foreign-operator",
		f.foreignScope,
		"foreign-authority/v1",
		memy.ActionPropose,
		memy.ActionAccept,
		memy.ActionCommit,
		memy.ActionRead,
	)
	f.foreignSource = memy.Source[DocumentRef]{
		ID:        f.source.ID,
		Revision:  "foreign-r1",
		Reference: DocumentRef{Document: procedureForeignDocumentID, Section: "foreign-private"},
	}
	if err := f.sources.Put(f.foreignScope, f.foreignSource); err != nil {
		return err
	}
	f.foreignPayload = ProcedureObservation{
		Service:        procedureService,
		Procedure:      "synthetic-private-foreign-observation",
		ObservedResult: "synthetic-private-foreign-result",
		Preconditions:  []string{"foreign-access-only"},
	}
	proposal, err := f.engine.Remember(
		ctx,
		"foreign-operator",
		f.foreignScope,
		"foreign-proposal",
		qualityPurpose,
		memy.Suggestion[ProcedureObservation, DocumentRef]{
			Payload:    f.foreignPayload,
			Sources:    []memy.Source[DocumentRef]{f.foreignSource},
			Extractor:  "foreign-manual/v1",
			Evidence:   "synthetic foreign observation",
			ObservedAt: f.clock.Now(),
			Valid:      memy.Interval{Known: true, From: f.clock.Now(), To: time.Time{}},
			ExpiresAt:  time.Time{}, Lineage: nil, Losses: nil, Uncertainties: nil},
	)
	if err != nil {
		return err
	}
	accepted, err := f.engine.Accept(
		ctx,
		"foreign-operator",
		f.foreignScope,
		proposal.ID,
		proposal.Digest,
		proposal.Revision,
		qualityPurpose,
	)
	if err != nil {
		return err
	}
	f.foreignReceipt, err = f.engine.Commit(
		ctx,
		"foreign-operator",
		f.foreignScope,
		qualityPurpose,
		memy.CommitRequest{
			OperationID: "foreign-commit",
			ProposalID:  proposal.ID,
			Acceptance:  accepted,
			RecordID:    "foreign-observation",
			Reconcile: memy.Reconciliation{
				Mode:          memy.Append,
				PolicyVersion: "foreign-resolver/v1",
				Basis:         "synthetic foreign baseline",
				Related:       nil},
			Expected: 0},
	)
	return err
}

func (f *procedureFixture) foreignPreserved(ctx context.Context) (bool, error) {
	records, err := fullSnapshot(
		ctx,
		f.engine,
		"foreign-operator",
		f.foreignScope,
		procedureReadOptions(),
	)
	if err != nil {
		return false, err
	}
	if len(records) != 1 {
		return false, nil
	}
	record := records[0]
	return record.ID == f.foreignReceipt.RecordID && record.Revision == f.foreignReceipt.Revision &&
		record.Scope == f.foreignScope &&
		reflect.DeepEqual(record.Payload, f.foreignPayload) &&
		record.State == memy.Active &&
		len(record.Provenance.Sources) == 1 &&
		reflect.DeepEqual(record.Provenance.Sources[0], f.foreignSource) &&
		len(record.Provenance.Lineage) == 0 &&
		record.Provenance.Extractor == "foreign-manual/v1" &&
		record.Valid.Known &&
		record.Valid.From.Equal(f.clock.Now()) &&
		record.RecordedAt.Equal(f.clock.Now()) &&
		record.ExpiresAt.Equal(f.clock.Now().Add(procedureRetentionHours*time.Hour)), nil
}

func (f *procedureFixture) crossScope(ctx context.Context, r *ScenarioReport) error {
	_, foreignGetErr := f.engine.Get(
		ctx,
		"operator",
		f.foreignScope,
		f.foreignReceipt.RecordID,
		procedureReadOptions(),
	)
	_, foreignSnapshotErr := fullSnapshot(
		ctx,
		f.engine,
		"operator",
		f.foreignScope,
		procedureReadOptions(),
	)
	_, missingInAErr := f.engine.Get(
		ctx,
		"operator",
		f.scope,
		f.foreignReceipt.RecordID,
		procedureReadOptions(),
	)
	r.Canonical.Checks = append(
		r.Canonical.Checks,
		procedureCheck(
			"cross-scope-read-denied",
			errors.Is(foreignGetErr, memy.ErrUnauthorized) && errors.Is(foreignSnapshotErr, memy.ErrUnauthorized) &&
				errors.Is(missingInAErr, memy.ErrNotFound),
			"foreign-get-snapshot-denied",
			procedureOriginalCount,
		),
	)
	preserved, err := f.foreignPreserved(ctx)
	if err != nil {
		return err
	}
	r.Canonical.Checks = append(
		r.Canonical.Checks,
		procedureCheck("foreign-state-preserved", preserved, "foreign-exact-original-preserved", 1),
	)
	search := f.ports.Search(
		f.scope,
		[]memy.Candidate{
			{RecordID: "foreign-observation", Revision: 1, Score: memy.ScoreOf(procedureSecondaryScore), Signals: nil},
			{RecordID: procedureRelevantA, Revision: 1, Score: memy.ScoreOf(1), Signals: nil},
		},
	)
	actual, err := f.recall(
		ctx,
		search,
		ProcedureQuery{Service: procedureService, MinimumSeverity: procedureMinimumSeverity},
	)
	if err != nil {
		return err
	}
	ok := len(actual.Records) == 1 && actual.Progress.CanonicalFiltered == 1
	if len(actual.Records) == 1 {
		ok = ok && actual.Records[0].Record.ID == procedureRelevantA && f.recordMatches(actual.Records[0].Record)
	}
	r.Effective.Checks = append(
		r.Effective.Checks,
		procedureCheck("effective-exact", ok, "foreign-candidate-filtered-one-authorized-ref", len(actual.Records)),
	)
	body, err := f.project(ctx, search, nil)
	if err != nil {
		return err
	}
	r.Rendered.Checks = append(
		r.Rendered.Checks,
		procedureCheck(
			"rendered-context",
			f.projectionSetMatches(body.Projections, []string{procedureRelevantA}) &&
				body.Progress.CanonicalFiltered == 1,
			"foreign-context-absent-exact-authorized-output",
			len(body.Projections),
		),
	)
	return f.canonical(ctx, r, procedureOriginalCount)
}
