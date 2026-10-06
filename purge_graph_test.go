package memy_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/skosovsky/memy"
)

func TestNativePurgeTraversesLongHistoricalGraph(t *testing.T) {
	for _, adapter := range []string{"memory", "sqlite"} {
		t.Run(adapter, func(t *testing.T) {
			// Arrange: real canonical envelopes/indexes, hostile key order, a 10k chain,
			// branches, a historical cycle and a head that no longer carries its old edge.
			const chain = 10000
			store, _ := costAdapter(t, adapter)
			f := newFixture(t, store)
			seedCostCorpus(t, f, chain+5)
			graph := make(map[string][]memy.RevisionRef, chain+3)
			for i := 1; i < chain; i++ {
				graph[fmt.Sprintf("cost-%d", i)] = []memy.RevisionRef{
					{RecordID: fmt.Sprintf("cost-%d", i-1), Revision: 1},
				}
			}
			a, b, h := fmt.Sprintf("cost-%d", chain), fmt.Sprintf("cost-%d", chain+1), fmt.Sprintf("cost-%d", chain+2)
			graph[a] = []memy.RevisionRef{{RecordID: "cost-5", Revision: 1}, {RecordID: b, Revision: 1}}
			graph[b] = []memy.RevisionRef{{RecordID: a, Revision: 1}}
			graph[h] = []memy.RevisionRef{{RecordID: b, Revision: 1}}
			err := store.Update(
				t.Context(),
				f.scope,
				func(bucket memy.Bucket) error { return seedHistoricalPurgeGraph(bucket, f.scope, graph, h) },
			)
			checkLifecycleFailuref(t, err != nil, "%v", err)
			// This is a completion/invariant test, not a wall-clock performance gate.
			// The overall go test timeout bounds a stuck run, including race overhead.
			ctx := t.Context()
			request := memy.ForgetRequest{
				OperationID:   "native-graph",
				Selector:      memy.Selector{Kind: memy.SelectRecord, ID: "cost-0"},
				Reason:        "delete",
				PolicyVersion: "delete/v1",
				Limit:         128,
				MaxBytes:      1 << 20,
			}
			// Act: only the first root is explicitly selected; production adjacency expands.
			receipt, err := fullForget(ctx, f.engine, f.actor, f.scope, "assist", request)
			checkLifecycleFailuref(t, err != nil, "%v", err)
			// Assert: unique dependent IDs, complete erasure and unrelated heads survive.
			checkLifecycleFailuref(
				t,
				receipt.State != memy.PurgeComplete || len(receipt.Batch.Records) != chain+3,
				"receipt state=%s IDs=%d",
				receipt.State,
				len(receipt.Batch.Records),
			)
			err = store.View(
				t.Context(),
				f.scope,
				func(bucket memy.Bucket) error { return assertHistoricalPurgeGraph(t, bucket, chain, h) },
			)
			checkLifecycleFailuref(t, err != nil, "%v", err)
			replay, err := fullForget(t.Context(), f.engine, f.actor, f.scope, "assist", request)
			checkLifecycleFailuref(
				t,
				err != nil || replay.Batch.Chunk != receipt.Batch.Chunk,
				"replay=%+v err=%v",
				replay,
				err,
			)
		})
	}
}

func seedHistoricalPurgeGraph(
	bucket memy.Bucket,
	scope memy.Scope,
	graph map[string][]memy.RevisionRef,
	h string,
) error {
	for id, refs := range graph {
		if entryErr := seedHistoricalPurgeEntry(bucket, scope, id, refs, h); entryErr != nil {
			return entryErr
		}
	}
	return nil
}

func assertHistoricalPurgeGraph(t *testing.T, bucket memy.Bucket, chain int, h string) error {
	heads, err := listEntries(bucket, "head/")
	if err != nil {
		return err
	}
	revoked := 0
	for _, entry := range heads {
		var envelope struct {
			Data struct {
				ID       string           `json:"id"`
				Revision memy.Version     `json:"revision"`
				State    memy.RecordState `json:"state"`
				Proposal json.RawMessage  `json:"proposal"`
			} `json:"data"`
		}
		if err = json.Unmarshal(entry.Value.Data, &envelope); err != nil {
			return err
		}
		if envelope.Data.State == memy.Revoked {
			revoked++
			want := memy.Version(2)
			if envelope.Data.ID == h {
				want = 3
			}
			checkLifecycleFailuref(
				t,
				envelope.Data.Revision != want,
				"revision %s=%d want %d",
				envelope.Data.ID,
				envelope.Data.Revision,
				want,
			)
		}
	}
	history, err := listEntries(bucket, "record/")
	if err != nil {
		return err
	}
	checkLifecycleFailuref(
		t,
		revoked != chain+3 || len(history) != 2,
		"revoked=%d remaining history=%d",
		revoked,
		len(history),
	)
	return nil
}

func seedHistoricalPurgeEntry(
	bucket memy.Bucket,
	scope memy.Scope,
	id string,
	refs []memy.RevisionRef,
	h string,
) error {
	encodedID, _ := json.Marshal(id)
	hash := sha256.Sum256(encodedID)
	headKey := fmt.Sprintf("head/%x", hash)
	value, err := bucket.Get(headKey)
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
	raw, _ := json.Marshal(envelope)
	for _, key := range []string{headKey, fmt.Sprintf("record/%x/%020d", hash, 1)} {
		v, err := bucket.Get(key)
		if err != nil {
			return err
		}
		if _, err = bucket.Put(key, v.Version, raw); err != nil {
			return err
		}
	}
	if err := rewriteFixtureMemberships(bucket, scope, id, 1); err != nil {
		return err
	}
	if id == h {
		// Its current revision is independent; only the old edge selects this ID.
		record["revision"] = json.RawMessage(`2`)
		record["lineage"] = json.RawMessage(`[]`)
		envelope["data"], _ = json.Marshal(record)
		raw, _ = json.Marshal(envelope)
		v, err := bucket.Get(headKey)
		if err != nil {
			return err
		}
		if _, err = bucket.Put(headKey, v.Version, raw); err != nil {
			return err
		}
		if _, err = bucket.Put(fmt.Sprintf("record/%x/%020d", hash, 2), 0, raw); err != nil {
			return err
		}
		if err := rewriteFixtureMemberships(bucket, scope, id, 2); err != nil {
			return err
		}
	}
	return nil
}
