package memy_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/internal/workcost"
	"github.com/skosovsky/memy/store/memory"
	"github.com/skosovsky/memy/store/sqlite"
)

func costAdapter(b testing.TB, adapter string) (memy.Store, string) {
	b.Helper()
	if adapter == "memory" {
		return memory.New(), ""
	}
	path := filepath.Join(b.TempDir(), "extended.db")
	s, err := sqlite.Open(context.Background(), path, sqlite.Options{})
	if err != nil {
		b.Fatal(err)
	}
	return s, path
}

// seedCostLineage adds canonical reconciliation lineage to two selected heads.
// Parents remain independent, so visited vertices/edges grow without inflating
// payload size or manufacturing an exponential transitive DAG.
func seedCostLineage(b testing.TB, f fixture, n int) {
	seedCostCorpus(b, f, n+2)
	err := f.config.Store.Update(context.Background(), f.scope, func(bucket memy.Bucket) error {
		refs := make([]memy.RevisionRef, 0, n)
		for i := range n {
			refs = append(refs, memy.RevisionRef{RecordID: fmt.Sprintf("cost-%d", i+2), Revision: 1})
		}
		for _, id := range []string{"cost-0", "cost-1"} {
			encodedID, _ := json.Marshal(id)
			hash := sha256.Sum256(encodedID)
			key := fmt.Sprintf("head/%x", hash)
			value, err := bucket.Get(key)
			if err != nil {
				return err
			}
			var envelope map[string]json.RawMessage
			if err = json.Unmarshal(value.Data, &envelope); err != nil {
				return err
			}
			var record map[string]json.RawMessage
			if err = json.Unmarshal(envelope["data"], &record); err != nil {
				return err
			}
			record["lineage"], _ = json.Marshal(refs)
			envelope["data"], _ = json.Marshal(record)
			encoded, _ := json.Marshal(envelope)
			for _, k := range []string{key, fmt.Sprintf("record/%x/%020d", hash, 1)} {
				v, err := bucket.Get(k)
				if err != nil {
					return err
				}
				if _, err = bucket.Put(k, v.Version, encoded); err != nil {
					return err
				}
			}
			if err := rewriteFixtureMemberships(bucket, f.scope, id, 1); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
}

func BenchmarkGrowingLineage(b *testing.B) {
	for _, adapter := range []string{"memory", "sqlite"} {
		for _, n := range []int{10, 100, 1000} {
			for _, op := range []string{"Get", "Recall", "Consolidate", "SourceForget", "Sweep"} {
				b.Run(fmt.Sprintf("%s/%d/%s", adapter, n, op), func(b *testing.B) {
					b.ReportAllocs()
					var docs, bytes, nodes, rows int64
					for range b.N {
						b.StopTimer()
						store, _ := costAdapter(b, adapter)
						f := newFixture(b, store)
						seedCostLineage(b, f, n)
						c := &workcost.Counters{}
						workcost.Start(c)
						b.StartTimer()
						var err error
						switch op {
						case "Get":
							_, err = f.engine.Get(b.Context(), f.actor, f.scope, "cost-0", memy.ReadOptions{})
						case "Recall":
							_, err = memy.Recall(b.Context(), f.engine, f.actor, f.scope, "fixed", costSearch{}, memy.ScoreRanker[preference, sourceRef]{}, memy.RecallOptions{Search: memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates}, Limit: 2})
						case "Consolidate":
							req := consolidationRequest([]memy.RevisionRef{{RecordID: "cost-0", Revision: 1}, {RecordID: "cost-1", Revision: 1}}, memy.ExactDedup)
							req.Budget.InputBytes = 16 << 20
							req.Budget.OutputBytes = 16 << 20
							_, err = memy.Consolidate(b.Context(), f.engine, f.actor, f.scope, req, nil)
						case "SourceForget":
							var receipt memy.PurgeReceipt
							receipt, err = fullForget(f.engine, b.Context(), f.actor, f.scope, "assist", memy.ForgetRequest{OperationID: "lineage-forget", Selector: memy.Selector{Kind: memy.SelectSource, ID: f.source.ID}, Reason: "benchmark", PolicyVersion: "cost/v1"})
							if err == nil && (receipt.State != memy.PurgeComplete || len(receipt.Batch.Records) != n+2) {
								b.Fatal("incomplete lineage purge")
							}
						case "Sweep":
							_, err = fullSweep(f.engine, b.Context(), f.actor, f.scope, "assist", "lineage-sweep")
						}
						b.StopTimer()
						workcost.Stop(c)
						if err != nil {
							b.Fatal(err)
						}
						docs += c.DecodedDocs.Load()
						bytes += c.DecodedBytes.Load()
						nodes += c.IndexNodes.Load()
						rows += c.SQLMetadataRows.Load() + c.SQLValueRows.Load()
						if err = store.Close(); err != nil {
							b.Fatal(err)
						}
					}
					for name, v := range map[string]int64{"decoded-docs/op": docs, "decoded-bytes/op": bytes, "index-nodes/op": nodes, "sql-returned-rows/op": rows} {
						b.ReportMetric(float64(v)/float64(b.N), name)
					}
				})
			}
		}
	}
}

// Handle-cold is a fresh SQLite connection after close. The OS page cache is
// deliberately not flushed; this benchmark makes no disk-cold claim.
func BenchmarkSQLiteReopenGet(b *testing.B) {
	for _, n := range []int{1000, 10000, 100000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				b.StopTimer()
				store, path := costAdapter(b, "sqlite")
				f := newFixture(b, store)
				seedCostCorpus(b, f, n)
				if err := store.Close(); err != nil {
					b.Fatal(err)
				}
				reopened, err := sqlite.Open(context.Background(), path, sqlite.Options{})
				if err != nil {
					b.Fatal(err)
				}
				f.config.Store = reopened
				f.engine, err = memy.New(f.config)
				if err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				_, err = f.engine.Get(b.Context(), f.actor, f.scope, "cost-0", memy.ReadOptions{})
				b.StopTimer()
				if err != nil {
					b.Fatal(err)
				}
				if err = reopened.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCallbackContention(b *testing.B) {
	for _, adapter := range []string{"memory", "sqlite"} {
		b.Run(adapter, func(b *testing.B) {
			b.ReportAllocs()
			var waits []time.Duration
			for iteration := range b.N {
				b.StopTimer()
				store, _ := costAdapter(b, adapter)
				f := newFixture(b, store)
				seedCostCorpus(b, f, 2)
				entered, release := make(chan struct{}), make(chan struct{})
				done := make(chan error, 1)
				go func() {
					_, err := memy.Recall(b.Context(), f.engine, f.actor, f.scope, "fixed", costSearch{}, heldRanker{entered, release}, memy.RecallOptions{Search: memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates}, Limit: 2})
					done <- err
				}()
				<-entered
				releaseTimer := time.AfterFunc(50*time.Millisecond, func() { close(release) })
				b.StartTimer()
				started := time.Now()
				for _, subject := range []string{"B", "C"} {
					scope := f.scope
					scope.Subject = subject
					err := store.Update(b.Context(), scope, func(bucket memy.Bucket) error {
						_, err := bucket.Put(fmt.Sprintf("write-%d", iteration), 0, []byte("live"))
						return err
					})
					if err != nil {
						b.Fatal(err)
					}
				}
				waits = append(waits, time.Since(started))
				b.StopTimer()
				// The callback is released 50ms after entry for identical before/after timing.
				_ = releaseTimer
				if err := <-done; err != nil {
					b.Fatal(err)
				}
				if err := store.Close(); err != nil {
					b.Fatal(err)
				}
			}
			slices.Sort(waits)
			b.ReportMetric(float64(waits[len(waits)/2]), "write-p50-ns")
			b.ReportMetric(float64(waits[(len(waits)-1)*95/100]), "write-p95-ns")
			b.ReportMetric(50000000, "held-callback-ns")
		})
	}
}
