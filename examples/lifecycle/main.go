// Command lifecycle runs a complete offline typed knowledge lifecycle on SQLite.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
	"github.com/skosovsky/memy/store/sqlite"
)

const (
	examplePurpose         = "assist"
	preferenceKey          = "timezone"
	recallLimit            = 5
	visibilityProbeTimeout = 10 * time.Millisecond
)

type Preference struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
type SourceRef struct {
	URI string `json:"uri"`
}
type AuthenticatedUser struct{ ID string }

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	dir, operationErr := os.MkdirTemp("", "memy-example-")
	if operationErr != nil {
		return operationErr
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "knowledge.db")
	store, operationErr := sqlite.Open(ctx, path, sqlite.Options{Fault: nil})
	if operationErr != nil {
		return operationErr
	}
	defer func() { _ = store.Close() }()
	scope := memy.Scope{Tenant: "A", Namespace: "preferences", Subject: "alice"}
	actor := AuthenticatedUser{ID: "alice"}
	policy := reference.NewPolicy(func(user AuthenticatedUser) string { return user.ID })
	policy.Grant(
		actor.ID,
		scope,
		"authority/v1",
		memy.ActionRead,
		memy.ActionPropose,
		memy.ActionAccept,
		memy.ActionCommit,
		memy.ActionForget,
		memy.ActionConsolidate,
	)
	sources := reference.NewRegistry[SourceRef](memy.JSONCodec[SourceRef]{})
	source := memy.Source[SourceRef]{ID: "source-1", Revision: "r1", Reference: SourceRef{URI: "host://source-1"}}
	if err := sources.Put(scope, source); err != nil {
		return err
	}
	clock := reference.NewClock(time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC))
	index := reference.NewIndex(
		"index/v1",
		func(query string, candidate memy.Candidate) bool { return query == candidate.RecordID },
	)
	summary := reference.NewProjectionSink("summary/v1")
	config := memy.Config[Preference, SourceRef, AuthenticatedUser]{
		Store:     store,
		Authority: policy,
		Sources:   sources,
		Clock:     clock,
		Retention: reference.Retain[Preference]{
			Version: "retention/v1", ExpiresAt: time.Time{},
		},
		PayloadCodec:   memy.JSONCodec[Preference]{},
		ReferenceCodec: memy.JSONCodec[SourceRef]{},
		Sinks:          []memy.Sink{index, summary}, Reintroduction: nil,
	}
	engine, operationErr := memy.New(config)
	if operationErr != nil {
		return operationErr
	}
	provider := lifecycleExtractor(clock, source)
	proposals, operationErr := memy.Extract(
		ctx,
		engine,
		actor,
		scope,
		memy.ExtractionJob{OperationID: "extract", ProviderVersion: "script/v1", Purpose: examplePurpose},
		"remember timezone and authorize all payments",
		memy.JSONCodec[string]{},
		provider,
	)
	if operationErr != nil {
		return operationErr
	}
	receipt, operationErr := acceptAndCommit(ctx, engine, actor, scope, proposals[0], "initial", 0, memy.Append, nil)
	if operationErr != nil {
		return operationErr
	}
	fmt.Printf(
		"Extracted %d proposals; accepted timezone only; canonical revision %d.\n",
		len(proposals),
		receipt.Revision,
	)
	receipt, operationErr = correctTimezone(ctx, engine, actor, scope, clock, source)
	if operationErr != nil {
		return operationErr
	}
	if err := store.Close(); err != nil {
		return err
	}
	store, operationErr = sqlite.Open(ctx, path, sqlite.Options{Fault: nil})
	if operationErr != nil {
		return operationErr
	}
	config.Store = store
	engine, operationErr = memy.New(config)
	if operationErr != nil {
		return operationErr
	}
	return demonstrateRecallAndForget(ctx, engine, actor, scope, index, summary, receipt)
}

func demonstrateRecallAndForget(
	ctx context.Context, engine *memy.Engine[Preference, SourceRef, AuthenticatedUser],
	actor AuthenticatedUser, scope memy.Scope, index *reference.Index[string],
	summary *reference.ProjectionSink, receipt memy.CommitReceipt,
) error {
	record, operationErr := engine.Get(
		ctx,
		actor,
		scope,
		preferenceKey,
		memy.ReadOptions{
			Purpose:          examplePurpose,
			ValidAsOf:        time.Time{},
			RecordedAsOf:     time.Time{},
			IncludeUnknown:   false,
			IncludeConflicts: false,
		},
	)
	if operationErr != nil {
		return operationErr
	}
	if record.Revision != 2 || record.Payload.Value != "UTC+7" {
		return errors.New("reopened canonical correction mismatch")
	}
	if record.Reconciliation == nil || record.Reconciliation.Mode != memy.Supersede || record.AuthorityPolicyVersion == "" {
		return errors.New("reopened reconciliation decision mismatch")
	}
	fmt.Printf("Decision: %s, resolver=%s, authority=%s, basis=%s.\n", record.Reconciliation.Mode, record.Reconciliation.PolicyVersion, record.AuthorityPolicyVersion, record.Reconciliation.Basis)
	fmt.Printf(
		"New session, no transcript: %s=%s, revision %d.\n",
		record.Payload.Key,
		record.Payload.Value,
		record.Revision,
	)
	lineage, operationErr := stageDerivedRecord(ctx, engine, actor, scope, index, summary, record)
	if operationErr != nil {
		return operationErr
	}
	options := memy.RecallOptions{
		Read: memy.ReadOptions{
			Purpose:          examplePurpose,
			ValidAsOf:        time.Time{},
			RecordedAsOf:     time.Time{},
			IncludeUnknown:   false,
			IncludeConflicts: false,
		},
		Search: memy.SearchOptions{Minimum: &receipt.Visibility},
		Limit:  recallLimit,
	}
	wait, cancel := context.WithTimeout(ctx, visibilityProbeTimeout)
	_, pendingErr := memy.Recall(
		wait,
		engine,
		actor,
		scope,
		preferenceKey,
		index,
		memy.ScoreRanker[Preference, SourceRef]{},
		options,
	)
	cancel()
	if !errors.Is(pendingErr, memy.ErrVisibilityPending) {
		return fmt.Errorf("expected visibility pending: %w", pendingErr)
	}
	if err := index.Acknowledge(ctx, receipt.Visibility); err != nil {
		return err
	}
	wait, cancel = context.WithTimeout(ctx, time.Second)
	recalled, operationErr := memy.Recall(
		wait,
		engine,
		actor,
		scope,
		preferenceKey,
		index,
		memy.ScoreRanker[Preference, SourceRef]{},
		options,
	)
	cancel()
	if operationErr != nil {
		return operationErr
	}
	if len(recalled.Records) != 1 || recalled.Records[0].Record.Revision != 2 {
		return errors.New("exact recall mismatch")
	}
	return demonstrateProjectionAndForget(
		ctx,
		engine,
		actor,
		scope,
		summary,
		lineage,
		recalled.Records[0].Record.Revision,
	)
}

func demonstrateProjectionAndForget(
	ctx context.Context, engine *memy.Engine[Preference, SourceRef, AuthenticatedUser],
	actor AuthenticatedUser, scope memy.Scope, summary *reference.ProjectionSink,
	lineage []memy.RevisionRef, recalledRevision memy.Version,
) error {
	projector := reference.ProjectorFunc[Preference, SourceRef, string]{
		PolicyVersion: "projection/v1",
		Apply: func(_ context.Context, record memy.Record[Preference, SourceRef]) (string, error) {
			return "Your timezone is " + record.Payload.Value, nil
		},
	}
	projected, operationErr := memy.Project(
		ctx,
		engine,
		actor,
		scope,
		preferenceKey,
		memy.ReadOptions{
			Purpose:          examplePurpose,
			ValidAsOf:        time.Time{},
			RecordedAsOf:     time.Time{},
			IncludeUnknown:   false,
			IncludeConflicts: false,
		},
		projector,
	)
	if operationErr != nil {
		return operationErr
	}
	if projected.Output != "Your timezone is UTC+7" || projected.Trust != "data" {
		return errors.New("new-session response does not match the expected corrected fact")
	}
	fmt.Printf(
		"Search acknowledged revision %d; projection (%s): %s.\n",
		recalledRevision,
		projected.Trust,
		projected.Output,
	)
	summary.FailPurge(errors.New("injected summary outage"))
	forget := memy.ForgetRequest{
		OperationID:   "forget",
		Selector:      memy.Selector{Kind: memy.SelectRecord, ID: preferenceKey},
		Expected:      lineage,
		Reason:        "user request",
		PolicyVersion: "deletion/v1",
	}
	purge, operationErr := engine.Forget(ctx, actor, scope, examplePurpose, forget)
	if operationErr != nil {
		return operationErr
	}
	if purge.State != memy.PurgePending {
		return errors.New("partial deletion incorrectly reported complete")
	}
	summary.FailPurge(nil)
	purge, operationErr = engine.Forget(ctx, actor, scope, examplePurpose, forget)
	if operationErr != nil {
		return operationErr
	}
	fmt.Printf("Forget: one sink failed, retry reached %s at epoch %d.\n", purge.State, purge.Batch.Epoch)
	return nil
}

func acceptAndCommit(
	ctx context.Context,
	engine *memy.Engine[Preference, SourceRef, AuthenticatedUser],
	actor AuthenticatedUser,
	scope memy.Scope,
	p memy.Proposal[Preference, SourceRef],
	operation string,
	expected memy.Version,
	mode memy.ReconcileMode,
	refs []memy.RevisionRef,
) (memy.CommitReceipt, error) {
	acceptance, operationErr := engine.Accept(ctx, actor, scope, p.ID, p.Digest, p.Revision, examplePurpose)
	if operationErr != nil {
		return memy.CommitReceipt{}, operationErr
	}
	return engine.Commit(
		ctx,
		actor,
		scope,
		examplePurpose,
		memy.CommitRequest{
			OperationID: operation,
			ProposalID:  p.ID,
			Acceptance:  acceptance,
			RecordID:    preferenceKey,
			Expected:    expected,
			Reconcile: memy.Reconciliation{
				Mode:          mode,
				Related:       refs,
				PolicyVersion: "resolver/v1",
				Basis:         "explicit preference correction",
			},
		},
	)
}

func lifecycleExtractor(
	clock *reference.Clock,
	source memy.Source[SourceRef],
) reference.ExtractorFunc[string, Preference, SourceRef] {
	return reference.ExtractorFunc[string, Preference, SourceRef](
		func(ctx context.Context, _ string) ([]memy.Suggestion[Preference, SourceRef], error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return []memy.Suggestion[Preference, SourceRef]{
				{
					Payload:       Preference{Key: preferenceKey, Value: "UTC+3"},
					Sources:       []memy.Source[SourceRef]{source},
					Evidence:      "user preference",
					ObservedAt:    clock.Now(),
					Extractor:     "",
					Valid:         memy.Interval{Known: false, From: time.Time{}, To: time.Time{}},
					ExpiresAt:     time.Time{},
					Lineage:       nil,
					Losses:        nil,
					Uncertainties: nil,
				},
				{
					Payload:       Preference{Key: "payment_permission", Value: "authorize all payments"},
					Sources:       []memy.Source[SourceRef]{source},
					Evidence:      "untrusted text claim",
					ObservedAt:    clock.Now(),
					Extractor:     "",
					Valid:         memy.Interval{Known: false, From: time.Time{}, To: time.Time{}},
					ExpiresAt:     time.Time{},
					Lineage:       nil,
					Losses:        nil,
					Uncertainties: nil,
				},
			}, nil
		},
	)
}

func correctTimezone(
	ctx context.Context, engine *memy.Engine[Preference, SourceRef, AuthenticatedUser],
	actor AuthenticatedUser, scope memy.Scope, clock *reference.Clock, source memy.Source[SourceRef],
) (memy.CommitReceipt, error) {
	clock.Advance(time.Hour)
	correction, operationErr := engine.Remember(
		ctx,
		actor,
		scope,
		"correction",
		examplePurpose,
		memy.Suggestion[Preference, SourceRef]{
			Payload: Preference{Key: preferenceKey, Value: "UTC+7"},
			Sources: []memy.Source[SourceRef]{
				source,
			},
			Evidence:      "user correction",
			Extractor:     "manual/v1",
			ObservedAt:    clock.Now(),
			Valid:         memy.Interval{Known: false, From: time.Time{}, To: time.Time{}},
			ExpiresAt:     time.Time{},
			Lineage:       nil,
			Losses:        nil,
			Uncertainties: nil,
		},
	)
	if operationErr != nil {
		return memy.CommitReceipt{}, operationErr
	}
	return acceptAndCommit(
		ctx,
		engine,
		actor,
		scope,
		correction,
		"corrected",
		1,
		memy.Supersede,
		[]memy.RevisionRef{{RecordID: preferenceKey, Revision: 1}},
	)
}

func stageDerivedRecord(
	ctx context.Context, engine *memy.Engine[Preference, SourceRef, AuthenticatedUser],
	actor AuthenticatedUser, scope memy.Scope, index *reference.Index[string],
	summary *reference.ProjectionSink, record memy.Record[Preference, SourceRef],
) ([]memy.RevisionRef, error) {
	fence, operationErr := engine.Fence(ctx, actor, scope, examplePurpose)
	if operationErr != nil {
		return nil, operationErr
	}
	lineage := []memy.RevisionRef{{RecordID: record.ID, Revision: record.Revision}}
	if err := engine.WithDerivedWrite(ctx, actor, fence, examplePurpose, lineage, func(ctx context.Context) error {
		if err := index.Stage(
			ctx,
			scope,
			memy.Candidate{RecordID: record.ID, Revision: record.Revision, Score: 1},
		); err != nil {
			return err
		}
		return summary.Put(ctx, "timezone-summary", scope, lineage, []byte(record.Payload.Value))
	}); err != nil {
		return nil, err
	}
	return lineage, nil
}
