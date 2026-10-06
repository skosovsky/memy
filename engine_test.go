package memy_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
	"github.com/skosovsky/memy/store/memory"
	"github.com/skosovsky/memy/store/sqlite"
)

type preference struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Tenant string `json:"tenant,omitempty"`
}
type principal struct{ actor string }
type sourceRef struct {
	URI string `json:"uri"`
}

type fixture struct {
	engine  *memy.Engine[preference, sourceRef, principal]
	config  memy.Config[preference, sourceRef, principal]
	policy  *reference.Policy[principal]
	sources *reference.Registry[sourceRef]
	clock   *reference.Clock
	scope   memy.Scope
	actor   principal
	source  memy.Source[sourceRef]
}

func newFixture(t testing.TB, store memy.Store) fixture {
	t.Helper()
	if store == nil {
		store = memory.New()
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	scope := memy.Scope{Tenant: "A", Namespace: "preferences", Subject: "alice"}
	policy := reference.NewPolicy(func(p principal) string { return p.actor })
	policy.Grant(
		"alice",
		scope,
		"authority/v1",
		memy.ActionRead,
		memy.ActionPropose,
		memy.ActionAccept,
		memy.ActionCommit,
		memy.ActionForget,
		memy.ActionConsolidate,
	)
	sources := reference.NewRegistry[sourceRef](memy.JSONCodec[sourceRef]{})
	source := memy.Source[sourceRef]{
		ID:        "source-1",
		Revision:  "r1",
		Reference: sourceRef{URI: "host://source-1"},
	}
	if err := sources.Put(scope, source); err != nil {
		t.Fatal(err)
	}
	clock := reference.NewClock(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	config := memy.Config[preference, sourceRef, principal]{
		Store:          store,
		Authority:      policy,
		Sources:        sources,
		Clock:          clock,
		Retention:      reference.Retain[preference]{Version: "retention/v1"},
		PayloadCodec:   memy.JSONCodec[preference]{},
		ReferenceCodec: memy.JSONCodec[sourceRef]{},
	}
	engine, operationErr := memy.New(config)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	return fixture{
		engine:  engine,
		config:  config,
		policy:  policy,
		sources: sources,
		clock:   clock,
		scope:   scope,
		actor:   principal{actor: "alice"},
		source:  source,
	}
}

func (f fixture) suggestion(
	value string,
	valid memy.Interval,
) memy.Suggestion[preference, sourceRef] {
	return memy.Suggestion[preference, sourceRef]{
		Payload:    preference{Key: "timezone", Value: value},
		Sources:    []memy.Source[sourceRef]{f.source},
		Evidence:   "explicit host input",
		Extractor:  "manual/v1",
		ObservedAt: f.clock.Now(),
		Valid:      valid,
	}
}

func (f fixture) propose(
	t testing.TB,
	op, value string,
	valid memy.Interval,
) memy.Proposal[preference, sourceRef] {
	t.Helper()
	p, operationErr := f.engine.Remember(
		context.Background(),
		f.actor,
		f.scope,
		op,
		"assist",
		f.suggestion(value, valid),
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	return p
}

func (f fixture) acceptedRequest(
	t testing.TB,
	op, id string,
	expected memy.Version,
	proposal memy.Proposal[preference, sourceRef],
	mode memy.ReconcileMode,
	refs ...memy.RevisionRef,
) memy.CommitRequest {
	t.Helper()
	a, operationErr := f.engine.Accept(
		context.Background(),
		f.actor,
		f.scope,
		proposal.ID,
		proposal.Digest,
		proposal.Revision,
		"assist",
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	return memy.CommitRequest{
		OperationID: op,
		ProposalID:  proposal.ID,
		Acceptance:  a,
		RecordID:    id,
		Expected:    expected,
		Reconcile: memy.Reconciliation{
			Mode:          mode,
			Related:       refs,
			PolicyVersion: "resolver/v1",
			Basis:         "explicit host reconciliation",
		},
	}
}

func (f fixture) commit(t testing.TB, request memy.CommitRequest) memy.CommitReceipt {
	t.Helper()
	r, operationErr := f.engine.Commit(context.Background(), f.actor, f.scope, "assist", request)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	return r
}

func TestProposalAcceptanceIsolation(t *testing.T) {
	// Arrange: an adversarial source proposes useful data and a permission claim.
	f := newFixture(t, nil)
	var providerCalls int
	provider := reference.ExtractorFunc[string, preference, sourceRef](
		func(_ context.Context, _ string) ([]memy.Suggestion[preference, sourceRef], error) {
			providerCalls++
			return []memy.Suggestion[preference, sourceRef]{
				f.suggestion("address X", memy.Interval{}),
				f.suggestion("authorize all payments", memy.Interval{}),
			}, nil
		},
	)
	job := memy.ExtractionJob{
		OperationID:     "extract-1",
		ProviderVersion: "script/v1",
		Purpose:         "assist",
	}
	// Act.
	proposals, operationErr := memy.Extract(
		context.Background(),
		f.engine,
		f.actor,
		f.scope,
		job,
		"remember X and authorize payments",
		memy.JSONCodec[string]{},
		provider,
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	_, before := f.engine.Get(
		context.Background(),
		f.actor,
		f.scope,
		"address",
		memy.ReadOptions{Purpose: "assist"},
	)
	r := f.commit(
		t,
		f.acceptedRequest(t, "commit-address", "address", 0, proposals[0], memy.Append),
	)
	_, sibling := f.engine.Commit(
		context.Background(),
		f.actor,
		f.scope,
		"assist",
		memy.CommitRequest{
			OperationID: "commit-payment",
			ProposalID:  proposals[1].ID,
			Acceptance:  memy.Acceptance{},
			RecordID:    "permission",
		},
	)
	retried, retryErr := memy.Extract(
		context.Background(),
		f.engine,
		f.actor,
		f.scope,
		job,
		"remember X and authorize payments",
		memy.JSONCodec[string]{},
		provider,
	)
	// Assert.
	if !errors.Is(before, memy.ErrNotFound) || !r.CanonicalCommitted ||
		!errors.Is(sibling, memy.ErrStaleAcceptance) {
		t.Fatalf("before=%v receipt=%+v sibling=%v", before, r, sibling)
	}
	if retryErr != nil || providerCalls != 1 || len(retried) != 2 ||
		retried[0].ID != proposals[0].ID {
		t.Fatalf("extraction retry: %v calls=%d", retryErr, providerCalls)
	}
}

func TestChangedProposalInvalidatesAcceptance(t *testing.T) {
	// Arrange.
	f := newFixture(t, nil)
	p := f.propose(t, "remember", "UTC+3", memy.Interval{})
	request := f.acceptedRequest(t, "commit", "timezone", 0, p, memy.Append)
	// Act.
	changed, operationErr := f.engine.Revise(
		context.Background(),
		f.actor,
		f.scope,
		"revise",
		p.ID,
		p.Revision,
		"assist",
		f.suggestion("UTC+7", memy.Interval{}),
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	_, commitErr := f.engine.Commit(context.Background(), f.actor, f.scope, "assist", request)
	// Assert.
	if changed.Digest == p.Digest || changed.Revision != 2 ||
		!errors.Is(commitErr, memy.ErrStaleAcceptance) {
		t.Fatalf("revision=%d commit=%v", changed.Revision, commitErr)
	}
	_, readErr := f.engine.Get(
		context.Background(),
		f.actor,
		f.scope,
		"timezone",
		memy.ReadOptions{},
	)
	if !errors.Is(readErr, memy.ErrNotFound) {
		t.Fatalf("stale commit changed state: %v", readErr)
	}
}

func TestAuthorityAndFieldProfilesFailClosed(t *testing.T) {
	// Arrange: payload lies about its tenant, authority still comes from the host.
	f := newFixture(t, nil)
	s := f.suggestion("private", memy.Interval{})
	s.Payload.Tenant = "B"
	p, operationErr := f.engine.Remember(
		context.Background(),
		f.actor,
		f.scope,
		"remember",
		"assist",
		s,
	)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	f.commit(t, f.acceptedRequest(t, "commit", "private", 0, p, memy.Append))
	// Act.
	_, strangerExisting := f.engine.Get(
		context.Background(),
		principal{actor: "bob"},
		f.scope,
		"private",
		memy.ReadOptions{},
	)
	_, strangerAbsent := f.engine.Get(
		context.Background(),
		principal{actor: "bob"},
		f.scope,
		"absent",
		memy.ReadOptions{},
	)
	f.policy.Fail(errors.New("IAM down"))
	_, unavailableRead := f.engine.Get(
		context.Background(),
		f.actor,
		f.scope,
		"private",
		memy.ReadOptions{},
	)
	_, unavailableWrite := f.engine.Remember(
		context.Background(),
		f.actor,
		f.scope,
		"write",
		"assist",
		s,
	)
	f.policy.Fail(nil)
	f.policy.RestrictFields("alice", f.scope, []string{"key"})
	_, restricted := f.engine.Get(
		context.Background(),
		f.actor,
		f.scope,
		"private",
		memy.ReadOptions{},
	)
	// Assert.
	if p.Scope != f.scope || !errors.Is(strangerExisting, memy.ErrUnauthorized) ||
		!errors.Is(strangerAbsent, memy.ErrUnauthorized) {
		t.Fatal("scope/authority violated")
	}
	if !errors.Is(unavailableRead, memy.ErrUnavailable) ||
		!errors.Is(unavailableWrite, memy.ErrUnavailable) ||
		!errors.Is(restricted, memy.ErrUnsupported) {
		t.Fatalf(
			"fail-closed read=%v write=%v field=%v",
			unavailableRead,
			unavailableWrite,
			restricted,
		)
	}
}

func TestPolicyAndSourceChangesRejectCommit(t *testing.T) {
	for _, scenario := range []string{"policy", "source"} {
		t.Run(scenario, func(t *testing.T) {
			// Arrange.
			f := newFixture(t, nil)
			p := f.propose(t, "remember", "UTC+3", memy.Interval{})
			request := f.acceptedRequest(t, "commit", "timezone", 0, p, memy.Append)
			// Act.
			if scenario == "policy" {
				f.policy.Grant(
					"alice",
					f.scope,
					"authority/v2",
					memy.ActionRead,
					memy.ActionPropose,
					memy.ActionAccept,
					memy.ActionCommit,
				)
			} else {
				newSource := f.source
				newSource.Revision = "r2"
				if err := f.sources.Put(f.scope, newSource); err != nil {
					t.Fatal(err)
				}
			}
			_, transactionErr := f.engine.Commit(
				context.Background(),
				f.actor,
				f.scope,
				"assist",
				request,
			)
			// Assert.
			if scenario == "policy" && !errors.Is(transactionErr, memy.ErrStaleAcceptance) {
				t.Fatalf("stale policy accepted: %v", transactionErr)
			}
			if scenario == "source" && !errors.Is(transactionErr, memy.ErrStaleInput) {
				t.Fatalf("stale source accepted: %v", transactionErr)
			}
		})
	}
}

func TestConcurrentCommitAndDurableIdempotency(t *testing.T) {
	// Arrange.
	path := filepath.Join(t.TempDir(), "memory.db")
	store, operationErr := sqlite.Open(context.Background(), path, sqlite.Options{})
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	f := newFixture(t, store)
	p := f.propose(t, "initial", "UTC+3", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit-initial", "timezone", 0, p, memy.Append))
	a, b := f.propose(t, "A", "UTC+7", memy.Interval{}), f.propose(t, "B", "UTC+8", memy.Interval{})
	old := memy.RevisionRef{RecordID: "timezone", Revision: 1}
	requests := []memy.CommitRequest{
		f.acceptedRequest(t, "writer-A", "timezone", 1, a, memy.Supersede, old),
		f.acceptedRequest(t, "writer-B", "timezone", 1, b, memy.Supersede, old),
	}
	type outcome struct {
		index   int
		receipt memy.CommitReceipt
		err     error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	var group sync.WaitGroup
	// Act.
	for i, request := range requests {
		group.Go(func() {
			<-start
			r, transactionErr := f.engine.Commit(
				context.Background(),
				f.actor,
				f.scope,
				"assist",
				request,
			)
			results <- outcome{i, r, transactionErr}
		})
	}
	close(start)
	group.Wait()
	close(results)
	winner, conflicts := outcome{index: -1}, 0
	for result := range results {
		switch {
		case result.err == nil:
			winner = result
		case errors.Is(result.err, memy.ErrConflict):
			conflicts++
		default:
			t.Fatal(result.err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, operationErr := sqlite.Open(context.Background(), path, sqlite.Options{})
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	f.config.Store = reopened
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	})
	f.engine, operationErr = memy.New(f.config)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	if winner.index < 0 {
		t.Fatal("no winner")
	}
	receipt, retryErr := f.engine.Commit(
		context.Background(),
		f.actor,
		f.scope,
		"assist",
		requests[winner.index],
	)
	changed := requests[winner.index]
	changed.RecordID = "different"
	_, differentErr := f.engine.Commit(context.Background(), f.actor, f.scope, "assist", changed)
	read, readErr := f.engine.Get(
		context.Background(),
		f.actor,
		f.scope,
		"timezone",
		memy.ReadOptions{},
	)
	// Assert.
	if conflicts != 1 || retryErr != nil || receipt != winner.receipt ||
		!errors.Is(differentErr, memy.ErrConflict) ||
		readErr != nil ||
		read.Revision != 2 {
		t.Fatalf(
			"conflicts=%d retry=%v different=%v read=%v revision=%d",
			conflicts,
			retryErr,
			differentErr,
			readErr,
			read.Revision,
		)
	}
}

func TestValidAndRecordedTime(t *testing.T) {
	// Arrange: a later correction must not appear in a prior recorded view.
	f := newFixture(t, nil)
	august := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	p := f.propose(t, "old", "UTC+3", memy.Interval{Known: true, To: august})
	f.commit(t, f.acceptedRequest(t, "commit-old", "timezone", 0, p, memy.Append))
	recordedBeforeCorrection := f.clock.Now()
	f.clock.Set(time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC))
	p = f.propose(t, "new", "UTC+7", memy.Interval{Known: true, From: august})
	f.commit(t, f.acceptedRequest(t, "commit-new", "timezone", 1, p, memy.Append))
	// Act.
	july, julyErr := f.engine.Get(
		context.Background(),
		f.actor,
		f.scope,
		"timezone",
		memy.ReadOptions{ValidAsOf: august.Add(-time.Hour)},
	)
	september, septErr := f.engine.Get(
		context.Background(),
		f.actor,
		f.scope,
		"timezone",
		memy.ReadOptions{ValidAsOf: august.Add(31 * 24 * time.Hour)},
	)
	_, unseenErr := f.engine.Get(
		context.Background(),
		f.actor,
		f.scope,
		"timezone",
		memy.ReadOptions{ValidAsOf: august.Add(time.Hour), RecordedAsOf: recordedBeforeCorrection},
	)
	// Assert.
	if julyErr != nil || septErr != nil || july.Payload.Value != "UTC+3" ||
		september.Payload.Value != "UTC+7" ||
		!errors.Is(unseenErr, memy.ErrNotFound) {
		t.Fatalf(
			"july=%+v %v september=%+v %v unseen=%v",
			july,
			julyErr,
			september,
			septErr,
			unseenErr,
		)
	}
}

func TestUnknownValidityAndOverlappingClaims(t *testing.T) {
	// Arrange.
	f := newFixture(t, nil)
	p := f.propose(t, "unknown", "UTC+3", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit-unknown", "timezone", 0, p, memy.Append))
	// Act.
	_, unknownErr := f.engine.Get(
		context.Background(),
		f.actor,
		f.scope,
		"timezone",
		memy.ReadOptions{ValidAsOf: f.clock.Now()},
	)
	p = f.propose(t, "overlap", "UTC+7", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "commit-overlap", "timezone", 1, p, memy.Append))
	_, overlapErr := f.engine.Get(
		context.Background(),
		f.actor,
		f.scope,
		"timezone",
		memy.ReadOptions{},
	)
	// Assert.
	if !errors.Is(unknownErr, memy.ErrNotFound) ||
		!errors.Is(overlapErr, memy.ErrUnresolvedConflict) {
		t.Fatalf("unknown=%v overlap=%v", unknownErr, overlapErr)
	}
}

func listEntries(b memy.Bucket, prefix string) ([]memy.Entry, error) {
	var result []memy.Entry
	options := memy.ScanOptions{Prefix: prefix, Limit: 256, MaxBytes: int(^uint(0) >> 1)}
	for {
		page, err := b.Scan(options)
		if err != nil {
			return nil, err
		}
		result = append(result, page.Entries...)
		if page.Complete {
			return result, nil
		}
		if page.Cursor == "" || len(page.Entries) == 0 {
			return nil, memy.ErrSchema
		}
		options.Cursor = page.Cursor
	}
}

func fullSnapshot[P, R, A any](
	ctx context.Context,
	e *memy.Engine[P, R, A],
	authority A,
	scope memy.Scope,
	read memy.ReadOptions,
) ([]memy.Record[P, R], error) {
	options := memy.SnapshotOptions{Read: read, Limit: 256, MaxBytes: int(^uint(0) >> 1)}
	var records []memy.Record[P, R]
	for {
		page, err := e.Snapshot(ctx, authority, scope, options)
		if err != nil {
			return nil, err
		}
		records = append(records, page.Records...)
		if page.Complete {
			return records, nil
		}
		if page.Cursor == "" || page.Scanned == 0 {
			return nil, memy.ErrSchema
		}
		options.Cursor = page.Cursor
	}
}

func fullForget[P, R, A any](
	ctx context.Context,
	e *memy.Engine[P, R, A],
	authority A,
	scope memy.Scope,
	purpose string,
	request memy.ForgetRequest,
) (memy.PurgeReceipt, error) {
	if request.Limit == 0 {
		request.Limit = 256
	}
	if request.MaxBytes == 0 {
		request.MaxBytes = 64 << 20
	}
	ids := make(map[string]bool)
	for {
		receipt, err := e.Forget(ctx, authority, scope, purpose, request)
		if err != nil {
			return receipt, err
		}
		for _, id := range receipt.Batch.Records {
			ids[id] = true
		}
		unattempted := false
		for _, sink := range receipt.Sinks {
			if !sink.Acknowledged && sink.ErrorCode == "" {
				unattempted = true
			}
		}
		if receipt.State != memy.RevocationCommitted &&
			(receipt.State != memy.PurgePending || !unattempted) {
			receipt.Batch.Records = make([]string, 0, len(ids))
			for id := range ids {
				receipt.Batch.Records = append(receipt.Batch.Records, id)
			}
			slices.Sort(receipt.Batch.Records)
			return receipt, nil
		}
	}
}

func fullSweep[P, R, A any](
	ctx context.Context,
	e *memy.Engine[P, R, A],
	authority A,
	scope memy.Scope,
	purpose, op string,
) (memy.SweepResult, error) {
	var total memy.SweepResult
	for {
		page, err := e.Sweep(
			ctx,
			authority,
			scope,
			purpose,
			memy.SweepRequest{OperationID: op, Limit: 256, MaxBytes: 64 << 20},
		)
		if err != nil {
			return total, err
		}
		total.ExpiredProposals += page.ExpiredProposals
		total.BudgetCharged += page.BudgetCharged
		total.Records = mergeSweepReceipts(total.Records, page.Records)
		if page.Complete {
			total.Complete = true
			return total, nil
		}
		if sweepHasBlockedPurge(page.Records) {
			return total, nil
		}
	}
}

func mergeSweepReceipts(records, receipts []memy.PurgeReceipt) []memy.PurgeReceipt {
	for _, receipt := range receipts {
		found := false
		for i := range records {
			if records[i].Batch.OperationID == receipt.Batch.OperationID {
				records[i] = receipt
				found = true
			}
		}
		if !found {
			records = append(records, receipt)
		}
	}
	return records
}

func sweepHasBlockedPurge(receipts []memy.PurgeReceipt) bool {
	for _, r := range receipts {
		if r.State == memy.PurgeFailed {
			return true
		}
		if r.State == memy.PurgePending {
			for _, sink := range r.Sinks {
				if !sink.Acknowledged && sink.ErrorCode != "" {
					return true
				}
			}
		}
	}
	return false
}
