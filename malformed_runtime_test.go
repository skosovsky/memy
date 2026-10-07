package memy_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

func TestMalformedCanonicalDocumentsFailClosed(t *testing.T) {
	for _, field := range []string{"state", "initial_state", "schema", "unknown_field", "proposal_payload", "id", "missing_lineage", "null_policy"} {
		t.Run(field, func(t *testing.T) {
			// Arrange: corruption is introduced through privileged store access.
			f := newFixture(t, nil)
			p := f.propose(t, "remember", "private value", memy.Interval{})
			f.commit(t, f.acceptedRequest(t, "commit", "timezone", 0, p, memy.Append))
			transactionErr := f.config.Store.Update(context.Background(), f.scope, func(b memy.Bucket) error {
				return corruptCanonicalHead(b, field)
			})
			if transactionErr != nil {
				t.Fatal(transactionErr)
			}
			// Act.
			output, transactionErr := f.engine.Get(
				context.Background(),
				f.actor,
				f.scope,
				"timezone",
				memy.ReadOptions{},
			)
			// Assert: unknown/corrupt state never produces zero/successful payload.
			if !errors.Is(transactionErr, memy.ErrSchema) || output.Payload.Value != "" {
				t.Fatalf("malformed state served: %+v err=%v", output, transactionErr)
			}
		})
	}
}

func corruptCanonicalHead(b memy.Bucket, field string) error {
	entries, listErr := listEntries(b, "head/")
	if listErr != nil {
		return listErr
	}
	var document map[string]json.RawMessage
	if decodeErr := json.Unmarshal(entries[0].Value.Data, &document); decodeErr != nil {
		return decodeErr
	}
	if field == "schema" {
		document[field] = json.RawMessage(`4`)
	} else {
		changed, mutationErr := corruptCanonicalData(document["data"], field)
		if mutationErr != nil {
			return mutationErr
		}
		document["data"] = changed
	}
	encoded, encodeErr := json.Marshal(document)
	if encodeErr != nil {
		return encodeErr
	}
	_, writeErr := b.Put(entries[0].Key, entries[0].Value.Version, encoded)
	return writeErr
}

func corruptCanonicalData(raw []byte, field string) ([]byte, error) {
	var data map[string]json.RawMessage
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	switch field {
	case "null_policy":
		data["policy_version"] = json.RawMessage(`null`)
	case "missing_lineage":
		delete(data, "lineage")
	case "proposal_payload":
		var proposal map[string]json.RawMessage
		if err := json.Unmarshal(data["proposal"], &proposal); err != nil {
			return nil, err
		}
		// Valid encoded JSON with an unchanged reviewed digest.
		proposal["payload"] = json.RawMessage(`"e30="`)
		encoded, err := json.Marshal(proposal)
		if err != nil {
			return nil, err
		}
		data["proposal"] = encoded
	default:
		data[field] = json.RawMessage(`"unknown"`)
	}
	return json.Marshal(data)
}

func TestNullRevocationEpochCannotResetFence(t *testing.T) {
	// Arrange: a real forget has advanced the scope epoch.
	f := newFixture(t, nil)
	_, operationErr := fullForget(context.Background(), f.engine, f.actor, f.scope, "assist", memy.ForgetRequest{
		OperationID:   "forget-scope",
		Selector:      memy.Selector{Kind: memy.SelectScope},
		Reason:        "withdraw",
		PolicyVersion: "deletion/v1",
	})
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	operationErr = f.config.Store.Update(context.Background(), f.scope, func(b memy.Bucket) error {
		stored, transactionErr := b.Get("epoch")
		if transactionErr != nil {
			return transactionErr
		}
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(stored.Data, &envelope); err != nil {
			return err
		}
		var epoch map[string]json.RawMessage
		if err := json.Unmarshal(envelope["data"], &epoch); err != nil {
			return err
		}
		epoch["value"] = json.RawMessage(`null`)
		envelope["data"], transactionErr = json.Marshal(epoch)
		if transactionErr != nil {
			return transactionErr
		}
		raw, transactionErr := json.Marshal(envelope)
		if transactionErr != nil {
			return transactionErr
		}
		_, transactionErr = b.Put("epoch", stored.Version, raw)
		return transactionErr
	})
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	// Act: null must never be interpreted as a fresh epoch zero.
	fence, operationErr := f.engine.Fence(context.Background(), f.actor, f.scope, "assist")
	// Assert.
	if !errors.Is(operationErr, memy.ErrSchema) || fence.Scope != (memy.Scope{}) {
		t.Fatalf("corrupt epoch served: %+v err=%v", fence, operationErr)
	}
}

func TestCorruptRelatedIdentityCannotReconcile(t *testing.T) {
	// Arrange: a valid document is stored under the wrong canonical identity.
	f := newFixture(t, nil)
	original := f.propose(t, "original", "original", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit-original", "original", 0, original, memy.Append))
	corruptionErr := f.config.Store.Update(context.Background(), f.scope, func(b memy.Bucket) error {
		return corruptCanonicalHead(b, "id")
	})
	if corruptionErr != nil {
		t.Fatal(corruptionErr)
	}
	candidate := f.propose(t, "candidate", "correction", memy.Interval{})
	request := f.acceptedRequest(
		t,
		"commit-correction",
		"correction",
		0,
		candidate,
		memy.Supersede,
		memy.RevisionRef{RecordID: "original", Revision: 1},
	)
	// Act.
	_, commitErr := f.engine.Commit(context.Background(), f.actor, f.scope, "assist", request)
	record, readErr := f.engine.Get(context.Background(), f.actor, f.scope, "correction", memy.ReadOptions{})
	// Assert: malformed identity cannot change related history or publish a revision.
	if !errors.Is(commitErr, memy.ErrSchema) || !errors.Is(readErr, memy.ErrNotFound) || record.Payload.Value != "" {
		t.Fatalf("commit=%v read=%+v err=%v", commitErr, record, readErr)
	}
}

func TestMalformedLineageDoesNotBecomeEmptyRecall(t *testing.T) {
	// Arrange: only the derived record is indexed; its parent's head is corrupted.
	f := newFixture(t, nil)
	original := f.propose(t, "original", "private original", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit-original", "original", 0, original, memy.Append))
	derived := f.propose(t, "derived", "private derived", memy.Interval{})
	receipt := f.commit(
		t,
		f.acceptedRequest(
			t,
			"commit-derived",
			"derived",
			0,
			derived,
			memy.Duplicate,
			memy.RevisionRef{RecordID: "original", Revision: 1},
		),
	)
	index := reference.NewIndex(
		"derived-index",
		func(query string, candidate memy.Candidate) bool { return query == candidate.RecordID },
	)
	if err := index.Stage(
		context.Background(),
		f.scope,
		memy.Candidate{RecordID: receipt.RecordID, Revision: receipt.Revision, Score: memy.ScoreOf(1)},
	); err != nil {
		t.Fatal(err)
	}
	if err := index.Acknowledge(context.Background(), receipt.Visibility); err != nil {
		t.Fatal(err)
	}
	corruptionErr := f.config.Store.Update(context.Background(), f.scope, func(b memy.Bucket) error {
		return corruptHeadSchema(b, "original")
	})
	if corruptionErr != nil {
		t.Fatal(corruptionErr)
	}
	// Act.
	recalled, recallErr := memy.Recall(
		context.Background(),
		f.engine,
		f.actor,
		f.scope,
		"derived",
		index,
		memy.ScoreRanker[preference, sourceRef]{},
		memy.RecallOptions{Search: memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates}, Limit: 1},
	)
	snapshot, snapshotErr := fullSnapshot(context.Background(), f.engine, f.actor, f.scope, memy.ReadOptions{})
	// Assert: canonical corruption remains an explicit schema error.
	if !errors.Is(recallErr, memy.ErrSchema) || !errors.Is(snapshotErr, memy.ErrSchema) || len(recalled.Records) != 0 ||
		len(snapshot) != 0 {
		t.Fatalf("recall=%+v err=%v snapshot=%v err=%v", recalled, recallErr, snapshot, snapshotErr)
	}
}

func corruptHeadSchema(b memy.Bucket, id string) error {
	entries, listErr := listEntries(b, "head/")
	if listErr != nil {
		return listErr
	}
	for _, entry := range entries {
		var envelope map[string]json.RawMessage
		if decodeErr := json.Unmarshal(entry.Value.Data, &envelope); decodeErr != nil {
			return decodeErr
		}
		var data struct {
			ID string `json:"id"`
		}
		if decodeErr := json.Unmarshal(envelope["data"], &data); decodeErr != nil {
			return decodeErr
		}
		if data.ID != id {
			continue
		}
		envelope["schema"] = json.RawMessage(`4`)
		raw, encodeErr := json.Marshal(envelope)
		if encodeErr != nil {
			return encodeErr
		}
		_, writeErr := b.Put(entry.Key, entry.Value.Version, raw)
		return writeErr
	}
	return errors.New("parent head missing")
}
