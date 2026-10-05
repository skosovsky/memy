package memy

import (
	"context"
	"errors"
	"slices"
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
}

// PurgeAck proves the sink applied this exact batch under its deletion contract.
type PurgeAck struct {
	Sink        string  `json:"sink"`
	OperationID string  `json:"operation_id"`
	Epoch       Version `json:"epoch"`
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
	Batch     PurgeBatch   `json:"batch"`
	State     PurgeState   `json:"state"`
	Sinks     []SinkResult `json:"sinks"`
	RevokedAt time.Time    `json:"revoked_at"`
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
	operationErr = e.config.Store.View(ctx, scope, func(b Bucket) error {
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
// executes while a canonical transaction excludes revocation; a later Forget
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
	return e.config.Store.View(ctx, fence.Scope, func(b Bucket) error {
		epoch, _, transactionErr := currentEpoch(b)
		if transactionErr != nil {
			return transactionErr
		}
		if epoch.Value != fence.Epoch {
			return ErrStaleInput
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
	requestDigest, operationErr := operationDigest(scope, decision.Actor, purpose, request)
	if operationErr != nil {
		return PurgeReceipt{}, operationErr
	}
	operationErr = e.config.Store.Update(ctx, scope, func(b Bucket) error {
		return e.revokeOperation(ctx, b, authority, scope, purpose, decision, request, requestDigest)
	})
	if operationErr != nil {
		return PurgeReceipt{}, operationErr
	}
	return e.finishPurge(ctx, authority, scope, purpose, request.OperationID)
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

func selectedRecord(r recordDisk, selector Selector, records map[string]bool) bool {
	if selectedProposal(r.Proposal, selector, records) {
		return true
	}
	for _, ref := range r.Lineage {
		if records[ref.RecordID] {
			return true
		}
	}
	return false
}

func revokeRecords(b Bucket, scope Scope, selector Selector, now time.Time, policy string) ([]string, error) {
	selection, selectionErr := selectRevocations(b, scope, selector)
	if selectionErr != nil {
		return nil, selectionErr
	}
	ids := make([]string, 0, len(selection.Selected))
	for id := range selection.Selected {
		record, exists := selection.Heads[id]
		if !exists {
			return nil, ErrSchema
		}
		if revokeErr := revokeRecord(b, record, selection.Versions[id], now, policy); revokeErr != nil {
			return nil, revokeErr
		}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids, nil
}

func purgeProposals(b Bucket, selector Selector, recordIDs []string, origins map[string]bool) error {
	selected := make(map[string]bool, len(recordIDs))
	for _, id := range recordIDs {
		selected[id] = true
	}
	entries, operationErr := b.List("proposal_revision/")
	if operationErr != nil {
		return operationErr
	}
	for _, entry := range entries {
		var p proposalDisk
		if err := decodeDocument(entry.Value.Data, "proposal", &p); err != nil {
			return err
		}
		if selectedProposal(p, selector, selected) {
			origins[p.ID] = true
		}
	}
	for id := range origins {
		if err := purgeProposal(b, id); err != nil {
			return err
		}
	}
	return nil
}

func purgeProposal(b Bucket, id string) error {
	history, operationErr := b.List(objectKey("proposal_revision", id) + "/")
	if operationErr != nil {
		return operationErr
	}
	for _, entry := range history {
		if _, err := b.Delete(entry.Key, entry.Value.Version); err != nil {
			return err
		}
	}
	for _, kind := range []string{"proposal", "acceptance"} {
		key := objectKey(kind, id)
		value, err := b.Get(key)
		if err != nil {
			return err
		}
		if value.Data != nil {
			if _, err := b.Delete(key, value.Version); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Engine[P, R, A]) finishPurge(
	ctx context.Context, authority A, scope Scope, purpose, operationID string,
) (PurgeReceipt, error) {
	original, authErr := e.authorize(ctx, authority, scope, ActionForget, purpose)
	if authErr != nil {
		return PurgeReceipt{}, authErr
	}
	receipt, readErr := e.readPurge(ctx, scope, operationID)
	if readErr != nil {
		return PurgeReceipt{}, readErr
	}
	if receipt.State == PurgeComplete {
		if finalAuthErr := e.reauthorize(ctx, authority, scope, ActionForget, purpose, original); finalAuthErr != nil {
			return PurgeReceipt{}, finalAuthErr
		}
		return receipt, nil
	}
	for _, result := range receipt.Sinks {
		if result.Acknowledged {
			continue
		}
		if cancellationErr := ctx.Err(); cancellationErr != nil {
			return receipt, cancellationErr
		}
		status := e.purgeSink(ctx, receipt.Batch, result.Name)
		updated, updateErr := e.persistSinkResult(ctx, authority, scope, purpose, operationID, status)
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

func (e *Engine[P, R, A]) revokeOperation(
	ctx context.Context, b Bucket, authority A, scope Scope, purpose string,
	decision Decision, request ForgetRequest, requestDigest string,
) error {
	_, opVersion, exists, transactionErr := operation(b, request.OperationID, "forget", requestDigest)
	if transactionErr != nil {
		return transactionErr
	}
	if exists {
		var receipt PurgeReceipt
		if _, err := readDocument(b, objectKey("purge", request.OperationID), "purge", &receipt); err != nil {
			return err
		}
		return e.reauthorize(ctx, authority, scope, ActionForget, purpose, decision)
	}
	if expectationErr := expectedForgetRevisions(b, scope, request.Expected); expectationErr != nil {
		return expectationErr
	}
	epoch, epochVersion, transactionErr := currentEpoch(b)
	if transactionErr != nil {
		return transactionErr
	}
	if epoch.Value >= MaxVersion {
		return ErrConflict
	}
	epoch.Value++
	now := recordedTime(e.config.Clock.Now(), epoch.RecordedAt)
	if now.IsZero() {
		return ErrInvalid
	}
	epoch.RecordedAt = now
	history, transactionErr := b.List("record/")
	if transactionErr != nil {
		return transactionErr
	}
	recordIDs, transactionErr := revokeRecords(b, scope, request.Selector, now, request.PolicyVersion)
	if transactionErr != nil {
		return transactionErr
	}
	origins, originErr := revokedOrigins(scope, history, recordIDs)
	if originErr != nil {
		return originErr
	}
	if err := purgeProposals(b, request.Selector, recordIDs, origins); err != nil {
		return err
	}
	if err := e.reauthorize(ctx, authority, scope, ActionForget, purpose, decision); err != nil {
		return err
	}
	if err := writeDocument(b, "epoch", "epoch", epochVersion, epoch); err != nil {
		return err
	}
	if ledgerErr := persistRevocation(b, request, epoch.Value); ledgerErr != nil {
		return ledgerErr
	}
	receipt := PurgeReceipt{
		Batch: PurgeBatch{
			OperationID: request.OperationID,
			Scope:       scope,
			Epoch:       epoch.Value,
			Selector:    request.Selector,
			Records:     recordIDs,
		},
		State:     RevocationCommitted,
		RevokedAt: now,
		Sinks:     make([]SinkResult, 0, len(e.config.Sinks)),
	}
	for _, sink := range e.config.Sinks {
		receipt.Sinks = append(receipt.Sinks, SinkResult{Name: sink.Name(), Acknowledged: false, ErrorCode: ""})
	}
	if err := writeDocument(b, objectKey("purge", request.OperationID), "purge", 0, receipt); err != nil {
		return err
	}
	transactionErr = writeDocument(b, objectKey("operation", request.OperationID), "operation", opVersion,
		operationDisk{Action: "forget", Digest: requestDigest, Proposals: nil, Receipt: nil, Epoch: nil})
	return transactionErr
}

func expectedForgetRevisions(b Bucket, scope Scope, expectedRefs []RevisionRef) error {
	for _, expected := range expectedRefs {
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

func revokedOrigins(scope Scope, history []Entry, recordIDs []string) (map[string]bool, error) {
	origins := make(map[string]bool)
	selectedIDs := make(map[string]bool, len(recordIDs))
	for _, id := range recordIDs {
		selectedIDs[id] = true
	}
	for _, entry := range history {
		var historical recordDisk
		if err := decodeDocument(entry.Value.Data, "record", &historical); err != nil {
			return nil, err
		}
		if historical.Scope != scope || entry.Key != revisionKey(historical.ID, historical.Revision) {
			return nil, ErrSchema
		}
		if selectedIDs[historical.ID] {
			origins[historical.Proposal.ID] = true
		}
	}
	return origins, nil
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
