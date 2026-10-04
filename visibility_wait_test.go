package memy_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/skosovsky/memy"
)

// Done signals that the reference index reached its blocking select. Authority
// checks only Err; the canonical store is read after search finishes.
type observedWaitContext struct {
	context.Context

	entered chan struct{}
	once    sync.Once
}

func (c *observedWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}

func TestCancellationDuringVisibilityWaitPreservesCanonicalCommit(t *testing.T) {
	// Arrange: committed data exists, but the requested index token is pending.
	f, index, summary := withSinks(t)
	receipt, _ := staged(t, f, index, summary)
	base, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ctx := &observedWaitContext{Context: base, entered: make(chan struct{}), once: sync.Once{}}
	done := make(chan error, 1)
	// Act: cancel only after the actual index wait has started.
	go func() {
		_, recallErr := memy.Recall(ctx, f.engine, f.actor, f.scope, "timezone", index,
			memy.ScoreRanker[preference, sourceRef]{}, memy.RecallOptions{
				Limit: 1, Search: memy.SearchOptions{Minimum: &receipt.Visibility},
			})
		done <- recallErr
	}()
	select {
	case <-ctx.entered:
	case <-base.Done():
		t.Fatal("visibility wait was not entered")
	}
	cancel()
	waitErr := <-done
	canonical, canonicalErr := f.engine.Get(context.Background(), f.actor, f.scope, "timezone", memy.ReadOptions{})
	ackErr := index.Acknowledge(context.Background(), receipt.Visibility)
	// Assert: canceled observation has no rollback effect on canonical or index input.
	if !errors.Is(waitErr, context.Canceled) || canonicalErr != nil || canonical.Revision != receipt.Revision ||
		ackErr != nil {
		t.Fatalf("wait=%v canonical=%+v err=%v ack=%v", waitErr, canonical, canonicalErr, ackErr)
	}
}
