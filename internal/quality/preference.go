package quality

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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
