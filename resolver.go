package memy

import "context"

// CommitTarget is a domain resolver's claim mapping and explicit reconciliation
// decision. All affected revisions must be included as conditional Related refs.
type CommitTarget struct {
	RecordID  string
	Expected  Version
	Reconcile Reconciliation
}

// Resolver maps a consumer-owned claim key K and typed candidate into a target.
// It never uses embedding similarity as authority or a default truth ordering.
type Resolver[P, R, K any] interface {
	Resolve(context.Context, K, Proposal[P, R], []Record[P, R]) (CommitTarget, error)
}
