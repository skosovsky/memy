package memy_test

import (
	"errors"
	"testing"
	"time"

	"github.com/skosovsky/memy"
)

func seedExpiryRecord(t *testing.T, f fixture, id, value string, lifetime time.Duration) memy.RevisionRef {
	t.Helper()
	suggestion := f.suggestion(value, memy.Interval{})
	if lifetime != 0 {
		suggestion.ExpiresAt = f.clock.Now().Add(lifetime)
	}
	proposal, err := f.engine.Remember(t.Context(), f.actor, f.scope, "remember-"+id, "assist", suggestion)
	if err != nil {
		t.Fatal(err)
	}
	receipt := f.commit(t, f.acceptedRequest(t, "commit-"+id, id, 0, proposal, memy.Append))
	return memy.RevisionRef{RecordID: receipt.RecordID, Revision: receipt.Revision}
}

func TestExactDedupExpiryUsesEachGroup(t *testing.T) {
	for _, profile := range []string{"memory", "sqlite"} {
		t.Run(profile, func(t *testing.T) {
			// Arrange: two duplicate groups and one earlier-expiring unrelated singleton.
			f := newFixture(t, codecLifecycleStore(t, profile))
			start := f.clock.Now()
			refs := []memy.RevisionRef{
				seedExpiryRecord(t, f, "long-a", "long", 24*time.Hour),
				seedExpiryRecord(t, f, "long-b", "long", 30*time.Hour),
				seedExpiryRecord(t, f, "short-a", "short", 2*time.Hour),
				seedExpiryRecord(t, f, "short-b", "short", 3*time.Hour),
				seedExpiryRecord(t, f, "singleton", "unrelated", time.Hour),
			}
			// Act.
			proposals, err := memy.Consolidate(
				t.Context(),
				f.engine,
				f.actor,
				f.scope,
				consolidationRequest(refs, memy.ExactDedup),
				nil,
			)
			if err != nil {
				t.Fatal(err)
			}
			// Assert: each proposal binds only its group's revisions and minimum deadline.
			if len(proposals) != 2 {
				t.Fatalf("groups: %+v", proposals)
			}
			for _, proposal := range proposals {
				assertDedupGroupExpiry(t, proposal, start)
				id := "derived-" + proposal.Suggestion.Payload.Value
				f.commit(t, f.acceptedRequest(t, "commit-"+id, id, 0, proposal, memy.Append))
			}
			f.clock.Set(start.Add(90 * time.Minute))
			assertGroupReadable(t, f, "derived-long")
			assertGroupReadable(t, f, "derived-short")
			f.clock.Set(start.Add(4 * time.Hour))
			assertGroupReadable(t, f, "derived-long")
			if _, err = f.engine.Get(
				t.Context(),
				f.actor,
				f.scope,
				"derived-short",
				memy.ReadOptions{Purpose: "assist"},
			); !errors.Is(
				err,
				memy.ErrNotFound,
			) {
				t.Fatalf("expired group delivered: %v", err)
			}
		})
	}
}

func TestExactDedupUnlimitedGroupIgnoresFiniteSingleton(t *testing.T) {
	// Arrange.
	f := newFixture(t, nil)
	refs := []memy.RevisionRef{
		seedExpiryRecord(t, f, "a", "unlimited", 0), seedExpiryRecord(t, f, "b", "unlimited", 0),
		seedExpiryRecord(t, f, "c", "finite singleton", time.Hour),
	}
	// Act.
	proposals, err := memy.Consolidate(
		t.Context(),
		f.engine,
		f.actor,
		f.scope,
		consolidationRequest(refs, memy.ExactDedup),
		nil,
	)
	// Assert.
	if err != nil || len(proposals) != 1 || !proposals[0].Suggestion.ExpiresAt.IsZero() {
		t.Fatalf("limited unrelated group: %+v %v", proposals, err)
	}
	f.commit(t, f.acceptedRequest(t, "commit-derived", "derived", 0, proposals[0], memy.Append))
	f.clock.Set(f.clock.Now().Add(2 * time.Hour))
	assertGroupReadable(t, f, "derived")
}

func assertGroupReadable(t *testing.T, f fixture, id string) {
	t.Helper()
	record, err := f.engine.Get(t.Context(), f.actor, f.scope, id, memy.ReadOptions{Purpose: "assist"})
	if err != nil || record.ID != id {
		t.Fatalf("%s not readable: %+v %v", id, record, err)
	}
}

func assertDedupGroupExpiry(t *testing.T, proposal memy.Proposal[preference, sourceRef], start time.Time) {
	t.Helper()
	lifetime := 24 * time.Hour
	if proposal.Suggestion.Payload.Value == "short" {
		lifetime = 2 * time.Hour
	}
	if !proposal.Suggestion.ExpiresAt.Equal(start.Add(lifetime)) || len(proposal.Suggestion.Lineage) != 2 {
		t.Fatalf("wrong group deadline/lineage: %+v", proposal)
	}
}
