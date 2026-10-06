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
	Stage, ID          string
	OK                 bool
	Expected, Observed string
	Count              int
}
type preferenceRunResult struct {
	Variation                  string
	Checks                     []preferenceObservation
	PayloadBytes, ContextBytes uint64
	CandidateBad               bool
	HostDecision               string
}

func (r *preferenceRunResult) check(stage, id string, ok bool, count int) {
	observed := "matched"
	if !ok {
		observed = "mismatched"
	}
	r.Checks = append(r.Checks, preferenceObservation{stage, id, ok, "matched", observed, count})
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
	f := &preferenceFixture{scope: memy.Scope{Tenant: "fixture", Namespace: "preferences", Subject: "synthetic-user"}, clock: reference.NewClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), source: memy.Source[string]{ID: "fixture-source", Revision: "r1", Reference: "synthetic-reference"}}
	f.store = memory.New()
	f.close = func() { _ = f.store.Close() }
	if disk {
		dir, err := os.MkdirTemp("", "memy-quality-preference-")
		if err != nil {
			return nil, err
		}
		s, err := sqlite.Open(ctx, filepath.Join(dir, "fixture.db"), sqlite.Options{})
		if err != nil {
			_ = os.RemoveAll(dir)
			return nil, err
		}
		f.store = s
		f.close = func() { _ = s.Close(); _ = os.RemoveAll(dir) }
	}
	f.policy = reference.NewPolicy(func(a string) string { return a })
	f.policy.Grant("host", f.scope, "authority/v1", memy.ActionRead, memy.ActionPropose, memy.ActionAccept, memy.ActionCommit, memy.ActionForget, memy.ActionConsolidate)
	sources := reference.NewRegistry[string](memy.JSONCodec[string]{})
	if err := sources.Put(f.scope, f.source); err != nil {
		f.close()
		return nil, err
	}
	f.index = reference.NewIndex("preference-index/v1", func(q preferenceQuery, c memy.Candidate) bool { return slices.Contains(q.IDs, c.RecordID) })
	f.config = memy.Config[Preference, string, string]{Store: f.store, Authority: f.policy, Sources: sources, Clock: f.clock, Retention: reference.Retain[Preference]{Version: "retention/v1", ExpiresAt: expires}, PayloadCodec: memy.JSONCodec[Preference]{}, ReferenceCodec: memy.JSONCodec[string]{}, Sinks: []memy.Sink{f.index}}
	var err error
	f.engine, err = memy.New(f.config)
	if err != nil {
		f.close()
		return nil, err
	}
	return f, nil
}
func preferencePayload(value string, negated bool) Preference {
	fact := PreferenceFact{Key: "drink", Value: value, Negated: negated}
	if value == "morning" || value == "evening" {
		fact.Value = "tea"
		fact.Qualifier = value
	}
	return Preference{Facts: []PreferenceFact{fact}}
}
func (f *preferenceFixture) suggestion(p Preference, valid memy.Interval) memy.Suggestion[Preference, string] {
	return memy.Suggestion[Preference, string]{Payload: p, Sources: []memy.Source[string]{f.source}, Evidence: "synthetic fixture assertion", Extractor: "scripted-preference/v2", ObservedAt: f.clock.Now(), Valid: valid}
}
func (f *preferenceFixture) commit(ctx context.Context, id string, p Preference, valid memy.Interval, expected memy.Version, mode memy.ReconcileMode, refs []memy.RevisionRef) (memy.Record[Preference, string], error) {
	proposal, err := f.engine.Remember(ctx, "host", f.scope, "propose-"+id+"-"+stringVersion(expected+1), "quality", f.suggestion(p, valid))
	if err != nil {
		return memy.Record[Preference, string]{}, err
	}
	return f.apply(ctx, id, proposal, expected, mode, refs)
}
func stringVersion(v memy.Version) string { b, _ := json.Marshal(v); return string(b) }
func (f *preferenceFixture) apply(ctx context.Context, id string, p memy.Proposal[Preference, string], expected memy.Version, mode memy.ReconcileMode, refs []memy.RevisionRef) (memy.Record[Preference, string], error) {
	a, err := f.engine.Accept(ctx, "host", f.scope, p.ID, p.Digest, p.Revision, "quality")
	if err != nil {
		return memy.Record[Preference, string]{}, err
	}
	receipt, err := f.engine.Commit(ctx, "host", f.scope, "quality", memy.CommitRequest{OperationID: "commit-" + id + "-" + stringVersion(expected+1), ProposalID: p.ID, Acceptance: a, RecordID: id, Expected: expected, Reconcile: memy.Reconciliation{Mode: mode, Related: refs, PolicyVersion: "resolver/v1", Basis: "fixture host decision"}})
	if err != nil {
		return memy.Record[Preference, string]{}, err
	}
	// Get intentionally refuses to collapse two conflicted revisions. Select the
	// receipt's exact canonical revision from a complete eligible snapshot.
	snapshots, err := fullSnapshot(f.engine, ctx, "host", f.scope, memy.ReadOptions{Purpose: "quality", IncludeUnknown: true, IncludeConflicts: true})
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

	if receipt.RecordID != id || receipt.Revision != expected+1 || receipt.Visibility.Scope != f.scope || receipt.Visibility.RecordID != id || receipt.Visibility.Revision != expected+1 || r.ID != receipt.RecordID || r.Revision != receipt.Revision || !receipt.CanonicalCommitted || !reflect.DeepEqual(r.Payload, p.Suggestion.Payload) || r.Valid != p.Suggestion.Valid || !reflect.DeepEqual(r.Provenance.Sources, p.Suggestion.Sources) || !preferenceRefsEqual(r.Provenance.Lineage, p.Suggestion.Lineage) || r.Scope != f.scope {
		return r, memy.ErrSchema
	}
	if r.State == memy.Conflicted {
		return r, nil
	} // Managed writes deliberately require active inputs.
	err = f.engine.WithDerivedWrite(ctx, "host", memy.EpochFence{Scope: f.scope, Epoch: r.Epoch}, "quality", []memy.RevisionRef{{RecordID: r.ID, Revision: r.Revision}}, func(ctx context.Context) error {
		return f.index.Stage(ctx, f.scope, memy.Candidate{RecordID: r.ID, Revision: r.Revision, Score: 1})
	})
	if err != nil {
		return r, err
	}
	err = f.index.Acknowledge(ctx, receipt.Visibility)
	return r, err
}
func preferenceSame(got, want memy.Record[Preference, string]) bool {
	return got.ID == want.ID && got.Revision == want.Revision && reflect.DeepEqual(got.Payload, want.Payload) && reflect.DeepEqual(got.Valid, want.Valid) && reflect.DeepEqual(got.Provenance.Sources, want.Provenance.Sources) && reflect.DeepEqual(got.Provenance.Lineage, want.Provenance.Lineage) && got.Scope == want.Scope
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
func (f *preferenceFixture) recall(ctx context.Context, ids []string, read memy.ReadOptions, limit, maxCandidates int, bytes uint64) ([]memy.Record[Preference, string], memy.ProjectedRecallResult[Preference, string], error) {
	return f.recallUsing(ctx, ids, read, limit, maxCandidates, bytes, f.index)
}
func (f *preferenceFixture) recallUsing(ctx context.Context, ids []string, read memy.ReadOptions, limit, maxCandidates int, bytes uint64, search memy.Search[preferenceQuery]) ([]memy.Record[Preference, string], memy.ProjectedRecallResult[Preference, string], error) {
	read.Purpose = "quality"
	opts := memy.RecallOptions{Read: read, Search: memy.SearchOptions{MaxCandidates: maxCandidates}, Limit: limit}
	result, err := memy.Recall(ctx, f.engine, "host", f.scope, preferenceQuery{ids}, search, memy.ScoreRanker[Preference, string]{}, opts)
	if err != nil {
		return nil, memy.ProjectedRecallResult[Preference, string]{}, err
	}
	records := make([]memy.Record[Preference, string], len(result.Records))
	for i, r := range result.Records {
		records[i] = r.Record
	}
	body, err := memy.RecallProjected(ctx, f.engine, "host", f.scope, preferenceQuery{ids}, search, memy.ScoreRanker[Preference, string]{}, opts, reference.ProjectorFunc[Preference, string, Preference]{PolicyVersion: "preference-projection/v2", Apply: func(_ context.Context, r memy.Record[Preference, string]) (Preference, error) { return r.Payload, nil }}, &memy.ProjectionBudget[Preference, string]{Max: bytes, Codec: memy.JSONCodec[Preference]{}, Policy: reference.JSONPacking[Preference, string]{}})
	return records, body, err
}
func preferenceDelivered(r *preferenceRunResult, records, want []memy.Record[Preference, string], body memy.ProjectedRecallResult[Preference, string]) {
	r.check("effective", "exact_revisions_payload_validity_sources_lineage", preferenceSetSame(records, want), len(records))
	ok := len(body.Projections) == len(want) && len(body.Omissions) == 0
	for _, w := range want {
		found := false
		for _, p := range body.Projections {
			if p.RecordID == w.ID && p.Revision == w.Revision && reflect.DeepEqual(p.Output, w.Payload) && reflect.DeepEqual(p.Provenance.Sources, w.Provenance.Sources) && reflect.DeepEqual(p.Provenance.Lineage, w.Provenance.Lineage) && p.Scope == w.Scope {
				found = true
				break
			}
		}
		ok = ok && found
	}
	encoded, err := json.Marshal(body)
	r.check("rendered", "exact_projection_body_and_budget", ok && err == nil && body.Budget.Exact && body.Budget.Used == uint64(len(encoded)) && body.Budget.Used <= body.Budget.Limit, len(body.Projections))
	r.ContextBytes = uint64(len(encoded))
	for _, record := range records {
		b, _ := json.Marshal(record.Payload)
		r.PayloadBytes += uint64(len(b))
	}
}
func preferenceSessions(ctx context.Context, seed uint64, limit, maxCandidates int, bytes uint64) (preferenceRunResult, error) {
	var out preferenceRunResult
	f, err := newPreferenceFixture(ctx, true, time.Time{})
	if err != nil {
		return out, err
	}
	defer f.close()
	known := memy.Interval{Known: true, From: f.clock.Now().Add(-24 * time.Hour), To: f.clock.Now().Add(24 * time.Hour)}
	old, err := f.commit(ctx, "corrected", preferencePayload("morning", false), known, 0, memy.Append, nil)
	if err != nil {
		return out, err
	}
	f.clock.Advance(time.Hour)
	corrected, err := f.commit(ctx, "corrected", preferencePayload("evening", true), known, 1, memy.Supersede, []memy.RevisionRef{{RecordID: old.ID, Revision: old.Revision}})
	if err != nil {
		return out, err
	}
	// A fresh host engine carries no session transcript or prior Recall result.
	fresh, err := memy.New(f.config)
	if err != nil {
		return out, err
	}
	f.engine = fresh
	items := []string{"unknown", "conflict"}
	rng := rand.New(rand.NewPCG(seed, seed^0x6a09e667))
	rng.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
	out.Variation = "insertion/" + strings.Join(items, "_")
	var unknown, conflict memy.Record[Preference, string]
	for _, id := range items {
		switch id {
		case "unknown":
			unknown, err = f.commit(ctx, id, preferencePayload("unobserved", false), memy.Interval{}, 0, memy.Append, nil)
		case "conflict":
			_, err = f.commit(ctx, id, preferencePayload("morning", false), known, 0, memy.Append, nil)
			if err == nil {
				f.clock.Advance(time.Hour)
				conflict, err = f.commit(ctx, id, preferencePayload("evening", false), known, 1, memy.Conflict, []memy.RevisionRef{{RecordID: id, Revision: 1}})
			}
		}
		if err != nil {
			return out, err
		}
	}
	records, body, err := f.recall(ctx, []string{"corrected"}, memy.ReadOptions{}, limit, maxCandidates, bytes)
	if err != nil {
		return out, err
	}
	preferenceDelivered(&out, records, []memy.Record[Preference, string]{corrected}, body)
	unknownHidden, _, err := f.recall(ctx, []string{"unknown"}, memy.ReadOptions{ValidAsOf: f.clock.Now()}, limit, maxCandidates, bytes)
	if err != nil {
		return out, err
	}
	unknownShown, _, err := f.recall(ctx, []string{"unknown"}, memy.ReadOptions{ValidAsOf: f.clock.Now(), IncludeUnknown: true}, limit, maxCandidates, bytes)
	if err != nil {
		return out, err
	}
	out.check("effective", "unknown_read_options", len(unknownHidden) == 0 && preferenceSetSame(unknownShown, []memy.Record[Preference, string]{unknown}), len(unknownShown))
	hidden, _, err := f.recallUsing(ctx, []string{"conflict"}, memy.ReadOptions{}, limit, maxCandidates, bytes, preferenceConflictSearch{Scope: f.scope})
	if err != nil {
		return out, err
	}
	shown, _, err := f.recallUsing(ctx, []string{"conflict"}, memy.ReadOptions{IncludeConflicts: true}, limit, maxCandidates, bytes, preferenceConflictSearch{Scope: f.scope})
	if err != nil {
		return out, err
	}
	out.check("effective", "conflict_read_options", len(hidden) == 0 && len(shown) == 2 && slices.ContainsFunc(shown, func(r memy.Record[Preference, string]) bool {
		return preferenceSame(r, conflict) && r.State == memy.Conflicted
	}), len(shown))
	_, conflictErr := f.engine.Get(ctx, "host", f.scope, "conflict", memy.ReadOptions{Purpose: "quality", IncludeConflicts: true})
	out.check("effective", "conflict_get_abstains", errors.Is(conflictErr, memy.ErrUnresolvedConflict), 1)
	history, err := fullSnapshot(f.engine, ctx, "host", f.scope, memy.ReadOptions{Purpose: "quality", RecordedAsOf: old.RecordedAt, IncludeUnknown: true, IncludeConflicts: true})
	if err != nil {
		return out, err
	}
	out.check("canonical", "historical_original_exact_revision", slices.ContainsFunc(history, func(r memy.Record[Preference, string]) bool { return preferenceSame(r, old) }), len(history))
	out.check("canonical", "scope_and_sources", slices.Equal(corrected.Provenance.Sources, []memy.Source[string]{f.source}) && corrected.Scope == f.scope, 1)
	return out, nil
}

func preferenceTemporal(ctx context.Context, seed uint64, limit, maxCandidates int, bytes uint64) (preferenceRunResult, error) {
	var out preferenceRunResult
	f, err := newPreferenceFixture(ctx, false, time.Time{})
	if err != nil {
		return out, err
	}
	defer f.close()
	t0 := f.clock.Now()
	interval := memy.Interval{Known: true, From: t0.Add(-time.Hour), To: t0.Add(time.Hour)}
	ids := []string{"past", "future"}
	rng := rand.New(rand.NewPCG(seed, seed^0xbb67ae85))
	rng.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
	out.Variation = "insertion/" + strings.Join(ids, "_")
	var past, future memy.Record[Preference, string]
	for _, id := range ids {
		if id == "past" {
			past, err = f.commit(ctx, id, preferencePayload("past", false), interval, 0, memy.Append, nil)
		} else {
			future, err = f.commit(ctx, id, preferencePayload("future", false), memy.Interval{Known: true, From: t0.Add(2 * time.Hour), To: t0.Add(4 * time.Hour)}, 0, memy.Append, nil)
		}
		if err != nil {
			return out, err
		}
	}
	records, body, err := f.recall(ctx, ids, memy.ReadOptions{ValidAsOf: t0}, limit, maxCandidates, bytes)
	if err != nil {
		return out, err
	}
	preferenceDelivered(&out, records, []memy.Record[Preference, string]{past}, body)
	later, _, err := f.recall(ctx, ids, memy.ReadOptions{ValidAsOf: t0.Add(3 * time.Hour)}, limit, maxCandidates, bytes)
	if err != nil {
		return out, err
	}
	out.check("effective", "valid_time_selects_distinct_fact", preferenceSetSame(later, []memy.Record[Preference, string]{future}), len(later))
	f.clock.Advance(5 * time.Hour)
	correction, err := f.commit(ctx, "past", preferencePayload("corrected", true), interval, 1, memy.Supersede, []memy.RevisionRef{{RecordID: past.ID, Revision: past.Revision}})
	if err != nil {
		return out, err
	}
	historic, _, err := f.recall(ctx, []string{"past"}, memy.ReadOptions{RecordedAsOf: t0, ValidAsOf: t0}, limit, maxCandidates, bytes)
	if err != nil {
		return out, err
	}
	current, _, err := f.recall(ctx, []string{"past"}, memy.ReadOptions{ValidAsOf: t0}, limit, maxCandidates, bytes)
	if err != nil {
		return out, err
	}
	out.check("effective", "recorded_time_selects_original_revision", preferenceSetSame(historic, []memy.Record[Preference, string]{past}) && preferenceSetSame(current, []memy.Record[Preference, string]{correction}), len(historic))
	_, err = fullForget(f.engine, ctx, "host", f.scope, "quality", memy.ForgetRequest{OperationID: "temporal-forget", Selector: memy.Selector{Kind: memy.SelectRecord, ID: "past"}, Reason: "fixture privacy event", PolicyVersion: "forget/v1"})
	if err != nil {
		return out, err
	}
	revoked, _, err := f.recall(ctx, []string{"past"}, memy.ReadOptions{RecordedAsOf: t0, ValidAsOf: t0}, limit, maxCandidates, bytes)
	if err != nil {
		return out, err
	}
	_, revokeErr := f.engine.Get(ctx, "host", f.scope, "past", memy.ReadOptions{Purpose: "quality", RecordedAsOf: t0, ValidAsOf: t0})
	revokedCanonical, snapshotErr := fullSnapshot(f.engine, ctx, "host", f.scope, memy.ReadOptions{Purpose: "quality", RecordedAsOf: t0, ValidAsOf: t0})
	if snapshotErr != nil {
		return out, snapshotErr
	}
	out.check("canonical", "historical_read_obeys_current_revoke", len(revoked) == 0 && errors.Is(revokeErr, memy.ErrNotFound) && !slices.ContainsFunc(revokedCanonical, func(r memy.Record[Preference, string]) bool { return r.ID == "past" }), len(revoked))
	expiry := t0.Add(time.Hour)
	expiring, err := newPreferenceFixture(ctx, false, expiry)
	if err != nil {
		return out, err
	}
	defer expiring.close()
	_, err = expiring.commit(ctx, "retained", preferencePayload("retained", false), interval, 0, memy.Append, nil)
	if err != nil {
		return out, err
	}
	expiring.clock.Set(t0.Add(2 * time.Hour))
	expired, _, err := expiring.recall(ctx, []string{"retained"}, memy.ReadOptions{RecordedAsOf: t0, ValidAsOf: t0}, limit, maxCandidates, bytes)
	if err != nil {
		return out, err
	}
	_, expiryErr := expiring.engine.Get(ctx, "host", expiring.scope, "retained", memy.ReadOptions{Purpose: "quality", RecordedAsOf: t0, ValidAsOf: t0})
	expiredCanonical, snapshotErr := fullSnapshot(expiring.engine, ctx, "host", expiring.scope, memy.ReadOptions{Purpose: "quality", RecordedAsOf: t0, ValidAsOf: t0})
	if snapshotErr != nil {
		return out, snapshotErr
	}
	out.check("canonical", "historical_read_obeys_current_retention", len(expired) == 0 && len(expiredCanonical) == 0 && errors.Is(expiryErr, memy.ErrNotFound), len(expired))
	return out, nil
}

func preferenceConsolidation(ctx context.Context, seed uint64, limit, maxCandidates int, bytes uint64, mode memy.ConsolidationMode, budget memy.Budget) (preferenceRunResult, error) {
	var out preferenceRunResult
	f, err := newPreferenceFixture(ctx, false, time.Time{})
	if err != nil {
		return out, err
	}
	defer f.close()
	valid := memy.Interval{Known: true, From: f.clock.Now().Add(-time.Hour), To: f.clock.Now().Add(24 * time.Hour)}
	ids := []string{"positive-a", "positive-b", "negative"}
	rng := rand.New(rand.NewPCG(seed, seed^0x3c6ef372))
	rng.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
	out.Variation = "insertion/" + strings.Join(ids, "_")
	originals := make([]memy.Record[Preference, string], 0, 3)
	for _, id := range ids {
		p := preferencePayload("morning", false)
		if id == "negative" {
			p = preferencePayload("evening", true)
		}
		r, err := f.commit(ctx, id, p, valid, 0, memy.Append, nil)
		if err != nil {
			return out, err
		}
		originals = append(originals, r)
	}
	before, err := fullSnapshot(f.engine, ctx, "host", f.scope, memy.ReadOptions{Purpose: "quality"})
	if err != nil {
		return out, err
	}
	want := slices.Clone(originals)
	selected := slices.Clone(ids)
	if mode != "" {
		inputs := make([]memy.RevisionRef, len(originals))
		for i, r := range originals {
			inputs[i] = memy.RevisionRef{RecordID: r.ID, Revision: r.Revision}
		}
		var provider memy.Consolidator[Preference, string]
		if mode != memy.ExactDedup {
			provider = reference.MergeFunc[Preference, string](func(_ context.Context, records []memy.Record[Preference, string], _ memy.Budget) (memy.MergeResult[Preference, string], error) {
				p := Preference{Facts: []PreferenceFact{{Key: "drink", Value: "tea", Qualifier: "morning"}, {Key: "drink", Value: "tea", Qualifier: "evening", Negated: true}}}
				v := valid
				if mode == memy.SemanticMerge {
					p = preferencePayload("always", false)
					v = memy.Interval{}
				}
				s := f.suggestion(p, v)
				return memy.MergeResult[Preference, string]{Suggestions: []memy.Suggestion[Preference, string]{s}, CostUnits: 1, Utility: 1}, nil
			})
		}
		proposals, err := memy.Consolidate(ctx, f.engine, "host", f.scope, memy.ConsolidationRequest{OperationID: "consolidate", Purpose: "quality", PolicyVersion: "consolidation/v2", Mode: mode, Inputs: inputs, Budget: budget, MinimumUtility: 0}, provider)
		if err != nil {
			return out, err
		}
		out.check("execution", "consolidation_proposal_created", len(proposals) == 1, len(proposals))
		if len(proposals) != 1 {
			return out, nil
		}
		proposal := proposals[0]
		expectedLineage := slices.Clone(inputs)
		if mode == memy.ExactDedup {
			expectedLineage = nil
			for _, r := range originals {
				if r.ID != "negative" {
					expectedLineage = append(expectedLineage, memy.RevisionRef{RecordID: r.ID, Revision: r.Revision})
				}
			}
		}
		good := proposal.Suggestion.Valid == valid && slices.Equal(proposal.Suggestion.Sources, []memy.Source[string]{f.source}) && preferenceRefsEqual(proposal.Suggestion.Lineage, expectedLineage)
		if mode == memy.ExactDedup {
			good = good && reflect.DeepEqual(proposal.Suggestion.Payload, preferencePayload("morning", false))
		} else {
			good = good && reflect.DeepEqual(proposal.Suggestion.Payload, Preference{Facts: []PreferenceFact{{Key: "drink", Value: "tea", Qualifier: "morning"}, {Key: "drink", Value: "tea", Qualifier: "evening", Negated: true}}})
		}
		out.CandidateBad = !good
		out.check("candidate", "facts_negation_validity_sources_lineage", good, len(proposals))
		out.check("candidate", "valid_interval_preserved", proposal.Suggestion.Valid == valid, 1)
		out.check("candidate", "source_identity_preserved", slices.Equal(proposal.Suggestion.Sources, []memy.Source[string]{f.source}), len(proposal.Suggestion.Sources))
		out.check("candidate", "exact_input_lineage", preferenceRefsEqual(proposal.Suggestion.Lineage, expectedLineage), len(proposal.Suggestion.Lineage))
		negationPresent := slices.ContainsFunc(proposal.Suggestion.Payload.Facts, func(f PreferenceFact) bool { return f.Value == "tea" && f.Qualifier == "evening" && f.Negated })
		out.check("candidate", "negation_preserved", mode == memy.ExactDedup || negationPresent, len(proposal.Suggestion.Payload.Facts))
		if mode == memy.SemanticMerge {
			out.HostDecision = "reject"
			err = f.engine.Reject(ctx, "host", f.scope, proposal.ID, proposal.Digest, "quality")
			out.check("host_review", "explicit_semantic_rejection", err == nil && !good, 1)
			if err != nil {
				return out, err
			}
		} else {
			out.HostDecision = "accept"
			applied, err := f.apply(ctx, "derived", proposal, 0, memy.Append, nil)
			if err != nil {
				return out, err
			}
			out.check("host_review", "explicit_apply_checked_receipt", good && applied.ID == "derived" && applied.Revision == 1, 1)
			if mode == memy.ExactDedup {
				want = []memy.Record[Preference, string]{applied}
				selected = []string{"derived", "negative"}
				for _, r := range originals {
					if r.ID == "negative" {
						want = append(want, r)
					}
				}
			} else {
				want = []memy.Record[Preference, string]{applied}
				selected = []string{"derived"}
			}
		}
	}
	after, err := fullSnapshot(f.engine, ctx, "host", f.scope, memy.ReadOptions{Purpose: "quality"})
	if err != nil {
		return out, err
	}
	originalsEqual := true
	for _, record := range before {
		originalsEqual = originalsEqual && slices.ContainsFunc(after, func(r memy.Record[Preference, string]) bool { return reflect.DeepEqual(r, record) })
	}
	out.check("canonical", "full_original_documents_preserved", originalsEqual && len(before) == 3, len(before))
	out.check("canonical", "scope_sources_and_exact_lineage", !slices.ContainsFunc(after, func(r memy.Record[Preference, string]) bool {
		return r.Scope != f.scope || !slices.Equal(r.Provenance.Sources, []memy.Source[string]{f.source})
	}), len(after))
	records, body, err := f.recall(ctx, selected, memy.ReadOptions{}, limit, maxCandidates, bytes)
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

func preferenceFailures(ctx context.Context, seed uint64, limit, maxCandidates int, bytes uint64) (preferenceRunResult, error) {
	var out preferenceRunResult
	f, err := newPreferenceFixture(ctx, false, time.Time{})
	if err != nil {
		return out, err
	}
	defer f.close()
	order := []string{"baseline", "irrelevant"}
	rng := rand.New(rand.NewPCG(seed, seed^0xa54ff53a))
	rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	out.Variation = "insertion/" + strings.Join(order, "_")
	var baseline memy.Record[Preference, string]
	for _, id := range order {
		record, operationErr := f.commit(ctx, id, preferencePayload(id, false), memy.Interval{}, 0, memy.Append, nil)
		if operationErr != nil {
			return out, operationErr
		}
		if id == "baseline" {
			baseline = record
		}
	}
	before, err := fullSnapshot(f.engine, ctx, "host", f.scope, memy.ReadOptions{Purpose: "quality"})
	if err != nil {
		return out, err
	}
	// Mutable fixture-owned failures are classified by sentinel, never message.
	failed := true
	provider := reference.ExtractorFunc[string, Preference, string](func(_ context.Context, _ string) ([]memy.Suggestion[Preference, string], error) {
		if failed {
			return nil, memy.ErrUnavailable
		}
		return []memy.Suggestion[Preference, string]{f.suggestion(preferencePayload("evening", true), memy.Interval{})}, nil
	})
	_, err = memy.Extract(ctx, f.engine, "host", f.scope, memy.ExtractionJob{OperationID: "failed-provider", ProviderVersion: "scripted-preference/v2", Purpose: "quality"}, "synthetic input", memy.JSONCodec[string]{}, provider)
	out.check("execution", "expected_provider_unavailable", errors.Is(err, memy.ErrUnavailable), 0)
	out.Checks[len(out.Checks)-1].Expected = "unavailable"
	out.Checks[len(out.Checks)-1].Observed = ErrorClass(err)
	if err == nil {
		return out, nil
	}
	if !errors.Is(err, memy.ErrUnavailable) {
		return out, err
	}
	failed = false
	recovery, err := memy.Extract(ctx, f.engine, "host", f.scope, memy.ExtractionJob{OperationID: "recovered-provider", ProviderVersion: "scripted-preference/v2", Purpose: "quality"}, "synthetic input", memy.JSONCodec[string]{}, provider)
	if err != nil {
		return out, err
	}
	out.check("execution", "provider_recovery_unaccepted_proposal", len(recovery) == 1, len(recovery))
	f.policy.Fail(memy.ErrUnavailable)
	unknown, err := f.engine.Get(ctx, "host", f.scope, "baseline", memy.ReadOptions{Purpose: "quality"})
	out.check("execution", "expected_policy_unavailable_fail_closed", errors.Is(err, memy.ErrUnavailable) && unknown.ID == "", 0)
	out.Checks[len(out.Checks)-1].Expected = "unavailable"
	out.Checks[len(out.Checks)-1].Observed = ErrorClass(err)
	f.policy.Fail(nil)
	if err == nil {
		return out, nil
	}
	if !errors.Is(err, memy.ErrUnavailable) {
		return out, err
	}
	after, err := fullSnapshot(f.engine, ctx, "host", f.scope, memy.ReadOptions{Purpose: "quality"})
	if err != nil {
		return out, err
	}
	out.check("canonical", "port_failures_do_not_apply_or_erase", reflect.DeepEqual(before, after), len(after))
	records, body, err := f.recall(ctx, []string{"baseline"}, memy.ReadOptions{}, limit, maxCandidates, bytes)
	if err != nil {
		return out, err
	}
	preferenceDelivered(&out, records, []memy.Record[Preference, string]{baseline}, body)
	return out, nil
}

func preferenceVersions() PortVersions {
	return PortVersions{Provider: "scripted-preference/v2", Model: "scripted", HostReview: "preference-review/v2", Retention: "retention/v1", Resolver: "resolver/v1", Consolidation: "consolidation/v2", Search: "preference-index/v1", Projector: "preference-projection/v2", Packing: "json-packing/v1", Grader: "none"}
}
func preferenceVersionsFor(id string) PortVersions {
	versions := preferenceVersions()
	if id == "preference-sessions" {
		versions.Search = "preference-index/v1+conflict-metadata/v2"
	}
	return versions
}
func preferencePlans() []CasePlan {
	base := []RequiredCheck{{Stage: "effective", ID: "exact_revisions_payload_validity_sources_lineage"}, {Stage: "rendered", ID: "exact_projection_body_and_budget"}, {Stage: "execution", ID: "completed"}}
	add := func(extra ...RequiredCheck) []RequiredCheck { return append(slices.Clone(base), extra...) }
	result := []CasePlan{
		{ID: "preference-sessions", Domain: "preference", Version: "preference-sessions/v2", Groups: []string{"sessions"}, Required: add(RequiredCheck{"effective", "unknown_read_options"}, RequiredCheck{"effective", "conflict_read_options"}, RequiredCheck{"effective", "conflict_get_abstains"}, RequiredCheck{"canonical", "historical_original_exact_revision"}, RequiredCheck{"canonical", "scope_and_sources"}), OptionalStages: []string{"candidate", "host_review"}},
		{ID: "preference-temporal", Domain: "preference", Version: "preference-temporal/v2", Groups: []string{"temporal"}, Required: add(RequiredCheck{"effective", "valid_time_selects_distinct_fact"}, RequiredCheck{"effective", "recorded_time_selects_original_revision"}, RequiredCheck{"canonical", "historical_read_obeys_current_revoke"}, RequiredCheck{"canonical", "historical_read_obeys_current_retention"}), OptionalStages: []string{"candidate", "host_review"}},
		{ID: "preference-port-failures", Domain: "preference", Version: "preference-port-failures/v2", Groups: []string{"abstention"}, Required: add(RequiredCheck{"execution", "expected_provider_unavailable"}, RequiredCheck{"execution", "provider_recovery_unaccepted_proposal"}, RequiredCheck{"execution", "expected_policy_unavailable_fail_closed"}, RequiredCheck{"canonical", "port_failures_do_not_apply_or_erase"}), OptionalStages: []string{"candidate", "host_review"}},
	}
	for _, entry := range []struct {
		id   string
		mode memy.ConsolidationMode
	}{{"baseline", ""}, {"exact", memy.ExactDedup}, {"domain", memy.DomainMerge}, {"semantic", memy.SemanticMerge}} {
		required := add(RequiredCheck{"canonical", "full_original_documents_preserved"}, RequiredCheck{"canonical", "scope_sources_and_exact_lineage"})
		optional := []string{}
		if entry.mode == "" {
			optional = []string{"candidate", "host_review"}
		} else {
			required = append(required, RequiredCheck{"execution", "consolidation_proposal_created"})
			if entry.mode == memy.SemanticMerge {
				required = append(required, RequiredCheck{"host_review", "explicit_semantic_rejection"}, RequiredCheck{"candidate", "source_identity_preserved"}, RequiredCheck{"candidate", "exact_input_lineage"})
			} else {
				required = append(required, RequiredCheck{"candidate", "facts_negation_validity_sources_lineage"}, RequiredCheck{"candidate", "valid_interval_preserved"}, RequiredCheck{"candidate", "source_identity_preserved"}, RequiredCheck{"candidate", "exact_input_lineage"}, RequiredCheck{"candidate", "negation_preserved"}, RequiredCheck{"host_review", "explicit_apply_checked_receipt"})
			}
		}
		result = append(result, CasePlan{ID: "preference-consolidation-" + entry.id, Domain: "preference", Version: "preference-consolidation/v2", Groups: []string{"consolidation"}, Required: required, OptionalStages: optional})
	}
	for i := range result {
		plan := &result[i]
		plan.Versions = preferenceVersionsFor(plan.ID)
		id := plan.ID
		plan.Run = func(ctx context.Context, c Corpus, run CaseRun) (ScenarioReport, error) {
			var observed preferenceRunResult
			var err error
			b := c.Budgets
			switch id {
			case "preference-sessions":
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
				case "preference-consolidation-semantic":
					mode = memy.SemanticMerge
				}
				observed, err = preferenceConsolidation(ctx, run.Seed, b.RecallLimit, b.MaxCandidates, b.ContextBytes, mode, memy.Budget{InputBytes: b.InputBytes, OutputBytes: b.OutputBytes, CostUnits: b.CostUnits})
			}
			return preferenceReport(id, run, observed, err), err
		}
	}
	return result
}
func preferenceReport(id string, run CaseRun, observed preferenceRunResult, err error) ScenarioReport {
	s := ScenarioReport{ID: id, Domain: "preference", Versions: preferenceVersionsFor(id), Mode: "scripted", Variation: observed.Variation, Candidate: StageResult{Status: NotApplicable}, HostReview: StageResult{Status: NotApplicable}}
	stage := func(name string) *StageResult {
		switch name {
		case "candidate":
			return &s.Candidate
		case "host_review":
			return &s.HostReview
		case "effective":
			return &s.Effective
		case "canonical":
			return &s.Canonical
		case "rendered":
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
		mandatory := o.Stage != "candidate" || id != "preference-consolidation-semantic" || o.ID == "source_identity_preserved" || o.ID == "exact_input_lineage"
		p.Checks = append(p.Checks, Check{ID: o.ID, Mandatory: mandatory, Status: status, Expected: o.Expected, Observed: o.Observed, Evidence: []Evidence{{Code: o.ID, Count: o.Count}}})
	}
	if observed.HostDecision != "" {
		s.HostReview.Version = "preference-review/v2"
		for i := range s.HostReview.Checks {
			s.HostReview.Checks[i].Expected = observed.HostDecision
			if s.HostReview.Checks[i].Status == Pass {
				s.HostReview.Checks[i].Observed = observed.HostDecision
			} else {
				s.HostReview.Checks[i].Observed = "unknown"
			}
		}
	}
	if err == nil {
		s.Execution.Checks = append(s.Execution.Checks, Check{ID: "completed", Mandatory: true, Status: Pass, Expected: "completed", Observed: "completed", Evidence: []Evidence{{Code: "fixture_completed", Count: 1}}})
		s.Metrics.PayloadBytes = KnownMeasurement("bytes/json-payload", observed.PayloadBytes)
		s.Metrics.ContextJSONBytes = KnownMeasurement("bytes/json-context", observed.ContextBytes)
	} else {
		s.Metrics.PayloadBytes = UnavailableMeasurement("bytes/json-payload", "execution_incomplete")
		s.Metrics.ContextJSONBytes = UnavailableMeasurement("bytes/json-context", "execution_incomplete")
	}
	s.Metrics.CanonicalBytes = UnavailableMeasurement("bytes/serialized-canonical", "not_measured_adapter_storage_excluded")
	cost := uint64(0)
	if id == "preference-consolidation-domain" || id == "preference-consolidation-semantic" {
		cost = 1
	}
	if err == nil {
		s.Metrics.ProviderCost = KnownMeasurement("scripted-cost-units", cost)
	} else {
		s.Metrics.ProviderCost = UnavailableMeasurement("scripted-cost-units", "execution_incomplete")
	}
	s.Metrics.RealProviderCost = UnavailableMeasurement("tokens-or-currency", "scripted_provider")
	return s
}

// Conflict candidates are scripted metadata, not managed derived artifacts.
// Engine.WithDerivedWrite rejects conflicted lineage; Recall must nevertheless
// honor IncludeConflicts when a search port supplies those exact revisions.
type preferenceConflictSearch struct{ Scope memy.Scope }

func (preferenceConflictSearch) Capabilities() memy.SearchCapabilities {
	return memy.SearchCapabilities{Scoped: true, BoundedCandidates: true}
}
func (search preferenceConflictSearch) Search(ctx context.Context, scope memy.Scope, query preferenceQuery, opts memy.SearchOptions) (memy.SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return memy.SearchResult{}, err
	}
	if opts.MaxCandidates < 1 {
		return memy.SearchResult{}, memy.ErrInvalid
	}
	result := memy.SearchResult{Coverage: []memy.Coverage{{Backend: "preference-conflict-metadata/v2", Status: "ready"}}}
	if scope != search.Scope {
		return memy.SearchResult{}, memy.ErrScopeViolation
	}
	if !slices.Contains(query.IDs, "conflict") {
		return result, nil
	}
	for revision := memy.Version(1); revision <= 2; revision++ {
		result.Candidates = append(result.Candidates, memy.Candidate{RecordID: "conflict", Revision: revision, Score: 1, Signals: []memy.SearchSignal{{Backend: "preference-conflict-metadata/v2", Rank: int(revision), Score: 1}}})
	}
	if len(result.Candidates) > opts.MaxCandidates {
		result.Candidates = result.Candidates[:opts.MaxCandidates]
		result.CandidatesTruncated = true
	}
	return result, nil
}
