package memy_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/store/memory"
	"github.com/skosovsky/memy/store/sqlite"
)

type heldRanker struct{ entered, release chan struct{} }

func (h heldRanker) Rank(ctx context.Context, records []memy.Ranked[preference, sourceRef]) ([]memy.Ranked[preference, sourceRef], error) {
	close(h.entered)
	select {
	case <-h.release:
		return records, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type heldProjector struct{ entered, release chan struct{} }

func (heldProjector) Version() string { return "held/v1" }
func (h heldProjector) Project(ctx context.Context, r memy.Record[preference, sourceRef]) (string, error) {
	close(h.entered)
	select {
	case <-h.release:
		return r.Payload.Value, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestCallbacksAllowMutationAndRejectRevokedFinalDelivery(t *testing.T) {
	for _, adapter := range []string{"memory", "sqlite"} {
		for _, operation := range []string{"Recall", "Project"} {
			t.Run(adapter+"/"+operation, func(t *testing.T) {
				// Arrange: trusted callback has already received initially permitted data.
				var store memy.Store = memory.New()
				if adapter == "sqlite" {
					var err error
					store, err = sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "delivery.db"), sqlite.Options{})
					if err != nil {
						t.Fatal(err)
					}
				}
				f := newFixture(t, store)
				p := f.propose(t, "remember", "private", memy.Interval{Known: true})
				f.commit(t, f.acceptedRequest(t, "commit", "cost-0", 0, p, memy.Append))
				entered, release := make(chan struct{}), make(chan struct{})
				done := make(chan error, 1)
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				go func() {
					if operation == "Recall" {
						result, err := memy.Recall(ctx, f.engine, f.actor, f.scope, "fixed", costSearch{}, heldRanker{entered, release}, memy.RecallOptions{Search: memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates}, Limit: 2})
						if len(result.Records) != 0 {
							done <- errors.New("revoked recall output")
							return
						}
						done <- err
						return
					}
					result, err := memy.Project(ctx, f.engine, f.actor, f.scope, "cost-0", memy.ReadOptions{}, heldProjector{entered, release})
					if result.Output != "" {
						done <- errors.New("revoked projection output")
						return
					}
					done <- err
				}()
				select {
				case <-entered:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				defer func() {
					close(release)
					err := <-done
					if !errors.Is(err, memy.ErrStaleInput) && !errors.Is(err, memy.ErrNotFound) {
						t.Errorf("final delivery error: %v", err)
					}
				}()

				// Act: both an independent write and canonical revoke finish before callback release.
				other := f.scope
				other.Subject = "independent"
				if err := store.Update(ctx, other, func(b memy.Bucket) error { _, err := b.Put("key", 0, []byte("value")); return err }); err != nil {
					t.Fatal(err)
				}
				r, err := fullForget(f.engine, ctx, f.actor, f.scope, "assist", memy.ForgetRequest{OperationID: "forget", Selector: memy.Selector{Kind: memy.SelectRecord, ID: "cost-0"}, Reason: "test", PolicyVersion: "deletion/v1"})

				// Assert: callback latency does not delay revoke; final output stays empty.
				if err != nil || r.State != memy.PurgeComplete {
					t.Fatalf("purge=%+v err=%v", r, err)
				}
			})
		}
	}
}
