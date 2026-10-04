package memy_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

type expiryAdvancingAuthority struct {
	base     memy.Authority[principal]
	clock    *reference.Clock
	deadline time.Time
	armed    bool
	checks   int
}

func (a *expiryAdvancingAuthority) Check(
	ctx context.Context, actor principal, access memy.Access,
) (memy.Decision, error) {
	decision, err := a.base.Check(ctx, actor, access)
	if a.armed && access.Action == memy.ActionRead {
		a.checks++
		if a.checks == 2 {
			a.clock.Set(a.deadline)
		}
	}
	return decision, err
}

func TestManagedWriteRejectsExpiryDuringFinalReauthorization(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		for _, transitive := range []bool{false, true} {
			name := backend + "/direct"
			if transitive {
				name = backend + "/transitive"
			}
			t.Run(name, func(t *testing.T) { managedDeadlineCase(t, backend, transitive) })
		}
	}
}

func managedDeadlineCase(t *testing.T, backend string, transitive bool) {
	t.Helper()
	// Arrange: authority grants access, but its final I/O can cross an exact input deadline.
	f := newFixture(t, raceStore(t, backend))
	deadline := f.clock.Now().Add(time.Hour)
	authority := &expiryAdvancingAuthority{base: f.policy, clock: f.clock, deadline: deadline}
	f.config.Authority = authority
	f = reloadEngine(t, f)
	suggestion := f.suggestion("finite", memy.Interval{})
	suggestion.ExpiresAt = deadline
	proposal, err := f.engine.Remember(context.Background(), f.actor, f.scope, "remember-input", "assist", suggestion)
	if err != nil {
		t.Fatal(err)
	}
	f.commit(t, f.acceptedRequest(t, "commit-input", "input", 0, proposal, memy.Append))
	ref := memy.RevisionRef{RecordID: "input", Revision: 1}
	if transitive {
		derived := lineageProposal(t, f, "remember-derived", "derived", ref)
		f.commit(t, f.acceptedRequest(t, "commit-derived", "derived", 0, derived, memy.Append))
		ref = memy.RevisionRef{RecordID: "derived", Revision: 1}
	}
	fence, err := f.engine.Fence(context.Background(), f.actor, f.scope, "assist")
	if err != nil {
		t.Fatal(err)
	}
	called := false
	callback := func(context.Context) error { called = true; return nil }
	if healthyErr := f.engine.WithDerivedWrite(context.Background(), f.actor, fence, "assist",
		[]memy.RevisionRef{ref}, callback); healthyErr != nil || !called {
		t.Fatalf("healthy managed callback: called=%t err=%v", called, healthyErr)
	}
	authority.armed, called = true, false
	// Act: validation succeeds first, then reauthorization advances to the exact expiry instant.
	err = f.engine.WithDerivedWrite(context.Background(), f.actor, fence, "assist", []memy.RevisionRef{ref}, callback)
	// Assert: no new managed artifact can start from already-expired evidence.
	if !errors.Is(err, memy.ErrStaleInput) || called || authority.checks != 2 {
		t.Fatalf("expired managed callback: called=%t err=%v checks=%d", called, err, authority.checks)
	}
}
