package memy

import (
	"errors"
	"slices"
	"time"
)

// deadlineGate performs no host I/O. The last clock sample covers every
// collected deadline, including dependencies validated before later callbacks.
func (e *Engine[P, R, A]) deadlineGate(b Bucket, scope Scope, deadlines []time.Time, refs []RevisionRef) error {
	deadlines = slices.Clone(deadlines)
	inputs, err := lineageDeadlines(b, scope, refs, "")
	if err != nil {
		return err
	}
	deadlines = append(deadlines, inputs...)
	return e.checkDeadlines(deadlines)
}

func lineageDeadlines(b Bucket, scope Scope, refs []RevisionRef, forbiddenRecord string) ([]time.Time, error) {
	var deadlines []time.Time
	pending := slices.Clone(refs)
	seen := make(map[RevisionRef]bool)
	for len(pending) != 0 {
		ref := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if forbiddenRecord != "" && ref.RecordID == forbiddenRecord {
			return nil, ErrInvalid
		}
		if seen[ref] {
			continue
		}
		seen[ref] = true
		if len(seen) > maximumLineageRecords {
			return nil, ErrBudget
		}
		var source recordDisk
		if _, err := readDocument(b, objectKey("head", ref.RecordID), "record", &source); err != nil {
			return nil, errors.Join(ErrStaleInput, err)
		}
		if source.Scope != scope || source.ID != ref.RecordID || source.Revision != ref.Revision ||
			source.State != Active {
			return nil, ErrStaleInput
		}
		deadlines = append(deadlines, proposalDeadline(source.Proposal))
		pending = append(pending, source.Lineage...)
	}
	return deadlines, nil
}

func (e *Engine[P, R, A]) checkDeadlines(deadlines []time.Time) error {
	now := e.config.Clock.Now()
	for _, deadline := range deadlines {
		if !deadline.IsZero() && !now.Before(deadline) {
			return ErrStaleInput
		}
	}
	return nil
}

func (e *Engine[P, R, A]) deliveryDeadlineGate(b Bucket, scope Scope, records []Record[P, R]) error {
	deadlines := make([]time.Time, 0, len(records))
	var refs []RevisionRef
	for _, record := range records {
		deadlines = append(deadlines, record.ExpiresAt)
		refs = append(refs, record.Provenance.Lineage...)
	}
	return e.deadlineGate(b, scope, deadlines, refs)
}

func (e *Engine[P, R, A]) commitDeadlineGate(
	b Bucket,
	scope Scope,
	target string,
	deadlines []time.Time,
	refs []RevisionRef,
) error {
	inputs, err := lineageDeadlines(b, scope, refs, target)
	if err != nil {
		return err
	}
	return e.checkDeadlines(append(slices.Clone(deadlines), inputs...))
}
