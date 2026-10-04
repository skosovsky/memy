package memy_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

type changingRetention struct {
	mu       sync.Mutex
	decision memy.Retention
	failure  error
}

func (p *changingRetention) Evaluate(ctx context.Context, _ memy.Scope, _ preference) (memy.Retention, error) {
	if err := ctx.Err(); err != nil {
		return memy.Retention{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.decision, p.failure
}

func (p *changingRetention) change(decision memy.Retention, failure error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.decision, p.failure = decision, failure
}

func installChangingRetention(t *testing.T, f fixture) (fixture, *changingRetention) {
	t.Helper()
	policy := &changingRetention{decision: memy.Retention{PolicyVersion: "retention/v1"}}
	f.config.Retention = policy
	var operationErr error
	f.engine, operationErr = memy.New(f.config)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	return f, policy
}

func TestRetentionChangeInvalidatesAcceptedCommit(t *testing.T) {
	// Arrange: review was performed under a concrete retention version.
	f, policy := installChangingRetention(t, newFixture(t, nil))
	p := f.propose(t, "remember", "private value", memy.Interval{})
	request := f.acceptedRequest(t, "commit", "timezone", 0, p, memy.Append)
	policy.change(memy.Retention{PolicyVersion: "retention/v2"}, nil)
	// Act.
	_, commitErr := f.engine.Commit(context.Background(), f.actor, f.scope, "assist", request)
	records, readErr := f.engine.Snapshot(context.Background(), f.actor, f.scope, memy.ReadOptions{})
	// Assert: a new host policy requires new review, not an implicit upgrade.
	if !errors.Is(commitErr, memy.ErrStaleInput) || readErr != nil || len(records) != 0 {
		t.Fatalf("commit=%v state=%v read=%v", commitErr, records, readErr)
	}
}

func TestRetentionReadChangesAndOutageFailClosed(t *testing.T) {
	for _, change := range []string{"version", "deadline", "outage"} {
		t.Run(change, func(t *testing.T) {
			// Arrange.
			f, policy := installChangingRetention(t, newFixture(t, nil))
			p := f.propose(t, "remember", "private value", memy.Interval{})
			f.commit(t, f.acceptedRequest(t, "commit", "timezone", 0, p, memy.Append))
			decision := memy.Retention{PolicyVersion: "retention/v1"}
			expected := memy.ErrStaleInput
			switch change {
			case "version":
				decision.PolicyVersion = "retention/v2"
				policy.change(decision, nil)
			case "deadline":
				decision.ExpiresAt = f.clock.Now().Add(time.Hour)
				policy.change(decision, nil)
			case "outage":
				expected = memy.ErrPolicyDenied
				policy.change(decision, errors.New("policy unavailable"))
			}
			// Act.
			record, readErr := f.engine.Get(context.Background(), f.actor, f.scope, "timezone", memy.ReadOptions{})
			records, snapshotErr := f.engine.Snapshot(context.Background(), f.actor, f.scope, memy.ReadOptions{})
			// Assert: no partial payload escapes with stale/unknown policy.
			if !errors.Is(readErr, expected) || !errors.Is(snapshotErr, expected) || record.Payload.Value != "" ||
				len(records) != 0 {
				t.Fatalf("get=%+v err=%v snapshot=%v err=%v", record, readErr, records, snapshotErr)
			}
		})
	}
}

func TestSweepAppliesShorterCurrentRetentionDeadline(t *testing.T) {
	// Arrange: policy changes from unlimited retention to an elapsed deadline.
	f, policy := installChangingRetention(t, newFixture(t, nil))
	p := f.propose(t, "remember", "private value", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit", "timezone", 0, p, memy.Append))
	policy.change(memy.Retention{PolicyVersion: "retention/v2", ExpiresAt: f.clock.Now().Add(-time.Second)}, nil)
	// Act.
	swept, sweepErr := f.engine.Sweep(context.Background(), f.actor, f.scope, "assist", "current-policy")
	_, readErr := f.engine.Get(context.Background(), f.actor, f.scope, "timezone", memy.ReadOptions{})
	// Assert: current policy triggers real revocation, rather than only denying reads.
	if sweepErr != nil || len(swept.Records) != 1 || swept.Records[0].State != memy.PurgeComplete ||
		!errors.Is(readErr, memy.ErrNotFound) {
		t.Fatalf("sweep=%+v err=%v read=%v", swept, sweepErr, readErr)
	}
}

func TestRecallRechecksRetentionAfterRanking(t *testing.T) {
	// Arrange: a trusted callback can overlap a policy change.
	f, index, summary := withSinks(t)
	f, policy := installChangingRetention(t, f)
	receipt, _ := staged(t, f, index, summary)
	if err := index.Acknowledge(context.Background(), receipt.Visibility); err != nil {
		t.Fatal(err)
	}
	ranker := rankerFunc[preference, sourceRef](
		func(_ context.Context, input []memy.Ranked[preference, sourceRef]) ([]memy.Ranked[preference, sourceRef], error) {
			policy.change(memy.Retention{PolicyVersion: "retention/v2"}, nil)
			return input, nil
		},
	)
	// Act.
	recalled, operationErr := memy.Recall(
		context.Background(),
		f.engine,
		f.actor,
		f.scope,
		"timezone",
		index,
		ranker,
		memy.RecallOptions{Read: memy.ReadOptions{}, Limit: 1},
	)
	// Assert: provider output does not bypass the final host decision.
	if !errors.Is(operationErr, memy.ErrStaleInput) || len(recalled.Records) != 0 {
		t.Fatalf("recall=%+v err=%v", recalled, operationErr)
	}
}

func TestProjectionRechecksRetentionAfterProvider(t *testing.T) {
	// Arrange.
	f, policy := installChangingRetention(t, newFixture(t, nil))
	p := f.propose(t, "remember", "private value", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit", "timezone", 0, p, memy.Append))
	projector := reference.ProjectorFunc[preference, sourceRef, string]{
		PolicyVersion: "projection/v1",
		Apply: func(_ context.Context, input memy.Record[preference, sourceRef]) (string, error) {
			policy.change(memy.Retention{PolicyVersion: "retention/v2"}, nil)
			return input.Payload.Value, nil
		},
	}
	// Act.
	projected, operationErr := memy.Project(
		context.Background(),
		f.engine,
		f.actor,
		f.scope,
		"timezone",
		memy.ReadOptions{},
		projector,
	)
	// Assert.
	if !errors.Is(operationErr, memy.ErrStaleInput) || projected.Output != "" {
		t.Fatalf("projection=%+v err=%v", projected, operationErr)
	}
}

func TestSweepPurgesDraftsUnderCurrentRetention(t *testing.T) {
	// Arrange: a proposal has never been accepted or committed.
	f, policy := installChangingRetention(t, newFixture(t, nil))
	p := f.propose(t, "draft", "private draft", memy.Interval{})
	policy.change(memy.Retention{PolicyVersion: "retention/v2", ExpiresAt: f.clock.Now().Add(-time.Second)}, nil)
	// Act.
	swept, operationErr := f.engine.Sweep(context.Background(), f.actor, f.scope, "assist", "purge-draft")
	proposal, readErr := f.engine.Proposal(context.Background(), f.actor, f.scope, p.ID, "assist")
	// Assert: draft and immutable revisions follow current retention too.
	if operationErr != nil || swept.ExpiredProposals != 1 || !errors.Is(readErr, memy.ErrNotFound) ||
		proposal.Suggestion.Payload.Value != "" {
		t.Fatalf("sweep=%+v err=%v proposal=%+v read=%v", swept, operationErr, proposal, readErr)
	}
}

func TestDuplicateRejectsInputWithChangedRetention(t *testing.T) {
	// Arrange: policy changes only for one consumer payload.
	f := newFixture(t, nil)
	policy := &selectiveRetention{decision: memy.Retention{PolicyVersion: "retention/v1"}}
	f.config.Retention = policy
	var operationErr error
	f.engine, operationErr = memy.New(f.config)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	original := f.propose(t, "original", "original", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit-original", "original", 0, original, memy.Append))
	candidate := f.propose(t, "copy", "copy", memy.Interval{})
	request := f.acceptedRequest(
		t,
		"commit-copy",
		"copy",
		0,
		candidate,
		memy.Duplicate,
		memy.RevisionRef{RecordID: "original", Revision: 1},
	)
	policy.changed = true
	// Act.
	_, operationErr = f.engine.Commit(context.Background(), f.actor, f.scope, "assist", request)
	// Assert: reconciliation links cannot bypass input policy revalidation.
	if !errors.Is(operationErr, memy.ErrStaleInput) {
		t.Fatalf("duplicate accepted stale policy: %v", operationErr)
	}
}

type selectiveRetention struct {
	decision memy.Retention
	changed  bool
}

func (p *selectiveRetention) Evaluate(ctx context.Context, _ memy.Scope, payload preference) (memy.Retention, error) {
	if err := ctx.Err(); err != nil {
		return memy.Retention{}, err
	}
	decision := p.decision
	if p.changed && payload.Value == "original" {
		decision.PolicyVersion = "retention/v2"
	}
	return decision, nil
}
