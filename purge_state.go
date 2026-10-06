package memy

import (
	"context"
	"errors"
	"slices"
)

const purgeUnsupported = "unsupported"

func (e *Engine[P, R, A]) readPurge(ctx context.Context, scope Scope, operationID string) (PurgeReceipt, error) {
	var receipt PurgeReceipt
	readErr := e.raw.FencedView(ctx, scope, func(b Bucket) error {
		_, documentErr := readDocument(b, objectKey("purge", operationID), "purge", &receipt)
		if documentErr != nil {
			return documentErr
		}
		if err := validatePurgeBinding(receipt, scope, operationID); err != nil {
			return err
		}
		var job purgeJobDisk
		if _, err := readDocument(b, objectKey("purge_job", operationID), "purge-job", &job); err != nil {
			return err
		}
		return validateStoredPurge(b, job, receipt)
	})
	if readErr != nil {
		return PurgeReceipt{}, readErr
	}
	return receipt, nil
}

func validatePurgeBinding(receipt PurgeReceipt, scope Scope, operationID string) error {
	if receipt.Batch.Scope != scope || receipt.Batch.OperationID != operationID {
		return ErrSchema
	}
	return nil
}

func (e *Engine[P, R, A]) purgeSink(ctx context.Context, batch PurgeBatch, name string) SinkResult {
	result := SinkResult{Name: name, Acknowledged: false, ErrorCode: purgeUnsupported}
	for _, sink := range e.config.Sinks {
		if sink.Name() != name {
			continue
		}
		input := batch
		input.Records = slices.Clone(batch.Records)
		ack, purgeErr := sink.Purge(ctx, input)
		result.ErrorCode = coverageUnavailable
		result.Acknowledged = purgeErr == nil && ack.Sink == name && ack.OperationID == batch.OperationID &&
			ack.Epoch == batch.Epoch && ack.Chunk == batch.Chunk
		switch {
		case result.Acknowledged:
			result.ErrorCode = ""
		case purgeErr == nil:
			result.ErrorCode = "invalid_ack"
		case errors.Is(purgeErr, ErrUnsupported):
			result.ErrorCode = purgeUnsupported
		}
		return result
	}
	return result
}

// Reloading and merging acknowledgements preserves concurrent retries. External
// error strings are deliberately excluded from the durable content-free receipt.
func (e *Engine[P, R, A]) persistSinkResult(
	ctx context.Context, authority A, scope Scope, purpose, operationID string, chunk uint64, status SinkResult,
) (PurgeReceipt, error) {
	decision, authErr := e.authorize(ctx, authority, scope, ActionForget, purpose)
	if authErr != nil {
		return PurgeReceipt{}, authErr
	}
	var receipt PurgeReceipt
	updateErr := e.raw.Update(ctx, scope, func(b Bucket) error {
		version, readErr := readDocument(b, objectKey("purge", operationID), "purge", &receipt)
		if readErr != nil {
			return readErr
		}
		if bindingErr := validatePurgeBinding(receipt, scope, operationID); bindingErr != nil {
			return bindingErr
		}
		var job purgeJobDisk
		if _, err := readDocument(b, objectKey("purge_job", operationID), "purge-job", &job); err != nil {
			return err
		}
		if err := validateStoredPurge(b, job, receipt); err != nil {
			return err
		}
		if receipt.Batch.Chunk != chunk || receipt.State == PurgeComplete || receipt.State == RevocationCommitted {
			return nil
		}
		mergeSinkResult(&receipt, status)
		if receipt.State == PurgeComplete {
			if err := finishPurgeJob(b, &receipt); err != nil {
				return err
			}
		}
		if reauthErr := e.reauthorize(ctx, authority, scope, ActionForget, purpose, decision); reauthErr != nil {
			return reauthErr
		}
		return writeDocument(b, objectKey("purge", operationID), "purge", version, receipt)
	})
	if updateErr != nil {
		return PurgeReceipt{}, updateErr
	}
	return receipt, nil
}

func mergeSinkResult(receipt *PurgeReceipt, status SinkResult) {
	receipt.State = PurgeComplete
	for index := range receipt.Sinks {
		current := &receipt.Sinks[index]
		if current.Name == status.Name && !current.Acknowledged {
			current.Acknowledged, current.ErrorCode = status.Acknowledged, status.ErrorCode
		}
		if !current.Acknowledged {
			if current.ErrorCode == purgeUnsupported || current.ErrorCode == "invalid_ack" {
				receipt.State = PurgeFailed
			} else if receipt.State != PurgeFailed {
				receipt.State = PurgePending
			}
		}
	}
}

// validateStoredPurge correlates progress and active ownership before exposing a receipt.
func validateStoredPurge(b Bucket, job purgeJobDisk, receipt PurgeReceipt) error {
	if err := validatePurgeStage(job, receipt); err != nil {
		return err
	}
	if receipt.State != PurgeComplete {
		_, err := boundActivePurge(b, job)
		return err
	}
	var active activePurgeDisk
	_, err := readDocument(b, "active_purge", "purge-active", &active)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if active.Scope != job.Scope || active.OperationID == job.OperationID {
		return ErrSchema
	}
	return nil
}
