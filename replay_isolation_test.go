package memy_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

func TestProposalReplayKeepsOriginalRevisionAfterLaterEdits(t *testing.T) {
	// Arrange.
	f := newFixture(t, nil)
	initial := f.propose(t, "initial", "UTC+3", memy.Interval{})
	second, operationErr := f.engine.Revise(
		context.Background(),
		f.actor,
		f.scope,
		"second",
		initial.ID,
		1,
		"assist",
		f.suggestion("UTC+7", memy.Interval{}),
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	_, operationErr = f.engine.Revise(
		context.Background(),
		f.actor,
		f.scope,
		"third",
		initial.ID,
		2,
		"assist",
		f.suggestion("UTC+8", memy.Interval{}),
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	// Act.
	replay, replayErr := f.engine.Revise(
		context.Background(),
		f.actor,
		f.scope,
		"second",
		initial.ID,
		1,
		"assist",
		f.suggestion("UTC+7", memy.Interval{}),
	)
	// ObservedAt is unchanged because the deterministic clock did not advance.
	firstReplay, firstErr := f.engine.Remember(
		context.Background(),
		f.actor,
		f.scope,
		"initial",
		"assist",
		f.suggestion("UTC+3", memy.Interval{}),
	)
	_, staleErr := f.engine.Accept(
		context.Background(),
		f.actor,
		f.scope,
		replay.ID,
		replay.Digest,
		replay.Revision,
		"assist",
	)
	// Assert: replay is the old operation's result, never a current-head substitute.
	if replayErr != nil || firstErr != nil || replay.Revision != 2 || replay.Digest != second.Digest ||
		replay.Suggestion.Payload.Value != "UTC+7" ||
		firstReplay.Revision != 1 ||
		firstReplay.Digest != initial.Digest ||
		!errors.Is(staleErr, memy.ErrStaleAcceptance) {
		t.Fatalf("replay=%+v err=%v initial=%v stale=%v", replay, replayErr, firstErr, staleErr)
	}
}

func TestDuplicateLineageRevokesTransitiveDependents(t *testing.T) {
	// Arrange: originals and two generations of duplicates use separate IDs.
	f := newFixture(t, nil)
	for i, id := range []string{"original", "copy", "copy-of-copy"} {
		p := f.propose(t, "remember-"+id, "same preference", memy.Interval{})
		mode := memy.Append
		var refs []memy.RevisionRef
		if i > 0 {
			mode = memy.Duplicate
			refs = []memy.RevisionRef{{RecordID: []string{"original", "copy"}[i-1], Revision: 1}}
		}
		f.commit(t, f.acceptedRequest(t, "commit-"+id, id, 0, p, mode, refs...))
	}
	before, operationErr := f.engine.Get(context.Background(), f.actor, f.scope, "copy", memy.ReadOptions{})
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	// Act.
	receipt, operationErr := fullForget(f.engine,
		context.Background(),
		f.actor,
		f.scope,
		"assist",
		memy.ForgetRequest{
			OperationID: "forget-original",
			Selector:    memy.Selector{Kind: memy.SelectRecord, ID: "original"},
			Expected: []memy.RevisionRef{
				{RecordID: "original", Revision: 1},
			},
			Reason:        "withdraw original",
			PolicyVersion: "deletion/v1",
		},
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	// Assert.
	if len(before.Provenance.Lineage) != 1 || before.Provenance.Lineage[0].RecordID != "original" ||
		len(receipt.Batch.Records) != 3 {
		t.Fatalf("lineage=%+v receipt=%+v", before.Provenance.Lineage, receipt)
	}
	for _, id := range []string{"original", "copy", "copy-of-copy"} {
		_, err := f.engine.Get(context.Background(), f.actor, f.scope, id, memy.ReadOptions{})
		if !errors.Is(err, memy.ErrNotFound) {
			t.Fatalf("dependent %s survived: %v", id, err)
		}
	}
}

type rankerFunc[P, R any] func(context.Context, []memy.Ranked[P, R]) ([]memy.Ranked[P, R], error)

func (f rankerFunc[P, R]) Rank(ctx context.Context, input []memy.Ranked[P, R]) ([]memy.Ranked[P, R], error) {
	return f(ctx, input)
}

func TestRankerCannotRewriteConsumerMapsOrProvenance(t *testing.T) {
	// Arrange: P and R contain maps, so cloning only the slice is insufficient.
	f := newFixture(t, nil)
	sources := reference.NewRegistry[map[string]string](memy.JSONCodec[map[string]string]{})
	source := memy.Source[map[string]string]{
		ID:        "map-source",
		Revision:  "r1",
		Reference: map[string]string{"uri": "host://original"},
	}
	if err := sources.Put(f.scope, source); err != nil {
		t.Fatal(err)
	}
	engine, operationErr := memy.New(
		memy.Config[map[string]string, map[string]string, principal]{
			Store:          f.config.Store,
			Authority:      f.policy,
			Sources:        sources,
			Clock:          f.clock,
			Retention:      reference.Retain[map[string]string]{Version: "retention/v1"},
			PayloadCodec:   memy.JSONCodec[map[string]string]{},
			ReferenceCodec: memy.JSONCodec[map[string]string]{},
		},
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	p, operationErr := engine.Remember(
		context.Background(),
		f.actor,
		f.scope,
		"remember-map",
		"assist",
		memy.Suggestion[map[string]string, map[string]string]{Payload: map[string]string{"value": "canonical"},
			Sources: []memy.Source[map[string]string]{source}, Evidence: "host", Extractor: "manual/v1"},
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	a, operationErr := engine.Accept(context.Background(), f.actor, f.scope, p.ID, p.Digest, p.Revision, "assist")
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	r, operationErr := engine.Commit(
		context.Background(),
		f.actor,
		f.scope,
		"assist",
		memy.CommitRequest{OperationID: "commit-map", ProposalID: p.ID, Acceptance: a, RecordID: "map",
			Reconcile: memy.Reconciliation{Mode: memy.Append, PolicyVersion: "resolver/v1", Basis: "first fact"}},
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	index := reference.NewIndex[string]("index", nil)
	if err := index.Stage(
		context.Background(),
		f.scope,
		memy.Candidate{RecordID: "map", Revision: 1, Score: 1},
	); err != nil {
		t.Fatal(err)
	}
	if err := index.Acknowledge(context.Background(), r.Visibility); err != nil {
		t.Fatal(err)
	}
	malicious := rankerFunc[map[string]string, map[string]string](
		func(_ context.Context, input []memy.Ranked[map[string]string, map[string]string]) ([]memy.Ranked[map[string]string, map[string]string], error) {
			input[0].Record.Payload["value"] = "forged"
			input[0].Record.Provenance.Sources[0].Reference["uri"] = "host://forged"
			input[0].Record.Provenance.Sources[0].ID = "forged"
			return input, nil
		},
	)
	// Act.
	recalled, operationErr := memy.Recall(
		context.Background(),
		engine,
		f.actor,
		f.scope,
		"map",
		index,
		malicious,
		memy.RecallOptions{Limit: 1},
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	canonical, operationErr := engine.Get(context.Background(), f.actor, f.scope, "map", memy.ReadOptions{})
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	// Assert: callback's forged payload cannot replace validated canonical data.
	selected := recalled.Records[0].Record
	if selected.Payload["value"] != "canonical" || selected.Provenance.Sources[0].ID != "map-source" ||
		selected.Provenance.Sources[0].Reference["uri"] != "host://original" ||
		canonical.Payload["value"] != "canonical" {
		t.Fatalf("mutated result: %+v", selected)
	}
}

func TestConsolidationReplayAfterInputCorrection(t *testing.T) {
	// Arrange.
	f := newFixture(t, nil)
	refs := seedCorpus(t, f, readCorpus(t))
	request := consolidationRequest(refs, memy.ExactDedup)
	original, operationErr := memy.Consolidate(context.Background(), f.engine, f.actor, f.scope, request, nil)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	p := f.propose(t, "correction", "corrected tea preference", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit-correction", refs[0].RecordID, 1, p, memy.Supersede, refs[0]))
	// Act.
	replay, operationErr := memy.Consolidate(context.Background(), f.engine, f.actor, f.scope, request, nil)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	_, staleErr := f.engine.Accept(
		context.Background(),
		f.actor,
		f.scope,
		replay[0].ID,
		replay[0].Digest,
		replay[0].Revision,
		"assist",
	)
	// Assert: receipt/content revision recovers, but stale inputs cannot commit.
	if replay[0].Digest != original[0].Digest || !errors.Is(staleErr, memy.ErrStaleInput) {
		t.Fatalf("replay=%+v stale=%v", replay, staleErr)
	}
}

func TestAcceptanceRejectsExpiredAncestorBeforeSweep(t *testing.T) {
	// Arrange: the immediate parent remains live, but its ancestor expires.
	f := newFixture(t, nil)
	suggestion := f.suggestion("observed once", memy.Interval{})
	suggestion.ExpiresAt = f.clock.Now().Add(time.Hour)
	original, operationErr := f.engine.Remember(
		context.Background(),
		f.actor,
		f.scope,
		"original",
		"assist",
		suggestion,
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	f.commit(t, f.acceptedRequest(t, "commit-original", "original", 0, original, memy.Append))
	parent := f.propose(t, "parent", "derived observation", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit-parent", "parent", 0, parent, memy.Duplicate,
		memy.RevisionRef{RecordID: "original", Revision: 1}))
	childSuggestion := f.suggestion("further derived observation", memy.Interval{})
	childSuggestion.Lineage = []memy.RevisionRef{{RecordID: "parent", Revision: 1}}
	child, operationErr := f.engine.Remember(context.Background(), f.actor, f.scope, "child", "assist", childSuggestion)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	f.clock.Advance(2 * time.Hour)
	// Act: no scheduler or Sweep has removed the ancestor yet.
	_, acceptErr := f.engine.Accept(
		context.Background(),
		f.actor,
		f.scope,
		child.ID,
		child.Digest,
		child.Revision,
		"assist",
	)
	_, readErr := f.engine.Get(context.Background(), f.actor, f.scope, "parent", memy.ReadOptions{})
	// Assert: derived knowledge cannot extend the lifetime of its evidence.
	if !errors.Is(acceptErr, memy.ErrStaleInput) || !errors.Is(readErr, memy.ErrStaleInput) {
		t.Fatalf("expired ancestor accepted=%v read=%v", acceptErr, readErr)
	}
}

func TestExtractionAbstainsWithoutEvidence(t *testing.T) {
	// Arrange.
	f := newFixture(t, nil)
	provider := reference.ExtractorFunc[string, preference, sourceRef](
		func(context.Context, string) ([]memy.Suggestion[preference, sourceRef], error) {
			return nil, nil
		},
	)
	// Act.
	proposals, operationErr := memy.Extract(context.Background(), f.engine, f.actor, f.scope,
		memy.ExtractionJob{OperationID: "abstain", ProviderVersion: "scripted/v1", Purpose: "assist"},
		"uncertain observation", memy.JSONCodec[string]{}, provider)
	records, readErr := fullSnapshot(f.engine,
		context.Background(),
		f.actor,
		f.scope,
		memy.ReadOptions{IncludeUnknown: true},
	)
	// Assert: a provider's empty result never becomes a confirmed fact.
	if !errors.Is(operationErr, memy.ErrMissingEvidence) || len(proposals) != 0 || readErr != nil || len(records) != 0 {
		t.Fatalf("proposals=%v err=%v records=%v read=%v", proposals, operationErr, records, readErr)
	}
}

func TestCanonicalProposalExpiryClosesReadsAndPurges(t *testing.T) {
	// Arrange: evidence has a finite lifetime even though retention is unbounded.
	f := newFixture(t, nil)
	suggestion := f.suggestion("temporary observation", memy.Interval{})
	suggestion.ExpiresAt = f.clock.Now().Add(time.Hour)
	p, operationErr := f.engine.Remember(context.Background(), f.actor, f.scope, "temporary", "assist", suggestion)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	f.commit(t, f.acceptedRequest(t, "commit-temporary", "temporary", 0, p, memy.Append))
	before, annotationErr := f.engine.Get(context.Background(), f.actor, f.scope, "temporary", memy.ReadOptions{})
	if annotationErr != nil || !before.ExpiresAt.Equal(suggestion.ExpiresAt) || !before.Retention.ExpiresAt.IsZero() {
		t.Fatalf("expiry annotation=%+v err=%v", before, annotationErr)
	}
	f.clock.Advance(time.Hour)
	// Act.
	_, readErr := f.engine.Get(context.Background(), f.actor, f.scope, "temporary", memy.ReadOptions{})
	swept, sweepErr := fullSweep(f.engine, context.Background(), f.actor, f.scope, "assist", "expire")
	var contentEntries []memy.Entry
	inspectErr := f.config.Store.View(context.Background(), f.scope, func(b memy.Bucket) error {
		var listErr error
		contentEntries, listErr = listEntries(b, "record/")
		return listErr
	})
	// Assert: expiry is a deletion lifecycle, not only a search filter.
	if !errors.Is(readErr, memy.ErrNotFound) || sweepErr != nil || len(swept.Records) != 1 ||
		swept.Records[0].State != memy.PurgeComplete || inspectErr != nil || len(contentEntries) != 0 {
		t.Fatalf("read=%v sweep=%+v err=%v content=%v inspect=%v", readErr, swept, sweepErr, contentEntries, inspectErr)
	}
}

func TestZeroClockCannotPersistMalformedProposal(t *testing.T) {
	for _, path := range []string{"remember", "extract"} {
		t.Run(path, func(t *testing.T) {
			// Arrange: an invalid clock must not create success followed by schema failure.
			f := newFixture(t, nil)
			f.clock.Set(time.Time{})
			var operationErr error
			// Act.
			if path == "remember" {
				_, operationErr = f.engine.Remember(
					context.Background(),
					f.actor,
					f.scope,
					"zero-clock",
					"assist",
					f.suggestion("value", memy.Interval{}),
				)
			} else {
				provider := reference.ExtractorFunc[string, preference, sourceRef](
					func(context.Context, string) ([]memy.Suggestion[preference, sourceRef], error) {
						return []memy.Suggestion[preference, sourceRef]{f.suggestion("value", memy.Interval{})}, nil
					},
				)
				_, operationErr = memy.Extract(
					context.Background(),
					f.engine,
					f.actor,
					f.scope,
					memy.ExtractionJob{OperationID: "zero-clock", ProviderVersion: "scripted/v1", Purpose: "assist"},
					"input",
					memy.JSONCodec[string]{},
					provider,
				)
			}
			var stored []memy.Entry
			inspectErr := f.config.Store.View(context.Background(), f.scope, func(b memy.Bucket) error {
				entries, listErr := listEntries(b, "")
				stored = entries
				return listErr
			})
			// Assert: neither content nor an operation receipt is partially persisted.
			if !errors.Is(operationErr, memy.ErrInvalid) || inspectErr != nil || len(stored) != 0 {
				t.Fatalf("operation=%v inspect=%v stored=%v", operationErr, inspectErr, stored)
			}
		})
	}
}

func TestZeroClockCannotPersistMalformedCommit(t *testing.T) {
	// Arrange: proposal/review were valid, then the host clock becomes invalid.
	f := newFixture(t, nil)
	p := f.propose(t, "remember", "private value", memy.Interval{})
	request := f.acceptedRequest(t, "commit", "timezone", 0, p, memy.Append)
	f.clock.Set(time.Time{})
	// Act.
	_, commitErr := f.engine.Commit(context.Background(), f.actor, f.scope, "assist", request)
	var histories []memy.Entry
	var epoch memy.Value
	inspectErr := f.config.Store.View(context.Background(), f.scope, func(b memy.Bucket) error {
		var listErr error
		histories, listErr = listEntries(b, "record/")
		if listErr != nil {
			return listErr
		}
		epoch, listErr = b.Get("epoch")
		return listErr
	})
	// Assert: no malformed recorded time or receipt is committed.
	if !errors.Is(commitErr, memy.ErrInvalid) || inspectErr != nil || len(histories) != 0 || epoch.Data != nil {
		t.Fatalf("commit=%v inspect=%v history=%v epoch=%+v", commitErr, inspectErr, histories, epoch)
	}
}
