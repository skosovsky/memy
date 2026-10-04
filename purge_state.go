package memy

import (
	"context"
	"errors"
	"slices"
)

const purgeUnsupported = "unsupported"

func (e *Engine[P, R, A]) readPurge(ctx context.Context, scope Scope, operationID string) (PurgeReceipt, error) {
	var receipt PurgeReceipt
	readErr := e.config.Store.View(ctx, scope, func(b Bucket) error {
		_, documentErr := readDocument(b, objectKey("purge", operationID), "purge", &receipt)
		if documentErr != nil {
			return documentErr
		}
		return validatePurgeBinding(receipt, scope, operationID)
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
		result.ErrorCode = "unavailable"
		result.Acknowledged = purgeErr == nil && ack.Sink == name && ack.OperationID == batch.OperationID &&
			ack.Epoch == batch.Epoch
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
	ctx context.Context, authority A, scope Scope, purpose, operationID string, status SinkResult,
) (PurgeReceipt, error) {
	decision, authErr := e.authorize(ctx, authority, scope, ActionForget, purpose)
	if authErr != nil {
		return PurgeReceipt{}, authErr
	}
	var receipt PurgeReceipt
	updateErr := e.config.Store.Update(ctx, scope, func(b Bucket) error {
		version, readErr := readDocument(b, objectKey("purge", operationID), "purge", &receipt)
		if readErr != nil {
			return readErr
		}
		if bindingErr := validatePurgeBinding(receipt, scope, operationID); bindingErr != nil {
			return bindingErr
		}
		mergeSinkResult(&receipt, status)
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
