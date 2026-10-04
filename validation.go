package memy

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"slices"
)

func validDigest(value string) bool {
	if len(value) != hex.EncodedLen(sha256.Size) {
		return false
	}
	_, operationErr := hex.DecodeString(value)
	return operationErr == nil
}

func validRef(ref RevisionRef) bool {
	return ref.RecordID != "" && ref.Revision > 0 && ref.Revision <= MaxVersion
}

func validProposal(p *proposalDisk) bool {
	if p.ID == "" || p.Revision == 0 || p.Revision > MaxVersion || p.Scope.Validate() != nil ||
		!validDigest(p.Digest) ||
		len(p.Payload) == 0 ||
		p.PayloadCodec == "" ||
		p.ReferenceCodec == "" ||
		p.Evidence == "" ||
		p.Extractor == "" ||
		p.CreatedAt.IsZero() ||
		p.Retention.PolicyVersion == "" ||
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
		if source.ID == "" || source.Revision == "" || len(source.Reference) == 0 {
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
	if r.ID == "" || r.Revision == 0 || r.Revision > MaxVersion || r.Scope.Validate() != nil || r.RecordedAt.IsZero() ||
		r.PolicyVersion == "" {
		return false
	}
	if r.State == Revoked {
		return r.InitialState == Revoked && reflect.ValueOf(r.Proposal).IsZero() &&
			len(r.Lineage) == 0 && len(r.Related) == 0 && len(r.Transitions) == 0
	}
	if r.InitialState != Active && r.InitialState != Conflicted {
		return false
	}
	if !validProposal(&r.Proposal) || r.Proposal.Scope != r.Scope || r.Proposal.State != Accepted {
		return false
	}
	return validTransitions(r) && validRevisionRefs(r.Related) && validRevisionRefs(r.Lineage) &&
		containsReviewedLineage(r)
}

func containsReviewedLineage(r *recordDisk) bool {
	for _, ref := range r.Proposal.Lineage {
		if !slices.Contains(r.Lineage, ref) {
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
		if change.At.Before(previous) || change.PolicyVersion == "" || change.Basis == "" ||
			(change.State != Superseded && change.State != Conflicted) || !validRevisionRefs(change.Related) {
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
		if ref.ID == "" || ref.Revision == 0 || ref.Revision > MaxVersion {
			return false
		}
	}
	return true
}

func validCommitReceipt(receipt *CommitReceipt) bool {
	return receipt != nil && receipt.CanonicalCommitted && receipt.OperationID != "" && receipt.RecordID != "" &&
		receipt.Revision > 0 && receipt.Revision <= MaxVersion && receipt.Visibility.Scope.Validate() == nil &&
		receipt.Visibility.RecordID == receipt.RecordID && receipt.Visibility.Revision == receipt.Revision
}

func validPurge(p *PurgeReceipt) bool {
	if p.Batch.OperationID == "" || p.Batch.Scope.Validate() != nil || p.Batch.Epoch == 0 ||
		p.Batch.Epoch > MaxVersion ||
		p.RevokedAt.IsZero() ||
		validateSelector(p.Batch.Scope, p.Batch.Selector) != nil {
		return false
	}
	switch p.State {
	case RevocationCommitted, PurgePending, PurgeComplete, PurgeFailed:
	default:
		return false
	}
	seen := make(map[string]bool)
	for _, result := range p.Sinks {
		if result.Name == "" || seen[result.Name] || (result.Acknowledged && result.ErrorCode != "") ||
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
		return data.ProposalID != "" && data.ProposalRevision > 0 && data.ProposalRevision <= MaxVersion &&
			validDigest(data.Digest) &&
			data.Actor != "" &&
			data.PolicyVersion != "" &&
			data.Scope.Validate() == nil
	case *epochDisk:
		return data.Value <= MaxVersion && !data.RecordedAt.IsZero()
	case *revocationDisk:
		return data.Epoch > 0 && data.Epoch <= MaxVersion && data.PolicyVersion != "" &&
			validDigest(data.ReasonDigest) &&
			(data.Selector.Kind == SelectScope || data.Selector.ID != "") &&
			validateSelector(Scope{Subject: data.Selector.ID, Tenant: "", Namespace: ""}, data.Selector) == nil
	case *PurgeReceipt:
		return validPurge(data)
	default:
		return false
	}
}
