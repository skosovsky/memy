package memy

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"

	"github.com/skosovsky/memy/internal/membershipproof"
)

func validDigest(value string) bool {
	if len(value) != hex.EncodedLen(sha256.Size) {
		return false
	}
	_, operationErr := hex.DecodeString(value)
	return operationErr == nil
}

func validRef(ref RevisionRef) bool {
	return validIdentifier(ref.RecordID) && ref.Revision > 0 && ref.Revision <= MaxVersion
}

func validProposal(p *proposalDisk) bool {
	if !validIdentifier(p.ID) || p.Revision == 0 || p.Revision > MaxVersion || p.Scope.Validate() != nil ||
		!validDigest(p.Digest) ||
		len(p.Payload) == 0 ||
		!validIdentifier(p.PayloadCodec) ||
		!validIdentifier(p.ReferenceCodec) ||
		p.Evidence == "" ||
		!validIdentifier(p.Extractor) ||
		p.CreatedAt.IsZero() ||
		!validIdentifier(p.Retention.PolicyVersion) ||
		p.Valid.Validate() != nil ||
		len(p.Sources) == 0 {
		return false
	}
	switch p.State {
	case Proposed, Accepted, Rejected, Expired:
	default:
		return false
	}
	for _, source := range p.Sources {
		if !validIdentifier(source.ID) || !validIdentifier(source.Revision) || len(source.Reference) == 0 {
			return false
		}
	}
	for _, ref := range p.Lineage {
		if !validRef(ref) {
			return false
		}
	}
	computed, operationErr := proposalContentDigest(*p)
	return operationErr == nil && computed == p.Digest
}

func validRecord(r *recordDisk) bool {
	if !validIdentifier(r.ID) || r.Revision == 0 || r.Revision > MaxVersion || r.Scope.Validate() != nil ||
		r.RecordedAt.IsZero() ||
		(r.State != Revoked && !validIdentifier(r.AuthorityPolicyVersion)) {
		return false
	}
	if r.State == Revoked {
		return r.InitialState == Revoked && reflect.ValueOf(r.Proposal).IsZero() &&
			len(r.Lineage) == 0 && r.Reconciliation == nil && r.AuthorityPolicyVersion == "" && len(r.Transitions) == 0
	}
	if r.InitialState != Active && r.InitialState != Conflicted {
		return false
	}
	if !validProposal(&r.Proposal) || r.Proposal.Scope != r.Scope || r.Proposal.State != Accepted {
		return false
	}
	return r.Reconciliation != nil && (r.InitialState == Conflicted) == (r.Reconciliation.Mode == Conflict) &&
		validReconciliation(*r.Reconciliation) == nil &&
		validTransitions(r) &&
		validRevisionRefs(r.Lineage) &&
		containsReviewedLineage(r)
}

func containsReviewedLineage(r *recordDisk) bool {
	canonical := make(map[RevisionRef]bool, len(r.Lineage))
	for _, ref := range r.Lineage {
		canonical[ref] = true
	}
	for _, ref := range r.Proposal.Lineage {
		if !canonical[ref] {
			return false
		}
	}
	return true
}

func validRevisionRefs(refs []RevisionRef) bool {
	for _, ref := range refs {
		if !validRef(ref) {
			return false
		}
	}
	return true
}

func validTransitions(r *recordDisk) bool {
	state, previous := r.InitialState, r.RecordedAt
	for _, change := range r.Transitions {
		if change.At.Before(previous) ||
			(change.State != Superseded && change.State != Conflicted) || !validRef(change.Decision) {
			return false
		}
		state, previous = change.State, change.At
	}
	return r.State == state
}

func validOperation(op *operationDisk) bool {
	if !validDigest(op.Digest) {
		return false
	}
	switch op.Action {
	case "remember", "extract", "revise":
		if len(op.Proposals) == 0 || op.Receipt != nil || op.Epoch != nil {
			return false
		}
		return validProposalRefs(op.Proposals)
	case "commit":
		return validCommitReceipt(op.Receipt) && len(op.Proposals) == 0 && op.Epoch == nil
	case "forget":
		if op.Receipt != nil || op.Epoch != nil || len(op.Proposals) != 0 {
			return false
		}
	case "reintroduce":
		if op.Epoch == nil || *op.Epoch == 0 || *op.Epoch > MaxVersion || op.Receipt != nil || len(op.Proposals) != 0 {
			return false
		}
	default:
		return false
	}
	return true
}

func validProposalRefs(refs []ProposalRef) bool {
	for _, ref := range refs {
		if !validIdentifier(ref.ID) || ref.Revision == 0 || ref.Revision > MaxVersion {
			return false
		}
	}
	return true
}

func validCommitReceipt(receipt *CommitReceipt) bool {
	return receipt != nil && receipt.CanonicalCommitted && validIdentifier(receipt.OperationID) &&
		validIdentifier(receipt.RecordID) &&
		receipt.Revision > 0 &&
		receipt.Revision <= MaxVersion &&
		receipt.Visibility.Scope.Validate() == nil &&
		receipt.Visibility.RecordID == receipt.RecordID &&
		receipt.Visibility.Revision == receipt.Revision
}

func validPurge(p *PurgeReceipt) bool {
	if !validIdentifier(p.Batch.OperationID) || p.Batch.Scope.Validate() != nil || p.Batch.Epoch == 0 ||
		p.Batch.Epoch > MaxVersion || p.Batch.Chunk > uint64(MaxVersion) || (p.State == PurgeComplete && !p.CanonicalComplete) ||
		len(
			p.Batch.Records,
		) > 1024 || (p.State != RevocationCommitted && (p.Batch.Chunk == 0 || !p.CanonicalComplete)) ||
		p.RevokedAt.IsZero() ||
		validateSelector(p.Batch.Scope, p.Batch.Selector) != nil {
		return false
	}
	for _, id := range p.Batch.Records {
		if !validIdentifier(id) {
			return false
		}
	}
	switch p.State {
	case RevocationCommitted, PurgePending, PurgeComplete, PurgeFailed:
	default:
		return false
	}
	seen := make(map[string]bool)
	for _, result := range p.Sinks {
		if !validIdentifier(result.Name) || seen[result.Name] || (result.Acknowledged && result.ErrorCode != "") ||
			(p.State == PurgeComplete && !result.Acknowledged) {
			return false
		}
		seen[result.Name] = true
	}
	return true
}

func validDocument(value any) bool {
	switch data := value.(type) {
	case *proposalDisk:
		return validProposal(data)
	case *recordDisk:
		return validRecord(data)
	case *operationDisk:
		return validOperation(data)
	case *Acceptance:
		return validAcceptance(data)
	case *epochDisk:
		return data.Value <= MaxVersion && !data.RecordedAt.IsZero()
	case *revocationDisk:
		return validRevocation(data)
	case *PurgeReceipt:
		return validPurge(data)
	case *sweepJobDisk:
		return validSweepJob(data)
	case *activePurgeDisk:
		return data.Scope.Validate() == nil && validIdentifier(data.OperationID)
	case *purgeJobDisk:
		return validPurgeJob(data)
	case *purgeItemDisk:
		return validPurgeItem(data)
	case *purgeEvidenceDisk:
		return validPurgeEvidence(data)
	case *membershipDisk:
		return validMembership(data)
	default:
		return false
	}
}

func validPurgeJob(job *purgeJobDisk) bool {
	if job.Scope.Validate() != nil || !validIdentifier(job.OperationID) || !validPurgePhase(job.Phase) ||
		job.Queued > uint64(MaxVersion) ||
		job.Next == 0 ||
		job.Clean == 0 ||
		job.Next > job.Queued+1 ||
		job.Clean > job.Queued+1 {
		return false
	}
	if job.Part != purgePartHistory && job.Part != purgePartMembers && job.Part != purgePartFinish {
		return false
	}
	if !validPurgeResume(job) {
		return false
	}
	if (job.Phase == lifecyclePhaseProposals || job.Phase == purgePhaseOrigins) &&
		(job.Clean != job.Queued+1 || job.Next != job.Queued+1) {
		return false
	}
	if (job.Phase == purgePhaseEmit || job.Phase == purgePhaseAck || job.Phase == lifecyclePhaseDone) &&
		job.Clean != job.Queued+1 {
		return false
	}
	if job.Origin != "" && (job.Phase != purgePhaseOrigins || !validIdentifier(job.Origin)) {
		return false
	}
	return len(job.After) <= maxJobCursorLength
}

func validAcceptance(data *Acceptance) bool {
	return validIdentifier(data.ProposalID) && data.ProposalRevision > 0 && data.ProposalRevision <= MaxVersion &&
		validDigest(data.Digest) &&
		validIdentifier(data.Actor) &&
		validIdentifier(data.PolicyVersion) &&
		data.Scope.Validate() == nil
}

func validRevocation(data *revocationDisk) bool {
	return data.Epoch > 0 && data.Epoch <= MaxVersion && validIdentifier(data.PolicyVersion) &&
		validDigest(data.ReasonDigest) &&
		(data.Selector.Kind == SelectScope || data.Selector.ID != "") &&
		validateSelector(Scope{Subject: data.Selector.ID, Tenant: "", Namespace: ""}, data.Selector) == nil
}

func validSweepJob(data *sweepJobDisk) bool {
	return data.Scope.Validate() == nil && validIdentifier(data.OperationID) && validDigest(data.Digest) &&
		(data.Phase == lifecyclePhaseRecords || data.Phase == lifecyclePhaseProposals || data.Phase == sweepPhaseOrigin || data.Phase == lifecyclePhaseDone) &&
		len(data.After) <= maxJobCursorLength &&
		(data.Origin == "" || validIdentifier(data.Origin)) &&
		(data.PurgeOperation == "" || validIdentifier(data.PurgeOperation))
}

func validPurgeItem(data *purgeItemDisk) bool {
	return data.Scope.Validate() == nil && validIdentifier(data.OperationID) && validIdentifier(data.ID) &&
		data.Revision <= MaxVersion &&
		((data.Kind == lifecycleRecordKind && data.Revision > 0) || (data.Kind == lifecycleProposalKind && data.Revision == 0))
}

func validPurgeEvidence(data *purgeEvidenceDisk) bool {
	return data.Scope.Validate() == nil && validIdentifier(data.OperationID) && validRef(data.Ref) &&
		validDigest(data.Root)
}

func validMembership(data *membershipDisk) bool {
	return data.Scope.Validate() == nil && validRef(data.Ref) && validIdentifier(data.MatchID) &&
		(data.Relation == "source" || data.Relation == "lineage") &&
		membershipproof.Shape(data.Proof)
}

func validPurgePhase(phase string) bool {
	switch phase {
	case purgePhaseSeed,
		purgePhaseExpand,
		lifecyclePhaseRecords,
		lifecyclePhaseProposals,
		purgePhaseOrigins,
		purgePhaseAck,
		lifecyclePhaseDone,
		purgePhaseEmit:
		return true
	}
	return false
}

func validPurgeResume(job *purgeJobDisk) bool {
	if job.Phase == purgePhaseAck {
		if job.Resume == lifecyclePhaseDone && job.Next != job.Queued+1 {
			return false
		}
		if job.Resume == purgePhaseEmit && job.Next > job.Queued {
			return false
		}
		if job.Resume != purgePhaseEmit && job.Resume != lifecyclePhaseDone {
			return false
		}
	} else if job.Resume != "" {
		return false
	}
	return true
}

const maxJobCursorLength = 4096
