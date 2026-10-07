package memy_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

func TestCanonicalReadsRevalidateSource(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		for _, change := range []string{"revision", "remove", "outage"} {
			t.Run(backend+"/"+change, func(t *testing.T) {
				canonicalSourceCase(t, backend, change)
			})
		}
	}
}

type expiringAuthority struct{ expires time.Time }

func (a expiringAuthority) Check(_ context.Context, actor principal, access memy.Access) (memy.Decision, error) {
	return memy.Decision{
		Allowed:       true,
		Actor:         actor.actor,
		Scope:         access.Scope,
		PolicyVersion: "v1",
		Fields:        nil,
		ExpiresAt:     a.expires,
	}, nil
}

func TestAcceptanceDeadlineSurvivesPersistence(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			// Arrange: a real monotonic deadline is serialized by acceptance persistence.
			f := newFixture(t, raceStore(t, backend))
			f.config.Authority = expiringAuthority{expires: time.Now().Add(time.Hour)}
			f.config.Clock = reference.SystemClock{}
			var err error
			f.engine, err = memy.New(f.config)
			if err != nil {
				t.Fatal(err)
			}
			p := f.propose(t, "remember", "fact", memy.Interval{})
			request := f.acceptedRequest(t, "commit", "timezone", 0, p, memy.Append)
			again, err := f.engine.Accept(context.Background(), f.actor, f.scope, p.ID, p.Digest, p.Revision, "assist")
			if err != nil {
				t.Fatal(err)
			}
			// Act: use the exact externally returned acceptance after a repeat accept.
			receipt, err := f.engine.Commit(context.Background(), f.actor, f.scope, "assist", request)
			// Assert: identical instants remain valid through JSON round trips.
			if err != nil || !receipt.CanonicalCommitted || !again.ExpiresAt.Equal(request.Acceptance.ExpiresAt) {
				t.Fatalf("receipt=%+v repeat=%+v err=%v", receipt, again, err)
			}
		})
	}
}

func TestHistoryKeyAndExactWireFieldBinding(t *testing.T) {
	for _, fault := range []string{"revision", "root_alias", "nested_alias"} {
		t.Run(fault, func(t *testing.T) {
			// Arrange: mutate persisted history through the privileged store port.
			f := newFixture(t, nil)
			p := f.propose(t, "remember", "private", memy.Interval{})
			f.commit(t, f.acceptedRequest(t, "commit", "timezone", 0, p, memy.Append))
			if err := f.config.Store.Update(
				context.Background(),
				f.scope,
				func(b memy.Bucket) error { return mutateHistory(b, fault) },
			); err != nil {
				t.Fatal(err)
			}
			// Act.
			record, getErr := f.engine.Get(context.Background(), f.actor, f.scope, "timezone", memy.ReadOptions{})
			snapshot, snapshotErr := fullSnapshot(context.Background(), f.engine, f.actor, f.scope, memy.ReadOptions{})
			// Assert: phantom revisions and case aliases cannot enter canonical output.
			if !errors.Is(getErr, memy.ErrSchema) || !errors.Is(snapshotErr, memy.ErrSchema) ||
				record.Payload.Value != "" ||
				len(snapshot) != 0 {
				t.Fatalf("record=%+v get=%v snapshot=%v", record, getErr, snapshotErr)
			}
		})
	}
}

func mutateHistory(b memy.Bucket, fault string) error {
	entries, err := listEntries(b, "record/")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		var envelope map[string]json.RawMessage
		if err = json.Unmarshal(entry.Value.Data, &envelope); err != nil {
			return err
		}
		if mutationErr := mutateHistoryEnvelope(envelope, fault); mutationErr != nil {
			return mutationErr
		}

		raw, encodeErr := json.Marshal(envelope)
		if encodeErr != nil {
			return encodeErr
		}
		if _, err = b.Put(entry.Key, entry.Value.Version, raw); err != nil {
			return err
		}
	}
	return nil
}

func TestConsolidationBudgetIncludesProviderAnnotations(t *testing.T) {
	// Arrange: enough input budget; output payload is tiny but evidence is large.
	f := newFixture(t, nil)
	refs := seedCorpus(t, f, readCorpus(t))
	request := consolidationRequest(refs, memy.DomainMerge)
	request.Budget.OutputBytes = 2048
	called := false
	provider := reference.MergeFunc[preference, sourceRef](
		func(_ context.Context, inputs []memy.Record[preference, sourceRef], _ memy.Budget) (memy.MergeResult[preference, sourceRef], error) {
			called = true
			result := gatedMerge(inputs[0], "")
			result.Suggestions[0].Evidence = strings.Repeat("X", 1<<20)
			return result, nil
		},
	)
	// Act.
	proposals, err := memy.Consolidate(context.Background(), f.engine, f.actor, f.scope, request, provider)
	state, stateErr := fullSnapshot(context.Background(), f.engine, f.actor, f.scope, memy.ReadOptions{})
	// Assert: provider annotations count, and failed review leaves originals intact.
	if !called || !errors.Is(err, memy.ErrBudget) || len(proposals) != 0 || stateErr != nil || len(state) != len(refs) {
		t.Fatalf("called=%t proposals=%d err=%v state=%d/%v", called, len(proposals), err, len(state), stateErr)
	}
}

func canonicalSourceCase(t *testing.T, backend, change string) {
	t.Helper()
	// Arrange: canonical and visible data cite the original source revision.
	f := newFixture(t, raceStore(t, backend))
	proposal := f.propose(t, "remember", "private", memy.Interval{})
	receipt := f.commit(t, f.acceptedRequest(t, "commit", "timezone", 0, proposal, memy.Append))
	index := reference.NewIndex[string]("index", nil)
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
	changeFixtureSource(t, f, change)
	// Act: all canonical delivery paths encounter the changed evidence.
	record, getErr := f.engine.Get(context.Background(), f.actor, f.scope, "timezone", memy.ReadOptions{})
	snapshot, snapshotErr := fullSnapshot(context.Background(), f.engine, f.actor, f.scope, memy.ReadOptions{})
	recalled, recallErr := memy.Recall(
		context.Background(),
		f.engine,
		f.actor,
		f.scope,
		"all",
		index,
		memy.ScoreRanker[preference, sourceRef]{},
		memy.RecallOptions{Search: memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates}, Limit: 1},
	)
	projection, projectErr := memy.Project(
		context.Background(),
		f.engine,
		f.actor,
		f.scope,
		"timezone",
		memy.ReadOptions{},
		reference.ProjectorFunc[preference, sourceRef, string]{
			PolicyVersion: "v1",
			Apply: func(_ context.Context, r memy.Record[preference, sourceRef]) (string, error) {
				return r.Payload.Value, nil
			},
		},
	)
	// Assert: errors do not carry successful payload, snapshot, or projection.
	for _, err := range []error{getErr, snapshotErr, recallErr, projectErr} {
		if !errors.Is(err, memy.ErrSourceUnavailable) {
			t.Fatalf("source gate: %v", err)
		}
	}
	if record.Payload.Value != "" || len(snapshot) != 0 || len(recalled.Records) != 0 ||
		projection.Output != "" {
		t.Fatal("stale content returned")
	}
}

func changeFixtureSource(t *testing.T, f fixture, change string) {
	t.Helper()
	switch change {
	case "revision":
		source := f.source
		source.Revision = "r2"
		if err := f.sources.Put(f.scope, source); err != nil {
			t.Fatal(err)
		}
	case "remove":
		f.sources.Remove(f.scope, f.source.ID)
	case "outage":
		f.sources.Fail(errors.New("source outage"))
	}
}

func mutateHistoryEnvelope(envelope map[string]json.RawMessage, fault string) error {
	if fault == "root_alias" {
		envelope["SCHEMA"] = json.RawMessage(`99`)
		return nil
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(envelope["data"], &data); err != nil {
		return err
	}
	if fault == "revision" {
		data["revision"] = json.RawMessage(`7`)
	} else {
		data["REVISION"] = json.RawMessage(`99`)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	envelope["data"] = raw
	return nil
}

type sourceChangingRanker struct{ change func() }

func (r sourceChangingRanker) Rank(
	ctx context.Context,
	records []memy.Ranked[preference, sourceRef],
) ([]memy.Ranked[preference, sourceRef], error) {
	r.change()
	return (memy.ScoreRanker[preference, sourceRef]{}).Rank(ctx, records)
}

func TestSourceRevalidatedAfterReadCallbacks(t *testing.T) {
	for _, callback := range []string{"rank", "project"} {
		t.Run(callback, func(t *testing.T) {
			// Arrange: the callback changes an external source after the initial read gate.
			f := newFixture(t, nil)
			p := f.propose(t, "remember", "private", memy.Interval{})
			receipt := f.commit(t, f.acceptedRequest(t, "commit", "timezone", 0, p, memy.Append))
			change := func() { f.sources.Remove(f.scope, f.source.ID) }
			// Act and assert: callback output is withheld after evidence disappears.
			if callback == "project" {
				result, err := memy.Project(
					context.Background(),
					f.engine,
					f.actor,
					f.scope,
					"timezone",
					memy.ReadOptions{},
					reference.ProjectorFunc[preference, sourceRef, string]{
						PolicyVersion: "v1",
						Apply: func(_ context.Context, _ memy.Record[preference, sourceRef]) (string, error) {
							change()
							return "private", nil
						},
					},
				)
				if !errors.Is(err, memy.ErrSourceUnavailable) || result.Output != "" {
					t.Fatalf("projection=%+v err=%v", result, err)
				}
				return
			}
			index := reference.NewIndex[string]("index", nil)
			if err := index.Stage(
				context.Background(),
				f.scope,
				memy.Candidate{RecordID: "timezone", Revision: 1, Score: memy.ScoreOf(1)},
			); err != nil {
				t.Fatal(err)
			}
			if err := index.Acknowledge(context.Background(), receipt.Visibility); err != nil {
				t.Fatal(err)
			}
			result, err := memy.Recall(
				context.Background(),
				f.engine,
				f.actor,
				f.scope,
				"all",
				index,
				sourceChangingRanker{change: change},
				memy.RecallOptions{Search: memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates}, Limit: 1},
			)
			if !errors.Is(err, memy.ErrSourceUnavailable) || len(result.Records) != 0 {
				t.Fatalf("recall=%+v err=%v", result, err)
			}
		})
	}
}

func TestFreshDirectSourceCannotValidateStaleTransitiveLineage(t *testing.T) {
	for _, phase := range []string{"accept", "commit"} {
		t.Run(phase, func(t *testing.T) {
			// Arrange: derived proposal has independent valid direct evidence and canonical lineage.
			f := newFixture(t, nil)
			p := f.propose(t, "original", "original", memy.Interval{})
			f.commit(t, f.acceptedRequest(t, "commit-original", "original", 0, p, memy.Append))
			fresh := f.source
			fresh.ID = "fresh"
			fresh.Reference.URI = "host://fresh"
			if err := f.sources.Put(f.scope, fresh); err != nil {
				t.Fatal(err)
			}
			suggestion := f.suggestion("derived", memy.Interval{})
			suggestion.Sources = []memy.Source[sourceRef]{fresh}
			suggestion.Lineage = []memy.RevisionRef{{RecordID: "original", Revision: 1}}
			derived, err := f.engine.Remember(context.Background(), f.actor, f.scope, "derived", "assist", suggestion)
			if err != nil {
				t.Fatal(err)
			}
			var request memy.CommitRequest
			if phase == "commit" {
				request = f.acceptedRequest(t, "commit-derived", "derived", 0, derived, memy.Append)
			}
			f.sources.Remove(f.scope, f.source.ID)
			// Act: validate the dependency at acceptance or commit after the source is removed.
			if phase == "accept" {
				_, err = f.engine.Accept(
					context.Background(),
					f.actor,
					f.scope,
					derived.ID,
					derived.Digest,
					derived.Revision,
					"assist",
				)
			} else {
				_, err = f.engine.Commit(context.Background(), f.actor, f.scope, "assist", request)
			}
			// Assert: direct fresh evidence cannot mask a stale ancestor.
			if !errors.Is(err, memy.ErrSourceUnavailable) {
				t.Fatalf("phase=%s err=%v", phase, err)
			}
			_, readErr := f.engine.Get(context.Background(), f.actor, f.scope, "derived", memy.ReadOptions{})
			if !errors.Is(readErr, memy.ErrNotFound) {
				t.Fatalf("derived published: %v", readErr)
			}
		})
	}
}
