package memy_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

type projectedSearch struct{ refs []memy.RevisionRef }

func (projectedSearch) Capabilities() memy.SearchCapabilities {
	return memy.SearchCapabilities{Scoped: true, BoundedCandidates: true}
}

func (s projectedSearch) Search(
	ctx context.Context,
	_ memy.Scope,
	_ string,
	opts memy.SearchOptions,
) (memy.SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return memy.SearchResult{}, err
	}
	out := memy.SearchResult{Coverage: []memy.Coverage{{Backend: "test", Status: "ready"}}}
	for i, r := range s.refs {
		if i == opts.MaxCandidates {
			out.CandidatesTruncated = true
			break
		}
		out.Candidates = append(
			out.Candidates,
			memy.Candidate{
				RecordID: r.RecordID,
				Revision: r.Revision,
				Score:    float64(len(s.refs) - i),
				Signals: []memy.SearchSignal{
					{Backend: "test", Rank: i + 1, Score: float64(len(s.refs) - i)},
				},
			},
		)
	}
	return out, nil
}

type projectedOutput map[string][]string
type projectedPolicy struct {
	selectFn  func(context.Context, memy.ProjectedRecallResult[projectedOutput, sourceRef]) (memy.OutputSelection, error)
	measureFn func(context.Context, memy.ProjectedRecallResult[projectedOutput, sourceRef]) (uint64, error)
	estimated bool
}

func (*projectedPolicy) Version() string { return "test/v1" }
func (*projectedPolicy) Unit() string    { return "test-units" }
func (p *projectedPolicy) Exact() bool   { return !p.estimated }

func (p *projectedPolicy) Select(
	ctx context.Context,
	b memy.ProjectedRecallResult[projectedOutput, sourceRef],
	_ uint64,
) (memy.OutputSelection, error) {
	if p.selectFn != nil {
		return p.selectFn(ctx, b)
	}
	return projectedSelectAll(b), nil
}

func (p *projectedPolicy) Measure(
	ctx context.Context,
	b memy.ProjectedRecallResult[projectedOutput, sourceRef],
) (uint64, error) {
	if p.measureFn != nil {
		return p.measureFn(ctx, b)
	}
	data, err := json.Marshal(b)
	return uint64(len(data)), err
}

func projectedSelectAll(
	b memy.ProjectedRecallResult[projectedOutput, sourceRef],
) memy.OutputSelection {
	s := memy.OutputSelection{}
	for _, p := range b.Projections {
		s.Refs = append(s.Refs, memy.RevisionRef{RecordID: p.RecordID, Revision: p.Revision})
	}
	return s
}
func projectedSeed(t *testing.T, f fixture, id, value string) memy.RevisionRef {
	t.Helper()
	p := f.propose(t, "remember-"+id, value, memy.Interval{})
	r := f.commit(t, f.acceptedRequest(t, "commit-"+id, id, 0, p, memy.Append))
	return memy.RevisionRef{RecordID: r.RecordID, Revision: r.Revision}
}

func projectedCall(
	ctx context.Context,
	f fixture,
	refs []memy.RevisionRef,
	read memy.ReadOptions,
	p *projectedPolicy,
	maximum uint64,
) (memy.ProjectedRecallResult[projectedOutput, sourceRef], error) {
	return memy.RecallProjected(
		ctx,
		f.engine,
		f.actor,
		f.scope,
		"query",
		projectedSearch{refs: refs},
		memy.ScoreRanker[preference, sourceRef]{},
		memy.RecallOptions{Read: read, Search: memy.SearchOptions{MaxCandidates: 10}, Limit: 10},
		reference.ProjectorFunc[preference, sourceRef, projectedOutput]{
			PolicyVersion: "projection/v1",
			Apply: func(_ context.Context, r memy.Record[preference, sourceRef]) (projectedOutput, error) {
				return projectedOutput{"values": {r.Payload.Value}}, nil
			},
		},
		&memy.ProjectionBudget[projectedOutput, sourceRef]{
			Max:    maximum,
			Codec:  memy.JSONCodec[projectedOutput]{},
			Policy: p,
		},
	)
}
func TestRecallProjectedPolicyMutationIsolation(t *testing.T) {
	// Arrange: both callbacks attempt to overwrite nested output and provenance.
	f := newFixture(t, nil)
	refs := []memy.RevisionRef{projectedSeed(t, f, "fact", "original")}
	mutate := func(b memy.ProjectedRecallResult[projectedOutput, sourceRef]) {
		b.Projections[0].Output["values"][0] = "forged"
		b.Projections[0].Provenance.Sources[0].Reference.URI = "forged"
		b.Projections[0].Scope.Tenant = "forged"
		b.Projections[0].Trust = "forged"
		b.Coverage[0].Backend = "forged"
	}
	p := &projectedPolicy{
		selectFn: func(_ context.Context, b memy.ProjectedRecallResult[projectedOutput, sourceRef]) (memy.OutputSelection, error) {
			s := projectedSelectAll(b)
			mutate(b)
			return s, nil
		},
		measureFn: func(_ context.Context, b memy.ProjectedRecallResult[projectedOutput, sourceRef]) (uint64, error) {
			mutate(b)
			return 1, nil
		},
	}
	// Act.
	got, err := projectedCall(t.Context(), f, refs, memy.ReadOptions{}, p, 10000)
	// Assert: selection can choose identities, but cannot replace authorized data.
	if err != nil || len(got.Projections) != 1 {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	v := got.Projections[0]
	if v.Output["values"][0] != "original" ||
		v.Provenance.Sources[0].Reference.URI != f.source.Reference.URI ||
		v.Scope != f.scope ||
		v.Trust != "data" ||
		got.Coverage[0].Backend != "test" {
		t.Fatalf("callback mutation escaped: %+v", got)
	}
}
func TestRecallProjectedRejectsInvalidSelection(t *testing.T) {
	for _, fault := range []string{"unknown", "duplicate", "missing", "unknown-omission", "duplicate-omission", "selected-and-omitted", "invalid-reason"} {
		t.Run(fault, func(t *testing.T) {
			// Arrange: two authorized identities must each be selected or explicitly omitted.
			f := newFixture(t, nil)
			refs := []memy.RevisionRef{
				projectedSeed(t, f, "first", "a"),
				projectedSeed(t, f, "second", "b"),
			}
			measured := false
			p := &projectedPolicy{
				selectFn: func(_ context.Context, b memy.ProjectedRecallResult[projectedOutput, sourceRef]) (memy.OutputSelection, error) {
					s := projectedSelectAll(b)
					switch fault {
					case "unknown":
						s.Refs[0].RecordID = "foreign"
					case "duplicate":
						s.Refs[1] = s.Refs[0]
					case "missing":
						s.Refs = s.Refs[:1]
					case "unknown-omission":
						s.Refs = s.Refs[:1]
						s.Omissions = []memy.BudgetOmission{
							{
								Ref:    memy.RevisionRef{RecordID: "foreign", Revision: 1},
								Reason: memy.OmittedBudget,
							},
						}
					case "duplicate-omission":
						s.Refs = nil
						s.Omissions = []memy.BudgetOmission{
							{Ref: refs[0], Reason: memy.OmittedBudget},
							{Ref: refs[0], Reason: memy.OmittedBudget},
						}
					case "selected-and-omitted":
						s.Omissions = []memy.BudgetOmission{
							{Ref: refs[0], Reason: memy.OmittedBudget},
						}
					case "invalid-reason":
						s.Refs = s.Refs[:1]
						s.Omissions = []memy.BudgetOmission{{Ref: refs[1], Reason: "authorized"}}
					}
					return s, nil
				},
				measureFn: func(context.Context, memy.ProjectedRecallResult[projectedOutput, sourceRef]) (uint64, error) {
					measured = true
					return 1, nil
				},
			}
			// Act.
			got, err := projectedCall(t.Context(), f, refs, memy.ReadOptions{}, p, 10000)
			// Assert: an invalid selection yields no body and never reaches measurement.
			if !errors.Is(err, memy.ErrInvalid) || measured ||
				!reflect.DeepEqual(got, memy.ProjectedRecallResult[projectedOutput, sourceRef]{}) {
				t.Fatalf("result=%+v err=%v measured=%t", got, err, measured)
			}
		})
	}
}
func TestRecallProjectedCostBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name      string
		used, max uint64
		estimate  bool
		want      error
	}{{"zero", 0, 100, false, memy.ErrInvalid}, {"overflow", math.MaxUint64, 100, false, memy.ErrBudget}, {"maximum", math.MaxUint64, math.MaxUint64, false, nil}, {"estimate", 42, 42, true, nil}} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange: consumer costs include boundary values that cannot safely become signed ints.
			f := newFixture(t, nil)
			refs := []memy.RevisionRef{projectedSeed(t, f, "fact", "value")}
			p := &projectedPolicy{
				estimated: tc.estimate,
				measureFn: func(context.Context, memy.ProjectedRecallResult[projectedOutput, sourceRef]) (uint64, error) {
					return tc.used, nil
				},
			}
			// Act.
			got, err := projectedCall(t.Context(), f, refs, memy.ReadOptions{}, p, tc.max)
			// Assert.
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
			if tc.want != nil {
				if !reflect.DeepEqual(
					got,
					memy.ProjectedRecallResult[projectedOutput, sourceRef]{},
				) {
					t.Fatalf("failed body=%+v", got)
				}
				return
			}
			if got.Budget.Used != tc.used || got.Budget.Limit != tc.max ||
				got.Budget.Exact == tc.estimate ||
				got.Budget.Unit != "test-units" {
				t.Fatalf("receipt=%+v", got.Budget)
			}
		})
	}
}
func TestRecallProjectedUsesExactHistoricalRevision(t *testing.T) {
	// Arrange: current head differs from the revision selected as of an earlier instant.
	f := newFixture(t, nil)
	old := projectedSeed(t, f, "fact", "old")
	asOf := f.clock.Now()
	f.clock.Advance(time.Minute)
	p := f.propose(t, "correction", "new", memy.Interval{})
	f.commit(t, f.acceptedRequest(t, "replace", "fact", old.Revision, p, memy.Supersede, old))
	// Act.
	got, err := projectedCall(
		t.Context(),
		f,
		[]memy.RevisionRef{old},
		memy.ReadOptions{RecordedAsOf: asOf},
		&projectedPolicy{},
		10000,
	)
	// Assert: projecting by current head ID would incorrectly return the correction.
	if err != nil || len(got.Projections) != 1 || got.Projections[0].Revision != old.Revision ||
		got.Projections[0].Output["values"][0] != "old" {
		t.Fatalf("result=%+v err=%v", got, err)
	}
}
func TestRecallProjectedRevalidatesAfterPolicyCallbacks(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		for _, phase := range []string{"select", "measure"} {
			for _, change := range []string{"source", "authority", "expiry", "forget", "cancel"} {
				t.Run(backend+"/"+phase+"/"+change, func(t *testing.T) {
					testProjectedCallbackRevalidation(t, backend, phase, change)
				})
			}
		}
	}
}

func TestRecallProjectedCapturesRequestedBudget(t *testing.T) {
	// Arrange: a callback holds the caller's budget pointer and tries to relax it.
	f := newFixture(t, nil)
	refs := []memy.RevisionRef{projectedSeed(t, f, "fact", "value")}
	budget := &memy.ProjectionBudget[projectedOutput, sourceRef]{
		Max:   10,
		Codec: memy.JSONCodec[projectedOutput]{},
	}
	budget.Policy = &projectedPolicy{
		selectFn: func(_ context.Context, b memy.ProjectedRecallResult[projectedOutput, sourceRef]) (memy.OutputSelection, error) {
			budget.Max = math.MaxUint64
			return projectedSelectAll(b), nil
		},
		measureFn: func(context.Context, memy.ProjectedRecallResult[projectedOutput, sourceRef]) (uint64, error) {
			return 11, nil
		},
	}
	projector := reference.ProjectorFunc[preference, sourceRef, projectedOutput]{
		PolicyVersion: "projection/v1",
		Apply: func(_ context.Context, r memy.Record[preference, sourceRef]) (projectedOutput, error) {
			return projectedOutput{"values": {r.Payload.Value}}, nil
		},
	}
	// Act.
	got, err := memy.RecallProjected(
		t.Context(),
		f.engine,
		f.actor,
		f.scope,
		"query",
		projectedSearch{refs: refs},
		memy.ScoreRanker[preference, sourceRef]{},
		memy.RecallOptions{Search: memy.SearchOptions{MaxCandidates: 10}, Limit: 10},
		projector,
		budget,
	)
	// Assert: the requested limit remains authoritative after policy execution.
	if !errors.Is(err, memy.ErrBudget) ||
		!reflect.DeepEqual(got, memy.ProjectedRecallResult[projectedOutput, sourceRef]{}) {
		t.Fatalf("body=%+v err=%v", got, err)
	}
}

func TestRecallProjectedPolicyAllowsConcurrentForget(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		for _, phase := range []string{"select", "measure"} {
			t.Run(backend+"/"+phase, func(t *testing.T) {
				testProjectedConcurrentForget(t, backend, phase)
			})
		}
	}
}

func TestRecallProjectedRejectsChangedEligibleState(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		for _, phase := range []string{"select", "measure"} {
			for _, omit := range []bool{false, true} {
				name := "selected"
				if omit {
					name = "omitted"
				}
				t.Run(backend+"/"+phase+"/"+name, func(t *testing.T) {
					testProjectedChangedState(t, backend, phase, omit)
				})
			}
		}
	}
}

func testProjectedCallbackRevalidation(t *testing.T, backend, phase, change string) {
	t.Helper()
	// Arrange: one revision is omitted; its identity must still be safe to return.
	f := newFixture(t, raceStore(t, backend))
	if change == "expiry" {
		f.config.Retention = reference.Retain[preference]{
			Version:   "retention/v1",
			ExpiresAt: f.clock.Now().Add(time.Hour),
		}
		var err error
		f.engine, err = memy.New(f.config)
		if err != nil {
			t.Fatal(err)
		}
	}
	refs := []memy.RevisionRef{projectedSeed(t, f, "first", "a")}
	// A distinct source ensures changing the omitted record does not invalidate the selected one.
	f.source = memy.Source[sourceRef]{
		ID:        "source-2",
		Revision:  "r1",
		Reference: sourceRef{URI: "host://source-2"},
	}
	if err := f.sources.Put(f.scope, f.source); err != nil {
		t.Fatal(err)
	}
	refs = append(refs, projectedSeed(t, f, "omitted", "b"))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	changed := false
	mutate := func() {
		changed = true
		mutateProjectedInput(t, f, change, cancel)
	}
	p := &projectedPolicy{
		selectFn: func(_ context.Context, _ memy.ProjectedRecallResult[projectedOutput, sourceRef]) (memy.OutputSelection, error) {
			if phase == "select" {
				mutate()
			}
			return memy.OutputSelection{
				Refs:      refs[:1],
				Omissions: []memy.BudgetOmission{{Ref: refs[1], Reason: memy.OmittedBudget}},
			}, nil
		},
		measureFn: func(context.Context, memy.ProjectedRecallResult[projectedOutput, sourceRef]) (uint64, error) {
			if phase == "measure" {
				mutate()
			}
			return 1, nil
		},
	}
	// Act.
	got, err := projectedCall(ctx, f, refs, memy.ReadOptions{}, p, 10000)
	// Assert: no projection, omission identity or coverage escapes a stale delivery.
	if !changed || err == nil ||
		!reflect.DeepEqual(got, memy.ProjectedRecallResult[projectedOutput, sourceRef]{}) {
		t.Fatalf("changed=%t body=%+v err=%v", changed, got, err)
	}
	want := map[string]error{
		"source":    memy.ErrSourceUnavailable,
		"authority": memy.ErrStaleAcceptance,
		"expiry":    memy.ErrNotFound,
		"forget":    memy.ErrNotFound,
		"cancel":    context.Canceled,
	}[change]
	if !errors.Is(err, want) {
		t.Fatal(err)
	}
}

func testProjectedConcurrentForget(t *testing.T, backend, phase string) {
	t.Helper()
	// Arrange: hold a policy callback while another goroutine removes its input.
	f := newFixture(t, raceStore(t, backend))
	refs := []memy.RevisionRef{projectedSeed(t, f, "fact", "value")}
	entered := make(chan struct{})
	release := make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	hold := func() error {
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	p := &projectedPolicy{
		selectFn: func(_ context.Context, b memy.ProjectedRecallResult[projectedOutput, sourceRef]) (memy.OutputSelection, error) {
			if phase == "select" {
				if err := hold(); err != nil {
					return memy.OutputSelection{}, err
				}
			}
			return projectedSelectAll(b), nil
		},
		measureFn: func(context.Context, memy.ProjectedRecallResult[projectedOutput, sourceRef]) (uint64, error) {
			if phase == "measure" {
				if err := hold(); err != nil {
					return 0, err
				}
			}
			return 1, nil
		},
	}
	type outcome struct {
		body memy.ProjectedRecallResult[projectedOutput, sourceRef]
		err  error
	}
	done := make(chan outcome, 1)
	// Act: Forget must finish before the held callback is released.
	go func() {
		body, err := projectedCall(ctx, f, refs, memy.ReadOptions{}, p, 10000)
		done <- outcome{body, err}
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("policy callback was not entered")
	}
	_, forgetErr := fullForget(
		ctx,
		f.engine,
		f.actor,
		f.scope,
		"assist",
		memy.ForgetRequest{
			OperationID:   "concurrent-forget",
			Selector:      memy.Selector{Kind: memy.SelectRecord, ID: "fact"},
			Reason:        "withdraw",
			PolicyVersion: "deletion/v1",
		},
	)
	close(release)
	var got outcome
	select {
	case got = <-done:
	case <-ctx.Done():
		t.Fatal("projected recall did not return")
	}
	// Assert: callbacks hold no canonical lock, and post-callback delivery fails closed.
	if forgetErr != nil || got.err == nil ||
		!reflect.DeepEqual(got.body, memy.ProjectedRecallResult[projectedOutput, sourceRef]{}) {
		t.Fatalf("Forget=%v recall=%v body=%+v", forgetErr, got.err, got.body)
	}
}

func testProjectedChangedState(t *testing.T, backend, phase string, omit bool) {
	t.Helper()
	// Arrange: IncludeConflicts keeps the original revision readable after its state changes.
	f := newFixture(t, raceStore(t, backend))
	old := projectedSeed(t, f, "fact", "old")
	refs := []memy.RevisionRef{old}
	if omit {
		refs = append(refs, projectedSeed(t, f, "unaffected", "safe"))
	}
	changed := false
	change := func() {
		changed = true
		p := f.propose(t, "conflicting-proposal", "new", memy.Interval{})
		f.commit(
			t,
			f.acceptedRequest(
				t,
				"conflicting-commit",
				"conflicting-fact",
				0,
				p,
				memy.Conflict,
				old,
			),
		)
	}
	p := &projectedPolicy{
		selectFn: func(_ context.Context, b memy.ProjectedRecallResult[projectedOutput, sourceRef]) (memy.OutputSelection, error) {
			if phase == "select" {
				change()
			}
			if omit {
				return memy.OutputSelection{
					Refs:      refs[1:],
					Omissions: []memy.BudgetOmission{{Ref: old, Reason: memy.OmittedBudget}},
				}, nil
			}
			return projectedSelectAll(b), nil
		},
		measureFn: func(context.Context, memy.ProjectedRecallResult[projectedOutput, sourceRef]) (uint64, error) {
			if phase == "measure" {
				change()
			}
			return 1, nil
		},
	}
	// Act: the already projected Active revision becomes Conflicted during packing.
	got, err := projectedCall(
		t.Context(),
		f,
		refs,
		memy.ReadOptions{IncludeConflicts: true},
		p,
		10000,
	)
	// Assert: eligibility alone is insufficient; stale projection state invalidates the body.
	if !changed || !errors.Is(err, memy.ErrStaleInput) ||
		!reflect.DeepEqual(got, memy.ProjectedRecallResult[projectedOutput, sourceRef]{}) {
		t.Fatalf("changed=%t body=%+v err=%v", changed, got, err)
	}
}

func mutateProjectedInput(t *testing.T, f fixture, change string, cancel context.CancelFunc) {
	t.Helper()
	switch change {
	case "source":
		s := f.source
		s.Revision = "r2"
		if err := f.sources.Put(f.scope, s); err != nil {
			t.Fatal(err)
		}
	case "authority":
		f.policy.Grant(f.actor.actor, f.scope, "authority/v2", memy.ActionRead)
	case "expiry":
		f.clock.Advance(2 * time.Hour)
	case "forget":
		_, err := fullForget(
			t.Context(),
			f.engine,
			f.actor,
			f.scope,
			"assist",
			memy.ForgetRequest{
				OperationID:   "forget-omitted",
				Selector:      memy.Selector{Kind: memy.SelectRecord, ID: "omitted"},
				Reason:        "withdraw",
				PolicyVersion: "deletion/v1",
			},
		)
		if err != nil {
			t.Fatal(err)
		}
	case "cancel":
		cancel()
	}
}
