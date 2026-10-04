package memy

import (
	"context"
	"errors"
	"time"
)

func (e *Engine[P, R, A]) currentRetention(ctx context.Context, scope Scope, p proposalDisk) (Retention, error) {
	if p.PayloadCodec != e.config.PayloadCodec.Version() || p.ReferenceCodec != e.config.ReferenceCodec.Version() {
		return Retention{}, ErrSchema
	}
	payload, decodeErr := e.config.PayloadCodec.Decode(p.Payload)
	if decodeErr != nil {
		return Retention{}, errors.Join(ErrSchema, decodeErr)
	}
	current, policyErr := e.config.Retention.Evaluate(ctx, scope, payload)
	if policyErr != nil || current.PolicyVersion == "" {
		return Retention{}, errors.Join(ErrPolicyDenied, policyErr)
	}
	return current, nil
}

func (e *Engine[P, R, A]) validateRetention(ctx context.Context, scope Scope, p proposalDisk) error {
	current, operationErr := e.currentRetention(ctx, scope, p)
	if operationErr != nil {
		return operationErr
	}
	if current.PolicyVersion != p.Retention.PolicyVersion || !current.ExpiresAt.Equal(p.Retention.ExpiresAt) {
		return ErrStaleInput
	}
	return nil
}

func (e *Engine[P, R, A]) validateRankedRetention(
	ctx context.Context,
	b Bucket,
	scope Scope,
	selected []Ranked[P, R],
) error {
	for _, item := range selected {
		var stored recordDisk
		if _, err := readDocument(b, revisionKey(item.Record.ID, item.Record.Revision), "record", &stored); err != nil {
			return err
		}
		if err := e.validateStoredSources(ctx, b, scope, stored.Proposal); err != nil {
			return err
		}
		if retentionErr := e.validateRetention(ctx, scope, stored.Proposal); retentionErr != nil {
			return retentionErr
		}
		if proposalExpired(stored.Proposal, e.config.Clock.Now()) {
			return ErrStaleInput
		}
		if lineageErr := e.validateReadLineage(ctx, b, stored.Lineage, scope); lineageErr != nil {
			return lineageErr
		}
	}
	return nil
}

func (e *Engine[P, R, A]) validateCurrentRead(ctx context.Context, b Bucket, scope Scope, current recordDisk) error {
	if err := e.validateStoredSources(ctx, b, scope, current.Proposal); err != nil {
		return err
	}
	if err := e.validateReadLineage(ctx, b, current.Lineage, scope); err != nil {
		return err
	}
	if err := e.validateRetention(ctx, scope, current.Proposal); err != nil {
		return err
	}
	if proposalExpired(current.Proposal, e.config.Clock.Now()) {
		return ErrStaleInput
	}
	return e.deadlineGate(b, scope, []time.Time{proposalDeadline(current.Proposal)}, current.Lineage)
}
