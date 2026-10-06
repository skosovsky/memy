package memy

import (
	"errors"
	"fmt"

	"github.com/skosovsky/memy/internal/membershipproof"
)

type membershipDisk struct {
	Scope    Scope                   `json:"scope"`
	Ref      RevisionRef             `json:"ref"`
	Relation string                  `json:"relation"`
	MatchID  string                  `json:"match_id"`
	Proof    membershipproof.Witness `json:"proof"`
}

func membershipIdentity(id string) string { hash, _ := digest(id); return hash }

func membershipPrefix(relation, id string) string { return objectKey("edge/"+relation, id) + "/" }
func membershipKey(m membershipDisk) string {
	return fmt.Sprintf(
		"%s%s/%020d",
		membershipPrefix(m.Relation, m.MatchID),
		membershipIdentity(m.Ref.RecordID),
		m.Ref.Revision,
	)
}
func membershipRecordKey(m membershipDisk) string {
	return fmt.Sprintf(
		"%s%s/%s/%020d",
		membershipPrefix("record", m.Ref.RecordID),
		m.Relation,
		membershipIdentity(m.MatchID),
		m.Ref.Revision,
	)
}

func revisionMemberships(r recordDisk) []membershipDisk {
	seen := make(map[string]bool)
	var members []membershipDisk
	add := func(relation, id string) {
		key := relation + "\x00" + id
		if seen[key] {
			return
		}
		seen[key] = true
		members = append(
			members,
			membershipDisk{
				Scope:    r.Scope,
				Ref:      RevisionRef{RecordID: r.ID, Revision: r.Revision},
				Relation: relation, Proof: membershipproof.Witness{Index: 0, Count: 0, Siblings: nil},
				MatchID: id,
			},
		)
	}
	for _, source := range r.Proposal.Sources {
		add("source", source.ID)
	}
	for _, ref := range r.Lineage {
		add("lineage", ref.RecordID)
	}
	return members
}
func membershipBinding(m membershipDisk) membershipproof.Binding {
	return membershipproof.Binding{
		Scope:    m.Scope.Key(),
		RecordID: m.Ref.RecordID,
		Revision: uint64(m.Ref.Revision),
		Relation: m.Relation,
		MatchID:  m.MatchID,
	}
}
func membershipRoot(r recordDisk) (string, []membershipDisk) {
	members := revisionMemberships(r)
	bindings := make([]membershipproof.Binding, len(members))
	for i, m := range members {
		bindings[i] = membershipBinding(m)
	}
	root, proofs := membershipproof.Build(bindings)
	for i := range members {
		members[i].Proof = proofs[i]
	}
	return root, members
}
func persistMemberships(b Bucket, r recordDisk) error {
	_, members := membershipRoot(r)
	for _, m := range members {
		if err := writeDocument(b, membershipKey(m), "membership", 0, m); err != nil {
			return err
		}
		if err := writeDocument(b, membershipRecordKey(m), "membership", 0, m); err != nil {
			return err
		}
	}
	return nil
}

func readMembership(_ Bucket, scope Scope, entry Entry, relation, id string) (membershipDisk, error) {
	var m membershipDisk
	if err := decodeDocument(entry.Value.Data, "membership", &m); err != nil {
		return m, err
	}
	if m.Scope != scope || m.Relation != relation || m.MatchID != id || entry.Key != membershipKey(m) {
		return m, ErrSchema
	}
	return m, nil
}

type purgeEvidenceDisk struct {
	Scope       Scope       `json:"scope"`
	OperationID string      `json:"operation_id"`
	Ref         RevisionRef `json:"ref"`
	Root        string      `json:"root"`
}

func validateMembership(b Bucket, scope Scope, m membershipDisk, job purgeJobDisk) error {
	key := jobPrefix(
		job.OperationID,
	) + objectKey(
		"evidence",
		fmt.Sprintf("%s/%020d", membershipIdentity(m.Ref.RecordID), m.Ref.Revision),
	)
	var evidence purgeEvidenceDisk
	version, err := readDocument(b, key, "purge-evidence", &evidence)
	if errors.Is(err, ErrNotFound) {
		var r recordDisk
		if _, err = readDocument(b, revisionKey(m.Ref.RecordID, m.Ref.Revision), "record", &r); err != nil {
			return errors.Join(ErrSchema, err)
		}
		if r.Scope != scope || r.ID != m.Ref.RecordID || r.Revision != m.Ref.Revision {
			return ErrSchema
		}
		root, _ := membershipRoot(r)
		evidence = purgeEvidenceDisk{Scope: scope, OperationID: job.OperationID, Ref: m.Ref, Root: root}
		if !membershipproof.Verify(membershipBinding(m), m.Proof, root) {
			return ErrSchema
		}
		return writeDocument(b, key, "purge-evidence", version, evidence)
	}
	if err != nil {
		return err
	}
	if evidence.Scope != scope || evidence.OperationID != job.OperationID || evidence.Ref != m.Ref ||
		!membershipproof.Verify(membershipBinding(m), m.Proof, evidence.Root) {
		return ErrSchema
	}
	return nil
}
