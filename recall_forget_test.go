package memy_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

func withSinks(t *testing.T) (fixture, *reference.Index[string], *reference.ProjectionSink) {
	t.Helper()
	return withSinksStore(t, nil)
}

func withSinksStore(t *testing.T, store memy.Store) (fixture, *reference.Index[string], *reference.ProjectionSink) {
	t.Helper()
	f := newFixture(t, store)
	index := reference.NewIndex("index/v1", func(query string, c memy.Candidate) bool { return query == c.RecordID })
	summary := reference.NewProjectionSink("summary/v1")
	f.config.Sinks = []memy.Sink{index, summary}
	var operationErr error
	f.engine, operationErr = memy.New(f.config)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	return f, index, summary
}

func staged(
	t *testing.T,
	f fixture,
	index *reference.Index[string],
	summary *reference.ProjectionSink,
) (memy.CommitReceipt, memy.EpochFence) {
	t.Helper()
	p := f.propose(t, "remember", "secret preference", memy.Interval{})
	receipt := f.commit(t, f.acceptedRequest(t, "commit", "timezone", 0, p, memy.Append))
	fence, operationErr := f.engine.Fence(context.Background(), f.actor, f.scope, "assist")
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	lineage := []memy.RevisionRef{{RecordID: receipt.RecordID, Revision: receipt.Revision}}
	operationErr = f.engine.WithDerivedWrite(
		context.Background(),
		f.actor,
		fence,
		"assist",
		lineage,
		func(ctx context.Context) error {
			if err := index.Stage(
				ctx,
				f.scope,
				memy.Candidate{RecordID: receipt.RecordID, Revision: receipt.Revision, Score: 1},
			); err != nil {
				return err
			}
			return summary.Put(ctx, "summary-1", f.scope, lineage, []byte("secret preference"))
		},
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	return receipt, fence
}

func TestCanonicalCommitAndSearchVisibility(t *testing.T) {
	// Arrange: canonical commit exists, index has not acknowledged it.
	f, index, summary := withSinks(t)
	receipt, _ := staged(t, f, index, summary)
	options := memy.RecallOptions{
		Read:   memy.ReadOptions{Purpose: "assist"},
		Search: memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates, Minimum: &receipt.Visibility},
		Limit:  5,
	}
	// Act.
	canonical, canonicalErr := f.engine.Get(context.Background(), f.actor, f.scope, "timezone", memy.ReadOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	pending, pendingErr := memy.Recall(
		ctx,
		f.engine,
		f.actor,
		f.scope,
		"timezone",
		index,
		memy.ScoreRanker[preference, sourceRef]{},
		options,
	)
	cancel()
	if err := index.Acknowledge(context.Background(), receipt.Visibility); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	visible, visibleErr := memy.Recall(
		ctx,
		f.engine,
		f.actor,
		f.scope,
		"timezone",
		index,
		memy.ScoreRanker[preference, sourceRef]{},
		options,
	)
	// Assert.
	if canonicalErr != nil || canonical.Revision != 1 || !errors.Is(pendingErr, memy.ErrVisibilityPending) ||
		len(pending.Records) != 0 {
		t.Fatalf("canonical=%v pending=%v", canonicalErr, pendingErr)
	}
	if visibleErr != nil || len(visible.Coverage) != 1 || visible.Coverage[0].Status != "ready" || !visible.Coverage[0].MinimumSatisfied || len(visible.Records) != 1 ||
		visible.Records[0].Record.Revision != receipt.Revision {
		t.Fatalf("visible=%+v err=%v", visible, visibleErr)
	}
}

func TestVisibilityCancellationAndBackendFailure(t *testing.T) {
	// Arrange.
	f, index, summary := withSinks(t)
	receipt, _ := staged(t, f, index, summary)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	cancel()
	options := memy.RecallOptions{
		Read:   memy.ReadOptions{Purpose: "assist"},
		Search: memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates, Minimum: &receipt.Visibility},
		Limit:  1,
	}
	// Act.
	_, cancelErr := memy.Recall(
		ctx,
		f.engine,
		f.actor,
		f.scope,
		"timezone",
		index,
		memy.ScoreRanker[preference, sourceRef]{},
		options,
	)
	canonical, canonicalErr := f.engine.Get(context.Background(), f.actor, f.scope, "timezone", memy.ReadOptions{})
	index.Fail(errors.New("search down"))
	options.Search.Minimum = nil
	failed, failedErr := memy.Recall(
		context.Background(),
		f.engine,
		f.actor,
		f.scope,
		"timezone",
		index,
		memy.ScoreRanker[preference, sourceRef]{},
		options,
	)
	// Assert.
	if !errors.Is(cancelErr, context.Canceled) || canonicalErr != nil || canonical.Revision != 1 {
		t.Fatalf("cancel=%v canonical=%v", cancelErr, canonicalErr)
	}
	if !errors.Is(failedErr, memy.ErrUnavailable) || len(failed.Coverage) != 1 ||
		failed.Coverage[0].Status != "unavailable" {
		t.Fatalf("failure faked absence: %+v %v", failed, failedErr)
	}
}

func TestForgetPartialPurgeRetryAndLateJobs(t *testing.T) {
	// Arrange.
	f, index, summary := withSinks(t)
	receipt, fence := staged(t, f, index, summary)
	if err := index.Acknowledge(context.Background(), receipt.Visibility); err != nil {
		t.Fatal(err)
	}
	summary.FailPurge(errors.New("summary unavailable"))
	request := memy.ForgetRequest{
		OperationID: "forget",
		Selector:    memy.Selector{Kind: memy.SelectRecord, ID: "timezone"},
		Expected: []memy.RevisionRef{
			{RecordID: "timezone", Revision: 1},
		},
		Reason:        "user request",
		PolicyVersion: "deletion/v1",
	}
	// Act.
	pending, operationErr := fullForget(f.engine, context.Background(), f.actor, f.scope, "assist", request)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	_, closedErr := f.engine.Get(
		context.Background(),
		f.actor,
		f.scope,
		"timezone",
		memy.ReadOptions{RecordedAsOf: f.clock.Now().Add(-time.Second)},
	)
	lateErr := f.engine.WithDerivedWrite(
		context.Background(),
		f.actor,
		fence,
		"assist",
		[]memy.RevisionRef{{RecordID: "timezone", Revision: 1}},
		func(ctx context.Context) error {
			return index.Stage(ctx, f.scope, memy.Candidate{RecordID: "timezone", Revision: 1})
		},
	)
	summary.FailPurge(nil)
	complete, completeErr := fullForget(f.engine, context.Background(), f.actor, f.scope, "assist", request)
	repeated, repeatErr := fullForget(f.engine, context.Background(), f.actor, f.scope, "assist", request)
	// A privileged stale index restoration still cannot expose canonical payload.
	if err := index.Stage(
		context.Background(),
		f.scope,
		memy.Candidate{RecordID: "timezone", Revision: 1},
	); err != nil {
		t.Fatal(err)
	}
	if err := index.Acknowledge(context.Background(), receipt.Visibility); err != nil {
		t.Fatal(err)
	}
	recalled, recallErr := memy.Recall(
		context.Background(),
		f.engine,
		f.actor,
		f.scope,
		"timezone",
		index,
		memy.ScoreRanker[preference, sourceRef]{},
		memy.RecallOptions{Search: memy.SearchOptions{MaxCandidates: memy.MaxSearchCandidates}, Limit: 1},
	)
	// Assert.
	if pending.State != memy.PurgePending || len(pending.Sinks) != 2 || !pending.Sinks[0].Acknowledged ||
		pending.Sinks[1].Acknowledged {
		t.Fatalf("partial receipt: %+v", pending)
	}
	if !errors.Is(closedErr, memy.ErrNotFound) || !errors.Is(lateErr, memy.ErrStaleInput) {
		t.Fatalf("read=%v late=%v", closedErr, lateErr)
	}
	if completeErr != nil || repeatErr != nil || complete.State != memy.PurgeComplete ||
		repeated.Batch.Epoch != complete.Batch.Epoch ||
		summary.Contains("summary-1") {
		t.Fatalf("complete=%+v err=%v repeated=%v", complete, completeErr, repeatErr)
	}
	if recallErr != nil || len(recalled.Records) != 0 {
		t.Fatalf("stale index leaked: %+v %v", recalled, recallErr)
	}
	assertForgottenBytes(t, f)
}

func assertForgottenBytes(t *testing.T, f fixture) {
	t.Helper()
	// Inspect all remaining canonical live bytes: no payload or evidence survives.
	encodedPayload, operationErr := (memy.JSONCodec[preference]{}).Encode(
		preference{Key: "timezone", Value: "secret preference"},
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	operationErr = f.config.Store.View(context.Background(), f.scope, func(b memy.Bucket) error {
		entries, transactionErr := listEntries(b, "")
		if transactionErr != nil {
			return transactionErr
		}
		for _, entry := range entries {
			if strings.Contains(string(entry.Value.Data), "secret preference") ||
				strings.Contains(string(entry.Value.Data), "explicit host input") ||
				strings.Contains(string(entry.Value.Data), base64.StdEncoding.EncodeToString(encodedPayload)) {
				return errors.New("forgotten content survived")
			}
		}
		return nil
	})
	if operationErr != nil {
		t.Fatal(operationErr)
	}
}

func TestSourceForgetPurgesHistoricalPayloadAfterCorrection(t *testing.T) {
	// Arrange: old source exists only in historical revision and origin proposal.
	f := newFixture(t, nil)
	p := f.propose(t, "old-source", "old secret", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit-old", "timezone", 0, p, memy.Append))
	oldSourceID := f.source.ID
	f.source = memy.Source[sourceRef]{ID: "source-2", Revision: "r1", Reference: sourceRef{URI: "host://source-2"}}
	if err := f.sources.Put(f.scope, f.source); err != nil {
		t.Fatal(err)
	}
	p = f.propose(t, "new-source", "new value", memy.Interval{})
	f.commit(
		t,
		f.acceptedRequest(
			t,
			"commit-new",
			"timezone",
			1,
			p,
			memy.Supersede,
			memy.RevisionRef{RecordID: "timezone", Revision: 1},
		),
	)
	// Act.
	receipt, operationErr := fullForget(f.engine,
		context.Background(),
		f.actor,
		f.scope,
		"assist",
		memy.ForgetRequest{
			OperationID:   "forget-source",
			Selector:      memy.Selector{Kind: memy.SelectSource, ID: oldSourceID},
			Reason:        "withdraw source",
			PolicyVersion: "deletion/v1",
		},
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	encoded, operationErr := (memy.JSONCodec[preference]{}).Encode(preference{Key: "timezone", Value: "old secret"})
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	var leaked bool
	operationErr = f.config.Store.View(context.Background(), f.scope, func(b memy.Bucket) error {
		entries, transactionErr := listEntries(b, "")
		if transactionErr != nil {
			return transactionErr
		}
		for _, entry := range entries {
			if strings.Contains(string(entry.Value.Data), base64.StdEncoding.EncodeToString(encoded)) {
				leaked = true
			}
		}
		return nil
	})
	// Assert: logical canonical storage does not retain the old payload even
	// though the source disappeared from the newest revision's provenance.
	if operationErr != nil || leaked || len(receipt.Batch.Records) != 1 || receipt.Batch.Records[0] != "timezone" {
		t.Fatalf("leak=%t receipt=%+v err=%v", leaked, receipt, operationErr)
	}
}

func TestLateExtractionFencedByForget(t *testing.T) {
	// Arrange: provider is paused after the engine captures the input epoch.
	f, _, _ := withSinks(t)
	entered, release := make(chan struct{}), make(chan struct{})
	provider := reference.ExtractorFunc[string, preference, sourceRef](
		func(context.Context, string) ([]memy.Suggestion[preference, sourceRef], error) {
			close(entered)
			<-release
			return []memy.Suggestion[preference, sourceRef]{f.suggestion("late secret", memy.Interval{})}, nil
		},
	)
	result := make(chan error, 1)
	// Act.
	go func() {
		_, transactionErr := memy.Extract(
			context.Background(),
			f.engine,
			f.actor,
			f.scope,
			memy.ExtractionJob{OperationID: "delayed", ProviderVersion: "script/v1"},
			"input",
			memy.JSONCodec[string]{},
			provider,
		)
		result <- transactionErr
	}()
	<-entered
	_, operationErr := fullForget(f.engine,
		context.Background(),
		f.actor,
		f.scope,
		"assist",
		memy.ForgetRequest{
			OperationID:   "forget-scope",
			Selector:      memy.Selector{Kind: memy.SelectScope},
			Reason:        "forget",
			PolicyVersion: "deletion/v1",
		},
	)
	close(release)
	lateErr := <-result
	// Assert.
	if operationErr != nil || !errors.Is(lateErr, memy.ErrStaleInput) {
		t.Fatalf("forget=%v late=%v", operationErr, lateErr)
	}
}

func TestTypedProjectionAndExportAuthority(t *testing.T) {
	// Arrange: output is consumer-owned text, but metadata remains data trust.
	f := newFixture(t, nil)
	p := f.propose(t, "remember", "UTC+7", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit", "timezone", 0, p, memy.Append))
	projector := reference.ProjectorFunc[preference, sourceRef, string]{
		PolicyVersion: "projection/v1",
		Apply: func(_ context.Context, r memy.Record[preference, sourceRef]) (string, error) {
			return r.Payload.Key + "=" + r.Payload.Value, nil
		},
	}
	// Act.
	output, operationErr := memy.Project(
		context.Background(),
		f.engine,
		f.actor,
		f.scope,
		"timezone",
		memy.ReadOptions{Purpose: "assist"},
		projector,
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	serialized, operationErr := json.Marshal(output)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	denied, deniedErr := memy.Project(
		context.Background(),
		f.engine,
		principal{actor: "bob"},
		f.scope,
		"timezone",
		memy.ReadOptions{},
		projector,
	)
	private, operationErr := json.Marshal(denied)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	// Assert.
	if output.Trust != "data" || output.CacheKey == "" || !strings.Contains(string(serialized), "UTC+7") ||
		!errors.Is(deniedErr, memy.ErrUnauthorized) ||
		strings.Contains(string(private), "UTC+7") {
		t.Fatalf("projection=%+v denied=%v", output, deniedErr)
	}
}
