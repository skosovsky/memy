package memy

import (
	"context"
	"errors"
)

// SweepRequest starts or resumes one durable maintenance pass.
type SweepRequest struct {
	OperationID     string
	Limit, MaxBytes int
}

// SweepResult reports confirmed progress for this call, including on error.
// The host owns aggregate progress; unknown outcomes cannot guarantee exactly-once counts.
type SweepResult struct {
	Records          []PurgeReceipt
	ExpiredProposals int
	Work             int
	Complete         bool
}
type sweepJobDisk struct {
	Scope          Scope  `json:"scope"`
	OperationID    string `json:"operation_id"`
	Digest         string `json:"digest"`
	Phase          string `json:"phase"`
	After          string `json:"after"`
	ResumeAfter    string `json:"resume_after"`
	Origin         string `json:"origin"`
	PurgeOperation string `json:"purge_operation"`
}

func activeSweepGate(b Bucket, scope Scope) error {
	var active activePurgeDisk
	_, err := readDocument(b, "active_sweep", "sweep-active", &active)
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
func boundActiveSweep(b Bucket, job sweepJobDisk) (Version, error) {
	var active activePurgeDisk
	version, err := readDocument(b, "active_sweep", "sweep-active", &active)
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

// Sweep freezes canonical mutation for a bounded, durable retention pass.
func (e *Engine[P, R, A]) Sweep(
	ctx context.Context,
	authority A,
	scope Scope,
	purpose string,
	request SweepRequest,
) (SweepResult, error) {
	if !validIdentifier(request.OperationID) || request.Limit < 1 || request.Limit > 1024 || request.MaxBytes <= 0 {
		return SweepResult{}, ErrInvalid
	}
	decision, err := e.authorize(ctx, authority, scope, ActionForget, purpose)
	if err != nil {
		return SweepResult{}, err
	}
	plan, err := operationDigest(scope, decision.Actor, purpose, struct{ OperationID string }{request.OperationID})
	if err != nil {
		return SweepResult{}, err
	}
	var result SweepResult
	for result.Work < request.Limit && !result.Complete {
		var pending string
		var delta SweepResult
		err = e.raw.Update(ctx, scope, func(b Bucket) error {
			return e.sweepStep(ctx, b, authority, scope, purpose, request, decision, plan, &delta, &pending)
		})
		if err != nil {
			return result, err
		}
		result.Work += delta.Work
		result.ExpiredProposals += delta.ExpiredProposals
		result.Complete = delta.Complete
		if pending != "" {
			stop, continuationErr := e.continueSweepPurge(ctx, authority, scope, purpose, request, &result, pending)
			if continuationErr != nil {
				return result, continuationErr
			}
			if stop {
				break
			}
		}
	}
	return result, nil
}

func (e *Engine[P, R, A]) sweepStep(
	ctx context.Context,
	b Bucket,
	authority A,
	scope Scope,
	purpose string,
	request SweepRequest,
	decision Decision,
	plan string,
	result *SweepResult,
	pending *string,
) error {
	var err error
	key := objectKey("sweep_job", request.OperationID)
	var job sweepJobDisk
	version, readErr := readDocument(b, key, "sweep-job", &job)
	if errors.Is(readErr, ErrNotFound) {
		if initErr := e.beginSweep(b, scope, request, plan, &job); initErr != nil {
			return initErr
		}
	} else if readErr != nil {
		return readErr
	}
	if job.Scope != scope || job.OperationID != request.OperationID {
		return ErrSchema
	}
	if job.Digest != plan {
		return ErrConflict
	}
	if job.Phase == lifecyclePhaseDone {
		if completedErr := validateCompletedSweepFence(b, scope, job); completedErr != nil {
			return completedErr
		}
		result.Complete = true
		return e.reauthorize(ctx, authority, scope, ActionForget, purpose, decision)
	}
	activeVersion, err := boundActiveSweep(b, job)
	if err != nil {
		return err
	}
	active, activeErr := discoverSweepPurge(b, scope, &job)
	if activeErr != nil {
		return activeErr
	}
	if job.PurgeOperation != "" {
		if purgeErr := sweepPendingPurge(b, scope, &job, active, pending); purgeErr != nil {
			return purgeErr
		}
	}
	if *pending == "" {
		if scanErr := e.scanSweep(
			ctx,
			b,
			authority,
			scope,
			purpose,
			request,
			decision,
			&job,
			activeVersion,
			result,
			pending,
		); scanErr != nil {
			return scanErr
		}
	}
	if err = e.reauthorize(ctx, authority, scope, ActionForget, purpose, decision); err != nil {
		return err
	}
	return writeDocument(b, key, "sweep-job", version, job)
}

func (e *Engine[P, R, A]) beginSweep(
	b Bucket,
	scope Scope,
	request SweepRequest,
	plan string,
	job *sweepJobDisk,
) error {
	var err error

	if guardErr := activeSweepGate(b, scope); guardErr != nil {
		return guardErr
	}
	epoch, ev, err := currentEpoch(b)
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
	if err = writeDocument(b, "epoch", "epoch", ev, epoch); err != nil {
		return err
	}
	active, err := b.Get("active_sweep")
	if err != nil {
		return err
	}
	if err = writeDocument(
		b,
		"active_sweep",
		"sweep-active",
		active.Version,
		activePurgeDisk{scope, request.OperationID},
	); err != nil {
		return err
	}
	*job = sweepJobDisk{
		Scope:          scope,
		OperationID:    request.OperationID,
		Digest:         plan,
		Phase:          lifecyclePhaseRecords,
		After:          "",
		ResumeAfter:    "",
		Origin:         "",
		PurgeOperation: "",
	}
	return nil
}

func sweepPendingPurge(b Bucket, scope Scope, job *sweepJobDisk, active activePurgeDisk, pending *string) error {
	var receipt PurgeReceipt
	_, err := readDocument(b, objectKey("purge", job.PurgeOperation), "purge", &receipt)
	if err != nil {
		return err
	}
	if guardErr := validatePurgeBinding(receipt, scope, job.PurgeOperation); guardErr != nil {
		return guardErr
	}
	var purgeJob purgeJobDisk
	if _, guardErr := readDocument(
		b,
		objectKey("purge_job", job.PurgeOperation),
		"purge-job",
		&purgeJob,
	); guardErr != nil {
		return guardErr
	}
	if guardErr := validatePurgeStage(purgeJob, receipt); guardErr != nil {
		return guardErr
	}
	if receipt.State == PurgeComplete {
		if active.OperationID == job.PurgeOperation {
			return ErrSchema
		}
		job.PurgeOperation = ""
	} else {
		*pending = job.PurgeOperation
	}
	return nil
}

func (e *Engine[P, R, A]) scanSweep(
	ctx context.Context,
	b Bucket,
	authority A,
	scope Scope,
	purpose string,
	request SweepRequest,
	decision Decision,
	job *sweepJobDisk,
	activeVersion Version,
	result *SweepResult,
	pending *string,
) error {
	var err error

	prefix := "record/"
	if job.Phase == lifecyclePhaseProposals {
		prefix = "proposal_revision/"
	}
	if job.Phase == sweepPhaseOrigin {
		prefix = objectKey("proposal_revision", job.Origin) + "/"
	}
	page, err := b.Scan(
		ScanOptions{Prefix: prefix, After: job.After, Plan: "", Cursor: "", Limit: 1, MaxBytes: request.MaxBytes},
	)
	if err != nil {
		return err
	}
	result.Work++
	if len(page.Entries) > 0 {
		entry := page.Entries[0]
		if entryErr := e.sweepEntry(
			ctx,
			b,
			authority,
			scope,
			purpose,
			request,
			decision,
			job,
			entry,
			pending,
		); entryErr != nil {
			return entryErr
		}
	}
	// Fresh suffix scans consume empty pages, so transitions do not hide work.
	if len(page.Entries) == 0 {
		return finishSweepPage(b, job, activeVersion, result)
	}
	return nil
}

func (e *Engine[P, R, A]) sweepEntry(
	ctx context.Context,
	b Bucket,
	authority A,
	scope Scope,
	purpose string,
	request SweepRequest,
	decision Decision,
	job *sweepJobDisk,
	entry Entry,
	pending *string,
) error {
	switch job.Phase {
	case lifecyclePhaseRecords:
		return e.sweepRecordsEntry(ctx, b, authority, scope, purpose, request, decision, job, entry, pending)
	case lifecyclePhaseProposals:
		return e.sweepProposalsEntry(ctx, b, authority, scope, purpose, request, decision, job, entry, pending)
	case sweepPhaseOrigin:
		return e.sweepOriginEntry(ctx, b, authority, scope, purpose, request, decision, job, entry, pending)
	default:
		return ErrSchema
	}
}

func (e *Engine[P, R, A]) sweepRecordsEntry(
	ctx context.Context,
	b Bucket,
	authority A,
	scope Scope,
	purpose string,
	request SweepRequest,
	decision Decision,
	job *sweepJobDisk,
	entry Entry,
	pending *string,
) error {
	var err error
	target, expired, err := e.expiredRecord(ctx, b, scope, e.config.Clock.Now(), entry)
	if err != nil {
		return err
	}
	if expired {
		op, err := digest(struct {
			Pass  string
			Scope Scope
			Ref   RevisionRef
		}{request.OperationID, scope, target.Ref})
		if err != nil {
			return err
		}
		req := ForgetRequest{
			OperationID:   op,
			Selector:      Selector{Kind: SelectRecord, ID: target.Ref.RecordID},
			Expected:      []RevisionRef{target.Ref},
			Reason:        "retention expiry",
			PolicyVersion: target.PolicyVersion,
			Limit:         0, MaxBytes: 0,
		}
		dg, err := operationDigest(scope, decision.Actor, purpose, req)
		if err != nil {
			return err
		}
		if err = e.beginPurge(ctx, b, authority, scope, purpose, decision, req, dg); err != nil {
			return err
		}
		job.PurgeOperation = op
		*pending = op
	}
	job.After = entry.Key
	return nil
}

func (e *Engine[P, R, A]) sweepProposalsEntry(
	ctx context.Context,
	_ Bucket,
	_ A,
	scope Scope,
	_ string,
	_ SweepRequest,
	_ Decision,
	job *sweepJobDisk,
	entry Entry,
	_ *string,
) error {
	var err error
	var proposal proposalDisk
	if err = decodeDocument(entry.Value.Data, lifecycleProposalKind, &proposal); err != nil {
		return err
	}
	if proposal.Scope != scope ||
		entry.Key != proposalRevisionKey(
			ProposalRef{ID: proposal.ID, Revision: proposal.Revision},
		) {
		return ErrSchema
	}
	current, err := e.currentRetention(ctx, scope, proposal)
	if err != nil {
		return err
	}
	if proposalExpired(proposal, e.config.Clock.Now()) ||
		retentionExpired(current, e.config.Clock.Now()) {
		job.ResumeAfter = entry.Key
		job.Origin = proposal.ID
		job.Phase = sweepPhaseOrigin
		job.After = ""
	} else {
		job.After = entry.Key
	}
	return nil
}

func (e *Engine[P, R, A]) sweepOriginEntry(
	_ context.Context,
	b Bucket,
	_ A,
	scope Scope,
	_ string,
	_ SweepRequest,
	_ Decision,
	job *sweepJobDisk,
	entry Entry,
	_ *string,
) error {
	var err error
	var proposal proposalDisk
	if err = decodeDocument(entry.Value.Data, lifecycleProposalKind, &proposal); err != nil {
		return err
	}
	if proposal.Scope != scope || proposal.ID != job.Origin ||
		entry.Key != proposalRevisionKey(
			ProposalRef{ID: proposal.ID, Revision: proposal.Revision},
		) {
		return ErrSchema
	}
	if _, err = b.Delete(entry.Key, entry.Value.Version); err != nil {
		return err
	}
	job.After = entry.Key
	return nil
}

func finishSweepPage(b Bucket, job *sweepJobDisk, activeVersion Version, result *SweepResult) error {
	var err error

	switch job.Phase {
	case lifecyclePhaseRecords:
		job.Phase = lifecyclePhaseProposals
		job.After = ""
	case lifecyclePhaseProposals:
		job.Phase = lifecyclePhaseDone
		result.Complete = true
		if _, err = b.Delete("active_sweep", activeVersion); err != nil {
			return err
		}
	case sweepPhaseOrigin:
		for _, kind := range []string{lifecycleProposalKind, "acceptance"} {
			key := objectKey(kind, job.Origin)
			value, err := b.Get(key)
			if err != nil {
				return err
			}
			if value.Data != nil {
				if _, err = b.Delete(key, value.Version); err != nil {
					return err
				}
			}
		}
		result.ExpiredProposals++
		job.Phase = lifecyclePhaseProposals
		job.After = job.ResumeAfter
		job.ResumeAfter = ""
		job.Origin = ""
	default:
		return ErrSchema
	}
	return nil
}

func (e *Engine[P, R, A]) continueSweepPurge(
	ctx context.Context,
	authority A,
	scope Scope,
	purpose string,
	request SweepRequest,
	result *SweepResult,
	pending string,
) (bool, error) {
	if result.Work >= request.Limit {
		return true, nil
	}
	receipt, used, err := e.advancePurge(
		ctx,
		authority,
		scope,
		purpose,
		ForgetRequest{
			OperationID:   pending,
			Selector:      Selector{Kind: "", ID: ""},
			Expected:      nil,
			Reason:        "",
			PolicyVersion: "",
			Limit:         request.Limit - result.Work,
			MaxBytes:      request.MaxBytes,
		},
	)
	if err != nil {
		return false, err
	}
	result.Work += used
	if receipt.State != RevocationCommitted {
		budget := request.Limit - result.Work
		finished, confirmed, finishErr := e.finishPurge(ctx, authority, scope, purpose, pending, budget)
		if finishErr != nil {
			result.Work += confirmed
			if finished.Batch.OperationID != "" {
				receipt = finished
			}
			result.Records = append(result.Records, receipt)
			return false, finishErr
		}
		receipt = finished
		// Conservatively charge the supplied callback budget; no hidden retry loop.
		result.Work += budget
	}
	result.Records = append(result.Records, receipt)
	if receipt.State == PurgeFailed || receipt.State == PurgePending {
		return true, nil
	}
	return false, nil
}

func validateCompletedSweepFence(b Bucket, scope Scope, job sweepJobDisk) error {
	var active activePurgeDisk
	_, pointerErr := readDocument(b, "active_sweep", "sweep-active", &active)
	if pointerErr != nil && !errors.Is(pointerErr, ErrNotFound) {
		return pointerErr
	}
	if pointerErr == nil && (active.Scope != scope || active.OperationID == job.OperationID) {
		return ErrSchema
	}
	return nil
}

func discoverSweepPurge(b Bucket, scope Scope, job *sweepJobDisk) (activePurgeDisk, error) {
	var err error
	var active activePurgeDisk
	_, err = readDocument(b, "active_purge", "purge-active", &active)
	if err == nil {
		if active.Scope != scope {
			return active, ErrSchema
		}
		job.PurgeOperation = active.OperationID
	} else if !errors.Is(err, ErrNotFound) {
		return active, err
	}
	return active, nil
}
