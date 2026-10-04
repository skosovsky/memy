package memy

import (
	"errors"
	"time"
)

func reconcile(b Bucket, scope Scope, newRef RevisionRef, policy Reconciliation, now time.Time) error {
	if policyErr := validReconciliation(policy); policyErr != nil {
		return policyErr
	}
	seen := make(map[string]bool)
	for _, ref := range policy.Related {
		if ref.RecordID == "" || ref.Revision == 0 || seen[ref.RecordID] {
			return ErrInvalid
		}
		seen[ref.RecordID] = true
		if relatedErr := reconcileRelated(b, scope, newRef, policy, ref, now); relatedErr != nil {
			return relatedErr
		}
	}
	return nil
}

func validReconciliation(policy Reconciliation) error {
	switch policy.Mode {
	case Append, Duplicate, Supersede, Conflict:
	default:
		return ErrInvalid
	}
	if policy.PolicyVersion == "" || policy.Basis == "" {
		return ErrPolicyDenied
	}
	if policy.Mode != Append && len(policy.Related) == 0 {
		return ErrIncomparable
	}
	return nil
}

func reconcileRelated(
	b Bucket,
	scope Scope,
	newRef RevisionRef,
	policy Reconciliation,
	ref RevisionRef,
	now time.Time,
) error {
	related, version, readErr := relatedRevision(b, scope, ref)
	if readErr != nil {
		return readErr
	}
	if policy.Mode == Append || policy.Mode == Duplicate {
		return nil
	}
	state := Superseded
	if policy.Mode == Conflict {
		state = Conflicted
	}
	related.State = state
	related.Transitions = append(related.Transitions, transition{
		At: now, State: state, PolicyVersion: policy.PolicyVersion, Basis: policy.Basis, Related: []RevisionRef{newRef},
	})
	var history recordDisk
	historyVersion, historyErr := readDocument(b, revisionKey(ref.RecordID, ref.Revision), "record", &history)
	if historyErr != nil {
		return historyErr
	}
	if history.ID != ref.RecordID || history.Scope != scope || history.Revision != ref.Revision {
		return ErrSchema
	}
	if saveErr := writeDocument(
		b,
		revisionKey(ref.RecordID, ref.Revision),
		"record",
		historyVersion,
		related,
	); saveErr != nil {
		return saveErr
	}
	if ref.RecordID != newRef.RecordID {
		return writeDocument(b, objectKey("head", ref.RecordID), "record", version, related)
	}
	return nil
}

func relatedRevision(b Bucket, scope Scope, ref RevisionRef) (recordDisk, Version, error) {
	var related recordDisk
	version, readErr := readDocument(b, objectKey("head", ref.RecordID), "record", &related)
	if readErr != nil {
		return recordDisk{}, 0, errors.Join(ErrConflict, readErr)
	}
	if related.ID != ref.RecordID {
		return recordDisk{}, 0, ErrSchema
	}
	if related.Scope != scope || related.Revision != ref.Revision || related.State == Revoked ||
		related.State == Superseded {
		return recordDisk{}, 0, ErrConflict
	}
	return related, version, nil
}
