package memy

import (
	"context"
	"errors"
	"time"
)

// SelectorKind controls the exact host-authorized revocation boundary.
type SelectorKind string

const (
	SelectRecord  SelectorKind = "record"
	SelectSource  SelectorKind = "source"
	SelectSubject SelectorKind = "subject"
	SelectScope   SelectorKind = "scope"
)

// Selector operates inside the separately authorized exact Scope.
type Selector struct {
	Kind SelectorKind `json:"kind"`
	ID   string       `json:"id"`
}

// ForgetRequest binds idempotent deletion to a versioned host policy.
type ForgetRequest struct {
	OperationID   string
	Selector      Selector
	Expected      []RevisionRef
	Reason        string
	PolicyVersion string
	Limit         int
	MaxBytes      int
}

// PurgeState describes the durable revocation and managed cleanup boundary.
type PurgeState string

const (
	RevocationCommitted PurgeState = "revocation_committed"
	PurgePending        PurgeState = "purge_pending"
	PurgeComplete       PurgeState = "complete"
	PurgeFailed         PurgeState = "failed"
)

// PurgeBatch is a content-free durable deletion handle shared with managed sinks.
type PurgeBatch struct {
	OperationID string   `json:"operation_id"`
	Scope       Scope    `json:"scope"`
	Epoch       Version  `json:"epoch"`
	Selector    Selector `json:"selector"`
	Records     []string `json:"records"`
	Chunk       uint64   `json:"chunk"`
}

// PurgeAck proves the sink applied this exact batch under its deletion contract.
type PurgeAck struct {
	Sink        string  `json:"sink"`
	OperationID string  `json:"operation_id"`
	Epoch       Version `json:"epoch"`
	Chunk       uint64  `json:"chunk"`
}

// Sink is an explicitly managed derived-data deletion participant. Its Purge
// must be idempotent and remove all artifacts dependent on the given records.
type Sink interface {
	Name() string
	Purge(context.Context, PurgeBatch) (PurgeAck, error)
}

// SinkResult never stores external error text, which could contain deleted data.
type SinkResult struct {
	Name         string `json:"name"`
	Acknowledged bool   `json:"acknowledged"`
	ErrorCode    string `json:"error_code"`
}

// PurgeReceipt reports logical deletion from canonical and all registered sinks.
// Unmanaged responses/backups and forensic disk erasure are outside its boundary.
type PurgeReceipt struct {
	Batch             PurgeBatch   `json:"batch"`
	State             PurgeState   `json:"state"`
	Sinks             []SinkResult `json:"sinks"`
	RevokedAt         time.Time    `json:"revoked_at"`
	CanonicalComplete bool         `json:"canonical_complete"`
}

type revocationDisk struct {
	Selector      Selector `json:"selector"`
	Epoch         Version  `json:"epoch"`
	PolicyVersion string   `json:"policy_version"`
	ReasonDigest  string   `json:"reason_digest"`
}

// EpochFence captures the durable scope epoch before a derived/extraction job.
type EpochFence struct {
	Scope Scope
	Epoch Version
}

// Fence obtains a versioned scope boundary without exposing payload.
func (e *Engine[P, R, A]) Fence(ctx context.Context, authority A, scope Scope, purpose string) (EpochFence, error) {
	decision, operationErr := e.authorize(ctx, authority, scope, ActionRead, purpose)
	if operationErr != nil {
		return EpochFence{}, operationErr
	}
	var result EpochFence
	operationErr = e.config.Store.FencedView(ctx, scope, func(b Bucket) error {
		epoch, _, transactionErr := currentEpoch(b)
		if transactionErr != nil {
			return transactionErr
		}
		result = EpochFence{Scope: scope, Epoch: epoch.Value}
		return e.reauthorize(ctx, authority, scope, ActionRead, purpose, decision)
	})
	if operationErr != nil {
		return EpochFence{}, operationErr
	}
	return result, nil
}

// WithDerivedWrite fences synchronous managed writes against concurrent revoke.
// The callback must obey context and must not recursively call this store. It
// executes under explicit exact-scope exclusion against revocation; a later Forget
// purges the artifact. Failure may mean an external effect occurred.
func (e *Engine[P, R, A]) WithDerivedWrite(
	ctx context.Context,
	authority A,
	fence EpochFence,
	purpose string,
	lineage []RevisionRef,
	fn func(context.Context) error,
) error {
	if fn == nil || len(lineage) == 0 {
		return ErrInvalid
	}
	decision, operationErr := e.authorize(ctx, authority, fence.Scope, ActionRead, purpose)
	if operationErr != nil {
		return operationErr
	}
	return e.raw.FencedView(ctx, fence.Scope, func(b Bucket) error {
		epoch, _, transactionErr := currentEpoch(b)
		if transactionErr != nil {
			return transactionErr
		}
		if epoch.Value != fence.Epoch {
			return ErrStaleInput
		}
		if err := activeSweepGate(b, fence.Scope); err != nil {
			return err
		}
		if err := activePurgeGate(b, fence.Scope); err != nil {
			return err
		}
		if err := e.validateReadLineage(ctx, b, lineage, fence.Scope); err != nil {
			return err
		}
		if err := e.reauthorize(ctx, authority, fence.Scope, ActionRead, purpose, decision); err != nil {
			return err
		}
		if err := e.deadlineGate(b, fence.Scope, nil, lineage); err != nil {
			return err
		}
		return fn(ctx)
	})
}

// Forget durably fences/revokes canonical knowledge before touching any sink.
// Retry of the same request resumes only pending cleanup and creates no new
// record revisions. A sink outage returns a durable pending receipt.
func (e *Engine[P, R, A]) Forget(
	ctx context.Context,
	authority A,
	scope Scope,
	purpose string,
	request ForgetRequest,
) (PurgeReceipt, error) {
	decision, operationErr := e.authorize(ctx, authority, scope, ActionForget, purpose)
	if operationErr != nil {
		return PurgeReceipt{}, operationErr
	}
	if request.Reason == "" || !validIdentifier(request.PolicyVersion) {
		return PurgeReceipt{}, ErrPolicyDenied
	}
	if err := validateSelector(scope, request.Selector); err != nil {
		return PurgeReceipt{}, err
	}
	if request.Limit < 1 || request.Limit > 1024 || request.MaxBytes <= 0 || len(request.Expected) > 256 {
		return PurgeReceipt{}, ErrInvalid
	}
	semantic := request
	semantic.Limit, semantic.MaxBytes = 0, 0
	requestDigest, operationErr := operationDigest(scope, decision.Actor, purpose, semantic)
	if operationErr != nil {
		return PurgeReceipt{}, operationErr
	}
	operationErr = e.raw.Update(ctx, scope, func(b Bucket) error {
		if err := activeSweepGate(b, scope); err != nil {
			return err
		}
		return e.beginPurge(ctx, b, authority, scope, purpose, decision, request, requestDigest)
	})
	if operationErr != nil {
		return PurgeReceipt{}, operationErr
	}
	receipt, used, operationErr := e.advancePurge(ctx, authority, scope, purpose, request)
	if operationErr != nil {
		return PurgeReceipt{}, operationErr
	}
	if receipt.State == RevocationCommitted {
		return receipt, nil
	}
	return e.finishPurge(ctx, authority, scope, purpose, request.OperationID, request.Limit-used)
}

func validateSelector(scope Scope, selector Selector) error {
	switch selector.Kind {
	case SelectRecord, SelectSource:
		if !validIdentifier(selector.ID) {
			return ErrInvalid
		}
	case SelectSubject:
		if selector.ID != scope.Subject {
			return ErrScopeViolation
		}
	case SelectScope:
		if selector.ID != "" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func validateRevocations(b Bucket, scope Scope, recordID string, sources []sourceDisk) error {
	selectors := []Selector{{Kind: SelectScope, ID: ""}, {Kind: SelectSubject, ID: scope.Subject}}
	if recordID != "" {
		selectors = append(selectors, Selector{Kind: SelectRecord, ID: recordID})
	}
	for _, source := range sources {
		selectors = append(selectors, Selector{Kind: SelectSource, ID: source.ID})
	}
	for _, selector := range selectors {
		value, err := b.Get(objectKey("revocation", string(selector.Kind)+"/"+selector.ID))
		if err != nil {
			return err
		}
		if value.Data != nil {
			return ErrRevoked
		}
	}
	return nil
}

func selectedProposal(p proposalDisk, selector Selector, records map[string]bool) bool {
	if selector.Kind == SelectScope || selector.Kind == SelectSubject {
		return true
	}
	if selector.Kind == SelectSource {
		for _, source := range p.Sources {
			if source.ID == selector.ID {
				return true
			}
		}
	}
	for _, ref := range p.Lineage {
		if records[ref.RecordID] {
			return true
		}
	}
	return false
}

func (e *Engine[P, R, A]) finishPurge(
	ctx context.Context, authority A, scope Scope, purpose, operationID string, limit int,
) (PurgeReceipt, error) {
	original, authErr := e.authorize(ctx, authority, scope, ActionForget, purpose)
	if authErr != nil {
		return PurgeReceipt{}, authErr
	}
	receipt, readErr := e.readPurge(ctx, scope, operationID)
	if readErr != nil {
		return PurgeReceipt{}, readErr
	}
	if receipt.State == PurgeComplete || receipt.State == RevocationCommitted {
		if finalAuthErr := e.reauthorize(ctx, authority, scope, ActionForget, purpose, original); finalAuthErr != nil {
			return PurgeReceipt{}, finalAuthErr
		}
		return receipt, nil
	}
	attempts := 0
	for _, result := range receipt.Sinks {
		if result.Acknowledged {
			continue
		}
		if attempts >= limit {
			break
		}
		attempts++
		if cancellationErr := ctx.Err(); cancellationErr != nil {
			return receipt, cancellationErr
		}
		status := e.purgeSink(ctx, receipt.Batch, result.Name)
		updated, updateErr := e.persistSinkResult(ctx, authority, scope, purpose, operationID, receipt.Batch.Chunk, status)
		if updateErr != nil {
			return receipt, updateErr
		}
		receipt = updated
	}
	if len(receipt.Sinks) == 0 {
		updated, updateErr := e.persistSinkResult(
			ctx,
			authority,
			scope,
			purpose,
			operationID,
			receipt.Batch.Chunk,
			SinkResult{Name: "", Acknowledged: false, ErrorCode: ""},
		)
		if updateErr != nil {
			return receipt, updateErr
		}
		receipt = updated
	}
	if finalAuthErr := e.reauthorize(ctx, authority, scope, ActionForget, purpose, original); finalAuthErr != nil {
		return PurgeReceipt{}, finalAuthErr
	}
	return receipt, nil
}

func expectedForgetRevisions(b Bucket, scope Scope, expectedRefs []RevisionRef) error {
	for _, expected := range expectedRefs {
		if !validRef(expected) {
			return ErrInvalid
		}
		var record recordDisk
		if _, err := readDocument(b, objectKey("head", expected.RecordID), "record", &record); err != nil {
			return errors.Join(ErrConflict, err)
		}
		if record.Revision != expected.Revision || record.Scope != scope || record.ID != expected.RecordID {
			return ErrConflict
		}
	}
	return nil
}

func persistRevocation(b Bucket, request ForgetRequest, epoch Version) error {
	reasonDigest, _ := digest(request.Reason)
	ledgerKey := objectKey("revocation", string(request.Selector.Kind)+"/"+request.Selector.ID)
	old, transactionErr := b.Get(ledgerKey)
	if transactionErr != nil {
		return transactionErr
	}
	if err := writeDocument(
		b,
		ledgerKey,
		"revocation",
		old.Version,
		revocationDisk{
			Selector:      request.Selector,
			Epoch:         epoch,
			PolicyVersion: request.PolicyVersion,
			ReasonDigest:  reasonDigest,
		},
	); err != nil {
		return err
	}
	return nil
}
