// Command retrieval runs deterministic offline adapters, RRF and exact JSON packing.
// Run with: go run ./examples/retrieval
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
	"github.com/skosovsky/memy/store/memory"
)

type Incident struct {
	Service   string `json:"service"`
	Procedure string `json:"procedure"`
}
type SourceRef struct {
	Document int    `json:"document"`
	Section  string `json:"section"`
}
type Operator struct{ ID string }

// Query is consumer-owned structured input, with no required text or embedding.
type Query struct {
	Service  string
	Severity int
}

// scriptedSearch is an offline fixture. Production sparse/dense integrations
// replace this with memy.Search[Query], retaining exact scope, bounded results,
// visibility semantics and one coverage identity. No SDK or network is required.
type scriptedSearch struct {
	ID         string
	Scope      memy.Scope
	Candidates []memy.Candidate
	Fail       bool
}

func (scriptedSearch) Capabilities() memy.SearchCapabilities {
	return memy.SearchCapabilities{Scoped: true, Visibility: false, BoundedCandidates: true}
}

func (s scriptedSearch) Search(
	ctx context.Context,
	scope memy.Scope,
	query Query,
	options memy.SearchOptions,
) (memy.SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return memy.SearchResult{}, err
	}
	if scope != s.Scope {
		return memy.SearchResult{}, memy.ErrScopeViolation
	}
	if options.MaxCandidates < 1 || options.MaxCandidates > memy.MaxSearchCandidates {
		return memy.SearchResult{}, memy.ErrInvalid
	}
	if options.Minimum != nil {
		return memy.SearchResult{}, memy.ErrUnsupported
	}
	result := memy.SearchResult{
		Coverage:            []memy.Coverage{{Backend: s.ID, Status: "eventual", MinimumSatisfied: false}},
		Candidates:          nil,
		CandidatesTruncated: false,
	}
	if s.Fail {
		result.Coverage[0].Status = "unavailable"
		return result, memy.ErrUnavailable
	}
	if query.Service != paymentsService || query.Severity < 2 {
		return result, nil
	}
	for i, candidate := range s.Candidates {
		if i == options.MaxCandidates {
			result.CandidatesTruncated = true
			break
		}
		candidate.Signals = []memy.SearchSignal{{Backend: s.ID, Rank: i + 1, Score: candidate.Score}}
		result.Candidates = append(result.Candidates, candidate)
	}
	return result, nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	ctx := context.Background()
	scope := memy.Scope{Tenant: "demo", Namespace: "runbooks", Subject: "oncall"}
	actor := Operator{ID: "sergey"}
	clock := reference.NewClock(time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC))
	authority := reference.NewPolicy(func(a Operator) string { return a.ID })
	authority.Grant(
		actor.ID,
		scope,
		"authority/v1",
		memy.ActionPropose,
		memy.ActionAccept,
		memy.ActionCommit,
		memy.ActionRead,
	)
	registry := reference.NewRegistry[SourceRef](memy.JSONCodec[SourceRef]{})
	source := memy.Source[SourceRef]{
		ID:        "runbook",
		Revision:  "r1",
		Reference: SourceRef{Document: runbookDocument, Section: paymentsService},
	}
	if err := registry.Put(scope, source); err != nil {
		return err
	}
	engine, err := memy.New(
		memy.Config[Incident, SourceRef, Operator]{
			Store:     memory.New(),
			Authority: authority,
			Sources:   registry,
			Clock:     clock,
			Retention: reference.Retain[Incident]{
				Version:   "retention/v1",
				ExpiresAt: clock.Now().Add(retentionHours * time.Hour),
			},
			PayloadCodec:   memy.JSONCodec[Incident]{},
			ReferenceCodec: memy.JSONCodec[SourceRef]{}, Sinks: nil, Reintroduction: nil,
		},
	)
	if err != nil {
		return err
	}
	if seedErr := seedRunbooks(ctx, engine, actor, scope, source, clock.Now()); seedErr != nil {
		return seedErr
	}
	sparse := scriptedSearch{
		ID: "sparse", Fail: false,
		Scope: scope,
		Candidates: []memy.Candidate{
			{RecordID: "a", Revision: 1, Score: sparsePrimaryScore, Signals: nil},
			{RecordID: "b", Revision: 1, Score: sparseSecondaryScore, Signals: nil},
			{RecordID: "a", Revision: staleRevision, Score: sparseStaleScore, Signals: nil},
			{RecordID: "c", Revision: 1, Score: sparseOversizedScore, Signals: nil},
			{RecordID: "unseen", Revision: 1, Score: sparseMissingScore, Signals: nil},
		},
	}
	dense := scriptedSearch{
		ID: "dense", Fail: false,
		Scope: scope,
		Candidates: []memy.Candidate{
			{RecordID: "b", Revision: 1, Score: densePrimaryScore, Signals: nil},
			{RecordID: "a", Revision: 1, Score: denseSecondaryScore, Signals: nil},
			{RecordID: "a", Revision: staleRevision, Score: denseStaleScore, Signals: nil},
			{RecordID: "c", Revision: 1, Score: denseOversizedScore, Signals: nil},
			{RecordID: "unseen", Revision: 1, Score: denseMissingScore, Signals: nil},
		},
	}
	search := reference.Composite[Query]{
		Backends:      []reference.Backend[Query]{{ID: sparse.ID, Search: sparse}, {ID: dense.ID, Search: dense}},
		RRF:           reference.RRFConfig{K: rrfOffset, Weights: nil},
		AllowDegraded: true,
	}
	query := Query{Service: paymentsService, Severity: incidentSeverity}
	options := memy.RecallOptions{
		Read: memy.ReadOptions{
			Purpose:          "assist",
			ValidAsOf:        time.Time{},
			RecordedAsOf:     time.Time{},
			IncludeUnknown:   false,
			IncludeConflicts: false,
		},
		Search: memy.SearchOptions{MaxCandidates: candidateLimit, Minimum: nil},
		Limit:  recallLimit,
	}
	return demonstrateRetrieval(ctx, engine, actor, scope, query, search, dense, options)
}

func seedRunbooks(
	ctx context.Context,
	engine *memy.Engine[Incident, SourceRef, Operator],
	actor Operator,
	scope memy.Scope,
	source memy.Source[SourceRef],
	observedAt time.Time,
) error {
	for _, entry := range []struct{ ID, Text string }{{"a", "Check queue lag."}, {"b", "Check retry pressure."}, {"c", strings.Repeat("Detailed diagnostic. ", oversizedDiagnosticRepeats)}} {
		proposal, err := engine.Remember(
			ctx,
			actor,
			scope,
			"proposal-"+entry.ID,
			"assist",
			memy.Suggestion[Incident, SourceRef]{
				Payload:       Incident{Service: paymentsService, Procedure: entry.Text},
				Sources:       []memy.Source[SourceRef]{source},
				Extractor:     "operator/v1",
				Evidence:      "approved runbook excerpt",
				ObservedAt:    observedAt,
				Valid:         memy.Interval{Known: true, From: observedAt, To: time.Time{}},
				Uncertainties: []string{"Confirm incident conditions before acting"},
				Losses:        []string{"Original formatting omitted"}, ExpiresAt: time.Time{}, Lineage: nil,
			},
		)
		if err != nil {
			return err
		}
		accepted, err := engine.Accept(ctx, actor, scope, proposal.ID, proposal.Digest, proposal.Revision, "assist")
		if err != nil {
			return err
		}
		_, err = engine.Commit(
			ctx,
			actor,
			scope,
			"assist",
			memy.CommitRequest{
				OperationID: "commit-" + entry.ID, Expected: 0,
				ProposalID: proposal.ID,
				Acceptance: accepted,
				RecordID:   entry.ID,
				Reconcile: memy.Reconciliation{
					Mode:          memy.Append,
					PolicyVersion: "resolver/v1",
					Basis:         "independent runbook entry", Related: nil,
				},
			},
		)
		if err != nil {
			return err
		}
	}
	return nil
}

const (
	paymentsService            = "payments"
	runbookDocument            = 42
	oversizedDiagnosticRepeats = 1000
	incidentSeverity           = 3
)

const (
	retentionHours = 24
	rrfOffset      = 60
	recallLimit    = 3
)

func demonstrateDegraded(
	ctx context.Context,
	engine *memy.Engine[Incident, SourceRef, Operator],
	actor Operator,
	scope memy.Scope,
	query Query,
	search reference.Composite[Query],
	dense scriptedSearch,
	options memy.RecallOptions,
	projector reference.ProjectorFunc[Incident, SourceRef, Incident],
	budget *memy.ProjectionBudget[Incident, SourceRef],
	limit uint64,
) error {
	// Simulate partial backend failure separately, preserving the healthy search
	// and reporting unavailable coverage rather than declaring complete knowledge.
	dense.Fail = true
	search.Backends[1].Search = dense
	degraded, err := memy.RecallProjected(
		ctx,
		engine,
		actor,
		scope,
		query,
		search,
		memy.ScoreRanker[Incident, SourceRef]{},
		options,
		projector,
		budget,
	)
	if err != nil {
		return err
	}
	degradedRaw, err := json.Marshal(degraded)
	if err != nil {
		return err
	}
	if uint64(len(degradedRaw)) != degraded.Budget.Used || degraded.Budget.Used > limit {
		return errors.New("degraded output exceeded strict budget")
	}
	fmt.Printf(
		"Partial failure: coverage=%+v checked=%d filtered=%d truncated=%t omissions=%+v bytes=%d\n",
		degraded.Coverage,
		degraded.Progress.CanonicalChecked,
		degraded.Progress.CanonicalFiltered,
		degraded.Progress.CandidatesTruncated,
		degraded.Omissions,
		len(degradedRaw),
	)
	fmt.Println(
		"Fixture proves contract behavior; it does not measure production retrieval quality or external backend billing.",
	)
	return nil
}

const (
	sparsePrimaryScore   = 9000
	sparseSecondaryScore = 7000
	sparseStaleScore     = 6000
	sparseOversizedScore = 5000
	sparseMissingScore   = 1000
	densePrimaryScore    = 0.92
	denseSecondaryScore  = 0.89
	denseStaleScore      = 0.85
	denseOversizedScore  = 0.79
	denseMissingScore    = 0.2
	staleRevision        = 99
	candidateLimit       = 4
)

func singleProjectionBody(
	all memy.ProjectedRecallResult[Incident, SourceRef],
) memy.ProjectedRecallResult[Incident, SourceRef] {
	target := all
	target.Projections = all.Projections[:1]
	target.Omissions = []memy.BudgetOmission{
		{
			Ref:    memy.RevisionRef{RecordID: all.Projections[1].RecordID, Revision: all.Projections[1].Revision},
			Reason: memy.OmittedBudget,
		},
		{
			Ref:    memy.RevisionRef{RecordID: all.Projections[2].RecordID, Revision: all.Projections[2].Revision},
			Reason: memy.OmittedOversized,
		},
	}
	return target
}

func demonstrateRetrieval(
	ctx context.Context,
	engine *memy.Engine[Incident, SourceRef, Operator],
	actor Operator,
	scope memy.Scope,
	query Query,
	search reference.Composite[Query],
	dense scriptedSearch,
	options memy.RecallOptions,
) error {
	ranked, err := memy.Recall(
		ctx,
		engine,
		actor,
		scope,
		query,
		search,
		memy.ScoreRanker[Incident, SourceRef]{},
		options,
	)
	if err != nil {
		return err
	}
	for _, record := range ranked.Records {
		fmt.Printf("RRF %s score=%.6f signals=%+v\n", record.Record.ID, record.Score, record.Signals)
	}
	projector := reference.ProjectorFunc[Incident, SourceRef, Incident]{
		PolicyVersion: "projection/v1",
		Apply: func(_ context.Context, record memy.Record[Incident, SourceRef]) (Incident, error) {
			return record.Payload, nil
		},
	}
	all, err := memy.RecallProjected(
		ctx,
		engine,
		actor,
		scope,
		query,
		search,
		memy.ScoreRanker[Incident, SourceRef]{},
		options,
		projector,
		nil,
	)
	if err != nil {
		return err
	}
	if len(all.Projections) != 3 || all.Progress.CanonicalFiltered != 1 || !all.Progress.CandidatesTruncated {
		return errors.New("fixture did not exercise bounded processing and stale filtering")
	}
	policy := reference.JSONPacking[Incident, SourceRef]{RejectOversized: false}
	// Choose an illustrative limit permitting exactly the first ranked record,
	// including the final omission envelope for the remaining two. A production
	// consumer supplies its own externally configured limit and output codec.
	target := singleProjectionBody(all)
	limit, err := policy.Measure(ctx, target)
	if err != nil {
		return err
	}
	budget := &memy.ProjectionBudget[Incident, SourceRef]{Max: limit, Codec: memy.JSONCodec[Incident]{}, Policy: policy}
	final, err := memy.RecallProjected(
		ctx,
		engine,
		actor,
		scope,
		query,
		search,
		memy.ScoreRanker[Incident, SourceRef]{},
		options,
		projector,
		budget,
	)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(final)
	if err != nil {
		return err
	}
	if uint64(len(raw)) != final.Budget.Used || final.Budget.Used > final.Budget.Limit || !final.Budget.Exact ||
		len(final.Omissions) != 2 ||
		len(final.Projections) != 1 {
		return errors.New("strict serialized budget verification failed")
	}
	if final.Projections[0].Trust != "data" || len(final.Projections[0].Provenance.Sources) != 1 ||
		len(final.Projections[0].Provenance.Uncertainties) != 1 ||
		final.Projections[0].ExpiresAt.IsZero() {
		return errors.New("projection metadata lost")
	}
	fmt.Printf("Final body (%d/%d bytes/json, receipt excluded): %s\n", len(raw), limit, raw)
	return demonstrateDegraded(ctx, engine, actor, scope, query, search, dense, options, projector, budget, limit)
}
