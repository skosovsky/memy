package quality

import (
	"context"
	"errors"
	"time"

	"github.com/skosovsky/memy"
)

func (f *procedureFixture) forget(ctx context.Context, r *ScenarioReport) error {
	ref := memy.RevisionRef{RecordID: procedureRelevantA, Revision: f.receipts[procedureRelevantA].Revision}
	fence, err := f.engine.Fence(ctx, "operator", f.scope, qualityPurpose)
	if err != nil {
		return err
	}
	err = f.engine.WithDerivedWrite(
		ctx,
		"operator",
		fence,
		qualityPurpose,
		[]memy.RevisionRef{ref},
		func(ctx context.Context) error {
			if stageErr := f.index.Stage(
				ctx,
				f.scope,
				memy.Candidate{RecordID: ref.RecordID, Revision: ref.Revision, Score: 1, Signals: nil},
			); stageErr != nil {
				return stageErr
			}
			return f.sink.Put(ctx, "synthetic-context", f.scope, []memy.RevisionRef{ref}, []byte("synthetic context"))
		},
	)
	if err != nil {
		return err
	}
	if err = f.index.Acknowledge(ctx, f.receipts[procedureRelevantA].Visibility); err != nil {
		return err
	}
	f.sink.FailPurge(memy.ErrUnavailable)
	request := memy.ForgetRequest{
		OperationID:   "forget-procedure",
		Selector:      memy.Selector{Kind: memy.SelectRecord, ID: ref.RecordID},
		Expected:      []memy.RevisionRef{ref},
		Reason:        "synthetic removal",
		PolicyVersion: "procedure-forget/v1",
		Limit:         procedurePurgeLimit,
		MaxBytes:      procedurePurgeByteCap,
	}
	pending, err := fullForget(ctx, f.engine, "operator", f.scope, qualityPurpose, request)
	if err != nil {
		return err
	}
	pendingOK := pending.State == memy.PurgePending && f.sink.Contains("synthetic-context")
	staleSearch := procedureSearch{
		"procedure-stale/v1",
		f.scope,
		[]memy.Candidate{{RecordID: ref.RecordID, Revision: ref.Revision, Score: 1, Signals: nil}},
		false,
	}
	body, pendingReadErr := f.project(ctx, staleSearch, nil)
	// Pending deletion is a known fail-closed result, not healthy search absence.
	pendingOK = pendingOK && errors.Is(pendingReadErr, memy.ErrRevoked) && len(body.Projections) == 0
	called := false
	lateErr := f.engine.WithDerivedWrite(
		ctx,
		"operator",
		fence,
		qualityPurpose,
		[]memy.RevisionRef{ref},
		func(context.Context) error { called = true; return nil },
	)
	lateOK := errors.Is(lateErr, memy.ErrStaleInput) && !called
	f.sink.FailPurge(nil)
	done, err := fullForget(ctx, f.engine, "operator", f.scope, qualityPurpose, request)
	if err != nil {
		return err
	}
	retryOK := done.State == memy.PurgeComplete && done.Batch.OperationID == pending.Batch.OperationID &&
		done.Batch.Epoch == pending.Batch.Epoch &&
		!f.sink.Contains("synthetic-context")
	indexed, err := f.recall(
		ctx,
		f.index,
		ProcedureQuery{Service: procedureService, MinimumSeverity: procedureMinimumSeverity},
	)
	if err != nil {
		return err
	}
	retryOK = retryOK && len(indexed.Records) == 0
	return f.verifyForget(ctx, r, ref, staleSearch, pendingOK, retryOK, lateOK)
}

func (f *procedureFixture) verifyForget(
	ctx context.Context,
	r *ScenarioReport,
	ref memy.RevisionRef,
	staleSearch procedureSearch,
	pendingOK, retryOK, lateOK bool,
) error {
	var err error
	// A current source revision change and removal independently forbid non-revoked records too.
	source2 := f.source
	source2.Revision = "r2"
	if err = f.sources.Put(f.scope, source2); err != nil {
		return err
	}
	_, changedErr := f.engine.Get(
		ctx,
		"operator",
		f.scope,
		"relevant-b",
		procedureReadOptions(),
	)
	f.sources.Remove(f.scope, f.source.ID)
	_, removedErr := f.engine.Get(
		ctx,
		"operator",
		f.scope,
		"relevant-b",
		procedureReadOptions(),
	)
	// Durable source revocation survives host registry restoration.
	sourceReceipt, sourceForgetErr := fullForget(
		ctx,
		f.engine,
		"operator",
		f.scope,
		qualityPurpose,
		memy.ForgetRequest{
			OperationID:   "forget-source-procedure",
			Selector:      memy.Selector{Kind: memy.SelectSource, ID: f.source.ID},
			Reason:        "synthetic source revocation",
			PolicyVersion: "procedure-forget/v1",
			Limit:         procedurePurgeLimit,
			MaxBytes:      procedurePurgeByteCap,
			Expected:      nil},
	)
	if sourceForgetErr != nil {
		return sourceForgetErr
	}
	restoredOK, restoreErr := f.verifySourceRestoration(ctx)
	if restoreErr != nil {
		return restoreErr
	}
	foreignPreserved, foreignErr := f.foreignPreserved(ctx)
	if foreignErr != nil {
		return foreignErr
	}
	sourceRevokedOK := sourceReceipt.State == memy.PurgeComplete && restoredOK
	finalBody, err := f.project(ctx, staleSearch, nil)
	if err != nil {
		return err
	}
	r.Effective.Checks = append(
		r.Effective.Checks,
		procedureCheck(
			"effective-exact",
			pendingOK && retryOK && lateOK,
			"revoked-context-absent-pending-and-retry",
			len(finalBody.Projections),
		),
		procedureCheck("pending-purge-denial", pendingOK, "pending-revocation-denied", 0),
		procedureCheck("managed-purge-retry", retryOK, "same-operation-purge-complete", 0),
	)
	r.Rendered.Checks = append(
		r.Rendered.Checks,
		procedureCheck(
			"rendered-context",
			len(finalBody.Projections) == 0 && finalBody.Progress.CanonicalFiltered == 1,
			"forbidden-rendered-context-absent",
			0,
		),
	)
	// Source checks are errors, not snapshots misread as healthy empty. Current purge retains a tombstone.
	_, revokedErr := f.currentRecord(ctx, ref.RecordID)
	r.Canonical.Checks = append(
		r.Canonical.Checks,
		procedureCheck(
			"canonical-state",
			errors.Is(revokedErr, memy.ErrNotFound),
			"canonical-revoked-identity-denied",
			1,
		),
		procedureCheck(
			"source-revision-change",
			errors.Is(changedErr, memy.ErrSourceUnavailable),
			"current-source-revision-denied",
			1,
		),
		procedureCheck("source-removal", errors.Is(removedErr, memy.ErrSourceUnavailable), "removed-source-denied", 1),
		procedureCheck("late-derived-fence", lateOK, "late-write-stale-before-callback", 0),
		procedureCheck("durable-source-revocation", sourceRevokedOK, "registry-restoration-cannot-unrevoke-source", 2),
		procedureCheck("foreign-after-forget", foreignPreserved, "foreign-canonical-preserved", 1),
	)
	return nil
}

func (f *procedureFixture) verifySourceRestoration(ctx context.Context) (bool, error) {
	if err := f.sources.Put(f.scope, f.source); err != nil {
		return false, err
	}
	_, restoredErr := f.engine.Get(
		ctx,
		"operator",
		f.scope,
		"relevant-b",
		procedureReadOptions(),
	)
	newSuggestion := memy.Suggestion[ProcedureObservation, DocumentRef]{
		Payload:    procedurePayload("restored"),
		Sources:    []memy.Source[DocumentRef]{f.source},
		Evidence:   "synthetic",
		Extractor:  f.ports.ExtractorVersion,
		ObservedAt: f.clock.Now(),
		Valid: memy.Interval{
			Known: false,
			From:  time.Time{},
			To:    time.Time{},
		},
		ExpiresAt:     time.Time{},
		Lineage:       nil,
		Losses:        nil,
		Uncertainties: nil,
	}
	_, restoredWriteErr := f.engine.Remember(
		ctx,
		"operator",
		f.scope,
		"restored-source-proposal",
		qualityPurpose,
		newSuggestion,
	)
	return errors.Is(restoredErr, memy.ErrNotFound) && errors.Is(restoredWriteErr, memy.ErrRevoked), nil
}
