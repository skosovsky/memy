package memy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
)

const (
	purgePhaseSeed          = "seed"
	purgePartHistory        = "history"
	lifecycleRecordKind     = "record"
	lifecycleProposalKind   = "proposal"
	purgePhaseAck           = "ack"
	lifecyclePhaseDone      = "done"
	purgePhaseEmit          = "emit"
	purgePhaseExpand        = "expand"
	lifecyclePhaseRecords   = "records"
	lifecyclePhaseProposals = "proposals"
	purgePhaseOrigins       = "origins"
	purgePartMembers        = "members"
	purgePartFinish         = "finish"
	sweepPhaseOrigin        = "origin"
)

type activePurgeDisk struct {
	Scope       Scope  `json:"scope"`
	OperationID string `json:"operation_id"`
}
type purgeJobDisk struct {
	Scope       Scope  `json:"scope"`
	OperationID string `json:"operation_id"`
	Phase       string `json:"phase"`
	Resume      string `json:"resume"`
	After       string `json:"after"`
	Queued      uint64 `json:"queued"`
	Next        uint64 `json:"next"`
	Clean       uint64 `json:"clean"`
	Part        string `json:"part"`
	Origin      string `json:"origin"`
}
type purgeItemDisk struct {
	Scope       Scope   `json:"scope"`
	OperationID string  `json:"operation_id"`
	ID          string  `json:"id"`
	Revision    Version `json:"revision"`
	Kind        string  `json:"kind"`
}

// lifecycleStore enforces the durable temporary scope fence on Engine canonical
// access. Raw Store access remains privileged; purge maintenance uses Engine.raw.
type lifecycleStore struct{ Store }

func (s lifecycleStore) View(ctx context.Context, scope Scope, fn func(Bucket) error) error {
	return s.Store.View(ctx, scope, func(b Bucket) error {
		if guardErr := activePurgeGate(b, scope); guardErr != nil {
			return guardErr
		}
		if guardErr := activeSweepGate(b, scope); guardErr != nil {
			return guardErr
		}
		return fn(b)
	})
}
func (s lifecycleStore) FencedView(ctx context.Context, scope Scope, fn func(Bucket) error) error {
	return s.Store.FencedView(ctx, scope, func(b Bucket) error {
		if guardErr := activePurgeGate(b, scope); guardErr != nil {
			return guardErr
		}
		if guardErr := activeSweepGate(b, scope); guardErr != nil {
			return guardErr
		}
		return fn(b)
	})
}
func (s lifecycleStore) Update(ctx context.Context, scope Scope, fn func(Bucket) error) error {
	return s.Store.Update(ctx, scope, func(b Bucket) error {
		if guardErr := activePurgeGate(b, scope); guardErr != nil {
			return guardErr
		}
		if guardErr := activeSweepGate(b, scope); guardErr != nil {
			return guardErr
		}
		return fn(b)
	})
}
func activePurgeGate(b Bucket, scope Scope) error {
	var active activePurgeDisk
	_, err := readDocument(b, "active_purge", "purge-active", &active)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if active.Scope != scope {
		return ErrSchema
	}
	return ErrRevoked
}
func jobPrefix(id string) string             { return objectKey("purge_work", id) + "/" }
func selectedJobKey(op, id string) string    { return jobPrefix(op) + objectKey("selected", id) }
func queueJobKey(op string, n uint64) string { return fmt.Sprintf("%squeue/%020d", jobPrefix(op), n) }
func originJobKey(op, id string) string      { return jobPrefix(op) + objectKey(purgePhaseOrigins, id) }

func (e *Engine[P, R, A]) beginPurge(
	ctx context.Context,
	b Bucket,
	authority A,
	scope Scope,
	purpose string,
	decision Decision,
	request ForgetRequest,
	requestDigest string,
) error {
	_, opVersion, exists, operationErr := operation(b, request.OperationID, "forget", requestDigest)
	if operationErr != nil {
		return operationErr
	}
	if exists {
		return e.reauthorize(ctx, authority, scope, ActionForget, purpose, decision)
	}
	if guardErr := activePurgeGate(b, scope); guardErr != nil {
		return guardErr
	}
	if guardErr := expectedForgetRevisions(b, scope, request.Expected); guardErr != nil {
		return guardErr
	}
	epoch, epochVersion, err := currentEpoch(b)
	if err != nil {
		return err
	}
	if epoch.Value >= MaxVersion {
		return ErrConflict
	}
	epoch.Value++
	epoch.RecordedAt = recordedTime(e.config.Clock.Now(), epoch.RecordedAt)
	if epoch.RecordedAt.IsZero() {
		return ErrInvalid
	}
	if guardErr := e.reauthorize(ctx, authority, scope, ActionForget, purpose, decision); guardErr != nil {
		return guardErr
	}
	if guardErr := writeDocument(b, "epoch", "epoch", epochVersion, epoch); guardErr != nil {
		return guardErr
	}
	if guardErr := persistRevocation(b, request, epoch.Value); guardErr != nil {
		return guardErr
	}
	activeVersion, err := b.Get("active_purge")
	if err != nil {
		return err
	}
	if err := writeDocument(
		b,
		"active_purge",
		"purge-active",
		activeVersion.Version,
		activePurgeDisk{scope, request.OperationID},
	); err != nil {
		return err
	}
	job := purgeJobDisk{
		Scope:       scope,
		OperationID: request.OperationID,
		Phase:       purgePhaseSeed,
		Resume:      "", After: "", Queued: 0, Origin: "",
		Next:  1,
		Clean: 1,
		Part:  purgePartHistory,
	}
	if guardErr := writeDocument(b, objectKey("purge_job", request.OperationID), "purge-job", 0, job); guardErr != nil {
		return guardErr
	}
	receipt := PurgeReceipt{
		Batch: PurgeBatch{
			OperationID: request.OperationID,
			Scope:       scope,
			Epoch:       epoch.Value,
			Selector:    request.Selector,
			Records:     []string{},
			Chunk:       0,
		},
		State:             RevocationCommitted,
		RevokedAt:         epoch.RecordedAt,
		Sinks:             []SinkResult{},
		CanonicalComplete: false,
	}
	for _, sink := range e.config.Sinks {
		receipt.Sinks = append(receipt.Sinks, SinkResult{Name: sink.Name(), Acknowledged: false, ErrorCode: ""})
	}
	if guardErr := writeDocument(b, objectKey("purge", request.OperationID), "purge", 0, receipt); guardErr != nil {
		return guardErr
	}
	return writeDocument(
		b,
		objectKey("operation", request.OperationID),
		"operation",
		opVersion,
		operationDisk{Action: "forget", Digest: requestDigest, Proposals: nil, Receipt: nil, Epoch: nil},
	)
}

func enqueuePurge(b Bucket, job *purgeJobDisk, id string) error {
	key := selectedJobKey(job.OperationID, id)
	value, err := b.Get(key)
	if err != nil {
		return err
	}
	if value.Data != nil {
		return nil
	}
	var head recordDisk
	_, err = readDocument(b, objectKey("head", id), lifecycleRecordKind, &head)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if head.Scope != job.Scope || head.ID != id {
		return ErrSchema
	}
	if job.Queued >= uint64(MaxVersion) {
		return ErrConflict
	}
	job.Queued++
	item := purgeItemDisk{
		Scope:       job.Scope,
		OperationID: job.OperationID,
		ID:          id,
		Revision:    head.Revision,
		Kind:        lifecycleRecordKind,
	}
	if guardErr := writeDocument(b, key, "purge-item", value.Version, item); guardErr != nil {
		return guardErr
	}
	return writeDocument(b, queueJobKey(job.OperationID, job.Queued), "purge-item", 0, item)
}
func jobItem(b Bucket, job purgeJobDisk, n uint64) (purgeItemDisk, error) {
	var item purgeItemDisk
	_, err := readDocument(b, queueJobKey(job.OperationID, n), "purge-item", &item)
	if err == nil &&
		(item.Scope != job.Scope || item.OperationID != job.OperationID || item.Kind != lifecycleRecordKind) {
		err = ErrSchema
	}
	return item, err
}
func jobScan(b Bucket, job *purgeJobDisk, prefix string, remaining *int, maxBytes int) (ScanPage, error) {
	page, err := b.Scan(
		ScanOptions{Prefix: prefix, After: job.After, Plan: "", Cursor: "", Limit: *remaining, MaxBytes: maxBytes},
	)
	if err != nil {
		return page, err
	}
	*remaining -= max(1, len(page.Entries))
	if len(page.Entries) > 0 {
		job.After = page.Entries[len(page.Entries)-1].Key
	}
	return page, nil
}
func addPurgeOrigin(b Bucket, job purgeJobDisk, id string) error {
	key := originJobKey(job.OperationID, id)
	value, err := b.Get(key)
	if err != nil {
		return err
	}
	if value.Data != nil {
		return nil
	}
	return writeDocument(
		b,
		key,
		"purge-item",
		value.Version,
		purgeItemDisk{Scope: job.Scope, OperationID: job.OperationID, ID: id, Kind: lifecycleProposalKind, Revision: 0},
	)
}
func readyPurgeChunk(job *purgeJobDisk, receipt *PurgeReceipt, records []string, done bool) error {
	if receipt.Batch.Chunk >= uint64(MaxVersion) {
		return ErrConflict
	}
	receipt.Batch.Chunk++
	receipt.Batch.Records = records
	receipt.CanonicalComplete = done
	receipt.State = PurgePending
	for i := range receipt.Sinks {
		receipt.Sinks[i] = SinkResult{Name: receipt.Sinks[i].Name, Acknowledged: false, ErrorCode: ""}
	}
	job.Resume = job.Phase
	job.Phase = purgePhaseAck
	return nil
}

func (e *Engine[P, R, A]) advancePurge(
	ctx context.Context,
	authority A,
	scope Scope,
	purpose string,
	request ForgetRequest,
) (PurgeReceipt, int, error) {
	decision, operationErr := e.authorize(ctx, authority, scope, ActionForget, purpose)
	if operationErr != nil {
		return PurgeReceipt{}, 0, operationErr
	}
	var receipt PurgeReceipt
	used := 0
	operationErr = e.raw.Update(ctx, scope, func(b Bucket) error {
		state, stopped, loadErr := loadPurgeAdvance(b, scope, request.OperationID)
		if loadErr != nil {
			return loadErr
		}
		receipt = state.receipt
		if stopped {
			return nil
		}
		job := state.job
		receiptVersion, jobVersion := state.receiptVersion, state.jobVersion
		receipt.Batch.Records = []string{}
		remaining := request.Limit
		for remaining > 0 && job.Phase != purgePhaseAck && job.Phase != lifecyclePhaseDone {
			if guardErr := ctx.Err(); guardErr != nil {
				return guardErr
			}
			if phaseErr := advancePurgePhase(b, scope, &job, &receipt, &remaining, request.MaxBytes); phaseErr != nil {
				return phaseErr
			}
		}
		used = request.Limit - remaining
		if guardErr := e.reauthorize(ctx, authority, scope, ActionForget, purpose, decision); guardErr != nil {
			return guardErr
		}
		if guardErr := writeDocument(
			b,
			objectKey("purge_job", job.OperationID),
			"purge-job",
			jobVersion,
			job,
		); guardErr != nil {
			return guardErr
		}
		return writeDocument(b, objectKey("purge", job.OperationID), "purge", receiptVersion, receipt)
	})
	if operationErr != nil {
		return PurgeReceipt{}, 0, operationErr
	}
	return receipt, used, nil
}

func finishPurgeJob(b Bucket, receipt *PurgeReceipt) error {
	var job purgeJobDisk
	version, err := readDocument(b, objectKey("purge_job", receipt.Batch.OperationID), "purge-job", &job)
	if err != nil {
		return err
	}
	if job.Scope != receipt.Batch.Scope || job.OperationID != receipt.Batch.OperationID || job.Phase != purgePhaseAck {
		return ErrSchema
	}
	activeVersion, err := boundActivePurge(b, job)
	if err != nil {
		return err
	}
	if receipt.CanonicalComplete && job.Resume == lifecyclePhaseDone {
		job.Phase = lifecyclePhaseDone
		receipt.State = PurgeComplete
		if _, guardErr := b.Delete("active_purge", activeVersion); guardErr != nil {
			return guardErr
		}
	} else {
		job.Phase = job.Resume
		receipt.State = RevocationCommitted
	}
	job.Resume = ""
	return writeDocument(b, objectKey("purge_job", job.OperationID), "purge-job", version, job)
}

func boundActivePurge(b Bucket, job purgeJobDisk) (Version, error) {
	var active activePurgeDisk
	version, err := readDocument(b, "active_purge", "purge-active", &active)
	if errors.Is(err, ErrNotFound) {
		return 0, ErrSchema
	}
	if err != nil {
		return 0, err
	}
	if active.Scope != job.Scope || active.OperationID != job.OperationID {
		return 0, ErrSchema
	}
	return version, nil
}

func validatePurgeStage(job purgeJobDisk, receipt PurgeReceipt) error {
	if job.Scope != receipt.Batch.Scope || job.OperationID != receipt.Batch.OperationID {
		return ErrSchema
	}
	switch job.Phase {
	case lifecyclePhaseDone:
		if receipt.State != PurgeComplete || !receipt.CanonicalComplete || job.Clean != job.Queued+1 ||
			job.Next != job.Queued+1 {
			return ErrSchema
		}
	case purgePhaseEmit, purgePhaseAck:
		if !receipt.CanonicalComplete || job.Clean != job.Queued+1 {
			return ErrSchema
		}
		if job.Phase == purgePhaseEmit && receipt.State != RevocationCommitted {
			return ErrSchema
		}
		if job.Phase == purgePhaseAck && receipt.State != PurgePending && receipt.State != PurgeFailed {
			return ErrSchema
		}
	default:
		if receipt.CanonicalComplete || receipt.State != RevocationCommitted {
			return ErrSchema
		}
	}
	return nil
}

func advancePurgePhase(
	b Bucket,
	scope Scope,
	job *purgeJobDisk,
	receipt *PurgeReceipt,
	remaining *int,
	maxBytes int,
) error {
	switch job.Phase {
	case purgePhaseSeed:
		return purgeSeed(b, scope, job, receipt, remaining, maxBytes)
	case purgePhaseExpand:
		return purgeExpand(b, scope, job, receipt, remaining, maxBytes)
	case lifecyclePhaseRecords:
		return purgeRecords(b, scope, job, receipt, remaining, maxBytes)
	case lifecyclePhaseProposals:
		return purgeProposals(b, scope, job, receipt, remaining, maxBytes)
	case purgePhaseOrigins:
		return purgeOrigins(b, scope, job, receipt, remaining, maxBytes)
	case purgePhaseEmit:
		return purgeEmit(b, scope, job, receipt, remaining, maxBytes)
	default:
		return ErrSchema
	}
}
func purgeSeed(b Bucket, scope Scope, job *purgeJobDisk, receipt *PurgeReceipt, remaining *int, maxBytes int) error {
	if receipt.Batch.Selector.Kind == SelectRecord {
		*remaining--
		if guardErr := enqueuePurge(b, job, receipt.Batch.Selector.ID); guardErr != nil {
			return guardErr
		}
		job.Phase = purgePhaseExpand
		job.After = ""
		return nil
	}
	prefix := "head/"
	if receipt.Batch.Selector.Kind == SelectSource {
		prefix = membershipPrefix("source", receipt.Batch.Selector.ID)
	}
	page, err := jobScan(b, job, prefix, remaining, maxBytes)
	if err != nil {
		return err
	}
	for _, entry := range page.Entries {
		id, identityErr := purgeSeedIdentity(b, scope, *job, receipt.Batch.Selector, entry)
		if identityErr != nil {
			return identityErr
		}
		if guardErr := enqueuePurge(b, job, id); guardErr != nil {
			return guardErr
		}
	}
	if page.Complete {
		job.Phase = purgePhaseExpand
		job.After = ""
	}
	return nil
}

func purgeExpand(b Bucket, scope Scope, job *purgeJobDisk, _ *PurgeReceipt, remaining *int, maxBytes int) error {
	if job.Next > job.Queued {
		job.Phase = lifecyclePhaseRecords
		job.After = ""
		return nil
	}
	item, err := jobItem(b, *job, job.Next)
	if err != nil {
		return err
	}
	page, err := jobScan(b, job, membershipPrefix("lineage", item.ID), remaining, maxBytes)
	if err != nil {
		return err
	}
	for _, entry := range page.Entries {
		m, err := readMembership(b, scope, entry, "lineage", item.ID)
		if err != nil {
			return err
		}
		if guardErr := validateMembership(b, scope, m, *job); guardErr != nil {
			return guardErr
		}
		if guardErr := enqueuePurge(b, job, m.Ref.RecordID); guardErr != nil {
			return guardErr
		}
	}
	if page.Complete {
		job.Next++
		job.After = ""
	}
	return nil
}

func purgeRecords(b Bucket, scope Scope, job *purgeJobDisk, receipt *PurgeReceipt, remaining *int, maxBytes int) error {
	if job.Clean > job.Queued {
		job.Phase = lifecyclePhaseProposals
		job.After = ""
		return nil
	}
	item, err := jobItem(b, *job, job.Clean)
	if err != nil {
		return err
	}
	switch job.Part {
	case purgePartHistory:
		return purgeRecordHistory(b, scope, job, receipt, remaining, maxBytes, item)
	case purgePartMembers:
		return purgeRecordMembers(b, scope, job, receipt, remaining, maxBytes, item)
	case purgePartFinish:
		return purgeRecordFinish(b, scope, job, receipt, remaining, maxBytes, item)
	default:
		return ErrSchema
	}
}

func purgeProposals(
	b Bucket,
	scope Scope,
	job *purgeJobDisk,
	receipt *PurgeReceipt,
	remaining *int,
	maxBytes int,
) error {
	page, err := jobScan(b, job, "proposal_revision/", remaining, maxBytes)
	if err != nil {
		return err
	}
	for _, entry := range page.Entries {
		if entryErr := purgeProposalEntry(b, scope, job, receipt, entry); entryErr != nil {
			return entryErr
		}
	}

	if page.Complete {
		job.Phase = purgePhaseOrigins
		job.After = ""
	}
	return nil
}

func purgeOrigins(b Bucket, scope Scope, job *purgeJobDisk, receipt *PurgeReceipt, remaining *int, maxBytes int) error {
	if job.Origin == "" {
		return startPurgeOrigin(b, scope, job, receipt, remaining, maxBytes)
	}

	page, err := jobScan(
		b,
		job,
		objectKey("proposal_revision", job.Origin)+"/",
		remaining,
		maxBytes,
	)
	if err != nil {
		return err
	}
	for _, entry := range page.Entries {
		var proposal proposalDisk
		if guardErr := decodeDocument(entry.Value.Data, lifecycleProposalKind, &proposal); guardErr != nil {
			return guardErr
		}
		if proposal.Scope != scope || proposal.ID != job.Origin ||
			entry.Key != proposalRevisionKey(ProposalRef{ID: proposal.ID, Revision: proposal.Revision}) {
			return ErrSchema
		}
		if _, guardErr := b.Delete(entry.Key, entry.Value.Version); guardErr != nil {
			return guardErr
		}
	}
	if page.Complete {
		return finishPurgeOrigin(b, job)
	}
	return nil
}

func purgeEmit(b Bucket, _ Scope, job *purgeJobDisk, receipt *PurgeReceipt, remaining *int, _ int) error {
	records := make([]string, 0)
	for *remaining > 0 && job.Next <= job.Queued {
		item, err := jobItem(b, *job, job.Next)
		if err != nil {
			return err
		}
		records = append(records, item.ID)
		job.Next++
		*remaining--
	}
	if job.Next > job.Queued {
		job.Phase = lifecyclePhaseDone
	}
	if guardErr := readyPurgeChunk(job, receipt, records, true); guardErr != nil {
		return guardErr
	}
	return nil
}

func purgeRecordHistory(
	b Bucket,
	scope Scope,
	job *purgeJobDisk,
	_ *PurgeReceipt,
	remaining *int,
	maxBytes int,
	item purgeItemDisk,
) error {
	page, err := jobScan(b, job, objectKey(lifecycleRecordKind, item.ID)+"/", remaining, maxBytes)
	if err != nil {
		return err
	}
	for _, entry := range page.Entries {
		if entryErr := purgeHistoryEntry(b, scope, job, item, entry); entryErr != nil {
			return entryErr
		}
	}

	if page.Complete {
		job.Part = purgePartMembers
		job.After = ""
	}
	return nil
}

func purgeRecordMembers(
	b Bucket,
	scope Scope,
	job *purgeJobDisk,
	_ *PurgeReceipt,
	remaining *int,
	maxBytes int,
	item purgeItemDisk,
) error {
	page, err := jobScan(b, job, membershipPrefix(lifecycleRecordKind, item.ID), remaining, maxBytes)
	if err != nil {
		return err
	}
	for _, entry := range page.Entries {
		if entryErr := purgeMembershipEntry(b, scope, job, item, entry); entryErr != nil {
			return entryErr
		}
	}

	if page.Complete {
		job.Part = purgePartFinish
		job.After = ""
	}
	return nil
}

func purgeRecordFinish(
	b Bucket,
	scope Scope,
	job *purgeJobDisk,
	receipt *PurgeReceipt,
	remaining *int,
	_ int,
	item purgeItemDisk,
) error {
	*remaining--
	var head recordDisk
	version, err := readDocument(b, objectKey("head", item.ID), lifecycleRecordKind, &head)
	if err != nil {
		return err
	}
	if head.Scope != scope || head.ID != item.ID || head.Revision != item.Revision {
		return ErrSchema
	}
	if head.State != Revoked {
		if head.Revision >= MaxVersion {
			return ErrConflict
		}
		var emptyProposal proposalDisk
		tombstone := recordDisk{
			ID:                     item.ID,
			Revision:               head.Revision + 1,
			Scope:                  scope,
			State:                  Revoked,
			InitialState:           Revoked,
			RecordedAt:             receipt.RevokedAt,
			Proposal:               emptyProposal,
			AuthorityPolicyVersion: "",
			Reconciliation:         nil,
			Lineage:                nil,
			Transitions:            nil,
		}
		if guardErr := writeDocument(
			b,
			objectKey("head", item.ID),
			lifecycleRecordKind,
			version,
			tombstone,
		); guardErr != nil {
			return guardErr
		}
	}
	job.Clean++
	job.Part = purgePartHistory
	job.After = ""
	return nil
}

func purgeHistoryEntry(b Bucket, scope Scope, job *purgeJobDisk, item purgeItemDisk, entry Entry) error {
	var r recordDisk
	if guardErr := decodeDocument(entry.Value.Data, lifecycleRecordKind, &r); guardErr != nil {
		return guardErr
	}
	if r.Scope != scope || r.ID != item.ID || entry.Key != revisionKey(r.ID, r.Revision) {
		return ErrSchema
	}
	if guardErr := addPurgeOrigin(b, *job, r.Proposal.ID); guardErr != nil {
		return guardErr
	}
	if _, guardErr := b.Delete(entry.Key, entry.Value.Version); guardErr != nil {
		return guardErr
	}
	return nil
}

func purgeMembershipEntry(b Bucket, scope Scope, _ *purgeJobDisk, item purgeItemDisk, entry Entry) error {
	var m membershipDisk
	if guardErr := decodeDocument(entry.Value.Data, "membership", &m); guardErr != nil {
		return guardErr
	}
	if m.Scope != scope || m.Ref.RecordID != item.ID || entry.Key != membershipRecordKey(m) {
		return ErrSchema
	}
	value, err := b.Get(membershipKey(m))
	if err != nil {
		return err
	}
	if !bytes.Equal(value.Data, entry.Value.Data) {
		return ErrSchema
	}
	if _, guardErr := b.Delete(membershipKey(m), value.Version); guardErr != nil {
		return guardErr
	}
	if _, guardErr := b.Delete(entry.Key, entry.Value.Version); guardErr != nil {
		return guardErr
	}
	return nil
}

func purgeProposalEntry(b Bucket, scope Scope, job *purgeJobDisk, receipt *PurgeReceipt, entry Entry) error {
	var p proposalDisk
	if guardErr := decodeDocument(entry.Value.Data, lifecycleProposalKind, &p); guardErr != nil {
		return guardErr
	}
	if p.Scope != scope ||
		entry.Key != proposalRevisionKey(ProposalRef{ID: p.ID, Revision: p.Revision}) {
		return ErrSchema
	}
	selected := selectedProposal(p, receipt.Batch.Selector, nil)
	for _, ref := range p.Lineage {
		value, err := b.Get(selectedJobKey(job.OperationID, ref.RecordID))
		if err != nil {
			return err
		}
		selected = selected || value.Data != nil
	}
	if selected {
		if guardErr := addPurgeOrigin(b, *job, p.ID); guardErr != nil {
			return guardErr
		}
	}
	return nil
}

func startPurgeOrigin(
	b Bucket,
	scope Scope,
	job *purgeJobDisk,
	receipt *PurgeReceipt,
	remaining *int,
	maxBytes int,
) error {
	page, err := b.Scan(
		ScanOptions{
			Prefix: jobPrefix(job.OperationID) + "origins/",
			After:  "", Plan: "", Cursor: "",
			Limit:    1,
			MaxBytes: maxBytes,
		},
	)
	if err != nil {
		return err
	}
	*remaining--
	if len(page.Entries) == 0 {
		job.Phase = purgePhaseEmit
		job.Next = 1
		receipt.CanonicalComplete = true
		return nil
	}
	var item purgeItemDisk
	if guardErr := decodeDocument(page.Entries[0].Value.Data, "purge-item", &item); guardErr != nil {
		return guardErr
	}
	if item.Scope != scope || item.OperationID != job.OperationID || item.Kind != lifecycleProposalKind ||
		page.Entries[0].Key != originJobKey(job.OperationID, item.ID) {
		return ErrSchema
	}
	job.Origin = item.ID
	job.After = ""
	return nil
}

func finishPurgeOrigin(b Bucket, job *purgeJobDisk) error {
	for _, kind := range []string{lifecycleProposalKind, "acceptance"} {
		key := objectKey(kind, job.Origin)
		value, err := b.Get(key)
		if err != nil {
			return err
		}
		if value.Data != nil {
			if _, guardErr := b.Delete(key, value.Version); guardErr != nil {
				return guardErr
			}
		}
	}
	key := originJobKey(job.OperationID, job.Origin)
	value, err := b.Get(key)
	if err != nil {
		return err
	}
	if _, guardErr := b.Delete(key, value.Version); guardErr != nil {
		return guardErr
	}
	job.Origin = ""
	job.After = ""
	return nil
}

func purgeSeedIdentity(b Bucket, scope Scope, job purgeJobDisk, selector Selector, entry Entry) (string, error) {
	if selector.Kind != SelectSource {
		var head recordDisk
		if decodeErr := decodeDocument(entry.Value.Data, lifecycleRecordKind, &head); decodeErr != nil {
			return "", decodeErr
		}
		if head.Scope != scope || entry.Key != objectKey("head", head.ID) {
			return "", ErrSchema
		}
		return head.ID, nil
	}
	m, membershipErr := readMembership(b, scope, entry, "source", selector.ID)
	if membershipErr != nil {
		return "", membershipErr
	}
	if validationErr := validateMembership(b, scope, m, job); validationErr != nil {
		return "", validationErr
	}
	return m.Ref.RecordID, nil
}

type purgeAdvanceDisk struct {
	receipt        PurgeReceipt
	job            purgeJobDisk
	receiptVersion Version
	jobVersion     Version
}

func loadPurgeAdvance(b Bucket, scope Scope, operationID string) (purgeAdvanceDisk, bool, error) {
	var receipt PurgeReceipt
	receiptVersion, err := readDocument(b, objectKey("purge", operationID), "purge", &receipt)
	if err != nil {
		return purgeAdvanceDisk{}, false, err
	}
	if validationErr := validatePurgeBinding(receipt, scope, operationID); validationErr != nil {
		return purgeAdvanceDisk{}, false, validationErr
	}
	var job purgeJobDisk
	jobVersion, err := readDocument(b, objectKey("purge_job", operationID), "purge-job", &job)
	if err != nil {
		return purgeAdvanceDisk{}, false, err
	}
	if job.Scope != scope || job.OperationID != operationID {
		return purgeAdvanceDisk{}, false, ErrSchema
	}
	if validationErr := validatePurgeStage(job, receipt); validationErr != nil {
		return purgeAdvanceDisk{}, false, validationErr
	}
	if receipt.State == PurgeComplete {
		if completedErr := validateCompletedPurgeFence(b, scope, job); completedErr != nil {
			return purgeAdvanceDisk{}, false, completedErr
		}
		return purgeAdvanceDisk{receipt, job, receiptVersion, jobVersion}, true, nil
	}
	if _, err := boundActivePurge(b, job); err != nil {
		return purgeAdvanceDisk{}, false, err
	}
	if job.Phase == purgePhaseAck {
		return purgeAdvanceDisk{receipt, job, receiptVersion, jobVersion}, true, nil
	}
	return purgeAdvanceDisk{receipt, job, receiptVersion, jobVersion}, false, nil
}

func validateCompletedPurgeFence(b Bucket, scope Scope, job purgeJobDisk) error {
	var active activePurgeDisk
	_, err := readDocument(b, "active_purge", "purge-active", &active)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if err == nil && (active.Scope != scope || active.OperationID == job.OperationID) {
		return ErrSchema
	}
	return nil
}
