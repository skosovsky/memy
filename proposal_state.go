package memy

import (
	"context"
	"errors"
)

func (e *Engine[P, R, A]) singleProposalReplay(
	ctx context.Context,
	b Bucket,
	scope Scope,
	op operationDisk,
) (Proposal[P, R], error) {
	if len(op.Proposals) != 1 {
		return Proposal[P, R]{}, ErrSchema
	}
	proposals, loadErr := e.loadProposals(ctx, b, scope, op.Proposals)
	if loadErr != nil {
		return Proposal[P, R]{}, loadErr
	}
	return proposals[0], nil
}

func (e *Engine[P, R, A]) prepareProposal(
	ctx context.Context,
	b Bucket,
	scope Scope,
	p proposalDisk,
) (proposalDisk, error) {
	epoch, _, epochErr := currentEpoch(b)
	if epochErr != nil {
		return proposalDisk{}, epochErr
	}
	retention, policyErr := e.currentRetention(ctx, scope, p)
	if policyErr != nil {
		return proposalDisk{}, policyErr
	}
	p.Scope, p.State, p.Epoch, p.Retention = scope, Proposed, epoch.Value, retention
	if p.CreatedAt.IsZero() {
		p.CreatedAt = e.config.Clock.Now().UTC()
	}
	if p.CreatedAt.IsZero() {
		return proposalDisk{}, ErrInvalid
	}
	var digestErr error
	p.Digest, digestErr = proposalContentDigest(p)
	if digestErr != nil {
		return proposalDisk{}, digestErr
	}
	if validationErr := e.validateInputs(ctx, b, scope, p); validationErr != nil {
		return proposalDisk{}, validationErr
	}
	return p, nil
}

func saveProposalOperation(
	b Bucket,
	operationID, action, requestDigest string,
	operationVersion, proposalVersion Version,
	p proposalDisk,
) error {
	if saveErr := persistProposal(b, p, proposalVersion); saveErr != nil {
		return saveErr
	}
	return writeDocument(b, objectKey("operation", operationID), "operation", operationVersion, operationDisk{
		Action:    action,
		Digest:    requestDigest,
		Proposals: []ProposalRef{{ID: p.ID, Revision: p.Revision}},
		Receipt:   nil,
		Epoch:     nil,
	})
}

func persistAcceptance(b Bucket, p proposalDisk, version Version, decision Acceptance) error {
	var old Acceptance
	acceptanceVersion, readErr := readDocument(b, objectKey("acceptance", p.ID), "acceptance", &old)
	if readErr != nil && !errors.Is(readErr, ErrNotFound) {
		return readErr
	}
	if readErr == nil && sameAcceptance(old, decision) && p.State == Accepted {
		return nil
	}
	p.State = Accepted
	if saveErr := writeDocument(b, objectKey("proposal", p.ID), "proposal", version, p); saveErr != nil {
		return saveErr
	}
	return writeDocument(b, objectKey("acceptance", p.ID), "acceptance", acceptanceVersion, decision)
}

func proposalRevisionMatches(p proposalDisk, scope Scope, id string, expected Version) error {
	if p.Revision != expected || p.ID != id || p.Scope != scope || expected >= MaxVersion {
		return ErrConflict
	}
	return nil
}

func (e *Engine[P, R, A]) replayRevised(
	ctx context.Context,
	b Bucket,
	authority A,
	scope Scope,
	purpose string,
	decision Decision,
	op operationDisk,
	id string,
	expected Version,
) (Proposal[P, R], error) {
	result, replayErr := e.singleProposalReplay(ctx, b, scope, op)
	if replayErr != nil {
		return Proposal[P, R]{}, replayErr
	}
	if result.ID != id || expected >= MaxVersion || result.Revision != expected+1 {
		return Proposal[P, R]{}, ErrSchema
	}
	if authErr := e.reauthorize(ctx, authority, scope, ActionPropose, purpose, decision); authErr != nil {
		return Proposal[P, R]{}, authErr
	}
	return result, nil
}
