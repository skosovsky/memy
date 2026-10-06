package memy_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

func TestCanonicalCannotDropOrReplaceReviewedLineage(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		for _, fault := range []string{"drop", "record", "revision"} {
			t.Run(backend+"/"+fault, func(t *testing.T) { reviewedLineageCase(t, backend, fault) })
		}
	}
}

func reviewedLineageCase(t *testing.T, backend, fault string) {
	t.Helper()
	// Arrange: derived has fresh direct evidence, but review requires original revision 1.
	f := newFixture(t, raceStore(t, backend))
	original := f.propose(t, "remember-original", "original", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit-original", "original", 0, original, memy.Append))
	fresh := f.source
	fresh.ID, fresh.Reference.URI = "fresh", "host://fresh"
	if err := f.sources.Put(f.scope, fresh); err != nil {
		t.Fatal(err)
	}
	suggestion := f.suggestion("derived", memy.Interval{})
	suggestion.Sources = []memy.Source[sourceRef]{fresh}
	suggestion.Lineage = []memy.RevisionRef{{RecordID: "original", Revision: 1}}
	proposal, err := f.engine.Remember(context.Background(), f.actor, f.scope, "remember-derived", "assist", suggestion)
	if err != nil {
		t.Fatal(err)
	}
	receipt := f.commit(t, f.acceptedRequest(t, "commit-derived", "derived", 0, proposal, memy.Append))
	index := reference.NewIndex[string]("index", nil)
	if stageErr := index.Stage(context.Background(), f.scope, memy.Candidate{
		RecordID: receipt.RecordID, Revision: receipt.Revision, Score: 1,
	}); stageErr != nil {
		t.Fatal(stageErr)
	}
	if ackErr := index.Acknowledge(context.Background(), receipt.Visibility); ackErr != nil {
		t.Fatal(ackErr)
	}
	// Act: privileged fault changes only canonical runtime lineage, keeping proposal/digest intact.
	if updateErr := f.config.Store.Update(context.Background(), f.scope, func(b memy.Bucket) error {
		return corruptReviewedLineage(b, "derived", fault)
	}); updateErr != nil {
		t.Fatal(updateErr)
	}
	// Assert: all delivery paths reject malformed dependency metadata before callbacks.
	assertReviewedLineageFailsClosed(t, f, index)
	f.sources.Remove(f.scope, f.source.ID)
	record, err := f.engine.Get(context.Background(), f.actor, f.scope, "derived", memy.ReadOptions{})
	if !errors.Is(err, memy.ErrSchema) || record.Payload.Value != "" {
		t.Fatalf("missing ancestor evidence bypassed: record=%+v err=%v", record, err)
	}
}

func assertReviewedLineageFailsClosed(t *testing.T, f fixture, index *reference.Index[string]) {
	t.Helper()
	record, getErr := f.engine.Get(context.Background(), f.actor, f.scope, "derived", memy.ReadOptions{})
	snapshot, snapshotErr := fullSnapshot(f.engine, context.Background(), f.actor, f.scope, memy.ReadOptions{})
	recalled, recallErr := memy.Recall(context.Background(), f.engine, f.actor, f.scope, "all", index,
		memy.ScoreRanker[preference, sourceRef]{}, memy.RecallOptions{Search: memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates}, Limit: 1})
	called := false
	projection, projectErr := memy.Project(context.Background(), f.engine, f.actor, f.scope, "derived",
		memy.ReadOptions{}, reference.ProjectorFunc[preference, sourceRef, string]{
			PolicyVersion: "v1",
			Apply: func(_ context.Context, r memy.Record[preference, sourceRef]) (string, error) {
				called = true
				return r.Payload.Value, nil
			},
		})
	for _, err := range []error{getErr, snapshotErr, recallErr, projectErr} {
		if !errors.Is(err, memy.ErrSchema) {
			t.Fatalf("corrupted reviewed lineage: %v", err)
		}
	}
	if called || record.Payload.Value != "" || len(snapshot) != 0 || len(recalled.Records) != 0 ||
		projection.Output != "" {
		t.Fatal("malformed reviewed lineage delivered content")
	}
}

func corruptReviewedLineage(b memy.Bucket, id, fault string) error {
	entries, err := listEntries(b, "")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		var envelope map[string]json.RawMessage
		if decodeErr := json.Unmarshal(entry.Value.Data, &envelope); decodeErr != nil {
			return decodeErr
		}
		if string(envelope["kind"]) != `"record"` {
			continue
		}
		var data map[string]json.RawMessage
		if decodeErr := json.Unmarshal(envelope["data"], &data); decodeErr != nil {
			return decodeErr
		}
		if string(data["id"]) != `"`+id+`"` {
			continue
		}
		lineage := []memy.RevisionRef{{RecordID: "original", Revision: 1}}
		switch fault {
		case "drop":
			lineage = nil
		case "record":
			lineage[0].RecordID = "different"
		case "revision":
			lineage[0].Revision = 2
		}
		data["lineage"], err = json.Marshal(lineage)
		if err != nil {
			return err
		}
		envelope["data"], err = json.Marshal(data)
		if err != nil {
			return err
		}
		raw, encodeErr := json.Marshal(envelope)
		if encodeErr != nil {
			return encodeErr
		}
		if _, err := b.Put(entry.Key, entry.Value.Version, raw); err != nil {
			return err
		}
	}
	return nil
}
