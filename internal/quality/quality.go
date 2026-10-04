// Package quality implements a consumer-owned offline experiment, not a core
// dependency or a model evaluator. All provider scripts and expected facts are
// fixed by the versioned synthetic corpus.
package quality

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
	"github.com/skosovsky/memy/store/memory"
)

const (
	baselineMode            = "baseline"
	experimentPurpose       = "quality"
	consolidationByteBudget = 10_000
	consolidationCostBudget = 5
)

// Preference is the experiment's consumer payload.
type Preference struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Case is a host-owned synthetic input, not provider authority output.
type Case struct {
	ID     string    `json:"id"`
	Tenant string    `json:"tenant"`
	Key    string    `json:"key"`
	Value  string    `json:"value"`
	Known  bool      `json:"known"`
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
}

// Corpus pins inputs, expected facts and provider/policy versions.
type Corpus struct {
	Version     string   `json:"version"`
	Provider    string   `json:"provider"`
	Resolver    string   `json:"resolver"`
	Projection  string   `json:"projection"`
	Records     []Case   `json:"records"`
	Expected    []string `json:"expected_unique_A"`
	BadSemantic string   `json:"scripted_bad_semantic"`
	AutoApply   bool     `json:"auto_apply"`
}

// Trial records candidate quality separately from effective accepted recall.
type Trial struct {
	Mode              string `json:"mode"`
	Accepted          bool   `json:"accepted"`
	ExpectedMatches   int    `json:"expected_matches"`
	ExpectedTotal     int    `json:"expected_total"`
	FalseMemory       int    `json:"false_memory"`
	PrivacyLeaks      int    `json:"privacy_leaks"`
	LostNegations     int    `json:"lost_negations"`
	LostIntervals     int    `json:"lost_intervals"`
	InputBytes        int    `json:"input_bytes"`
	CandidateBytes    int    `json:"candidate_bytes"`
	ProviderCostUnits int64  `json:"provider_cost_units"`
	ProposalLatencyNS int64  `json:"proposal_latency_ns"`
	RecallLatencyNS   int64  `json:"recall_latency_ns"`
	CandidateRecords  int    `json:"candidate_records"`
	EffectiveRecords  int    `json:"effective_records"`
	OriginalsRetained int    `json:"originals_retained"`
}

// Report describes one measured local run, not a statistical performance claim.
type Report struct {
	CorpusVersion     string  `json:"corpus_version"`
	ExtractorVersion  string  `json:"extractor_version"`
	ResolverVersion   string  `json:"resolver_version"`
	ProjectionVersion string  `json:"projection_version"`
	StoreConsistency  string  `json:"store_consistency"`
	GoVersion         string  `json:"go_version"`
	AutoApply         bool    `json:"auto_apply"`
	Trials            []Trial `json:"trials"`
}

type user struct{ id string }
type session struct {
	engine   *memy.Engine[Preference, string, user]
	store    *memory.Store
	scope    memy.Scope
	actor    user
	corpus   Corpus
	index    *reference.Index[string]
	selected *reference.Index[string]
	refs     []memy.RevisionRef
	baseline []memy.Record[Preference, string]
}

// Load rejects incomplete or unversioned experiment configuration.
func Load(path string) (Corpus, error) {
	raw, operationErr := os.ReadFile(path)
	if operationErr != nil {
		return Corpus{}, operationErr
	}
	corpus, operationErr := (memy.JSONCodec[Corpus]{}).Decode(raw)
	if operationErr != nil {
		return Corpus{}, operationErr
	}
	if validationErr := validateCorpus(corpus); validationErr != nil {
		return Corpus{}, validationErr
	}
	return corpus, nil
}

// Run measures all four policies against independently initialized state.
func Run(ctx context.Context, corpus Corpus) (Report, error) {
	if validationErr := validateCorpus(corpus); validationErr != nil {
		return Report{}, validationErr
	}
	var report Report
	report.CorpusVersion, report.ExtractorVersion = corpus.Version, corpus.Provider
	report.ResolverVersion, report.ProjectionVersion = corpus.Resolver, corpus.Projection
	report.GoVersion, report.StoreConsistency = runtime.Version(), "memory/v1: atomic CAS; nondurable; index visibility acknowledged"
	for _, mode := range []string{baselineMode, string(memy.ExactDedup), string(memy.DomainMerge), string(memy.SemanticMerge)} {
		trial, err := runTrial(ctx, corpus, mode)
		if err != nil {
			return Report{}, err
		}
		report.Trials = append(report.Trials, trial)
	}
	return report, nil
}

func newSession(corpus Corpus) (*session, error) {
	store := memory.New()
	scope := memy.Scope{Tenant: "A", Namespace: experimentPurpose, Subject: "user"}
	policy := reference.NewPolicy(func(actor user) string { return actor.id })
	sources := reference.NewRegistry[string](memy.JSONCodec[string]{})
	for _, item := range corpus.Records {
		recordScope := scope
		recordScope.Tenant = item.Tenant
		policy.Grant(
			item.Tenant,
			recordScope,
			"quality-authority/v1",
			memy.ActionRead,
			memy.ActionPropose,
			memy.ActionAccept,
			memy.ActionCommit,
			memy.ActionConsolidate,
		)
		if err := sources.Put(
			recordScope,
			memy.Source[string]{
				ID:        item.ID,
				Revision:  corpus.Version,
				Reference: "corpus://" + corpus.Version + "/" + item.ID,
			},
		); err != nil {
			_ = store.Close()
			return nil, err
		}
	}
	index := reference.NewIndex[string]("baseline-index", nil)
	selected := reference.NewIndex[string]("selected-index", nil)
	var config memy.Config[Preference, string, user]
	config.Store, config.Authority, config.Sources = store, policy, sources
	config.Clock = reference.NewClock(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	config.Retention = reference.Retain[Preference]{Version: "quality-retention/v1", ExpiresAt: time.Time{}}
	config.PayloadCodec, config.ReferenceCodec = memy.JSONCodec[Preference]{}, memy.JSONCodec[string]{}
	config.Sinks = []memy.Sink{index, selected, reference.NewProjectionSink("quality-summary")}
	engine, operationErr := memy.New(config)
	if operationErr != nil {
		_ = store.Close()
		return nil, operationErr
	}
	var result session
	result.engine, result.store, result.corpus = engine, store, corpus
	result.scope, result.actor, result.index, result.selected = scope, user{id: "A"}, index, selected
	return &result, nil
}

func (s *session) seed(ctx context.Context) error {
	for _, item := range s.corpus.Records {
		scope := s.scope
		scope.Tenant = item.Tenant
		actor := user{id: item.Tenant}
		provider := reference.ExtractorFunc[string, Preference, string](
			func(context.Context, string) ([]memy.Suggestion[Preference, string], error) {
				var suggestion memy.Suggestion[Preference, string]
				suggestion.Payload = Preference{Key: item.Key, Value: item.Value}
				suggestion.Valid = memy.Interval{Known: item.Known, From: item.From, To: item.To}
				suggestion.Sources = []memy.Source[string]{
					{
						ID:        item.ID,
						Revision:  s.corpus.Version,
						Reference: "corpus://" + s.corpus.Version + "/" + item.ID,
					},
				}
				suggestion.Evidence = "fixed synthetic source"
				return []memy.Suggestion[Preference, string]{suggestion}, nil
			},
		)
		job := memy.ExtractionJob{
			OperationID:     "extract-" + item.ID,
			ProviderVersion: s.corpus.Provider,
			Purpose:         experimentPurpose,
		}
		proposals, err := memy.Extract(ctx, s.engine, actor, scope, job, item.Value, memy.JSONCodec[string]{}, provider)
		if err != nil {
			return err
		}
		receipt, err := s.accept(ctx, actor, scope, proposals[0], item.ID)
		if err != nil {
			return err
		}
		if err := publish(ctx, s.engine, actor, scope, s.index, receipt); err != nil {
			return err
		}
		if item.Tenant == "A" {
			s.refs = append(s.refs, memy.RevisionRef{RecordID: item.ID, Revision: receipt.Revision})
		}
	}
	result, operationErr := s.recall(ctx, s.index)
	if operationErr != nil {
		return operationErr
	}
	s.baseline = result
	return nil
}

func (s *session) accept(
	ctx context.Context,
	actor user,
	scope memy.Scope,
	p memy.Proposal[Preference, string],
	id string,
) (memy.CommitReceipt, error) {
	acceptance, operationErr := s.engine.Accept(ctx, actor, scope, p.ID, p.Digest, p.Revision, experimentPurpose)
	if operationErr != nil {
		return memy.CommitReceipt{}, operationErr
	}
	var request memy.CommitRequest
	request.OperationID, request.RecordID, request.ProposalID, request.Acceptance = "commit-"+id, id, p.ID, acceptance
	request.Reconcile = memy.Reconciliation{
		Mode:          memy.Append,
		PolicyVersion: s.corpus.Resolver,
		Basis:         "explicit synthetic host review", Related: nil,
	}
	return s.engine.Commit(ctx, actor, scope, experimentPurpose, request)
}

func publish(
	ctx context.Context,
	engine *memy.Engine[Preference, string, user],
	actor user,
	scope memy.Scope,
	index *reference.Index[string],
	receipt memy.CommitReceipt,
) error {
	fence, operationErr := engine.Fence(ctx, actor, scope, experimentPurpose)
	if operationErr != nil {
		return operationErr
	}
	ref := memy.RevisionRef{RecordID: receipt.RecordID, Revision: receipt.Revision}
	if err := engine.WithDerivedWrite(
		ctx,
		actor,
		fence,
		experimentPurpose,
		[]memy.RevisionRef{ref},
		func(ctx context.Context) error {
			return index.Stage(
				ctx,
				scope,
				memy.Candidate{RecordID: receipt.RecordID, Revision: receipt.Revision, Score: 1},
			)
		},
	); err != nil {
		return err
	}
	return index.Acknowledge(ctx, receipt.Visibility)
}

func (s *session) recall(
	ctx context.Context,
	index *reference.Index[string],
) ([]memy.Record[Preference, string], error) {
	var options memy.RecallOptions
	options.Read.Purpose, options.Limit = experimentPurpose, 100
	result, operationErr := memy.Recall(
		ctx,
		s.engine,
		s.actor,
		s.scope,
		"all",
		index,
		memy.ScoreRanker[Preference, string]{},
		options,
	)
	if operationErr != nil {
		return nil, operationErr
	}
	records := make([]memy.Record[Preference, string], 0, len(result.Records))
	for _, record := range result.Records {
		records = append(records, record.Record)
	}
	return records, nil
}

func runTrial(ctx context.Context, corpus Corpus, mode string) (Trial, error) {
	s, operationErr := newSession(corpus)
	if operationErr != nil {
		return Trial{}, operationErr
	}
	defer func() { _ = s.store.Close() }()
	if err := s.seed(ctx); err != nil {
		return Trial{}, err
	}
	var trial Trial
	trial.Mode, trial.Accepted = mode, mode == baselineMode
	trial.InputBytes = payloadBytes(s.baseline)
	candidate := s.baseline
	selectedIndex := s.index
	if mode != baselineMode {
		var selectionErr error
		candidate, selectedIndex, selectionErr = s.reviewTrial(ctx, &trial, mode)
		if selectionErr != nil {
			return Trial{}, selectionErr
		}
	} else {
		measure(&trial, corpus, candidate)
	}
	trial.CandidateRecords, trial.CandidateBytes = len(candidate), payloadBytes(candidate)
	start := time.Now()
	effective, operationErr := s.recall(ctx, selectedIndex)
	trial.RecallLatencyNS = time.Since(start).Nanoseconds()
	if operationErr != nil {
		return Trial{}, operationErr
	}
	trial.EffectiveRecords = len(effective)
	state, operationErr := s.engine.Snapshot(
		ctx,
		s.actor,
		s.scope,
		memy.ReadOptions{
			Purpose:          experimentPurpose,
			ValidAsOf:        time.Time{},
			RecordedAsOf:     time.Time{},
			IncludeUnknown:   false,
			IncludeConflicts: false,
		},
	)
	if operationErr != nil {
		return Trial{}, operationErr
	}
	trial.OriginalsRetained = countOriginals(state, s.refs)
	// Verify foreign scope through the same public read path, independently of
	// the successful recall and the provider's text.
	foreign := s.scope
	foreign.Tenant = "B"
	_, foreignErr := s.engine.Snapshot(
		ctx,
		s.actor,
		foreign,
		memy.ReadOptions{
			Purpose:          experimentPurpose,
			ValidAsOf:        time.Time{},
			RecordedAsOf:     time.Time{},
			IncludeUnknown:   false,
			IncludeConflicts: false,
		},
	)
	if !errors.Is(foreignErr, memy.ErrUnauthorized) {
		trial.PrivacyLeaks++
	}
	return trial, nil
}

func countOriginals(records []memy.Record[Preference, string], refs []memy.RevisionRef) int {
	originals := make(map[memy.RevisionRef]bool, len(refs))
	for _, ref := range refs {
		originals[ref] = true
	}
	count := 0
	for _, record := range records {
		if originals[memy.RevisionRef{RecordID: record.ID, Revision: record.Revision}] {
			count++
		}
	}
	return count
}

func (s *session) reviewTrial(
	ctx context.Context, trial *Trial, mode string,
) ([]memy.Record[Preference, string], *reference.Index[string], error) {
	start := time.Now()
	proposals, cost, consolidationErr := s.consolidate(ctx, memy.ConsolidationMode(mode))
	trial.ProposalLatencyNS, trial.ProviderCostUnits = time.Since(start).Nanoseconds(), cost
	if consolidationErr != nil {
		return nil, nil, consolidationErr
	}
	candidate := preview(s.baseline, proposals, mode)
	measure(trial, s.corpus, candidate)
	trial.Accepted = trial.FalseMemory == 0 && trial.PrivacyLeaks == 0 && trial.LostNegations == 0 &&
		trial.LostIntervals == 0 && trial.ExpectedMatches == trial.ExpectedTotal
	if trial.Accepted {
		if applyErr := s.apply(ctx, proposals, mode); applyErr != nil {
			return nil, nil, applyErr
		}
		return candidate, s.selected, nil
	}
	for _, proposal := range proposals {
		if rejectionErr := s.engine.Reject(
			ctx,
			s.actor,
			s.scope,
			proposal.ID,
			proposal.Digest,
			experimentPurpose,
		); rejectionErr != nil {
			return nil, nil, rejectionErr
		}
	}
	return candidate, s.index, nil
}

func (s *session) consolidate(
	ctx context.Context,
	mode memy.ConsolidationMode,
) ([]memy.Proposal[Preference, string], int64, error) {
	var cost int64
	provider := reference.MergeFunc[Preference, string](
		func(_ context.Context, inputs []memy.Record[Preference, string], _ memy.Budget) (memy.MergeResult[Preference, string], error) {
			var result memy.MergeResult[Preference, string]
			result.Utility = 1
			if mode == memy.SemanticMerge {
				cost, result.CostUnits = 1, 1
				var suggestion memy.Suggestion[Preference, string]
				suggestion.Payload = Preference{Key: "drink", Value: s.corpus.BadSemantic}
				suggestion.Evidence = "scripted overgeneralization"
				suggestion.Losses = []string{"frequency qualifier", "evening negation", "valid interval"}
				result.Suggestions = []memy.Suggestion[Preference, string]{suggestion}
				return result, nil
			}
			seen := make(map[Preference]bool)
			for _, record := range inputs {
				if seen[record.Payload] {
					continue
				}
				seen[record.Payload] = true
				var suggestion memy.Suggestion[Preference, string]
				suggestion.Payload, suggestion.Valid = record.Payload, record.Valid
				suggestion.Evidence = "exact typed domain equality"
				result.Suggestions = append(result.Suggestions, suggestion)
			}
			return result, nil
		},
	)
	var request memy.ConsolidationRequest
	request.OperationID, request.Purpose, request.PolicyVersion, request.Mode, request.Inputs = "consolidation", experimentPurpose, "quality-consolidation/v1", mode, s.refs
	request.Budget = memy.Budget{
		InputBytes:  consolidationByteBudget,
		OutputBytes: consolidationByteBudget,
		CostUnits:   consolidationCostBudget,
	}
	proposals, operationErr := memy.Consolidate(ctx, s.engine, s.actor, s.scope, request, provider)
	return proposals, cost, operationErr
}

func preview(
	baseline []memy.Record[Preference, string],
	proposals []memy.Proposal[Preference, string],
	mode string,
) []memy.Record[Preference, string] {
	var records []memy.Record[Preference, string]
	if mode == string(memy.ExactDedup) {
		replaced := make(map[string]bool)
		for _, p := range proposals {
			for _, ref := range p.Suggestion.Lineage {
				replaced[ref.RecordID] = true
			}
		}
		for _, record := range baseline {
			if !replaced[record.ID] {
				records = append(records, record)
			}
		}
	}
	for _, p := range proposals {
		var record memy.Record[Preference, string]
		record.Payload, record.Valid, record.Scope = p.Suggestion.Payload, p.Suggestion.Valid, p.Scope
		record.Provenance.Sources = p.Suggestion.Sources
		records = append(records, record)
	}
	return records
}

func (s *session) apply(ctx context.Context, proposals []memy.Proposal[Preference, string], mode string) error {
	replaced := make(map[string]bool)
	for i, p := range proposals {
		for _, ref := range p.Suggestion.Lineage {
			replaced[ref.RecordID] = true
		}
		receipt, err := s.accept(ctx, s.actor, s.scope, p, mode+"/"+strconv.Itoa(i))
		if err != nil {
			return err
		}
		if err := publish(ctx, s.engine, s.actor, s.scope, s.selected, receipt); err != nil {
			return err
		}
	}
	if mode != string(memy.ExactDedup) {
		return nil
	}
	for _, record := range s.baseline {
		if replaced[record.ID] {
			continue
		}
		receipt := memy.CommitReceipt{
			RecordID: record.ID,
			Revision: record.Revision,
			Visibility: memy.VisibilityToken{
				Scope:    s.scope,
				RecordID: record.ID,
				Revision: record.Revision,
			},
			OperationID:        "",
			CanonicalCommitted: false,
		}
		if err := publish(ctx, s.engine, s.actor, s.scope, s.selected, receipt); err != nil {
			return err
		}
	}
	return nil
}

func measure(trial *Trial, corpus Corpus, records []memy.Record[Preference, string]) {
	trial.ExpectedTotal = len(corpus.Expected)
	expected := make(map[Preference]memy.Interval)
	for _, item := range corpus.Records {
		if item.Tenant == "A" {
			expected[Preference{Key: item.Key, Value: item.Value}] = memy.Interval{
				Known: item.Known,
				From:  item.From,
				To:    item.To,
			}
		}
	}
	seen := make(map[Preference]bool)
	negativePresent := false
	for _, record := range records {
		valid, ok := expected[record.Payload]
		if !ok {
			trial.FalseMemory++
		} else if record.Valid == valid && !seen[record.Payload] {
			trial.ExpectedMatches++
		}
		seen[record.Payload] = true
		if record.Payload.Value == "never drinks tea in the evening" {
			negativePresent = true
		}
		if !record.Valid.Known || record.Valid.From != corpus.Records[0].From ||
			record.Valid.To != corpus.Records[0].To {
			trial.LostIntervals++
		}
		if record.Scope.Tenant != "A" {
			trial.PrivacyLeaks++
		}
		trial.PrivacyLeaks += foreignSources(record.Provenance.Sources)
	}
	if !negativePresent {
		trial.LostNegations++
	}
}

func payloadBytes(records []memy.Record[Preference, string]) int {
	total := 0
	for _, record := range records {
		encoded, _ := json.Marshal(record.Payload)
		total += len(encoded)
	}
	return total
}

func foreignSources(sources []memy.Source[string]) int {
	leaks := 0
	for _, source := range sources {
		if source.ID == "tea-other-scope" {
			leaks++
		}
	}
	return leaks
}
