package memy_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
	"github.com/skosovsky/memy/store/sqlite"
)

type sweepRetentionFailure struct {
	decision memy.Retention
	calls    int
	failAt   int
	failure  error
}

func (p *sweepRetentionFailure) Evaluate(ctx context.Context, _ memy.Scope, _ preference) (memy.Retention, error) {
	if err := ctx.Err(); err != nil {
		return memy.Retention{}, err
	}
	p.calls++
	if p.calls == p.failAt {
		return memy.Retention{}, p.failure
	}
	return p.decision, nil
}

func TestSweepProgressSurvivesLateRetentionError(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			// Arrange: the second visited draft fails after the first origin is deleted.
			path := filepath.Join(t.TempDir(), "progress.db")
			raw := raceStore(t, "memory")
			if backend == "sqlite" {
				_ = raw.Close()
				var err error
				raw, err = sqlite.Open(t.Context(), path, sqlite.Options{})
				if err != nil {
					t.Fatal(err)
				}
			}
			f := newFixture(t, raw)
			injected := errors.New("retention unavailable")
			policy := &sweepRetentionFailure{decision: memy.Retention{PolicyVersion: "retention/v1"}, failure: injected}
			f.config.Retention = policy
			var err error
			f.engine, err = memy.New(f.config)
			if err != nil {
				t.Fatal(err)
			}
			f.propose(t, "first", "first", memy.Interval{})
			f.propose(t, "second", "second", memy.Interval{})
			policy.calls, policy.failAt = 0, 2
			policy.decision.ExpiresAt = f.clock.Now().Add(-time.Second)
			req := memy.SweepRequest{OperationID: "progress", Limit: 256, MaxBytes: 1 << 20}
			// Act: preserve this response and resume the same identity after repair/reopen.
			partial, sweepErr := f.engine.Sweep(t.Context(), f.actor, f.scope, "assist", req)
			policy.failAt = 0
			if backend == "sqlite" {
				f = reopenFixture(t, f, path)
			}
			resumed, resumeErr := f.engine.Sweep(t.Context(), f.actor, f.scope, "assist", req)
			// Assert: confirmed deletion is reported once in each successful transaction.
			if !errors.Is(sweepErr, memy.ErrPolicyDenied) || partial.Complete || partial.ExpiredProposals != 1 ||
				resumeErr != nil || !resumed.Complete || resumed.ExpiredProposals != 1 {
				t.Fatalf("partial=%+v err=%v resumed=%+v err=%v", partial, sweepErr, resumed, resumeErr)
			}
		})
	}
}

type sweepProgressStore struct {
	memy.Store

	armed   bool
	mode    string
	origins int
	failure error
	cancel  context.CancelFunc
}

type sweepProgressBucket struct {
	memy.Bucket

	originDeleted   bool
	completeReceipt bool
}

func (b *sweepProgressBucket) Delete(key string, version memy.Version) (memy.Version, error) {
	next, err := b.Bucket.Delete(key, version)
	if err == nil && strings.HasPrefix(key, "proposal/") {
		b.originDeleted = true
	}
	return next, err
}

func (b *sweepProgressBucket) Put(key string, version memy.Version, data []byte) (memy.Version, error) {
	next, err := b.Bucket.Put(key, version, data)
	if err == nil && strings.HasPrefix(key, "purge/") && bytes.Contains(data, []byte(`"state":"complete"`)) {
		b.completeReceipt = true
	}
	return next, err
}

func (s *sweepProgressStore) Update(ctx context.Context, scope memy.Scope, fn func(memy.Bucket) error) error {
	target := false
	complete := false
	err := s.Store.Update(ctx, scope, func(b memy.Bucket) error {
		wrapped := &sweepProgressBucket{Bucket: b}
		if callbackErr := fn(wrapped); callbackErr != nil {
			return callbackErr
		}
		complete = wrapped.completeReceipt
		if s.armed && wrapped.originDeleted {
			s.origins++
			target = s.origins == 2
			if target && s.mode == "rollback" {
				return s.failure
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if target && s.mode == "unknown" {
		return errors.Join(memy.ErrUnknownOutcome, s.failure)
	}
	if s.armed && ((s.origins == 1 && s.mode == "cancel") || (complete && s.mode == "final")) {
		s.cancel()
	}
	return nil
}

func TestSweepProgressCountsOnlyConfirmedTransactions(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		for _, mode := range []string{"rollback", "unknown", "cancel"} {
			t.Run(backend+"/"+mode, func(t *testing.T) {
				sweepProgressTransactionCase(t, backend, mode)
			})
		}
	}
}

func sweepProgressTransactionCase(t *testing.T, backend, mode string) {
	t.Helper()
	// Arrange: interrupt after one confirmed origin deletion.
	wrapped := &sweepProgressStore{
		Store:   raceStore(t, backend),
		mode:    mode,
		failure: errors.New("storage fault"),
	}
	f := newFixture(t, wrapped)
	f.config.Retention = reference.Retain[preference]{
		Version:   "retention/v1",
		ExpiresAt: f.clock.Now().Add(time.Hour),
	}
	var err error
	f.engine, err = memy.New(f.config)
	if err != nil {
		t.Fatal(err)
	}
	f.propose(t, "first", "first", memy.Interval{})
	f.propose(t, "second", "second", memy.Interval{})
	f.clock.Advance(2 * time.Hour)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	wrapped.armed, wrapped.cancel = true, cancel
	req := memy.SweepRequest{OperationID: "progress", Limit: 256, MaxBytes: 1 << 20}
	// Act.
	partial, sweepErr := f.engine.Sweep(ctx, f.actor, f.scope, "assist", req)
	wrapped.armed = false
	resumed, resumeErr := f.engine.Sweep(t.Context(), f.actor, f.scope, "assist", req)
	// Assert: rollback/unknown attempts are excluded, prior success survives.
	expected := wrapped.failure
	count := 1
	if mode == "cancel" {
		expected = context.Canceled
	}
	if mode == "unknown" {
		expected = memy.ErrUnknownOutcome
		count = 0
	}
	if !errors.Is(sweepErr, expected) || partial.Complete || partial.ExpiredProposals != 1 ||
		partial.BudgetCharged == 0 {
		t.Fatalf("partial=%+v err=%v", partial, sweepErr)
	}
	if resumeErr != nil || !resumed.Complete || resumed.ExpiredProposals != count {
		t.Fatalf("partial=%+v err=%v resumed=%+v err=%v", partial, sweepErr, resumed, resumeErr)
	}
}

type cancelSweepSink struct {
	memy.Sink

	cancel context.CancelFunc
}

func (s *cancelSweepSink) Purge(ctx context.Context, batch memy.PurgeBatch) (memy.PurgeAck, error) {
	ack, err := s.Sink.Purge(ctx, batch)
	if s.cancel != nil {
		s.cancel()
	}
	return ack, err
}

func TestSweepProgressPreservesPendingReceiptOnCancellation(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			// Arrange: cancel after a sink side effect, before its durable acknowledgement.
			f, index, summary := withSinksStore(t, raceStore(t, backend))
			sink := &cancelSweepSink{Sink: index}
			f.config.Sinks = []memy.Sink{sink, summary}
			f.config.Retention = reference.Retain[preference]{
				Version:   "retention/v1",
				ExpiresAt: f.clock.Now().Add(time.Hour),
			}
			var err error
			f.engine, err = memy.New(f.config)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = staged(t, f, index, summary)
			f.clock.Advance(2 * time.Hour)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			sink.cancel = cancel
			req := memy.SweepRequest{OperationID: "receipts", Limit: 256, MaxBytes: 1 << 20}
			// Act: the durable pending receipt must survive the failed continuation.
			partial, sweepErr := f.engine.Sweep(ctx, f.actor, f.scope, "assist", req)
			sink.cancel = nil
			resumed, resumeErr := fullSweep(t.Context(), f.engine, f.actor, f.scope, "assist", req.OperationID)
			// Assert: the attempted acknowledgement is never reported as committed.
			if !errors.Is(sweepErr, context.Canceled) || partial.Complete || len(partial.Records) != 1 ||
				partial.Records[0].State != memy.PurgePending || partial.Records[0].Sinks[0].Acknowledged ||
				resumeErr != nil || !resumed.Complete || len(resumed.Records) != 1 || resumed.Records[0].State != memy.PurgeComplete {
				t.Fatalf("partial=%+v err=%v resumed=%+v err=%v", partial, sweepErr, resumed, resumeErr)
			}
		})
	}
}

func TestSweepProgressPreservesFinalAcknowledgements(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			// Arrange: cancel after the final sink acknowledgement transaction succeeds.
			store := &sweepProgressStore{Store: raceStore(t, backend), mode: "final"}
			f, index, summary := withSinksStore(t, store)
			f.config.Retention = reference.Retain[preference]{
				Version:   "retention/v1",
				ExpiresAt: f.clock.Now().Add(time.Hour),
			}
			var err error
			f.engine, err = memy.New(f.config)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = staged(t, f, index, summary)
			f.clock.Advance(2 * time.Hour)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			store.armed, store.cancel = true, cancel
			// Act: final authorization fails after the receipt is durably complete.
			partial, sweepErr := f.engine.Sweep(
				ctx,
				f.actor,
				f.scope,
				"assist",
				memy.SweepRequest{OperationID: "final", Limit: 256, MaxBytes: 1 << 20},
			)
			// Assert: pass interruption does not erase the separate completed purge.
			if !errors.Is(sweepErr, context.Canceled) || partial.Complete || len(partial.Records) != 1 {
				t.Fatalf("partial=%+v err=%v", partial, sweepErr)
			}
			receipt := partial.Records[0]
			if receipt.State != memy.PurgeComplete || !receipt.Sinks[0].Acknowledged ||
				!receipt.Sinks[1].Acknowledged ||
				partial.BudgetCharged != 13 {
				t.Fatalf("partial=%+v", partial)
			}
		})
	}
}
