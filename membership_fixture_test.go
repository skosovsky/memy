package memy_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/internal/membershipproof"
)

// rewriteFixtureMemberships mirrors atomic canonical index construction in
// privileged benchmark corpora. It never participates in measured Engine work.
func rewriteFixtureMemberships(b memy.Bucket, scope memy.Scope, id string, revision memy.Version) error {
	encodedID, _ := json.Marshal(id)
	hash := sha256.Sum256(encodedID)
	value, err := b.Get(fmt.Sprintf("record/%x/%020d", hash, revision))
	if err != nil {
		return err
	}
	var doc struct {
		Data struct {
			Lineage  []memy.RevisionRef `json:"lineage"`
			Proposal struct {
				Sources []struct {
					ID string `json:"id"`
				} `json:"sources"`
			} `json:"proposal"`
		} `json:"data"`
	}
	if err = json.Unmarshal(value.Data, &doc); err != nil {
		return err
	}
	var bindings []membershipproof.Binding
	seen := map[string]bool{}
	add := func(relation, match string) {
		key := relation + "\x00" + match
		if seen[key] {
			return
		}
		seen[key] = true
		bindings = append(
			bindings,
			membershipproof.Binding{
				Scope:    scope.Key(),
				RecordID: id,
				Revision: uint64(revision),
				Relation: relation,
				MatchID:  match,
			},
		)
	}
	for _, source := range doc.Data.Proposal.Sources {
		add("source", source.ID)
	}
	for _, ref := range doc.Data.Lineage {
		add("lineage", ref.RecordID)
	}
	_, proofs := membershipproof.Build(bindings)
	for i, binding := range bindings {
		match, _ := json.Marshal(binding.MatchID)
		mh := sha256.Sum256(match)
		raw, err := json.Marshal(
			map[string]any{
				"schema": memy.SchemaVersion,
				"kind":   "membership",
				"data": map[string]any{
					"scope":    scope,
					"ref":      memy.RevisionRef{RecordID: id, Revision: revision},
					"relation": binding.Relation,
					"match_id": binding.MatchID,
					"proof":    proofs[i],
				},
			},
		)
		if err != nil {
			return err
		}
		for _, key := range []string{fmt.Sprintf("edge/%s/%x/%x/%020d", binding.Relation, mh, hash, revision), fmt.Sprintf("edge/record/%x/%s/%x/%020d", hash, binding.Relation, mh, revision)} {
			v, err := b.Get(key)
			if err != nil {
				return err
			}
			if _, err = b.Put(key, v.Version, raw); err != nil {
				return err
			}
		}
	}
	return nil
}
