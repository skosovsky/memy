package memy_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

type claimKey struct{ Name string }

func TestTypedDomainResolverMapsClaimWithoutInferringScope(t *testing.T) {
	// Arrange: the host owns a domain key independent of payload identity claims.
	f := newFixture(t, nil)
	p := f.propose(t, "remember", "UTC+7", memy.Interval{})
	acceptance, operationErr := f.engine.Accept(
		context.Background(),
		f.actor,
		f.scope,
		p.ID,
		p.Digest,
		p.Revision,
		"assist",
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	snapshot, operationErr := f.engine.Snapshot(context.Background(), f.actor, f.scope, memy.ReadOptions{})
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	resolver := reference.ResolverFunc[preference, sourceRef, claimKey](
		func(_ context.Context, key claimKey, proposal memy.Proposal[preference, sourceRef], _ []memy.Record[preference, sourceRef]) (memy.CommitTarget, error) {
			if key.Name != proposal.Suggestion.Payload.Key {
				return memy.CommitTarget{}, memy.ErrIncomparable
			}
			return memy.CommitTarget{
				RecordID: "preference/" + key.Name,
				Reconcile: memy.Reconciliation{
					Mode:          memy.Append,
					PolicyVersion: "claim-resolver/v1",
					Basis:         "exact consumer key equality",
				},
			}, nil
		},
	)
	// Act.
	target, operationErr := resolver.Resolve(context.Background(), claimKey{Name: "timezone"}, p, snapshot)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	request := memy.CommitRequest{
		OperationID: "commit",
		ProposalID:  p.ID,
		Acceptance:  acceptance,
		RecordID:    target.RecordID,
		Expected:    target.Expected,
		Reconcile:   target.Reconcile,
	}
	receipt := f.commit(t, request)
	replay := f.commit(t, request)
	_, incomparable := resolver.Resolve(context.Background(), claimKey{Name: "other-claim"}, p, snapshot)
	// Assert: canonical conditional commit is distinct from the resolver decision.
	if receipt.RecordID != "preference/timezone" || receipt != replay ||
		!errors.Is(incomparable, memy.ErrIncomparable) {
		t.Fatalf("target=%+v receipt=%+v incomparable=%v", target, receipt, incomparable)
	}
}

func TestConflictHistoryAndExplicitResolution(t *testing.T) {
	// Arrange.
	f := newFixture(t, nil)
	p := f.propose(t, "A", "UTC+3", memy.Interval{Known: true})
	f.commit(t, f.acceptedRequest(t, "commit-A", "claim-A", 0, p, memy.Append))
	beforeConflict := f.clock.Now()
	f.clock.Advance(time.Hour)
	p = f.propose(t, "B", "UTC+7", memy.Interval{Known: true})
	f.commit(
		t,
		f.acceptedRequest(
			t,
			"commit-B",
			"claim-B",
			0,
			p,
			memy.Conflict,
			memy.RevisionRef{RecordID: "claim-A", Revision: 1},
		),
	)
	conflictTime := f.clock.Now()
	// Act.
	_, hiddenErr := f.engine.Get(context.Background(), f.actor, f.scope, "claim-A", memy.ReadOptions{})
	conflicted, operationErr := f.engine.Get(
		context.Background(),
		f.actor,
		f.scope,
		"claim-A",
		memy.ReadOptions{IncludeConflicts: true},
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	previous, operationErr := f.engine.Get(
		context.Background(),
		f.actor,
		f.scope,
		"claim-A",
		memy.ReadOptions{RecordedAsOf: beforeConflict},
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	f.clock.Advance(time.Hour)
	p = f.propose(t, "resolved", "UTC+7", memy.Interval{Known: true})
	f.commit(t, f.acceptedRequest(t, "commit-resolved", "resolved", 0, p, memy.Supersede,
		memy.RevisionRef{RecordID: "claim-A", Revision: 1}, memy.RevisionRef{RecordID: "claim-B", Revision: 1}))
	historicalConflict, operationErr := f.engine.Get(
		context.Background(),
		f.actor,
		f.scope,
		"claim-A",
		memy.ReadOptions{RecordedAsOf: conflictTime, IncludeConflicts: true},
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	resolved, operationErr := f.engine.Get(context.Background(), f.actor, f.scope, "resolved", memy.ReadOptions{})
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	// Assert: latest recording is not automatic truth; host resolution is explicit.
	if !errors.Is(hiddenErr, memy.ErrNotFound) || conflicted.State != memy.Conflicted ||
		previous.State != memy.Active ||
		historicalConflict.State != memy.Conflicted ||
		resolved.State != memy.Active ||
		len(resolved.Reconciliation.Related) != 2 {
		t.Fatalf(
			"conflicted=%+v previous=%+v historical=%+v resolved=%+v",
			conflicted,
			previous,
			historicalConflict,
			resolved,
		)
	}
}

func TestRegressedClockCannotBackdateRecordedCorrection(t *testing.T) {
	// Arrange.
	f := newFixture(t, nil)
	p := f.propose(t, "old", "UTC+3", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit-old", "timezone", 0, p, memy.Append))
	before := f.clock.Now()
	f.clock.Set(before.Add(-time.Hour))
	// Act.
	p = f.propose(t, "new", "UTC+7", memy.Interval{})
	f.commit(
		t,
		f.acceptedRequest(
			t,
			"commit-new",
			"timezone",
			1,
			p,
			memy.Supersede,
			memy.RevisionRef{RecordID: "timezone", Revision: 1},
		),
	)
	current, operationErr := f.engine.Get(context.Background(), f.actor, f.scope, "timezone", memy.ReadOptions{})
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	historical, operationErr := f.engine.Get(
		context.Background(),
		f.actor,
		f.scope,
		"timezone",
		memy.ReadOptions{RecordedAsOf: before},
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	// Assert: recorded ordering uses a monotonic ledger boundary, observed time
	// still reports the earlier host observation without masquerading as recording.
	if !current.RecordedAt.After(before) || current.ObservedAt != before.Add(-time.Hour) ||
		historical.Payload.Value != "UTC+3" {
		t.Fatalf("current=%+v historical=%+v", current, historical)
	}
}
