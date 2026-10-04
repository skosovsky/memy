package memy_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/skosovsky/memy"
)

type renewingAuthority struct{ clock memy.Clock }

func (a renewingAuthority) Check(_ context.Context, actor principal, access memy.Access) (memy.Decision, error) {
	return memy.Decision{
		Allowed:       true,
		Actor:         actor.actor,
		Scope:         access.Scope,
		PolicyVersion: "authority/v1",
		ExpiresAt:     a.clock.Now().Add(time.Hour),
	}, nil
}

func TestAcceptanceCannotReturnExpiredInitialAuthorityLease(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			// Arrange: reauthorization can renew the lease after source I/O crosses its initial expiry.
			f, sources := advancingFixture(t, backend)
			f.config.Authority = renewingAuthority{clock: f.clock}
			f = reloadEngine(t, f)
			p := f.propose(t, "remember", "private", memy.Interval{})
			sources.armed = true
			// Act.
			acceptance, err := f.engine.Accept(
				context.Background(),
				f.actor,
				f.scope,
				p.ID,
				p.Digest,
				p.Revision,
				"assist",
			)
			stored, readErr := f.engine.Proposal(context.Background(), f.actor, f.scope, p.ID, "assist")
			// Assert: failed review returns no expired acceptance and leaves the proposal Proposed.
			if !errors.Is(err, memy.ErrStaleInput) || acceptance.ProposalID != "" || readErr != nil ||
				stored.State != memy.Proposed {
				t.Fatalf("acceptance=%+v err=%v proposal=%+v read=%v", acceptance, err, stored, readErr)
			}
			sources.armed = false
			// A new review operation can bind a fresh decision and complete normally.
			request := f.acceptedRequest(t, "commit", "timezone", 0, p, memy.Append)
			if !f.clock.Now().Before(request.Acceptance.ExpiresAt) {
				t.Fatal("retry returned expired acceptance")
			}
			receipt := f.commit(t, request)
			if !receipt.CanonicalCommitted {
				t.Fatal("fresh lease did not permit commit")
			}
		})
	}
}

func TestTransitiveTargetCannotInvalidateItsOwnCommit(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		for _, mode := range []memy.ReconcileMode{memy.Append, memy.Supersede} {
			t.Run(backend+"/"+string(mode), func(t *testing.T) { transitiveTargetCase(t, backend, mode) })
		}
	}
}

func transitiveTargetCase(t *testing.T, backend string, mode memy.ReconcileMode) {
	t.Helper()
	// Arrange: a dependent requires the original; replacing that original would invalidate the new record.
	// An independent related record checks reconciliation rollback.
	f := newFixture(t, raceStore(t, backend))
	a := f.propose(t, "remember-a", "original", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit-a", "a", 0, a, memy.Append))
	c := f.propose(t, "remember-c", "unrelated", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit-c", "c", 0, c, memy.Append))
	b := lineageProposal(t, f, "remember-b", "derived", memy.RevisionRef{RecordID: "a", Revision: 1})
	f.commit(t, f.acceptedRequest(t, "commit-b", "b", 0, b, memy.Append))
	updated := lineageProposal(t, f, "remember-a2", "updated", memy.RevisionRef{RecordID: "b", Revision: 1})
	var related []memy.RevisionRef
	if mode == memy.Supersede {
		related = []memy.RevisionRef{{RecordID: "c", Revision: 1}}
	}
	request := f.acceptedRequest(t, "commit-a2", "a", 1, updated, mode, related...)
	// Act: the target appears only transitively, so the direct self check is insufficient.
	receipt, err := f.engine.Commit(context.Background(), f.actor, f.scope, "assist", request)
	// Assert: no self-invalidating revision; even staged reconciliation rolls back.
	if !errors.Is(err, memy.ErrInvalid) || receipt.CanonicalCommitted {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	for id, value := range map[string]string{"a": "original", "b": "derived", "c": "unrelated"} {
		record, readErr := f.engine.Get(context.Background(), f.actor, f.scope, id, memy.ReadOptions{})
		if readErr != nil || record.Revision != 1 || record.State != memy.Active || record.Payload.Value != value {
			t.Fatalf("prior %s=%+v err=%v", id, record, readErr)
		}
	}
	// A different target preserves all mandatory inputs and can use the unconsumed operation identity.
	request.RecordID = "safe"
	request.Expected = 0
	safe := f.commit(t, request)
	replay := f.commit(t, request)
	record, readErr := f.engine.Get(context.Background(), f.actor, f.scope, "safe", memy.ReadOptions{})
	if safe != replay || readErr != nil || record.Revision != 1 || record.Payload.Value != "updated" {
		t.Fatalf("safe=%+v replay=%+v record=%+v err=%v", safe, replay, record, readErr)
	}
}

func lineageProposal(
	t *testing.T,
	f fixture,
	operation, value string,
	ref memy.RevisionRef,
) memy.Proposal[preference, sourceRef] {
	t.Helper()
	suggestion := f.suggestion(value, memy.Interval{})
	suggestion.Lineage = []memy.RevisionRef{ref}
	proposal, err := f.engine.Remember(context.Background(), f.actor, f.scope, operation, "assist", suggestion)
	if err != nil {
		t.Fatal(err)
	}
	return proposal
}
