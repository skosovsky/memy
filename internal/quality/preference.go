package quality

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
	"github.com/skosovsky/memy/store/memory"
	"github.com/skosovsky/memy/store/sqlite"
)

const (
	preferenceMorning             = "morning"
	preferenceDomain              = "preference"
	preferenceSourceIdentityCheck = "source_identity_preserved"
	preferenceUnknownID           = "unknown"
	preferenceSessionsSalt        = 0x6a09e667
	preferenceTemporalSalt        = 0xbb67ae85
	preferenceConsolidationSalt   = 0x3c6ef372
	preferenceFailuresSalt        = 0xa54ff53a
	preferenceFutureReadOffset    = 3 * time.Hour

	preferenceMatched           = "matched"
	preferenceDrink             = "drink"
	preferenceEvening           = "evening"
	preferenceTea               = "tea"
	preferenceProviderVersion   = "scripted-preference/v2"
	preferencePurpose           = "quality"
	preferenceConflictID        = "conflict"
	preferencePastID            = "past"
	preferenceNegativeID        = "negative"
	preferenceDerivedID         = "derived"
	preferenceBaselineID        = "baseline"
	preferenceUnavailable       = "unavailable"
	preferencePackingVersion    = "json-packing/v1"
	preferenceSessionsID        = "preference-sessions"
	preferenceCompleted         = "completed"
	preferenceCandidateStage    = "candidate"
	preferenceAbstentionGroup   = "abstention"
	preferenceInputLineageCheck = "exact_input_lineage"
	preferenceSemanticID        = "preference-consolidation-semantic"
	preferenceDay               = 24 * time.Hour
	preferenceFutureEnd         = 4 * time.Hour
	preferenceCorrectionDelay   = 5 * time.Hour
)

// Preference is owned by this fixture, including the meaning of negation.
// Neither the manifest nor memy interprets a particular key or English phrase.
type Preference struct {
	Facts []PreferenceFact `json:"facts"`
}
type PreferenceFact struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	Negated   bool   `json:"negated"`
	Qualifier string `json:"qualifier"`
}
type preferenceQuery struct{ IDs []string }
type preferenceObservation struct {
	Stage    string `json:"stage"`
	ID       string `json:"id"`
	OK       bool   `json:"ok"`
	Expected string `json:"expected"`
	Observed string `json:"observed"`
	Count    int    `json:"count"`
}
type preferenceRunResult struct {
	Variation    string                  `json:"variation"`
	Checks       []preferenceObservation `json:"checks"`
	PayloadBytes uint64                  `json:"payload_bytes"`
	ContextBytes uint64                  `json:"context_bytes"`
	CandidateBad bool                    `json:"candidate_bad"`
	HostDecision string                  `json:"host_decision"`
}

func (r *preferenceRunResult) check(stage, id string, ok bool, count int) {
	observed := preferenceMatched
	if !ok {
		observed = "mismatched"
	}
	r.Checks = append(r.Checks, preferenceObservation{stage, id, ok, preferenceMatched, observed, count})
}

type preferenceFixture struct {
	engine *memy.Engine[Preference, string, string]
	store  memy.Store
	config memy.Config[Preference, string, string]
	policy *reference.Policy[string]
	clock  *reference.Clock
	index  *reference.Index[preferenceQuery]
	scope  memy.Scope
	source memy.Source[string]
	close  func()
}

func newPreferenceFixture(ctx context.Context, disk bool, expires time.Time) (*preferenceFixture, error) {
	var config memy.Config[Preference, string, string]
	f := &preferenceFixture{
		scope:  memy.Scope{Tenant: "fixture", Namespace: "preferences", Subject: "synthetic-user"},
		clock:  reference.NewClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		source: memy.Source[string]{ID: "fixture-source", Revision: "r1", Reference: "synthetic-reference"},
		engine: nil, store: nil, config: config, policy: nil, index: nil, close: nil}
	f.store = memory.New()
	f.close = func() { _ = f.store.Close() }
	if disk {
		dir, err := os.MkdirTemp("", "memy-quality-preference-")
		if err != nil {
			return nil, err
		}
		s, err := sqlite.Open(ctx, filepath.Join(dir, "fixture.db"), sqlite.Options{Fault: nil})
		if err != nil {
			_ = os.RemoveAll(dir)
			return nil, err
		}
		f.store = s
		f.close = func() { _ = s.Close(); _ = os.RemoveAll(dir) }
	}
	f.policy = reference.NewPolicy(func(a string) string { return a })
	f.policy.Grant(
		"host",
		f.scope,
		"authority/v1",
		memy.ActionRead,
		memy.ActionPropose,
		memy.ActionAccept,
		memy.ActionCommit,
		memy.ActionForget,
		memy.ActionConsolidate,
	)
	sources := reference.NewRegistry[string](memy.JSONCodec[string]{})
	if err := sources.Put(f.scope, f.source); err != nil {
		f.close()
		return nil, err
	}
	f.index = reference.NewIndex(
		"preference-index/v1",
		func(q preferenceQuery, c memy.Candidate) bool { return slices.Contains(q.IDs, c.RecordID) },
	)
	f.config = memy.Config[Preference, string, string]{
		Store:          f.store,
		Authority:      f.policy,
		Sources:        sources,
		Clock:          f.clock,
		Retention:      reference.Retain[Preference]{Version: "retention/v1", ExpiresAt: expires},
		PayloadCodec:   memy.JSONCodec[Preference]{},
		ReferenceCodec: memy.JSONCodec[string]{},
		Sinks:          []memy.Sink{f.index},
		Reintroduction: nil}
	var err error
	f.engine, err = memy.New(f.config)
	if err != nil {
		f.close()
		return nil, err
	}
	return f, nil
}
func preferencePayload(value string, negated bool) Preference {
	fact := PreferenceFact{Key: preferenceDrink, Value: value, Negated: negated, Qualifier: ""}
	if value == preferenceMorning || value == preferenceEvening {
		fact.Value = preferenceTea
		fact.Qualifier = value
	}
	return Preference{Facts: []PreferenceFact{fact}}
}
func (f *preferenceFixture) suggestion(p Preference, valid memy.Interval) memy.Suggestion[Preference, string] {
	return memy.Suggestion[Preference, string]{
		Payload:    p,
		Sources:    []memy.Source[string]{f.source},
		Evidence:   "synthetic fixture assertion",
		Extractor:  preferenceProviderVersion,
		ObservedAt: f.clock.Now(),
		Valid:      valid,
		ExpiresAt:  time.Time{}, Lineage: nil, Losses: nil, Uncertainties: nil}
}

func (f *preferenceFixture) commit(
	ctx context.Context,
	id string,
	p Preference,
	valid memy.Interval,
	expected memy.Version,
	mode memy.ReconcileMode,
	refs []memy.RevisionRef,
) (memy.Record[Preference, string], error) {
	proposal, err := f.engine.Remember(
		ctx,
		"host",
		f.scope,
		"propose-"+id+"-"+stringVersion(expected+1),
		preferencePurpose,
		f.suggestion(p, valid),
	)
	if err != nil {
		return memy.Record[Preference, string]{}, err
	}
	return f.apply(ctx, id, proposal, expected, mode, refs)
}
func stringVersion(v memy.Version) string { b, _ := json.Marshal(v); return string(b) }

func (f *preferenceFixture) apply(
	ctx context.Context,
	id string,
	p memy.Proposal[Preference, string],
	expected memy.Version,
	mode memy.ReconcileMode,
	refs []memy.RevisionRef,
) (memy.Record[Preference, string], error) {
	a, err := f.engine.Accept(ctx, "host", f.scope, p.ID, p.Digest, p.Revision, preferencePurpose)
	if err != nil {
		return memy.Record[Preference, string]{}, err
	}
	receipt, err := f.engine.Commit(
		ctx,
		"host",
		f.scope,
		preferencePurpose,
		memy.CommitRequest{
			OperationID: "commit-" + id + "-" + stringVersion(expected+1),
			ProposalID:  p.ID,
			Acceptance:  a,
			RecordID:    id,
			Expected:    expected,
			Reconcile: memy.Reconciliation{
				Mode:          mode,
				Related:       refs,
				PolicyVersion: "resolver/v1",
				Basis:         "fixture host decision",
			},
		},
	)
	if err != nil {
		return memy.Record[Preference, string]{}, err
	}
	// Get intentionally refuses to collapse two conflicted revisions. Select the
	// receipt's exact canonical revision from a complete eligible snapshot.
	snapshots, err := fullSnapshot(
		ctx,
		f.engine,
		"host",
		f.scope,
		preferenceReadOptions(time.Time{}, time.Time{}, true, true),
	)
	var r memy.Record[Preference, string]
	if err != nil {
		return r, err
	}
	for _, record := range snapshots {
		if record.ID == receipt.RecordID && record.Revision == receipt.Revision {
			r = record
			break
		}
	}

	if receipt.RecordID != id || receipt.Revision != expected+1 || receipt.Visibility.Scope != f.scope ||
		receipt.Visibility.RecordID != id ||
		receipt.Visibility.Revision != expected+1 ||
		r.ID != receipt.RecordID ||
		r.Revision != receipt.Revision ||
		!receipt.CanonicalCommitted ||
		!reflect.DeepEqual(r.Payload, p.Suggestion.Payload) ||
		r.Valid != p.Suggestion.Valid ||
		!reflect.DeepEqual(r.Provenance.Sources, p.Suggestion.Sources) ||
		!preferenceRefsEqual(r.Provenance.Lineage, p.Suggestion.Lineage) ||
		r.Scope != f.scope {
		return r, memy.ErrSchema
	}
	if r.State == memy.Conflicted {
		return r, nil
	} // Managed writes deliberately require active inputs.
	err = f.engine.WithDerivedWrite(
		ctx,
		"host",
		memy.EpochFence{Scope: f.scope, Epoch: r.Epoch},
		preferencePurpose,
		[]memy.RevisionRef{{RecordID: r.ID, Revision: r.Revision}},
		func(ctx context.Context) error {
			return f.index.Stage(
				ctx,
				f.scope,
				memy.Candidate{RecordID: r.ID, Revision: r.Revision, Score: 1, Signals: nil},
			)
		},
	)
	if err != nil {
		return r, err
	}
	err = f.index.Acknowledge(ctx, receipt.Visibility)
	return r, err
}
func preferenceSame(got, want memy.Record[Preference, string]) bool {
	return got.ID == want.ID && got.Revision == want.Revision && reflect.DeepEqual(got.Payload, want.Payload) &&
		reflect.DeepEqual(got.Valid, want.Valid) &&
		reflect.DeepEqual(got.Provenance.Sources, want.Provenance.Sources) &&
		reflect.DeepEqual(got.Provenance.Lineage, want.Provenance.Lineage) &&
		got.Scope == want.Scope
}
func preferenceSetSame(got, want []memy.Record[Preference, string]) bool {
	if len(got) != len(want) {
		return false
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			if preferenceSame(g, w) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (f *preferenceFixture) recall(
	ctx context.Context,
	ids []string,
	read memy.ReadOptions,
	limit, maxCandidates int,
	bytes uint64,
) ([]memy.Record[Preference, string], memy.ProjectedRecallResult[Preference, string], error) {
	return f.recallUsing(ctx, ids, read, limit, maxCandidates, bytes, f.index)
}

func (f *preferenceFixture) recallUsing(
	ctx context.Context,
	ids []string,
	read memy.ReadOptions,
	limit, maxCandidates int,
	bytes uint64,
	search memy.Search[preferenceQuery],
) ([]memy.Record[Preference, string], memy.ProjectedRecallResult[Preference, string], error) {
	read.Purpose = preferencePurpose
	opts := memy.RecallOptions{
		Read:   read,
		Search: memy.SearchOptions{MaxCandidates: maxCandidates, Minimum: nil},
		Limit:  limit,
	}
	result, err := memy.Recall(
		ctx,
		f.engine,
		"host",
		f.scope,
		preferenceQuery{ids},
		search,
		memy.ScoreRanker[Preference, string]{},
		opts,
	)
	if err != nil {
		return nil, memy.ProjectedRecallResult[Preference, string]{}, err
	}
	records := make([]memy.Record[Preference, string], len(result.Records))
	for i, r := range result.Records {
		records[i] = r.Record
	}
	body, err := memy.RecallProjected(
		ctx,
		f.engine,
		"host",
		f.scope,
		preferenceQuery{ids},
		search,
		memy.ScoreRanker[Preference, string]{},
		opts,
		reference.ProjectorFunc[Preference, string, Preference]{
			PolicyVersion: "preference-projection/v2",
			Apply:         func(_ context.Context, r memy.Record[Preference, string]) (Preference, error) { return r.Payload, nil },
		},
		&memy.ProjectionBudget[Preference, string]{
			Max:    bytes,
			Codec:  memy.JSONCodec[Preference]{},
			Policy: reference.JSONPacking[Preference, string]{RejectOversized: false},
		},
	)
	return records, body, err
}

func preferenceDelivered(
	r *preferenceRunResult,
	records, want []memy.Record[Preference, string],
	body memy.ProjectedRecallResult[Preference, string],
) {
	r.check(
		stageEffective,
		"exact_revisions_payload_validity_sources_lineage",
		preferenceSetSame(records, want),
		len(records),
	)
	ok := len(body.Projections) == len(want) && len(body.Omissions) == 0
	for _, w := range want {
		found := false
		for _, p := range body.Projections {
			if p.RecordID == w.ID && p.Revision == w.Revision && reflect.DeepEqual(p.Output, w.Payload) &&
				reflect.DeepEqual(p.Provenance.Sources, w.Provenance.Sources) &&
				reflect.DeepEqual(p.Provenance.Lineage, w.Provenance.Lineage) &&
				p.Scope == w.Scope {
				found = true
				break
			}
		}
		ok = ok && found
	}
	encoded, err := json.Marshal(body)
	r.check(
		stageRendered,
		"exact_projection_body_and_budget",
		ok && err == nil && body.Budget.Exact && body.Budget.Used == uint64(len(encoded)) &&
			body.Budget.Used <= body.Budget.Limit,
		len(body.Projections),
	)
	r.ContextBytes = uint64(len(encoded))
	for _, record := range records {
		b, _ := json.Marshal(record.Payload)
		r.PayloadBytes += uint64(len(b))
	}
}

func preferenceSessions(
	ctx context.Context,
	seed uint64,
	limit, maxCandidates int,
	bytes uint64,
) (preferenceRunResult, error) {
	var out preferenceRunResult
	f, err := newPreferenceFixture(ctx, true, time.Time{})
	if err != nil {
		return out, err
	}
	defer f.close()
	known := memy.Interval{Known: true, From: f.clock.Now().Add(-preferenceDay), To: f.clock.Now().Add(preferenceDay)}
	old, err := f.commit(ctx, "corrected", preferencePayload(preferenceMorning, false), known, 0, memy.Append, nil)
	if err != nil {
		return out, err
	}
	f.clock.Advance(time.Hour)
	corrected, err := f.commit(
		ctx,
		"corrected",
		preferencePayload(preferenceEvening, true),
		known,
		1,
		memy.Supersede,
		[]memy.RevisionRef{{RecordID: old.ID, Revision: old.Revision}},
	)
	if err != nil {
		return out, err
	}
	// A fresh host engine carries no session transcript or prior Recall result.
	fresh, err := memy.New(f.config)
	if err != nil {
		return out, err
	}
	f.engine = fresh
	items := []string{preferenceUnknownID, preferenceConflictID}
	shufflePreference(seed, preferenceSessionsSalt, items)
	out.Variation = "insertion/" + strings.Join(items, "_")
	unknown, conflict, err := f.insertSessionAlternatives(ctx, items, known)
	if err != nil {
		return out, err
	}
	records, body, err := f.recall(
		ctx,
		[]string{"corrected"},
		preferenceReadOptions(time.Time{}, time.Time{}, false, false),
		limit,
		maxCandidates,
		bytes,
	)
	if err != nil {
		return out, err
	}
	preferenceDelivered(&out, records, []memy.Record[Preference, string]{corrected}, body)
	if readErr := f.preferenceSessionReads(ctx, &out, unknown, conflict, limit, maxCandidates, bytes); readErr != nil {
		return out, readErr
	}
	history, err := fullSnapshot(ctx, f.engine,
		"host",
		f.scope,
		preferenceReadOptions(time.Time{}, old.RecordedAt, true, true),
	)
	if err != nil {
		return out, err
	}
	out.check(
		stageCanonical,
		"historical_original_exact_revision",
		slices.ContainsFunc(history, func(r memy.Record[Preference, string]) bool { return preferenceSame(r, old) }),
		len(history),
	)
	out.check(
		stageCanonical,
		"scope_and_sources",
		slices.Equal(corrected.Provenance.Sources, []memy.Source[string]{f.source}) && corrected.Scope == f.scope,
		1,
	)
	return out, nil
}

func preferenceTemporal(
	ctx context.Context,
	seed uint64,
	limit, maxCandidates int,
	bytes uint64,
) (preferenceRunResult, error) {
	var out preferenceRunResult
	f, err := newPreferenceFixture(ctx, false, time.Time{})
	if err != nil {
		return out, err
	}
	defer f.close()
	t0 := f.clock.Now()
	interval := memy.Interval{Known: true, From: t0.Add(-time.Hour), To: t0.Add(time.Hour)}
	ids := []string{preferencePastID, "future"}
	shufflePreference(seed, preferenceTemporalSalt, ids)
	out.Variation = "insertion/" + strings.Join(ids, "_")
	var past, future memy.Record[Preference, string]
	for _, id := range ids {
		if id == preferencePastID {
			past, err = f.commit(ctx, id, preferencePayload(preferencePastID, false), interval, 0, memy.Append, nil)
		} else {
			future, err = f.commit(
				ctx,
				id,
				preferencePayload("future", false),
				memy.Interval{Known: true, From: t0.Add(2 * time.Hour), To: t0.Add(preferenceFutureEnd)},
				0,
				memy.Append,
				nil,
			)
		}
		if err != nil {
			return out, err
		}
	}
	records, body, err := f.recall(
		ctx,
		ids,
		preferenceReadOptions(t0, time.Time{}, false, false),
		limit,
		maxCandidates,
		bytes,
	)
	if err != nil {
		return out, err
	}
	preferenceDelivered(&out, records, []memy.Record[Preference, string]{past}, body)
	later, _, err := f.recall(
		ctx,
		ids,
		preferenceReadOptions(t0.Add(preferenceFutureReadOffset), time.Time{}, false, false),
		limit,
		maxCandidates,
		bytes,
	)
	if err != nil {
		return out, err
	}
	out.check(
		stageEffective,
		"valid_time_selects_distinct_fact",
		preferenceSetSame(later, []memy.Record[Preference, string]{future}),
		len(later),
	)
	if correctionErr := f.preferenceTemporalCorrection(
		ctx,
		&out,
		past,
		t0,
		interval,
		limit,
		maxCandidates,
		bytes,
	); correctionErr != nil {
		return out, correctionErr
	}
	if revokeErr := f.preferenceTemporalRevoke(ctx, &out, t0, limit, maxCandidates, bytes); revokeErr != nil {
		return out, revokeErr
	}
	err = preferenceRetention(ctx, &out, t0, interval, limit, maxCandidates, bytes)
	return out, err
}

func preferenceConsolidation(
	ctx context.Context,
	seed uint64,
	limit, maxCandidates int,
	bytes uint64,
	mode memy.ConsolidationMode,
	budget memy.Budget,
) (preferenceRunResult, error) {
	var out preferenceRunResult
	f, err := newPreferenceFixture(ctx, false, time.Time{})
	if err != nil {
		return out, err
	}
	defer f.close()
	valid := memy.Interval{Known: true, From: f.clock.Now().Add(-time.Hour), To: f.clock.Now().Add(preferenceDay)}
	ids := []string{"positive-a", "positive-b", preferenceNegativeID}
	shufflePreference(seed, preferenceConsolidationSalt, ids)
	out.Variation = "insertion/" + strings.Join(ids, "_")
	originals := make([]memy.Record[Preference, string], 0, len(ids))
	for _, id := range ids {
		p := preferencePayload(preferenceMorning, false)
		if id == preferenceNegativeID {
			p = preferencePayload(preferenceEvening, true)
		}
		r, commitErr := f.commit(ctx, id, p, valid, 0, memy.Append, nil)
		if commitErr != nil {
			return out, commitErr
		}
		originals = append(originals, r)
	}
	before, err := fullSnapshot(
		ctx,
		f.engine,
		"host",
		f.scope,
		preferenceReadOptions(time.Time{}, time.Time{}, false, false),
	)
	if err != nil {
		return out, err
	}
	want := slices.Clone(originals)
	selected := slices.Clone(ids)
	if mode != "" {
		proposals, inputs, proposalErr := f.preferenceProposal(ctx, originals, valid, mode, budget)
		if proposalErr != nil {
			return out, proposalErr
		}
		out.check(stageExecution, "consolidation_proposal_created", len(proposals) == 1, len(proposals))
		if len(proposals) != 1 {
			return out, nil
		}
		good := preferenceCandidate(&out, proposals[0], originals, inputs, valid, f.source, mode)
		want, selected, err = f.reviewPreference(ctx, &out, proposals[0], originals, mode, good)
		if err != nil {
			return out, err
		}
	}
	after, err := fullSnapshot(
		ctx,
		f.engine,
		"host",
		f.scope,
		preferenceReadOptions(time.Time{}, time.Time{}, false, false),
	)
	if err != nil {
		return out, err
	}
	preferenceOriginalsPreserved(&out, before, after, f.scope, f.source, len(originals))
	records, body, err := f.recall(
		ctx,
		selected,
		preferenceReadOptions(time.Time{}, time.Time{}, false, false),
		limit,
		maxCandidates,
		bytes,
	)
	if err != nil {
		return out, err
	}
	preferenceDelivered(&out, records, want, body)
	return out, nil
}
func preferenceRefsEqual(a, b []memy.RevisionRef) bool {
	if len(a) != len(b) {
		return false
	}
	for _, r := range a {
		if !slices.Contains(b, r) {
			return false
		}
	}
	return true
}

func preferenceFailures(
	ctx context.Context,
	seed uint64,
	limit, maxCandidates int,
	bytes uint64,
) (preferenceRunResult, error) {
	var out preferenceRunResult
	f, err := newPreferenceFixture(ctx, false, time.Time{})
	if err != nil {
		return out, err
	}
	defer f.close()
	order := []string{preferenceBaselineID, "irrelevant"}
	shufflePreference(seed, preferenceFailuresSalt, order)
	out.Variation = "insertion/" + strings.Join(order, "_")
	var baseline memy.Record[Preference, string]
	for _, id := range order {
		record, operationErr := f.commit(
			ctx,
			id,
			preferencePayload(id, false),
			memy.Interval{Known: false, From: time.Time{}, To: time.Time{}},
			0,
			memy.Append,
			nil,
		)
		if operationErr != nil {
			return out, operationErr
		}
		if id == preferenceBaselineID {
			baseline = record
		}
	}
	before, err := fullSnapshot(
		ctx,
		f.engine,
		"host",
		f.scope,
		preferenceReadOptions(time.Time{}, time.Time{}, false, false),
	)
	if err != nil {
		return out, err
	}
	recovered, providerErr := f.preferenceProviderFailure(ctx, &out)
	if providerErr != nil || !recovered {
		return out, providerErr
	}
	f.policy.Fail(memy.ErrUnavailable)
	unknown, err := f.engine.Get(
		ctx,
		"host",
		f.scope,
		preferenceBaselineID,
		preferenceReadOptions(time.Time{}, time.Time{}, false, false),
	)
	out.check(
		stageExecution,
		"expected_policy_unavailable_fail_closed",
		errors.Is(err, memy.ErrUnavailable) && unknown.ID == "",
		0,
	)
	out.Checks[len(out.Checks)-1].Expected = preferenceUnavailable
	out.Checks[len(out.Checks)-1].Observed = ErrorClass(err)
	f.policy.Fail(nil)
	if err == nil {
		return out, nil
	}
	if !errors.Is(err, memy.ErrUnavailable) {
		return out, err
	}
	after, err := fullSnapshot(
		ctx,
		f.engine,
		"host",
		f.scope,
		preferenceReadOptions(time.Time{}, time.Time{}, false, false),
	)
	if err != nil {
		return out, err
	}
	out.check(stageCanonical, "port_failures_do_not_apply_or_erase", reflect.DeepEqual(before, after), len(after))
	records, body, err := f.recall(
		ctx,
		[]string{preferenceBaselineID},
		preferenceReadOptions(time.Time{}, time.Time{}, false, false),
		limit,
		maxCandidates,
		bytes,
	)
	if err != nil {
		return out, err
	}
	preferenceDelivered(&out, records, []memy.Record[Preference, string]{baseline}, body)
	return out, nil
}

func preferenceVersions() PortVersions {
	return PortVersions{
		Provider:      preferenceProviderVersion,
		Model:         fixtureModeScripted,
		HostReview:    "preference-review/v2",
		Retention:     "retention/v1",
		Resolver:      "resolver/v1",
		Consolidation: "consolidation/v2",
		Search:        "preference-index/v1",
		Projector:     "preference-projection/v2",
		Packing:       preferencePackingVersion,
		Grader:        "none",
	}
}
func preferenceVersionsFor(id string) PortVersions {
	versions := preferenceVersions()
	if id == preferenceSessionsID {
		versions.Search = "preference-index/v1+conflict-metadata/v2"
	}
	return versions
}
func preferencePlans() []CasePlan {
	base := []RequiredCheck{
		{Stage: stageEffective, ID: "exact_revisions_payload_validity_sources_lineage"},
		{Stage: stageRendered, ID: "exact_projection_body_and_budget"},
		{Stage: stageExecution, ID: preferenceCompleted},
	}
	add := func(extra ...RequiredCheck) []RequiredCheck { return append(slices.Clone(base), extra...) }
	result := preferenceBasePlans(add)
	for _, entry := range []struct {
		id   string
		mode memy.ConsolidationMode
	}{{preferenceBaselineID, ""}, {"exact", memy.ExactDedup}, {"domain", memy.DomainMerge}, {"semantic", memy.SemanticMerge}} {
		required := add(
			RequiredCheck{stageCanonical, "full_original_documents_preserved"},
			RequiredCheck{stageCanonical, "scope_sources_and_exact_lineage"},
		)
		optional := []string{}
		if entry.mode == "" {
			optional = []string{preferenceCandidateStage, stageHostReview}
		} else {
			required = append(required, RequiredCheck{stageExecution, "consolidation_proposal_created"})
			if entry.mode == memy.SemanticMerge {
				required = append(
					required,
					RequiredCheck{stageHostReview, "explicit_semantic_rejection"},
					RequiredCheck{preferenceCandidateStage, preferenceSourceIdentityCheck},
					RequiredCheck{preferenceCandidateStage, preferenceInputLineageCheck},
				)
			} else {
				required = append(
					required,
					RequiredCheck{preferenceCandidateStage, "facts_negation_validity_sources_lineage"},
					RequiredCheck{preferenceCandidateStage, "valid_interval_preserved"},
					RequiredCheck{preferenceCandidateStage, preferenceSourceIdentityCheck},
					RequiredCheck{preferenceCandidateStage, preferenceInputLineageCheck},
					RequiredCheck{preferenceCandidateStage, "negation_preserved"},
					RequiredCheck{stageHostReview, "explicit_apply_checked_receipt"},
				)
			}
		}
		result = append(
			result,
			CasePlan{
				ID:             "preference-consolidation-" + entry.id,
				Domain:         preferenceDomain,
				Version:        "preference-consolidation/v2",
				Groups:         []string{"consolidation"},
				Required:       required,
				OptionalStages: optional,
				Versions:       preferenceVersionsFor("preference-consolidation-" + entry.id), Run: nil},
		)
	}
	for i := range result {
		plan := &result[i]
		plan.Versions = preferenceVersionsFor(plan.ID)
		id := plan.ID
		plan.Run = func(ctx context.Context, c Corpus, run CaseRun) (ScenarioReport, error) {
			return runPreferencePlan(ctx, id, c, run)
		}
	}
	return result
}
func preferenceReport(id string, _ CaseRun, observed preferenceRunResult, err error) ScenarioReport {
	var emptyStage StageResult
	var metrics Metrics
	s := ScenarioReport{
		ID:         id,
		Domain:     preferenceDomain,
		Versions:   preferenceVersionsFor(id),
		Mode:       fixtureModeScripted,
		Variation:  observed.Variation,
		Candidate:  StageResult{Status: NotApplicable, Checks: nil, Version: ""},
		HostReview: StageResult{Status: NotApplicable, Checks: nil, Version: ""},
		Version:    "",
		Repeat:     0,
		Seed:       0,
		Effective:  emptyStage,
		Canonical:  emptyStage,
		Rendered:   emptyStage,
		Execution:  emptyStage,
		Metrics:    metrics,
		Final:      "",
		Diagnostic: "",
		Probes:     nil,
	}
	stage := func(name string) *StageResult {
		switch name {
		case preferenceCandidateStage:
			return &s.Candidate
		case stageHostReview:
			return &s.HostReview
		case stageEffective:
			return &s.Effective
		case stageCanonical:
			return &s.Canonical
		case stageRendered:
			return &s.Rendered
		default:
			return &s.Execution
		}
	}
	for _, o := range observed.Checks {
		p := stage(o.Stage)
		p.Status = Pass
		status := Pass
		if !o.OK {
			status = Fail
		}
		mandatory := o.Stage != preferenceCandidateStage || id != preferenceSemanticID ||
			o.ID == preferenceSourceIdentityCheck ||
			o.ID == preferenceInputLineageCheck
		p.Checks = append(
			p.Checks,
			Check{
				ID:        o.ID,
				Mandatory: mandatory,
				Status:    status,
				Expected:  o.Expected,
				Observed:  o.Observed,
				Evidence:  []Evidence{{Code: o.ID, Count: o.Count, Aliases: nil}},
			},
		)
	}
	if observed.HostDecision != "" {
		s.HostReview.Version = "preference-review/v2"
		for i := range s.HostReview.Checks {
			s.HostReview.Checks[i].Expected = observed.HostDecision
			if s.HostReview.Checks[i].Status == Pass {
				s.HostReview.Checks[i].Observed = observed.HostDecision
			} else {
				s.HostReview.Checks[i].Observed = preferenceUnknownID
			}
		}
	}
	if err == nil {
		s.Execution.Checks = append(
			s.Execution.Checks,
			Check{
				ID:        preferenceCompleted,
				Mandatory: true,
				Status:    Pass,
				Expected:  preferenceCompleted,
				Observed:  preferenceCompleted,
				Evidence:  []Evidence{{Code: "fixture_completed", Count: 1, Aliases: nil}},
			},
		)
	}
	s.Metrics = preferenceMetrics(id, observed, err)
	return s
}

// Conflict candidates are scripted metadata, not managed derived artifacts.
// Engine.WithDerivedWrite rejects conflicted lineage; Recall must nevertheless
// honor IncludeConflicts when a search port supplies those exact revisions.
type preferenceConflictSearch struct{ Scope memy.Scope }

func (preferenceConflictSearch) Capabilities() memy.SearchCapabilities {
	return memy.SearchCapabilities{Scoped: true, BoundedCandidates: true, Visibility: false}
}

func (search preferenceConflictSearch) Search(
	ctx context.Context,
	scope memy.Scope,
	query preferenceQuery,
	opts memy.SearchOptions,
) (memy.SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return memy.SearchResult{Candidates: nil, Coverage: nil, CandidatesTruncated: false}, err
	}
	if opts.MaxCandidates < 1 {
		return memy.SearchResult{Candidates: nil, Coverage: nil, CandidatesTruncated: false}, memy.ErrInvalid
	}
	result := memy.SearchResult{
		Coverage: []memy.Coverage{
			{Backend: "preference-conflict-metadata/v2", Status: "ready", MinimumSatisfied: false},
		},
		Candidates:          nil,
		CandidatesTruncated: false,
	}
	if scope != search.Scope {
		return memy.SearchResult{Candidates: nil, Coverage: nil, CandidatesTruncated: false}, memy.ErrScopeViolation
	}
	if !slices.Contains(query.IDs, preferenceConflictID) {
		return result, nil
	}
	for revision := memy.Version(1); revision <= 2; revision++ {
		result.Candidates = append(
			result.Candidates,
			memy.Candidate{
				RecordID: preferenceConflictID,
				Revision: revision,
				Score:    1,
				Signals: []memy.SearchSignal{
					{Backend: "preference-conflict-metadata/v2", Rank: int(revision), Score: 1},
				},
			},
		)
	}
	if len(result.Candidates) > opts.MaxCandidates {
		result.Candidates = result.Candidates[:opts.MaxCandidates]
		result.CandidatesTruncated = true
	}
	return result, nil
}

// shufflePreference varies fixture event order reproducibly; it creates no security token.
func shufflePreference(seed, salt uint64, values []string) {
	//nolint:gosec // Deterministic fixture scheduling requires a reproducible PRNG, not cryptographic randomness.
	rng := rand.New(rand.NewPCG(seed, salt^seed))
	rng.Shuffle(len(values), func(i, j int) { values[i], values[j] = values[j], values[i] })
}

func (f *preferenceFixture) insertSessionAlternatives(
	ctx context.Context,
	items []string,
	known memy.Interval,
) (memy.Record[Preference, string], memy.Record[Preference, string], error) {
	var unknown, conflict memy.Record[Preference, string]
	var err error
	for _, id := range items {
		switch id {
		case preferenceUnknownID:
			unknown, err = f.commit(
				ctx,
				id,
				preferencePayload("unobserved", false),
				memy.Interval{Known: false, From: time.Time{}, To: time.Time{}},
				0,
				memy.Append,
				nil,
			)
		case preferenceConflictID:
			_, err = f.commit(ctx, id, preferencePayload(preferenceMorning, false), known, 0, memy.Append, nil)
			if err == nil {
				f.clock.Advance(time.Hour)
				conflict, err = f.commit(
					ctx,
					id,
					preferencePayload(preferenceEvening, false),
					known,
					1,
					memy.Conflict,
					[]memy.RevisionRef{{RecordID: id, Revision: 1}},
				)
			}
		}
		if err != nil {
			return unknown, conflict, err
		}
	}
	return unknown, conflict, nil
}

func preferenceRetention(
	ctx context.Context,
	out *preferenceRunResult,
	t0 time.Time,
	interval memy.Interval,
	limit, maxCandidates int,
	bytes uint64,
) error {
	expiry := t0.Add(time.Hour)
	expiring, err := newPreferenceFixture(ctx, false, expiry)
	if err != nil {
		return err
	}
	defer expiring.close()
	_, err = expiring.commit(ctx, "retained", preferencePayload("retained", false), interval, 0, memy.Append, nil)
	if err != nil {
		return err
	}
	expiring.clock.Set(t0.Add(2 * time.Hour))
	expired, _, err := expiring.recall(
		ctx,
		[]string{"retained"},
		preferenceReadOptions(t0, t0, false, false),
		limit,
		maxCandidates,
		bytes,
	)
	if err != nil {
		return err
	}
	_, expiryErr := expiring.engine.Get(
		ctx,
		"host",
		expiring.scope,
		"retained",
		preferenceReadOptions(t0, t0, false, false),
	)
	expiredCanonical, snapshotErr := fullSnapshot(
		ctx,
		expiring.engine,
		"host",
		expiring.scope,
		preferenceReadOptions(t0, t0, false, false),
	)
	if snapshotErr != nil {
		return snapshotErr
	}
	out.check(
		stageCanonical,
		"historical_read_obeys_current_retention",
		len(expired) == 0 && len(expiredCanonical) == 0 && errors.Is(expiryErr, memy.ErrNotFound),
		len(expired),
	)
	return nil
}

func (f *preferenceFixture) preferenceMergeProvider(
	mode memy.ConsolidationMode,
	valid memy.Interval,
) memy.Consolidator[Preference, string] {
	if mode == memy.ExactDedup {
		return nil
	}
	return reference.MergeFunc[Preference, string](
		func(_ context.Context, _ []memy.Record[Preference, string], _ memy.Budget) (memy.MergeResult[Preference, string], error) {
			p := Preference{
				Facts: []PreferenceFact{
					{Key: preferenceDrink, Value: preferenceTea, Qualifier: preferenceMorning, Negated: false},
					{Key: preferenceDrink, Value: preferenceTea, Qualifier: preferenceEvening, Negated: true},
				},
			}
			v := valid
			if mode == memy.SemanticMerge {
				p = preferencePayload("always", false)
				v = memy.Interval{Known: false, From: time.Time{}, To: time.Time{}}
			}
			s := f.suggestion(p, v)
			return memy.MergeResult[Preference, string]{
				Suggestions: []memy.Suggestion[Preference, string]{s},
				CostUnits:   1,
				Utility:     1,
			}, nil
		},
	)
}

func (f *preferenceFixture) preferenceProposal(
	ctx context.Context,
	originals []memy.Record[Preference, string],
	valid memy.Interval,
	mode memy.ConsolidationMode,
	budget memy.Budget,
) ([]memy.Proposal[Preference, string], []memy.RevisionRef, error) {
	inputs := make([]memy.RevisionRef, len(originals))
	for i, r := range originals {
		inputs[i] = memy.RevisionRef{RecordID: r.ID, Revision: r.Revision}
	}
	provider := f.preferenceMergeProvider(mode, valid)
	proposals, err := memy.Consolidate(
		ctx,
		f.engine,
		"host",
		f.scope,
		memy.ConsolidationRequest{
			OperationID:    "consolidate",
			Purpose:        preferencePurpose,
			PolicyVersion:  "consolidation/v2",
			Mode:           mode,
			Inputs:         inputs,
			Budget:         budget,
			MinimumUtility: 0,
		},
		provider,
	)
	if err != nil {
		return nil, nil, err
	}
	return proposals, inputs, nil
}

func preferenceCandidate(
	out *preferenceRunResult,
	proposal memy.Proposal[Preference, string],
	originals []memy.Record[Preference, string],
	inputs []memy.RevisionRef,
	valid memy.Interval,
	source memy.Source[string],
	mode memy.ConsolidationMode,
) bool {
	expectedLineage := slices.Clone(inputs)
	if mode == memy.ExactDedup {
		expectedLineage = nil
		for _, r := range originals {
			if r.ID != preferenceNegativeID {
				expectedLineage = append(expectedLineage, memy.RevisionRef{RecordID: r.ID, Revision: r.Revision})
			}
		}
	}
	good := proposal.Suggestion.Valid == valid &&
		slices.Equal(proposal.Suggestion.Sources, []memy.Source[string]{source}) &&
		preferenceRefsEqual(proposal.Suggestion.Lineage, expectedLineage)
	if mode == memy.ExactDedup {
		good = good && reflect.DeepEqual(proposal.Suggestion.Payload, preferencePayload(preferenceMorning, false))
	} else {
		good = good &&
			reflect.DeepEqual(
				proposal.Suggestion.Payload,
				Preference{
					Facts: []PreferenceFact{
						{Key: preferenceDrink, Value: preferenceTea, Qualifier: preferenceMorning, Negated: false},
						{Key: preferenceDrink, Value: preferenceTea, Qualifier: preferenceEvening, Negated: true},
					},
				},
			)
	}
	out.CandidateBad = !good
	out.check(preferenceCandidateStage, "facts_negation_validity_sources_lineage", good, 1)
	out.check(preferenceCandidateStage, "valid_interval_preserved", proposal.Suggestion.Valid == valid, 1)
	out.check(
		preferenceCandidateStage,
		preferenceSourceIdentityCheck,
		slices.Equal(proposal.Suggestion.Sources, []memy.Source[string]{source}),
		len(proposal.Suggestion.Sources),
	)
	out.check(
		preferenceCandidateStage,
		preferenceInputLineageCheck,
		preferenceRefsEqual(proposal.Suggestion.Lineage, expectedLineage),
		len(proposal.Suggestion.Lineage),
	)
	negationPresent := slices.ContainsFunc(
		proposal.Suggestion.Payload.Facts,
		func(f PreferenceFact) bool {
			return f.Value == preferenceTea && f.Qualifier == preferenceEvening && f.Negated
		},
	)
	out.check(
		preferenceCandidateStage,
		"negation_preserved",
		mode == memy.ExactDedup || negationPresent,
		len(proposal.Suggestion.Payload.Facts),
	)
	return good
}

func (f *preferenceFixture) reviewPreference(
	ctx context.Context,
	out *preferenceRunResult,
	proposal memy.Proposal[Preference, string],
	originals []memy.Record[Preference, string],
	mode memy.ConsolidationMode,
	good bool,
) ([]memy.Record[Preference, string], []string, error) {
	if mode == memy.SemanticMerge {
		out.HostDecision = "reject"
		err := f.engine.Reject(ctx, "host", f.scope, proposal.ID, proposal.Digest, preferencePurpose)
		out.check(stageHostReview, "explicit_semantic_rejection", err == nil && !good, 1)
		ids := make([]string, len(originals))
		for i, record := range originals {
			ids[i] = record.ID
		}
		return slices.Clone(originals), ids, err
	}
	out.HostDecision = "accept"
	applied, err := f.apply(ctx, preferenceDerivedID, proposal, 0, memy.Append, nil)
	if err != nil {
		return nil, nil, err
	}
	out.check(
		stageHostReview,
		"explicit_apply_checked_receipt",
		good && applied.ID == preferenceDerivedID && applied.Revision == 1,
		1,
	)
	want := []memy.Record[Preference, string]{applied}
	selected := []string{preferenceDerivedID}
	if mode == memy.ExactDedup {
		selected = append(selected, preferenceNegativeID)
		for _, record := range originals {
			if record.ID == preferenceNegativeID {
				want = append(want, record)
			}
		}
	}
	return want, selected, nil
}

func (f *preferenceFixture) preferenceProviderFailure(ctx context.Context, out *preferenceRunResult) (bool, error) {
	// Mutable fixture-owned failures are classified by sentinel, never message.
	failed := true
	provider := reference.ExtractorFunc[string, Preference, string](
		func(_ context.Context, _ string) ([]memy.Suggestion[Preference, string], error) {
			if failed {
				return nil, memy.ErrUnavailable
			}
			return []memy.Suggestion[Preference, string]{
				f.suggestion(
					preferencePayload(preferenceEvening, true),
					memy.Interval{Known: false, From: time.Time{}, To: time.Time{}},
				),
			}, nil
		},
	)
	_, err := memy.Extract(
		ctx,
		f.engine,
		"host",
		f.scope,
		memy.ExtractionJob{
			OperationID:     "failed-provider",
			ProviderVersion: preferenceProviderVersion,
			Purpose:         preferencePurpose,
		},
		"synthetic input",
		memy.JSONCodec[string]{},
		provider,
	)
	out.check(stageExecution, "expected_provider_unavailable", errors.Is(err, memy.ErrUnavailable), 0)
	out.Checks[len(out.Checks)-1].Expected = preferenceUnavailable
	out.Checks[len(out.Checks)-1].Observed = ErrorClass(err)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, memy.ErrUnavailable) {
		return false, err
	}
	failed = false
	recovery, err := memy.Extract(
		ctx,
		f.engine,
		"host",
		f.scope,
		memy.ExtractionJob{
			OperationID:     "recovered-provider",
			ProviderVersion: preferenceProviderVersion,
			Purpose:         preferencePurpose,
		},
		"synthetic input",
		memy.JSONCodec[string]{},
		provider,
	)
	if err != nil {
		return false, err
	}
	out.check(stageExecution, "provider_recovery_unaccepted_proposal", len(recovery) == 1, len(recovery))
	return true, nil
}

func preferenceReadOptions(validAsOf, recordedAsOf time.Time, includeUnknown, includeConflicts bool) memy.ReadOptions {
	return memy.ReadOptions{
		Purpose:          preferencePurpose,
		ValidAsOf:        validAsOf,
		RecordedAsOf:     recordedAsOf,
		IncludeUnknown:   includeUnknown,
		IncludeConflicts: includeConflicts,
	}
}

func preferenceMetrics(id string, observed preferenceRunResult, err error) Metrics {
	var metrics Metrics
	if err == nil {
		metrics.PayloadBytes = KnownMeasurement("bytes/json-payload", observed.PayloadBytes)
		metrics.ContextJSONBytes = KnownMeasurement("bytes/json-context", observed.ContextBytes)
	} else {
		metrics.PayloadBytes = UnavailableMeasurement("bytes/json-payload", "execution_incomplete")
		metrics.ContextJSONBytes = UnavailableMeasurement("bytes/json-context", "execution_incomplete")
	}
	metrics.CanonicalBytes = UnavailableMeasurement(
		"bytes/serialized-canonical",
		"not_measured_adapter_storage_excluded",
	)
	cost := uint64(0)
	if id == "preference-consolidation-domain" || id == preferenceSemanticID {
		cost = 1
	}
	if err == nil {
		metrics.ProviderCost = KnownMeasurement("scripted-cost-units", cost)
	} else {
		metrics.ProviderCost = UnavailableMeasurement("scripted-cost-units", "execution_incomplete")
	}
	metrics.RealProviderCost = UnavailableMeasurement("tokens-or-currency", "scripted_provider")
	return metrics
}

func preferenceBasePlans(add func(...RequiredCheck) []RequiredCheck) []CasePlan {
	return []CasePlan{
		{
			ID:      preferenceSessionsID,
			Domain:  preferenceDomain,
			Version: "preference-sessions/v2",
			Groups:  []string{"sessions"},
			Required: add(
				RequiredCheck{stageEffective, "unknown_read_options"},
				RequiredCheck{stageEffective, "conflict_read_options"},
				RequiredCheck{stageEffective, "conflict_get_abstains"},
				RequiredCheck{stageCanonical, "historical_original_exact_revision"},
				RequiredCheck{stageCanonical, "scope_and_sources"},
			),
			OptionalStages: []string{preferenceCandidateStage, stageHostReview},
			Versions:       preferenceVersionsFor(preferenceSessionsID),
			Run:            nil,
		},
		{
			ID:      "preference-temporal",
			Domain:  preferenceDomain,
			Version: "preference-temporal/v2",
			Groups:  []string{"temporal"},
			Required: add(
				RequiredCheck{stageEffective, "valid_time_selects_distinct_fact"},
				RequiredCheck{stageEffective, "recorded_time_selects_original_revision"},
				RequiredCheck{stageCanonical, "historical_read_obeys_current_revoke"},
				RequiredCheck{stageCanonical, "historical_read_obeys_current_retention"},
			),
			OptionalStages: []string{preferenceCandidateStage, stageHostReview},
			Versions:       preferenceVersionsFor("preference-temporal"),
			Run:            nil,
		},
		{
			ID:      "preference-port-failures",
			Domain:  preferenceDomain,
			Version: "preference-port-failures/v2",
			Groups:  []string{preferenceAbstentionGroup},
			Required: add(
				RequiredCheck{stageExecution, "expected_provider_unavailable"},
				RequiredCheck{stageExecution, "provider_recovery_unaccepted_proposal"},
				RequiredCheck{stageExecution, "expected_policy_unavailable_fail_closed"},
				RequiredCheck{stageCanonical, "port_failures_do_not_apply_or_erase"},
			),
			OptionalStages: []string{preferenceCandidateStage, stageHostReview},
			Versions:       preferenceVersionsFor("preference-port-failures"),
			Run:            nil,
		},
	}
}

func runPreferencePlan(ctx context.Context, id string, c Corpus, run CaseRun) (ScenarioReport, error) {
	var observed preferenceRunResult
	var err error
	b := c.Budgets
	switch id {
	case preferenceSessionsID:
		observed, err = preferenceSessions(ctx, run.Seed, b.RecallLimit, b.MaxCandidates, b.ContextBytes)
	case "preference-temporal":
		observed, err = preferenceTemporal(ctx, run.Seed, b.RecallLimit, b.MaxCandidates, b.ContextBytes)
	case "preference-port-failures":
		observed, err = preferenceFailures(ctx, run.Seed, b.RecallLimit, b.MaxCandidates, b.ContextBytes)
	default:
		mode := memy.ConsolidationMode("")
		switch id {
		case "preference-consolidation-exact":
			mode = memy.ExactDedup
		case "preference-consolidation-domain":
			mode = memy.DomainMerge
		case preferenceSemanticID:
			mode = memy.SemanticMerge
		}
		observed, err = preferenceConsolidation(
			ctx,
			run.Seed,
			b.RecallLimit,
			b.MaxCandidates,
			b.ContextBytes,
			mode,
			memy.Budget{InputBytes: b.InputBytes, OutputBytes: b.OutputBytes, CostUnits: b.CostUnits},
		)
	}
	return preferenceReport(id, run, observed, err), err
}

func (f *preferenceFixture) preferenceSessionReads(
	ctx context.Context,
	out *preferenceRunResult,
	unknown, conflict memy.Record[Preference, string],
	limit, maxCandidates int,
	bytes uint64,
) error {
	unknownHidden, _, err := f.recall(
		ctx,
		[]string{preferenceUnknownID},
		preferenceReadOptions(f.clock.Now(), time.Time{}, false, false),
		limit,
		maxCandidates,
		bytes,
	)
	if err != nil {
		return err
	}
	unknownShown, _, err := f.recall(
		ctx,
		[]string{preferenceUnknownID},
		preferenceReadOptions(f.clock.Now(), time.Time{}, true, false),
		limit,
		maxCandidates,
		bytes,
	)
	if err != nil {
		return err
	}
	out.check(
		stageEffective,
		"unknown_read_options",
		len(unknownHidden) == 0 && preferenceSetSame(unknownShown, []memy.Record[Preference, string]{unknown}),
		len(unknownShown),
	)
	hidden, _, err := f.recallUsing(
		ctx,
		[]string{preferenceConflictID},
		preferenceReadOptions(time.Time{}, time.Time{}, false, false),
		limit,
		maxCandidates,
		bytes,
		preferenceConflictSearch{Scope: f.scope},
	)
	if err != nil {
		return err
	}
	shown, _, err := f.recallUsing(
		ctx,
		[]string{preferenceConflictID},
		preferenceReadOptions(time.Time{}, time.Time{}, false, true),
		limit,
		maxCandidates,
		bytes,
		preferenceConflictSearch{Scope: f.scope},
	)
	if err != nil {
		return err
	}
	out.check(
		stageEffective,
		"conflict_read_options",
		len(hidden) == 0 && len(shown) == 2 && slices.ContainsFunc(shown, func(r memy.Record[Preference, string]) bool {
			return preferenceSame(r, conflict) && r.State == memy.Conflicted
		}),
		len(shown),
	)
	_, conflictErr := f.engine.Get(
		ctx,
		"host",
		f.scope,
		preferenceConflictID,
		preferenceReadOptions(time.Time{}, time.Time{}, false, true),
	)
	out.check(stageEffective, "conflict_get_abstains", errors.Is(conflictErr, memy.ErrUnresolvedConflict), 1)
	return nil
}

func (f *preferenceFixture) preferenceTemporalCorrection(
	ctx context.Context,
	out *preferenceRunResult,
	past memy.Record[Preference, string],
	t0 time.Time,
	interval memy.Interval,
	limit, maxCandidates int,
	bytes uint64,
) error {
	f.clock.Advance(preferenceCorrectionDelay)
	correction, err := f.commit(
		ctx,
		preferencePastID,
		preferencePayload("corrected", true),
		interval,
		1,
		memy.Supersede,
		[]memy.RevisionRef{{RecordID: past.ID, Revision: past.Revision}},
	)
	if err != nil {
		return err
	}
	historic, _, err := f.recall(
		ctx,
		[]string{preferencePastID},
		preferenceReadOptions(t0, t0, false, false),
		limit,
		maxCandidates,
		bytes,
	)
	if err != nil {
		return err
	}
	current, _, err := f.recall(
		ctx,
		[]string{preferencePastID},
		preferenceReadOptions(t0, time.Time{}, false, false),
		limit,
		maxCandidates,
		bytes,
	)
	if err != nil {
		return err
	}
	out.check(
		stageEffective,
		"recorded_time_selects_original_revision",
		preferenceSetSame(historic, []memy.Record[Preference, string]{past}) &&
			preferenceSetSame(current, []memy.Record[Preference, string]{correction}),
		len(historic),
	)
	return nil
}

func (f *preferenceFixture) preferenceTemporalRevoke(
	ctx context.Context,
	out *preferenceRunResult,
	t0 time.Time,
	limit, maxCandidates int,
	bytes uint64,
) error {
	_, err := fullForget(ctx, f.engine,
		"host",
		f.scope,
		preferencePurpose,
		memy.ForgetRequest{
			OperationID:   "temporal-forget",
			Selector:      memy.Selector{Kind: memy.SelectRecord, ID: preferencePastID},
			Reason:        "fixture privacy event",
			PolicyVersion: "forget/v1",
			Expected:      nil, Limit: 0, MaxBytes: 0},
	)
	if err != nil {
		return err
	}
	revoked, _, err := f.recall(
		ctx,
		[]string{preferencePastID},
		preferenceReadOptions(t0, t0, false, false),
		limit,
		maxCandidates,
		bytes,
	)
	if err != nil {
		return err
	}
	_, revokeErr := f.engine.Get(
		ctx,
		"host",
		f.scope,
		preferencePastID,
		preferenceReadOptions(t0, t0, false, false),
	)
	revokedCanonical, snapshotErr := fullSnapshot(
		ctx,
		f.engine,
		"host",
		f.scope,
		preferenceReadOptions(t0, t0, false, false),
	)
	if snapshotErr != nil {
		return snapshotErr
	}
	out.check(
		stageCanonical,
		"historical_read_obeys_current_revoke",
		len(revoked) == 0 && errors.Is(revokeErr, memy.ErrNotFound) &&
			!slices.ContainsFunc(
				revokedCanonical,
				func(r memy.Record[Preference, string]) bool { return r.ID == preferencePastID },
			),
		len(revoked),
	)
	return nil
}

func preferenceOriginalsPreserved(
	out *preferenceRunResult,
	before, after []memy.Record[Preference, string],
	scope memy.Scope,
	source memy.Source[string],
	originalCount int,
) {
	originalsEqual := true
	for _, record := range before {
		originalsEqual = originalsEqual &&
			slices.ContainsFunc(
				after,
				func(r memy.Record[Preference, string]) bool { return reflect.DeepEqual(r, record) },
			)
	}
	out.check(
		stageCanonical,
		"full_original_documents_preserved",
		originalsEqual && len(before) == originalCount,
		len(before),
	)
	out.check(
		stageCanonical,
		"scope_sources_and_exact_lineage",
		!slices.ContainsFunc(after, func(r memy.Record[Preference, string]) bool {
			return r.Scope != scope || !slices.Equal(r.Provenance.Sources, []memy.Source[string]{source})
		}),
		len(after),
	)
}
