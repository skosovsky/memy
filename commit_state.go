package memy

import (
	"context"
	"errors"
	"slices"
	"time"
)

type preparedCommit struct {
	Record       recordDisk
	HeadVersion  Version
	Epoch        epochDisk
	EpochVersion Version
}

func (e *Engine[P, R, A]) commitOperation(
	ctx context.Context,
	b Bucket,
	authority A,
	scope Scope,
	purpose string,
	decision Decision,
	request CommitRequest,
	requestDigest string,
) (CommitReceipt, error) {
	op, opVersion, exists, ledgerErr := operation(b, request.OperationID, "commit", requestDigest)
	if ledgerErr != nil {
		return CommitReceipt{}, ledgerErr
	}
	if exists {
		return e.replayCommit(ctx, authority, scope, purpose, decision, request, op)
	}
	proposal, acceptanceErr := e.acceptedProposal(ctx, b, scope, decision, request)
	if acceptanceErr != nil {
		return CommitReceipt{}, acceptanceErr
	}
	prepared, prepareErr := e.prepareCommit(ctx, b, scope, decision, request, proposal)
	if prepareErr != nil {
		return CommitReceipt{}, prepareErr
	}
	if reconcileErr := reconcile(
		b,
		scope,
		RevisionRef{RecordID: prepared.Record.ID, Revision: prepared.Record.Revision},
		request.Reconcile,
		prepared.Record.RecordedAt,
	); reconcileErr != nil {
		return CommitReceipt{}, reconcileErr
	}
	if authErr := e.reauthorize(ctx, authority, scope, ActionCommit, purpose, decision); authErr != nil {
		return CommitReceipt{}, authErr
	}
	if retentionErr := e.validateRetention(ctx, scope, proposal); retentionErr != nil {
		return CommitReceipt{}, retentionErr
	}
	if err := e.commitDeadlineGate(
		b,
		scope,
		request.RecordID,
		[]time.Time{proposalDeadline(proposal), request.Acceptance.ExpiresAt},
		prepared.Record.Lineage,
	); err != nil {
		return CommitReceipt{}, err
	}
	return persistCommit(b, scope, request.OperationID, requestDigest, opVersion, prepared)
}

func (e *Engine[P, R, A]) replayCommit(
	ctx context.Context,
	authority A,
	scope Scope,
	purpose string,
	decision Decision,
	request CommitRequest,
	op operationDisk,
) (CommitReceipt, error) {
	if op.Receipt == nil || op.Receipt.OperationID != request.OperationID || op.Receipt.RecordID != request.RecordID ||
		op.Receipt.Visibility.Scope != scope {
		return CommitReceipt{}, ErrSchema
	}
	if authErr := e.reauthorize(ctx, authority, scope, ActionCommit, purpose, decision); authErr != nil {
		return CommitReceipt{}, authErr
	}
	return *op.Receipt, nil
}

func (e *Engine[P, R, A]) acceptedProposal(
	ctx context.Context,
	b Bucket,
	scope Scope,
	decision Decision,
	request CommitRequest,
) (proposalDisk, error) {
	var p proposalDisk
	if _, readErr := readDocument(b, objectKey("proposal", request.ProposalID), "proposal", &p); readErr != nil {
		return proposalDisk{}, readErr
	}
	if p.Scope != scope || p.ID != request.ProposalID {
		return proposalDisk{}, ErrSchema
	}
	var acceptance Acceptance
	if _, readErr := readDocument(
		b,
		objectKey("acceptance", request.ProposalID),
		"acceptance",
		&acceptance,
	); readErr != nil {
		return proposalDisk{}, errors.Join(ErrStaleAcceptance, readErr)
	}
	if !acceptanceMatches(p, acceptance, request.Acceptance, decision, scope, e.config.Clock.Now()) {
		return proposalDisk{}, ErrStaleAcceptance
	}
	if validationErr := e.validateInputs(ctx, b, scope, p); validationErr != nil {
		return proposalDisk{}, validationErr
	}
	return p, nil
}

func acceptanceMatches(
	p proposalDisk,
	stored, requested Acceptance,
	decision Decision,
	scope Scope,
	now time.Time,
) bool {
	return sameAcceptance(stored, requested) && stored.ProposalID == p.ID && stored.Digest == p.Digest &&
		stored.ProposalRevision == p.Revision &&
		p.State == Accepted &&
		stored.Actor == decision.Actor &&
		stored.Scope == scope &&
		stored.PolicyVersion == decision.PolicyVersion &&
		(stored.ExpiresAt.IsZero() || now.Before(stored.ExpiresAt))
}

func commitHeadVersion(b Bucket, scope Scope, request CommitRequest) (Version, error) {
	var head recordDisk
	version, readErr := readDocument(b, objectKey("head", request.RecordID), "record", &head)
	if readErr != nil && !errors.Is(readErr, ErrNotFound) {
		return 0, readErr
	}
	if readErr == nil && (head.Scope != scope || head.ID != request.RecordID) {
		return 0, ErrSchema
	}
	if readErr == nil && head.State == Revoked {
		return 0, ErrRevoked
	}
	if head.Revision != request.Expected {
		return 0, ErrConflict
	}
	return version, nil
}

func (e *Engine[P, R, A]) prepareCommit(
	ctx context.Context,
	b Bucket,
	scope Scope,
	decision Decision,
	request CommitRequest,
	p proposalDisk,
) (preparedCommit, error) {
	headVersion, headErr := commitHeadVersion(b, scope, request)
	if headErr != nil {
		return preparedCommit{}, headErr
	}
	if revocationErr := validateRevocations(b, scope, request.RecordID, p.Sources); revocationErr != nil {
		return preparedCommit{}, revocationErr
	}
	epoch, epochVersion, epochErr := currentEpoch(b)
	if epochErr != nil {
		return preparedCommit{}, epochErr
	}
	recordedAt := recordedTime(e.config.Clock.Now(), epoch.RecordedAt)
	if recordedAt.IsZero() {
		return preparedCommit{}, ErrInvalid
	}
	lineage, lineageErr := commitLineage(request, p.Lineage)
	if lineageErr != nil {
		return preparedCommit{}, lineageErr
	}
	if request.Reconcile.Mode == Duplicate {
		if validationErr := e.validateReadLineage(ctx, b, lineage, scope); validationErr != nil {
			return preparedCommit{}, validationErr
		}
	}
	state := Active
	if request.Reconcile.Mode == Conflict {
		state = Conflicted
	}
	record := recordDisk{
		ID:                     request.RecordID,
		Revision:               request.Expected + 1,
		Scope:                  scope,
		Proposal:               p,
		State:                  state,
		InitialState:           state,
		RecordedAt:             recordedAt,
		AuthorityPolicyVersion: decision.PolicyVersion,
		Reconciliation:         cloneReconciliation(&request.Reconcile),
		Lineage:                lineage,
		Transitions:            nil,
	}
	epoch.RecordedAt = recordedAt
	return preparedCommit{Record: record, HeadVersion: headVersion, Epoch: epoch, EpochVersion: epochVersion}, nil
}

func commitLineage(request CommitRequest, inputs []RevisionRef) ([]RevisionRef, error) {
	lineage := slices.Clone(inputs)
	if request.Reconcile.Mode == Duplicate {
		for _, ref := range request.Reconcile.Related {
			if !slices.Contains(lineage, ref) {
				lineage = append(lineage, ref)
			}
		}
	}
	for _, ref := range lineage {
		if ref.RecordID == request.RecordID {
			return nil, ErrInvalid
		}
	}
	return lineage, nil
}

func persistCommit(
	b Bucket,
	scope Scope,
	operationID, requestDigest string,
	operationVersion Version,
	prepared preparedCommit,
) (CommitReceipt, error) {
	record := prepared.Record
	if err := persistMemberships(b, record); err != nil {
		return CommitReceipt{}, err
	}
	if saveErr := writeDocument(b, revisionKey(record.ID, record.Revision), "record", 0, record); saveErr != nil {
		return CommitReceipt{}, saveErr
	}
	if saveErr := writeDocument(
		b,
		objectKey("head", record.ID),
		"record",
		prepared.HeadVersion,
		record,
	); saveErr != nil {
		return CommitReceipt{}, saveErr
	}
	if saveErr := writeDocument(b, "epoch", "epoch", prepared.EpochVersion, prepared.Epoch); saveErr != nil {
		return CommitReceipt{}, saveErr
	}
	receipt := CommitReceipt{
		OperationID:        operationID,
		RecordID:           record.ID,
		Revision:           record.Revision,
		CanonicalCommitted: true,
		Visibility:         VisibilityToken{Scope: scope, RecordID: record.ID, Revision: record.Revision},
	}
	saveErr := writeDocument(
		b,
		objectKey("operation", operationID),
		"operation",
		operationVersion,
		operationDisk{Action: "commit", Digest: requestDigest, Proposals: nil, Receipt: &receipt, Epoch: nil},
	)
	if saveErr != nil {
		return CommitReceipt{}, saveErr
	}
	return receipt, nil
}

// Deadlines bind instants, not [time.Time]'s serialization-dependent metadata.
func sameAcceptance(a, b Acceptance) bool {
	return a.ProposalID == b.ProposalID && a.ProposalRevision == b.ProposalRevision &&
		a.Digest == b.Digest && a.Actor == b.Actor && a.Scope == b.Scope &&
		a.PolicyVersion == b.PolicyVersion && a.ExpiresAt.Equal(b.ExpiresAt)
}
