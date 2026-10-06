package memy

import (
	"context"
	"errors"
	"time"
)

type retentionTarget struct {
	Ref           RevisionRef
	PolicyVersion string
}

func (e *Engine[P, R, A]) expiredRecord(
	ctx context.Context,
	b Bucket,
	scope Scope,
	now time.Time,
	entry Entry,
) (retentionTarget, bool, error) {
	var stored recordDisk
	if decodeErr := decodeDocument(entry.Value.Data, "record", &stored); decodeErr != nil {
		return retentionTarget{}, false, decodeErr
	}
	if stored.Scope != scope || entry.Key != revisionKey(stored.ID, stored.Revision) {
		return retentionTarget{}, false, ErrSchema
	}
	current, policyErr := e.currentRetention(ctx, scope, stored.Proposal)
	if policyErr != nil {
		return retentionTarget{}, false, policyErr
	}
	currentExpired := retentionExpired(current, now)
	if !proposalExpired(stored.Proposal, now) && !currentExpired {
		return retentionTarget{}, false, nil
	}
	var head recordDisk
	if _, readErr := readDocument(b, objectKey("head", stored.ID), "record", &head); readErr != nil {
		return retentionTarget{}, false, readErr
	}
	if head.Scope != scope || head.ID != stored.ID {
		return retentionTarget{}, false, ErrSchema
	}
	if head.State == Revoked {
		return retentionTarget{}, false, nil
	}
	policyVersion := stored.Proposal.Retention.PolicyVersion
	if currentExpired {
		policyVersion = current.PolicyVersion
	}
	return retentionTarget{
		Ref:           RevisionRef{RecordID: head.ID, Revision: head.Revision},
		PolicyVersion: policyVersion,
	}, true, nil
}

func retentionExpired(retention Retention, now time.Time) bool {
	return !retention.ExpiresAt.IsZero() && !now.Before(retention.ExpiresAt)
}

// ReintroductionRequest is an explicit host decision to permit new proposals
// after a scope/source/subject tombstone. Retired record IDs remain retired;
// record reintroduction must use a new ID. No forgotten payload is recovered.
type ReintroductionRequest struct {
	OperationID   string
	Selector      Selector
	ExpectedEpoch Version
	PolicyVersion string
}

// ReintroductionPolicy owns permission to retire a tombstone. Nil configuration
// keeps tombstones indefinitely; the library never silently expires a fence.
type ReintroductionPolicy[A any] interface {
	Allow(context.Context, A, Scope, ReintroductionRequest) error
}

// Reintroduce retires a tombstone only after explicit host policy approval and
// advances the epoch so jobs captured before the decision remain fenced.
func (e *Engine[P, R, A]) Reintroduce(
	ctx context.Context,
	authority A,
	scope Scope,
	purpose string,
	request ReintroductionRequest,
) (EpochFence, error) {
	if nilPort(e.config.Reintroduction) {
		return EpochFence{}, ErrUnsupported
	}
	if request.Selector.Kind == SelectRecord {
		return EpochFence{}, ErrPolicyDenied
	}
	if err := validateSelector(scope, request.Selector); err != nil {
		return EpochFence{}, err
	}
	if !validIdentifier(request.PolicyVersion) {
		return EpochFence{}, ErrPolicyDenied
	}
	decision, operationErr := e.authorize(ctx, authority, scope, ActionForget, purpose)
	if operationErr != nil {
		return EpochFence{}, operationErr
	}
	requestDigest, operationErr := operationDigest(scope, decision.Actor, purpose, request)
	if operationErr != nil {
		return EpochFence{}, operationErr
	}
	var result EpochFence
	operationErr = e.config.Store.Update(ctx, scope, func(b Bucket) error {
		fence, transactionErr := e.reintroduceOperation(
			ctx,
			b,
			authority,
			scope,
			purpose,
			decision,
			request,
			requestDigest,
		)
		result = fence
		return transactionErr
	})
	if operationErr != nil {
		return EpochFence{}, operationErr
	}
	return result, nil
}

func (e *Engine[P, R, A]) reintroduceOperation(
	ctx context.Context, b Bucket, authority A, scope Scope, purpose string,
	decision Decision, request ReintroductionRequest, requestDigest string,
) (EpochFence, error) {
	op, opVersion, exists, transactionErr := operation(b, request.OperationID, "reintroduce", requestDigest)
	if transactionErr != nil {
		return EpochFence{}, transactionErr
	}
	if exists {
		if op.Epoch == nil {
			return EpochFence{}, ErrSchema
		}
		return EpochFence{
			Scope: scope,
			Epoch: *op.Epoch,
		}, e.reauthorize(
			ctx,
			authority,
			scope,
			ActionForget,
			purpose,
			decision,
		)
	}
	epoch, version, transactionErr := currentEpoch(b)
	if transactionErr != nil {
		return EpochFence{}, transactionErr
	}
	if epoch.Value != request.ExpectedEpoch || epoch.Value >= MaxVersion {
		return EpochFence{}, ErrConflict
	}
	ledgerKey := objectKey("revocation", string(request.Selector.Kind)+"/"+request.Selector.ID)
	ledger, transactionErr := b.Get(ledgerKey)
	if transactionErr != nil {
		return EpochFence{}, transactionErr
	}
	if ledger.Data == nil {
		return EpochFence{}, ErrNotFound
	}
	var revoked revocationDisk
	if decodeErr := decodeDocument(ledger.Data, "revocation", &revoked); decodeErr != nil {
		return EpochFence{}, decodeErr
	}
	if revoked.Selector != request.Selector || revoked.Epoch > epoch.Value {
		return EpochFence{}, ErrSchema
	}
	if err := completedPurges(b, scope); err != nil {
		return EpochFence{}, err
	}
	if err := e.config.Reintroduction.Allow(ctx, authority, scope, request); err != nil {
		return EpochFence{}, errors.Join(ErrPolicyDenied, err)
	}
	if err := e.reauthorize(ctx, authority, scope, ActionForget, purpose, decision); err != nil {
		return EpochFence{}, err
	}
	if _, err := b.Delete(ledgerKey, ledger.Version); err != nil {
		return EpochFence{}, err
	}
	epoch.Value++
	if err := writeDocument(b, "epoch", "epoch", version, epoch); err != nil {
		return EpochFence{}, err
	}
	result := EpochFence{Scope: scope, Epoch: epoch.Value}
	transactionErr = writeDocument(
		b,
		objectKey("operation", request.OperationID),
		"operation",
		opVersion,
		operationDisk{
			Action:    "reintroduce",
			Digest:    requestDigest,
			Epoch:     &epoch.Value,
			Proposals: nil,
			Receipt:   nil,
		},
	)
	return result, transactionErr
}

// completedPurges requires all durable participants to finish before retiring a fence.
func completedPurges(b Bucket, scope Scope) error {
	entries, listErr := scanAll(b, "purge/")
	if listErr != nil {
		return listErr
	}
	for _, entry := range entries {
		var receipt PurgeReceipt
		if err := decodeDocument(entry.Value.Data, "purge", &receipt); err != nil {
			return err
		}
		if receipt.Batch.Scope != scope {
			return ErrSchema
		}
		if receipt.State != PurgeComplete {
			return ErrVisibilityPending
		}
	}
	return nil
}
