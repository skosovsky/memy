package memy

import (
	"context"
	"errors"
	"time"
)

// SweepResult reports host-triggered retention cleanup through normal purge.
type SweepResult struct {
	Records          []PurgeReceipt
	ExpiredProposals int
}

// Sweep applies stored and current retention deadlines without a scheduler.
// The host supplies a stable operation prefix and triggers/retries execution.
func (e *Engine[P, R, A]) Sweep(
	ctx context.Context,
	authority A,
	scope Scope,
	purpose, operationPrefix string,
) (SweepResult, error) {
	if operationPrefix == "" {
		return SweepResult{}, ErrInvalid
	}
	decision, authErr := e.authorize(ctx, authority, scope, ActionForget, purpose)
	if authErr != nil {
		return SweepResult{}, authErr
	}
	var expired []retentionTarget
	var result SweepResult
	transactionErr := e.config.Store.Update(ctx, scope, func(b Bucket) error {
		targets, scanErr := e.expiredRecords(ctx, b, scope, e.config.Clock.Now())
		if scanErr != nil {
			return scanErr
		}
		count, purgeErr := e.expiredProposals(ctx, b, scope, e.config.Clock.Now())
		if purgeErr != nil {
			return purgeErr
		}
		expired, result.ExpiredProposals = targets, count
		return e.reauthorize(ctx, authority, scope, ActionForget, purpose, decision)
	})
	if transactionErr != nil {
		return SweepResult{}, transactionErr
	}
	for _, target := range expired {
		receipt, purgeErr := e.expireRecord(ctx, authority, scope, purpose, operationPrefix, target)
		if purgeErr != nil {
			return result, purgeErr
		}
		result.Records = append(result.Records, receipt)
	}
	// Resume durable batches after payload and original expiry have been purged.
	var pending []string
	scanErr := e.config.Store.View(ctx, scope, func(b Bucket) error {
		ids, listErr := pendingPurges(b, scope)
		if listErr != nil {
			return listErr
		}
		pending = ids
		return e.reauthorize(ctx, authority, scope, ActionForget, purpose, decision)
	})
	if scanErr != nil {
		return result, scanErr
	}
	for _, operationID := range pending {
		receipt, purgeErr := e.finishPurge(ctx, authority, scope, purpose, operationID)
		if purgeErr != nil {
			return result, purgeErr
		}
		result.Records = mergePurgeReceipt(result.Records, receipt)
	}
	return result, nil
}

type retentionTarget struct {
	Ref           RevisionRef
	PolicyVersion string
}

func (e *Engine[P, R, A]) expiredRecords(
	ctx context.Context,
	b Bucket,
	scope Scope,
	now time.Time,
) ([]retentionTarget, error) {
	history, listErr := b.List("record/")
	if listErr != nil {
		return nil, listErr
	}
	selected := make(map[string]bool)
	var targets []retentionTarget
	for _, entry := range history {
		target, expired, scanErr := e.expiredRecord(ctx, b, scope, now, entry)
		if scanErr != nil {
			return nil, scanErr
		}
		if !expired || selected[target.Ref.RecordID] {
			continue
		}
		targets = append(targets, target)
		selected[target.Ref.RecordID] = true
	}
	return targets, nil
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
	if stored.Scope != scope {
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

func (e *Engine[P, R, A]) expiredProposals(ctx context.Context, b Bucket, scope Scope, now time.Time) (int, error) {
	proposals, listErr := b.List("proposal_revision/")
	if listErr != nil {
		return 0, listErr
	}
	expiredIDs := make(map[string]bool)
	for _, entry := range proposals {
		var stored proposalDisk
		if decodeErr := decodeDocument(entry.Value.Data, "proposal", &stored); decodeErr != nil {
			return 0, decodeErr
		}
		if stored.Scope != scope {
			return 0, ErrSchema
		}
		current, policyErr := e.currentRetention(ctx, scope, stored)
		if policyErr != nil {
			return 0, policyErr
		}
		if proposalExpired(stored, now) || retentionExpired(current, now) {
			expiredIDs[stored.ID] = true
		}
	}
	for id := range expiredIDs {
		if purgeErr := purgeProposal(b, id); purgeErr != nil {
			return 0, purgeErr
		}
	}
	return len(expiredIDs), nil
}

func (e *Engine[P, R, A]) expireRecord(
	ctx context.Context,
	authority A,
	scope Scope,
	purpose, prefix string,
	target retentionTarget,
) (PurgeReceipt, error) {
	operationID, digestErr := digest(struct {
		Prefix string
		Scope  Scope
		Ref    RevisionRef
	}{prefix, scope, target.Ref})
	if digestErr != nil {
		return PurgeReceipt{}, digestErr
	}
	return e.Forget(ctx, authority, scope, purpose, ForgetRequest{
		OperationID: operationID, Selector: Selector{Kind: SelectRecord, ID: target.Ref.RecordID},
		Expected: []RevisionRef{target.Ref}, Reason: "retention expiry", PolicyVersion: target.PolicyVersion,
	})
}

func pendingPurges(b Bucket, scope Scope) ([]string, error) {
	entries, listErr := b.List("purge/")
	if listErr != nil {
		return nil, listErr
	}
	var ids []string
	for _, entry := range entries {
		var receipt PurgeReceipt
		if decodeErr := decodeDocument(entry.Value.Data, "purge", &receipt); decodeErr != nil {
			return nil, decodeErr
		}
		if receipt.Batch.Scope != scope {
			return nil, ErrSchema
		}
		if receipt.State != PurgeComplete {
			ids = append(ids, receipt.Batch.OperationID)
		}
	}
	return ids, nil
}

func mergePurgeReceipt(receipts []PurgeReceipt, receipt PurgeReceipt) []PurgeReceipt {
	for index, old := range receipts {
		if old.Batch.OperationID == receipt.Batch.OperationID {
			receipts[index] = receipt
			return receipts
		}
	}
	return append(receipts, receipt)
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
	if request.PolicyVersion == "" {
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
	entries, listErr := b.List("purge/")
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
