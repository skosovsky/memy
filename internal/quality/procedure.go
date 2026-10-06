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
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
	"github.com/skosovsky/memy/store/memory"
	"github.com/skosovsky/memy/store/sqlite"
)

const (
	procedureForgetMemoryCase = "procedure-forget-memory"
	procedureForgetSQLiteCase = "procedure-forget-sqlite"
	procedureRetrievalCase    = "procedure-retrieval"
	procedureGuardedCase      = "procedure-poisoning-guarded"
	procedurePermissiveCase   = "procedure-poisoning-permissive"

	procedureCrossScopeCase = "procedure-cross-scope"

	procedureFailuresCase = "procedure-controlled-failures"

	procedureAbstentionCase = "procedure-abstention"
	procedureStaleScore     = 10
	procedurePurgeByteCap   = 1 << 20

	procedureEvidence       = "synthetic observed procedure"
	procedureUncertainty    = "synthetic observation only"
	procedureRRFOffset      = 60
	procedureSeedSalt       = 0x9e3779b97f4a7c15
	procedureRelevantScore  = 1000
	procedureSecondaryScore = 100
	procedureStaleRevision  = 99
	procedureMissingScore   = 0.5

	procedureFormattingLoss     = "formatting omitted"
	procedureUnavailable        = "unavailable"
	procedureService            = "checkout"
	procedureDistractor         = "distractor"
	procedureDomain             = "procedure"
	procedureRetentionVersion   = "procedure-retention/v1"
	procedureResolverVersion    = "procedure-resolver/v1"
	procedureRelevantA          = "relevant-a"
	procedureProjectorVersion   = "procedure-projector/v1"
	procedureConflictID         = "conflict"
	procedureGuardedHostVersion = "guarded-host/v1"
	procedureDocumentID         = 17
	procedureForeignDocumentID  = 29
	procedureMinimumSeverity    = 3
	procedureRetentionHours     = 24
	procedureExpiredHours       = 25
	procedureOriginalCount      = 3
	procedureConflictCount      = 4
	procedureRetrievalLimit     = 4
	procedurePurgeLimit         = 20
)

// ProcedureObservation is consumer-owned knowledge; Procedure is never executed.
type ProcedureObservation struct {
	Service        string   `json:"service"`
	Procedure      string   `json:"procedure"`
	ObservedResult string   `json:"observed_result"`
	Preconditions  []string `json:"preconditions"`
}
type DocumentRef struct {
	Document int    `json:"document"`
	Section  string `json:"section"`
}
type ProcedureQuery struct {
	Service         string
	MinimumSeverity int
}
type ProcedureInput struct {
	Observation ProcedureObservation
	Source      memy.Source[DocumentRef]
	At          time.Time
}

// ProcedurePorts substitutes typed consumer adapters while preserving fixture oracles.
type ProcedurePorts struct {
	Mode, ExtractorVersion, SearchVersion, GraderVersion string
	Extractor                                            memy.Extractor[ProcedureInput, ProcedureObservation, DocumentRef]
	Search                                               func(memy.Scope, []memy.Candidate) memy.Search[ProcedureQuery]
	Grade                                                func(context.Context, []memy.Projection[ProcedureObservation, DocumentRef]) (bool, error)
}

func DefaultProcedurePorts() ProcedurePorts {
	return ProcedurePorts{
		Mode:             fixtureModeScripted,
		ExtractorVersion: "procedure-extractor/v1",
		SearchVersion:    "procedure-rrf/v1",
		GraderVersion:    portNone,
		Extractor: reference.ExtractorFunc[ProcedureInput, ProcedureObservation, DocumentRef](
			func(ctx context.Context, in ProcedureInput) ([]memy.Suggestion[ProcedureObservation, DocumentRef], error) {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				return []memy.Suggestion[ProcedureObservation, DocumentRef]{
					{
						Payload:       in.Observation,
						Sources:       []memy.Source[DocumentRef]{in.Source},
						Evidence:      procedureEvidence,
						ObservedAt:    in.At,
						Valid:         memy.Interval{Known: true, From: in.At, To: time.Time{}},
						Uncertainties: []string{procedureUncertainty},
						Losses:        []string{procedureFormattingLoss},
						Extractor:     "",
						ExpiresAt:     time.Time{}, Lineage: nil,
					},
				}, nil
			},
		),
		Search: func(scope memy.Scope, candidates []memy.Candidate) memy.Search[ProcedureQuery] {
			left := slices.Clone(candidates)
			right := slices.Clone(candidates)
			slices.Reverse(right)
			if len(left) > 0 {
				left = append(left[:1], append([]memy.Candidate{left[0]}, left[1:]...)...)
			}
			return reference.Composite[ProcedureQuery]{
				Backends: []reference.Backend[ProcedureQuery]{
					{ID: "procedure-sparse/v1", Search: procedureSearch{"procedure-sparse/v1", scope, left, false}},
					{ID: "procedure-dense/v1", Search: procedureSearch{"procedure-dense/v1", scope, right, false}},
				},
				RRF:           reference.RRFConfig{K: procedureRRFOffset, Weights: nil},
				AllowDegraded: true,
			}
		},
		Grade: nil}
}

type procedureSearch struct {
	id          string
	scope       memy.Scope
	candidates  []memy.Candidate
	unavailable bool
}

func (procedureSearch) Capabilities() memy.SearchCapabilities {
	return memy.SearchCapabilities{Scoped: true, BoundedCandidates: true, Visibility: false}
}

func (s procedureSearch) Search(
	ctx context.Context,
	scope memy.Scope,
	q ProcedureQuery,
	o memy.SearchOptions,
) (memy.SearchResult, error) {
	result := memy.SearchResult{
		Coverage:            []memy.Coverage{{Backend: s.id, Status: "eventual", MinimumSatisfied: false}},
		Candidates:          nil,
		CandidatesTruncated: false,
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if scope != s.scope {
		return result, memy.ErrScopeViolation
	}
	if o.Minimum != nil {
		return result, memy.ErrUnsupported
	}
	if o.MaxCandidates < 1 || o.MaxCandidates > memy.MaxSearchCandidates {
		return result, memy.ErrInvalid
	}
	if s.unavailable {
		result.Coverage[0].Status = procedureUnavailable
		return result, memy.ErrUnavailable
	}
	if q.Service != procedureService || q.MinimumSeverity < 2 {
		return result, nil
	}
	// Distractors have their own structured service and are excluded by this fixture-owned search oracle.
	for _, c := range s.candidates {
		if c.RecordID == procedureDistractor {
			continue
		}
		if len(result.Candidates) == o.MaxCandidates {
			result.CandidatesTruncated = true
			break
		}
		c.Signals = []memy.SearchSignal{{Backend: s.id, Rank: len(result.Candidates) + 1, Score: c.Score}}
		result.Candidates = append(result.Candidates, c)
	}
	return result, nil
}

type procedureFixture struct {
	engine                      *memy.Engine[ProcedureObservation, DocumentRef, string]
	scope                       memy.Scope
	clock                       *reference.Clock
	authority                   *reference.Policy[string]
	sources                     *reference.Registry[DocumentRef]
	source                      memy.Source[DocumentRef]
	index                       *reference.Index[ProcedureQuery]
	sink                        *reference.ProjectionSink
	ports                       ProcedurePorts
	close                       func() error
	receipts                    map[string]memy.CommitReceipt
	payloads                    map[string]ProcedureObservation
	candidateLimit, recallLimit int
	providerCalls               uint64
	costLimit                   uint64
	committed                   int64
	recordedAt                  map[string]time.Time
	foreignScope                memy.Scope
	foreignSource               memy.Source[DocumentRef]
	foreignPayload              ProcedureObservation
	foreignReceipt              memy.CommitReceipt
}

func newProcedureFixture(ctx context.Context, storage string, ports ProcedurePorts) (*procedureFixture, error) {
	var store memy.Store = memory.New()
	closeFn := func() error { return nil }
	if storage == "sqlite" {
		dir, err := os.MkdirTemp("", "memy-quality-procedure-")
		if err != nil {
			return nil, err
		}
		s, err := sqlite.Open(ctx, filepath.Join(dir, "fixture.db"), sqlite.Options{Fault: nil})
		if err != nil {
			return nil, errors.Join(err, os.RemoveAll(dir))
		}
		store = s
		closeFn = func() error { return errors.Join(s.Close(), os.RemoveAll(dir)) }
	}
	var emptyScope memy.Scope
	var emptyPayload ProcedureObservation
	var emptyReceipt memy.CommitReceipt
	f := &procedureFixture{
		scope:     memy.Scope{Tenant: "synthetic", Namespace: procedureDomain, Subject: "operator"},
		clock:     reference.NewClock(time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)),
		authority: reference.NewPolicy(func(a string) string { return a }),
		sources:   reference.NewRegistry[DocumentRef](memy.JSONCodec[DocumentRef]{}),
		index: reference.NewIndex(
			"procedure-managed/v1",
			func(q ProcedureQuery, _ memy.Candidate) bool { return q.Service == procedureService },
		),
		sink:       reference.NewProjectionSink("procedure-context/v1"),
		ports:      ports,
		close:      closeFn,
		receipts:   map[string]memy.CommitReceipt{},
		payloads:   map[string]ProcedureObservation{},
		recordedAt: map[string]time.Time{},
		engine:     nil,
		source: memy.Source[DocumentRef]{
			ID:        "",
			Revision:  "",
			Reference: DocumentRef{Document: 0, Section: ""},
		},
		candidateLimit: 0,
		recallLimit:    0,
		providerCalls:  0,
		costLimit:      0,
		committed:      0,
		foreignScope:   emptyScope,
		foreignSource: memy.Source[DocumentRef]{
			ID:        "",
			Revision:  "",
			Reference: DocumentRef{Document: 0, Section: ""},
		},
		foreignPayload: emptyPayload,
		foreignReceipt: emptyReceipt,
	}
	f.authority.Grant(
		"operator",
		f.scope,
		"procedure-authority/v1",
		memy.ActionPropose,
		memy.ActionAccept,
		memy.ActionCommit,
		memy.ActionRead,
		memy.ActionForget,
	)
	f.source = memy.Source[DocumentRef]{
		ID:        "synthetic-document",
		Revision:  "r1",
		Reference: DocumentRef{Document: procedureDocumentID, Section: "observations"},
	}
	if err := f.sources.Put(f.scope, f.source); err != nil {
		return nil, errors.Join(err, closeFn())
	}
	e, err := memy.New(
		memy.Config[ProcedureObservation, DocumentRef, string]{
			Store:     store,
			Authority: f.authority,
			Sources:   f.sources,
			Clock:     f.clock,
			Retention: reference.Retain[ProcedureObservation]{
				Version:   procedureRetentionVersion,
				ExpiresAt: f.clock.Now().Add(procedureRetentionHours * time.Hour),
			},
			PayloadCodec:   memy.JSONCodec[ProcedureObservation]{},
			ReferenceCodec: memy.JSONCodec[DocumentRef]{},
			Sinks:          []memy.Sink{f.index, f.sink},
			Reintroduction: nil},
	)
	if err != nil {
		return nil, errors.Join(err, closeFn())
	}
	f.engine = e
	return f, nil
}

func (f *procedureFixture) commit(
	ctx context.Context,
	id string,
	p ProcedureObservation,
	mode memy.ReconcileMode,
	related []memy.RevisionRef,
) error {
	if f.costLimit > 0 && f.providerCalls >= f.costLimit {
		return memy.ErrBudget
	}
	f.providerCalls++
	proposals, err := memy.Extract(
		ctx,
		f.engine,
		"operator",
		f.scope,
		memy.ExtractionJob{
			OperationID:     "extract-" + id,
			ProviderVersion: f.ports.ExtractorVersion,
			Purpose:         qualityPurpose,
		},
		ProcedureInput{Observation: p, Source: f.source, At: f.clock.Now()},
		memy.JSONCodec[ProcedureInput]{},
		f.ports.Extractor,
	)
	if err != nil {
		return err
	}
	if len(proposals) != 1 {
		return memy.ErrInvalid
	}
	proposal := proposals[0]
	accepted, err := f.engine.Accept(
		ctx,
		"operator",
		f.scope,
		proposal.ID,
		proposal.Digest,
		proposal.Revision,
		qualityPurpose,
	)
	if err != nil {
		return err
	}
	receipt, err := f.engine.Commit(
		ctx,
		"operator",
		f.scope,
		qualityPurpose,
		memy.CommitRequest{
			OperationID: "commit-" + id,
			ProposalID:  proposal.ID,
			Acceptance:  accepted,
			RecordID:    id,
			Reconcile: memy.Reconciliation{
				Mode:          mode,
				Related:       related,
				PolicyVersion: procedureResolverVersion,
				Basis:         "synthetic independent observation",
			},
			Expected: 0},
	)
	if err == nil {
		f.receipts[id] = receipt
		f.committed++
		f.recordedAt[id] = f.clock.Now().Add(time.Duration(f.committed-1) * time.Nanosecond)
		f.payloads[id] = p
	}
	return err
}
func procedurePayload(id string) ProcedureObservation {
	return ProcedureObservation{
		Service:        procedureService,
		Procedure:      "inspect-" + id,
		ObservedResult: "healthy-" + id,
		Preconditions:  []string{"read-only observation", "operator review"},
	}
}
func (f *procedureFixture) seed(ctx context.Context, seed uint64) error {
	ids := []string{procedureRelevantA, "relevant-b", procedureDistractor}
	// #nosec G404 -- deterministic fixture permutation; no security-sensitive randomness.
	r := rand.New(rand.NewPCG(seed, seed^procedureSeedSalt))
	r.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
	for _, id := range ids {
		p := procedurePayload(id)
		if id == procedureDistractor {
			p.Service = "inventory"
		}
		if err := f.commit(ctx, id, p, memy.Append, nil); err != nil {
			return err
		}
	}
	return nil
}
func (f *procedureFixture) candidates() []memy.Candidate {
	return []memy.Candidate{
		{
			RecordID: procedureRelevantA,
			Revision: f.receipts[procedureRelevantA].Revision,
			Score:    procedureRelevantScore,
			Signals:  nil,
		},
		{
			RecordID: "relevant-b",
			Revision: f.receipts["relevant-b"].Revision,
			Score:    procedureSecondaryScore,
			Signals:  nil,
		},
		{RecordID: procedureRelevantA, Revision: procedureStaleRevision, Score: procedureStaleScore, Signals: nil},
		{RecordID: procedureDistractor, Revision: 1, Score: 1, Signals: nil},
		{RecordID: "unseen", Revision: 1, Score: procedureMissingScore, Signals: nil},
	}
}
func (f *procedureFixture) options() memy.RecallOptions {
	return memy.RecallOptions{
		Read:   procedureReadOptions(),
		Search: memy.SearchOptions{MaxCandidates: f.candidateLimit, Minimum: nil},
		Limit:  f.recallLimit,
	}
}
func procedureProjector() memy.Projector[ProcedureObservation, DocumentRef, ProcedureObservation] {
	return reference.ProjectorFunc[ProcedureObservation, DocumentRef, ProcedureObservation]{
		PolicyVersion: procedureProjectorVersion,
		Apply: func(_ context.Context, r memy.Record[ProcedureObservation, DocumentRef]) (ProcedureObservation, error) {
			return r.Payload, nil
		},
	}
}

func (f *procedureFixture) recall(
	ctx context.Context,
	search memy.Search[ProcedureQuery],
	query ProcedureQuery,
) (memy.RecallResult[ProcedureObservation, DocumentRef], error) {
	return memy.Recall(
		ctx,
		f.engine,
		"operator",
		f.scope,
		query,
		search,
		memy.ScoreRanker[ProcedureObservation, DocumentRef]{},
		f.options(),
	)
}

func (f *procedureFixture) project(
	ctx context.Context,
	search memy.Search[ProcedureQuery],
	budget *memy.ProjectionBudget[ProcedureObservation, DocumentRef],
) (memy.ProjectedRecallResult[ProcedureObservation, DocumentRef], error) {
	return memy.RecallProjected(
		ctx,
		f.engine,
		"operator",
		f.scope,
		ProcedureQuery{Service: procedureService, MinimumSeverity: procedureMinimumSeverity},
		search,
		memy.ScoreRanker[ProcedureObservation, DocumentRef]{},
		f.options(),
		procedureProjector(),
		budget,
	)
}
func (f *procedureFixture) recordMatches(r memy.Record[ProcedureObservation, DocumentRef]) bool {
	receipt, ok := f.receipts[r.ID]
	return ok && r.Revision == receipt.Revision && reflect.DeepEqual(r.Payload, f.payloads[r.ID]) &&
		r.Scope == f.scope &&
		r.Valid.Known &&
		r.Valid.From.Equal(f.clock.Now()) &&
		len(r.Provenance.Sources) == 1 &&
		reflect.DeepEqual(r.Provenance.Sources[0], f.source) &&
		r.Provenance.Extractor == f.ports.ExtractorVersion &&
		len(r.Provenance.Lineage) == 0 &&
		slices.Equal(r.Provenance.Uncertainties, []string{procedureUncertainty}) &&
		slices.Equal(r.Provenance.Losses, []string{procedureFormattingLoss}) &&
		r.Provenance.Evidence == procedureEvidence &&
		r.ObservedAt.Equal(f.clock.Now()) &&
		r.RecordedAt.Equal(f.recordedAt[r.ID]) &&
		r.ExpiresAt.Equal(f.clock.Now().Add(procedureRetentionHours*time.Hour))
}
func (f *procedureFixture) projectionMatches(p memy.Projection[ProcedureObservation, DocumentRef]) bool {
	r, ok := f.receipts[p.RecordID]
	return ok && p.Revision == r.Revision && reflect.DeepEqual(p.Output, f.payloads[p.RecordID]) &&
		p.Scope == f.scope &&
		p.Trust == "data" &&
		len(p.Provenance.Sources) == 1 &&
		reflect.DeepEqual(p.Provenance.Sources[0], f.source) &&
		p.ExpiresAt.Equal(f.clock.Now().Add(procedureRetentionHours*time.Hour)) &&
		len(p.Provenance.Lineage) == 0 &&
		p.Provenance.Extractor == f.ports.ExtractorVersion &&
		slices.Equal(p.Provenance.Uncertainties, []string{procedureUncertainty}) &&
		slices.Equal(p.Provenance.Losses, []string{procedureFormattingLoss}) &&
		p.Provenance.Evidence == procedureEvidence
}

// Serialized body bytes include all projection/provenance and omission envelopes.
func procedureBodyBytes(body memy.ProjectedRecallResult[ProcedureObservation, DocumentRef]) (uint64, error) {
	raw, err := json.Marshal(body)
	return uint64(len(raw)), err
}

func procedurePlans() []CasePlan {
	var plans []CasePlan
	for _, entry := range []struct{ id, group string }{{procedureRetrievalCase, "retrieval"}, {procedureCrossScopeCase, "retrieval"}, {procedureAbstentionCase, groupAbstention}, {procedureGuardedCase, "poisoning"}, {procedurePermissiveCase, "poisoning"}, {procedureForgetMemoryCase, groupForget}, {procedureForgetSQLiteCase, groupForget}, {procedureFailuresCase, groupAbstention}} {
		id := entry.id
		plans = append(
			plans,
			CasePlan{
				ID:             id,
				Domain:         procedureDomain,
				Version:        "procedure-fixtures/v2",
				Groups:         []string{entry.group},
				Versions:       procedureCaseVersions(id, DefaultProcedurePorts()),
				Required:       procedureRequired(id),
				OptionalStages: procedureOptional(id),
				Run: func(ctx context.Context, c Corpus, run CaseRun) (ScenarioReport, error) {
					return RunProcedureCase(ctx, id, c, run, DefaultProcedurePorts())
				},
			},
		)
	}
	return plans
}
func procedureCheck(id string, ok bool, expected string, count int) Check {
	status := Pass
	observed := expected
	if !ok {
		status = Fail
		observed = "mismatch"
	}
	return Check{
		ID:        id,
		Mandatory: true,
		Status:    status,
		Expected:  expected,
		Observed:  observed,
		Evidence:  []Evidence{{Code: id, Count: count, Aliases: nil}},
	}
}
func procedureReport(id string, run CaseRun, ports ProcedurePorts) ScenarioReport {
	return ScenarioReport{
		ID:         id,
		Domain:     procedureDomain,
		Version:    "procedure-fixtures/v2",
		Repeat:     run.Repeat,
		Seed:       run.Seed,
		Variation:  "seeded-insertion-order",
		Versions:   procedureCaseVersions(id, ports),
		Mode:       ports.Mode,
		Candidate:  StageResult{Status: NotApplicable, Checks: nil, Version: ""},
		HostReview: StageResult{Status: NotApplicable, Checks: nil, Version: ""},
		Effective:  StageResult{Version: ports.SearchVersion, Status: Unknown, Checks: nil},
		Canonical:  StageResult{Version: procedureRetentionVersion, Status: Unknown, Checks: nil},
		Rendered:   StageResult{Version: procedureProjectorVersion, Status: Unknown, Checks: nil},
		Execution:  StageResult{Version: ports.ExtractorVersion, Status: Unknown, Checks: nil},
		Metrics: Metrics{
			CanonicalBytes:   UnavailableMeasurement("serialized-canonical-bytes", "not_measured"),
			RealProviderCost: UnavailableMeasurement("provider-tokens-currency", fixtureModeScripted),
			PayloadBytes: Measurement{
				Status: "",
				Unit:   "",
				Value:  nil,
				Reason: "",
			},
			ContextJSONBytes: Measurement{Status: "", Unit: "", Value: nil, Reason: ""},
			ProviderCost:     Measurement{Status: "", Unit: "", Value: nil, Reason: ""},
		},
		Final: VerdictUnknown, Diagnostic: "", Probes: nil}
}

// RunProcedureCase executes the same typed oracles with consumer-supplied ports.
func RunProcedureCase(
	ctx context.Context,
	id string,
	c Corpus,
	run CaseRun,
	ports ProcedurePorts,
) (ScenarioReport, error) {
	report := procedureReport(id, run, ports)
	if ports.Extractor == nil || ports.Search == nil || ports.Mode == "" || ports.ExtractorVersion == "" ||
		ports.SearchVersion == "" ||
		ports.GraderVersion == "" {
		return report, memy.ErrInvalid
	}
	storage := "memory"
	if id == procedureForgetSQLiteCase {
		storage = "sqlite"
	}
	f, err := newProcedureFixture(ctx, storage, ports)
	if err != nil {
		return report, err
	}
	report, runErr := f.runCase(ctx, id, c, run, report)
	return report, errors.Join(runErr, f.close())
}

func (f *procedureFixture) runCase(
	ctx context.Context,
	id string,
	c Corpus,
	run CaseRun,
	report ScenarioReport,
) (ScenarioReport, error) {
	var err error
	ports := f.ports
	if c.Budgets.MaxCandidates < 1 || c.Budgets.RecallLimit < 1 || c.Budgets.ContextBytes < 1 ||
		c.Budgets.CostUnits < 1 {
		return report, memy.ErrInvalid
	}
	f.candidateLimit = min(procedureRetrievalLimit, c.Budgets.MaxCandidates)
	f.recallLimit = min(procedureRetrievalLimit, c.Budgets.RecallLimit)
	f.costLimit = uint64(c.Budgets.CostUnits)
	if err = f.seed(ctx, run.Seed); err != nil {
		return report, err
	}
	if id == procedureCrossScopeCase || id == procedureForgetMemoryCase || id == procedureForgetSQLiteCase {
		if err = f.seedForeign(ctx); err != nil {
			return report, err
		}
	}
	search := ports.Search(f.scope, f.candidates())
	if search == nil {
		return report, memy.ErrInvalid
	}
	switch id {
	case procedureCrossScopeCase:
		err = f.crossScope(ctx, &report)
	case procedureRetrievalCase:
		err = f.retrieval(ctx, c, &report, search)
	case procedureAbstentionCase:
		err = f.abstention(ctx, &report, search)
	case procedureGuardedCase, procedurePermissiveCase:
		err = f.poisoning(ctx, &report, id == procedureGuardedCase)
	case procedureForgetMemoryCase, procedureForgetSQLiteCase:
		err = f.forget(ctx, &report)
	case procedureFailuresCase:
		err = f.failures(ctx, &report)
	default:
		return report, memy.ErrInvalid
	}
	if err != nil {
		return report, err
	}
	report.Execution.Checks = append(
		report.Execution.Checks,
		procedureCheck("procedure-completed", true, "completed", 1),
		procedureCheck(
			"configured-boundaries",
			f.candidateLimit <= c.Budgets.MaxCandidates && f.recallLimit <= c.Budgets.RecallLimit &&
				f.providerCalls <= uint64(c.Budgets.CostUnits),
			"within-configured-candidate-recall-call-caps",
			f.candidateLimit,
		),
	)
	if ports.Mode == fixtureModeScripted {
		report.Metrics.ProviderCost = KnownMeasurement("scripted-call-unit", f.providerCalls)
	} else {
		report.Metrics.ProviderCost = UnavailableMeasurement("provider-cost-unit", "external-not-measured")
	}
	report.Metrics.RealProviderCost = UnavailableMeasurement("provider-tokens-currency", "provider-cost-not-measured")
	return report, nil
}
func (f *procedureFixture) canonical(ctx context.Context, report *ScenarioReport, expected int) error {
	records, err := fullSnapshot(
		ctx,
		f.engine,
		"operator",
		f.scope,
		memy.ReadOptions{
			Purpose:          qualityPurpose,
			IncludeConflicts: true,
			ValidAsOf:        time.Time{},
			RecordedAsOf:     time.Time{},
			IncludeUnknown:   false,
		},
	)
	if err != nil {
		return err
	}
	ok := len(records) == expected
	for _, r := range records {
		ok = ok && f.recordMatches(r)
	}
	report.Canonical.Checks = append(
		report.Canonical.Checks,
		procedureCheck("canonical-state", ok, "exact-originals-source-retention", len(records)),
	)
	provenanceOK := len(records) == expected
	for _, record := range records {
		provenanceOK = provenanceOK && len(record.Provenance.Lineage) == 0 && len(record.Provenance.Sources) == 1 &&
			reflect.DeepEqual(record.Provenance.Sources[0], f.source) &&
			record.Provenance.Extractor == f.ports.ExtractorVersion &&
			record.Valid.Known &&
			record.ExpiresAt.Equal(f.clock.Now().Add(procedureRetentionHours*time.Hour))
	}
	report.Canonical.Checks = append(
		report.Canonical.Checks,
		procedureCheck("canonical-provenance", provenanceOK, "exact-sources-lineage-validity-expiry", len(records)),
	)
	return nil
}

func (f *procedureFixture) retrieval(
	ctx context.Context,
	c Corpus,
	r *ScenarioReport,
	search memy.Search[ProcedureQuery],
) error {
	recall, err := f.recall(
		ctx,
		search,
		ProcedureQuery{Service: procedureService, MinimumSeverity: procedureMinimumSeverity},
	)
	if err != nil {
		return err
	}
	ok := len(recall.Records) == 2 && recall.Progress.CanonicalFiltered >= 1 && recall.Progress.CandidatesTruncated
	for _, item := range recall.Records {
		ok = ok && f.recordMatches(item.Record) && item.Record.ID != procedureDistractor && len(item.Signals) == 2
	}
	r.Effective.Checks = append(
		r.Effective.Checks,
		procedureCheck("effective-exact", ok, "two-relevant-exact-revisions", len(recall.Records)),
	)
	all, err := f.project(ctx, search, nil)
	if err != nil {
		return err
	}
	if len(all.Projections) != 2 {
		return memy.ErrInvalid
	}
	target := all
	target.Projections = all.Projections[:1]
	target.Omissions = []memy.BudgetOmission{
		{
			Ref:    memy.RevisionRef{RecordID: all.Projections[1].RecordID, Revision: all.Projections[1].Revision},
			Reason: memy.OmittedBudget,
		},
	}
	packing := reference.JSONPacking[ProcedureObservation, DocumentRef]{RejectOversized: false}
	limit, err := packing.Measure(ctx, target)
	if err != nil {
		return err
	}
	if c.Budgets.ContextBytes < limit {
		limit = c.Budgets.ContextBytes
	}
	packed, err := f.project(
		ctx,
		search,
		&memy.ProjectionBudget[ProcedureObservation, DocumentRef]{
			Max:    limit,
			Codec:  memy.JSONCodec[ProcedureObservation]{},
			Policy: packing,
		},
	)
	if err != nil {
		return err
	}
	bytes, err := procedureBodyBytes(packed)
	if err != nil {
		return err
	}
	ok = f.packedMatches(packed, all, bytes, limit)
	grade, gradeErr := f.gradeProjections(ctx, packed.Projections)
	if gradeErr != nil {
		return gradeErr
	}
	ok = ok && grade
	r.Rendered.Checks = append(
		r.Rendered.Checks,
		procedureCheck("rendered-context", ok, "one-packed-exact-data-projection", len(packed.Projections)),
	)
	var payloadBytes uint64
	for _, projection := range packed.Projections {
		raw, marshalErr := json.Marshal(projection.Output)
		if marshalErr != nil {
			return marshalErr
		}
		payloadBytes += uint64(len(raw))
	}
	r.Metrics.PayloadBytes = KnownMeasurement("payload-json-bytes", payloadBytes)
	r.Metrics.ContextJSONBytes = KnownMeasurement("final-body-json-bytes", bytes)
	return f.checkCurrentRetention(ctx, r)
}

func (f *procedureFixture) checkCurrentRetention(ctx context.Context, r *ScenarioReport) error {
	if err := f.canonical(ctx, r, procedureOriginalCount); err != nil {
		return err
	}
	f.clock.Advance(procedureExpiredHours * time.Hour)
	_, expiryErr := f.engine.Get(
		ctx,
		"operator",
		f.scope,
		procedureRelevantA,
		procedureReadOptions(),
	)
	r.Canonical.Checks = append(
		r.Canonical.Checks,
		procedureCheck("current-retention", errors.Is(expiryErr, memy.ErrNotFound), "expired-current-read-denied", 1),
	)
	return nil
}

func (f *procedureFixture) abstention(
	ctx context.Context,
	r *ScenarioReport,
	search memy.Search[ProcedureQuery],
) error {
	empty, err := f.recall(ctx, search, ProcedureQuery{Service: "no-match", MinimumSeverity: procedureMinimumSeverity})
	if err != nil {
		return err
	}
	outage, err := f.recall(
		ctx,
		procedureSearch{"procedure-outage/v1", f.scope, nil, true},
		ProcedureQuery{Service: procedureService, MinimumSeverity: procedureMinimumSeverity},
	)
	outageOK := errors.Is(err, memy.ErrUnavailable) && len(outage.Records) == 0
	original := f.receipts[procedureRelevantA]
	p := procedurePayload(procedureConflictID)
	if err = f.commit(
		ctx,
		procedureConflictID,
		p,
		memy.Conflict,
		[]memy.RevisionRef{{RecordID: original.RecordID, Revision: original.Revision}},
	); err != nil {
		return err
	}
	conflicted, conflictErr := f.engine.Get(
		ctx,
		"operator",
		f.scope,
		procedureRelevantA,
		memy.ReadOptions{
			Purpose:          qualityPurpose,
			IncludeConflicts: true,
			ValidAsOf:        time.Time{},
			RecordedAsOf:     time.Time{},
			IncludeUnknown:   false,
		},
	)
	r.Effective.Checks = append(
		r.Effective.Checks,
		procedureCheck(
			"effective-exact",
			len(empty.Records) == 0 && outageOK && conflictErr == nil && conflicted.State == memy.Conflicted,
			"healthy-empty-conflict-unavailable-distinct",
			procedureOriginalCount,
		),
	)
	// Projected bodies are separate observations, not conclusions inferred from Recall.
	emptyBody, emptyBodyErr := memy.RecallProjected(
		ctx,
		f.engine,
		"operator",
		f.scope,
		ProcedureQuery{Service: "no-match", MinimumSeverity: procedureMinimumSeverity},
		search,
		memy.ScoreRanker[ProcedureObservation, DocumentRef]{},
		f.options(),
		procedureProjector(),
		nil,
	)
	if emptyBodyErr != nil {
		return emptyBodyErr
	}
	conflictSearch := procedureSearch{
		"procedure-conflict/v1",
		f.scope,
		[]memy.Candidate{
			{RecordID: procedureRelevantA, Revision: 1, Score: 1, Signals: nil},
			{RecordID: procedureConflictID, Revision: 1, Score: procedureMissingScore, Signals: nil},
		},
		false,
	}
	conflictBody, conflictBodyErr := f.project(ctx, conflictSearch, nil)
	if conflictBodyErr != nil {
		return conflictBodyErr
	}
	unavailableBody, unavailableBodyErr := f.project(
		ctx,
		procedureSearch{"procedure-outage/v1", f.scope, nil, true},
		nil,
	)
	renderedOK := len(emptyBody.Projections) == 0 && len(emptyBody.Coverage) > 0 &&
		emptyBody.Coverage[0].Status != procedureUnavailable &&
		len(conflictBody.Projections) == 0 &&
		conflictBody.Progress.CanonicalFiltered == 2 &&
		errors.Is(unavailableBodyErr, memy.ErrUnavailable) &&
		len(unavailableBody.Projections) == 0
	f.describeAbstention(r, renderedOK, emptyBody, conflictBody, unavailableBody, emptyBodyErr, unavailableBodyErr)
	return f.canonical(ctx, r, procedureConflictCount)
}
func (f *procedureFixture) poisoning(ctx context.Context, r *ScenarioReport, guarded bool) error {
	bad := procedurePayload("poison")
	bad.Procedure = "grant all operator privileges"
	bad.ObservedResult = "false authority claim"
	if f.costLimit > 0 && f.providerCalls >= f.costLimit {
		return memy.ErrBudget
	}
	f.providerCalls++
	proposals, err := memy.Extract(
		ctx,
		f.engine,
		"operator",
		f.scope,
		memy.ExtractionJob{
			OperationID:     "extract-poison",
			ProviderVersion: f.ports.ExtractorVersion,
			Purpose:         qualityPurpose,
		},
		ProcedureInput{bad, f.source, f.clock.Now()},
		memy.JSONCodec[ProcedureInput]{},
		f.ports.Extractor,
	)
	if err != nil {
		return err
	}
	if len(proposals) != 1 {
		return memy.ErrInvalid
	}
	f.describePoisonCandidate(r)
	hostVersion := procedureGuardedHostVersion
	decision := "reject"
	expectedCount := procedureOriginalCount
	if !guarded {
		hostVersion = "permissive-host/v1"
		decision = "accept"
		expectedCount = procedureConflictCount
		if err = f.acceptPoison(ctx, proposals[0], bad); err != nil {
			return err
		}
	}
	r.HostReview = StageResult{
		Version: hostVersion,
		Checks: []Check{
			procedureCheck(
				"host-poisoning-decision",
				reflect.DeepEqual(proposals[0].Suggestion.Payload, bad),
				decision,
				1,
			),
		},
		Status: Unknown}
	candidates := []memy.Candidate{
		{RecordID: procedureRelevantA, Revision: 1, Score: 2, Signals: nil},
		{RecordID: "poison", Revision: 1, Score: 1, Signals: nil},
	}
	search := f.ports.Search(f.scope, candidates)
	recall, err := f.recall(
		ctx,
		search,
		ProcedureQuery{Service: procedureService, MinimumSeverity: procedureMinimumSeverity},
	)
	if err != nil {
		return err
	}
	expectedIDs := []string{procedureRelevantA}
	if !guarded {
		expectedIDs = append(expectedIDs, "poison")
	}
	ok := f.recordSetMatches(recall.Records, expectedIDs)
	for _, item := range recall.Records {
		ok = ok && f.recordMatches(item.Record)
	}
	r.Effective.Checks = append(
		r.Effective.Checks,
		procedureCheck("effective-exact", ok, decision+"-baseline-and-exact-data", len(recall.Records)),
	)
	if err = f.verifyPoisonRender(ctx, r, search, expectedIDs, guarded); err != nil {
		return err
	}
	return f.canonical(ctx, r, expectedCount)
}
func (f *procedureFixture) forget(ctx context.Context, r *ScenarioReport) error {
	ref := memy.RevisionRef{RecordID: procedureRelevantA, Revision: f.receipts[procedureRelevantA].Revision}
	fence, err := f.engine.Fence(ctx, "operator", f.scope, qualityPurpose)
	if err != nil {
		return err
	}
	err = f.engine.WithDerivedWrite(
		ctx,
		"operator",
		fence,
		qualityPurpose,
		[]memy.RevisionRef{ref},
		func(ctx context.Context) error {
			if stageErr := f.index.Stage(
				ctx,
				f.scope,
				memy.Candidate{RecordID: ref.RecordID, Revision: ref.Revision, Score: 1, Signals: nil},
			); stageErr != nil {
				return stageErr
			}
			return f.sink.Put(ctx, "synthetic-context", f.scope, []memy.RevisionRef{ref}, []byte("synthetic context"))
		},
	)
	if err != nil {
		return err
	}
	if err = f.index.Acknowledge(ctx, f.receipts[procedureRelevantA].Visibility); err != nil {
		return err
	}
	f.sink.FailPurge(memy.ErrUnavailable)
	request := memy.ForgetRequest{
		OperationID:   "forget-procedure",
		Selector:      memy.Selector{Kind: memy.SelectRecord, ID: ref.RecordID},
		Expected:      []memy.RevisionRef{ref},
		Reason:        "synthetic removal",
		PolicyVersion: "procedure-forget/v1",
		Limit:         procedurePurgeLimit,
		MaxBytes:      procedurePurgeByteCap,
	}
	pending, err := fullForget(ctx, f.engine, "operator", f.scope, qualityPurpose, request)
	if err != nil {
		return err
	}
	pendingOK := pending.State == memy.PurgePending && f.sink.Contains("synthetic-context")
	staleSearch := procedureSearch{
		"procedure-stale/v1",
		f.scope,
		[]memy.Candidate{{RecordID: ref.RecordID, Revision: ref.Revision, Score: 1, Signals: nil}},
		false,
	}
	body, pendingReadErr := f.project(ctx, staleSearch, nil)
	// Pending deletion is a known fail-closed result, not healthy search absence.
	pendingOK = pendingOK && errors.Is(pendingReadErr, memy.ErrRevoked) && len(body.Projections) == 0
	called := false
	lateErr := f.engine.WithDerivedWrite(
		ctx,
		"operator",
		fence,
		qualityPurpose,
		[]memy.RevisionRef{ref},
		func(context.Context) error { called = true; return nil },
	)
	lateOK := errors.Is(lateErr, memy.ErrStaleInput) && !called
	f.sink.FailPurge(nil)
	done, err := fullForget(ctx, f.engine, "operator", f.scope, qualityPurpose, request)
	if err != nil {
		return err
	}
	retryOK := done.State == memy.PurgeComplete && done.Batch.OperationID == pending.Batch.OperationID &&
		done.Batch.Epoch == pending.Batch.Epoch &&
		!f.sink.Contains("synthetic-context")
	indexed, err := f.recall(
		ctx,
		f.index,
		ProcedureQuery{Service: procedureService, MinimumSeverity: procedureMinimumSeverity},
	)
	if err != nil {
		return err
	}
	retryOK = retryOK && len(indexed.Records) == 0
	return f.verifyForget(ctx, r, ref, staleSearch, pendingOK, retryOK, lateOK)
}

func (f *procedureFixture) verifyForget(
	ctx context.Context,
	r *ScenarioReport,
	ref memy.RevisionRef,
	staleSearch procedureSearch,
	pendingOK, retryOK, lateOK bool,
) error {
	var err error
	// A current source revision change and removal independently forbid non-revoked records too.
	source2 := f.source
	source2.Revision = "r2"
	if err = f.sources.Put(f.scope, source2); err != nil {
		return err
	}
	_, changedErr := f.engine.Get(
		ctx,
		"operator",
		f.scope,
		"relevant-b",
		procedureReadOptions(),
	)
	f.sources.Remove(f.scope, f.source.ID)
	_, removedErr := f.engine.Get(
		ctx,
		"operator",
		f.scope,
		"relevant-b",
		procedureReadOptions(),
	)
	// Durable source revocation survives host registry restoration.
	sourceReceipt, sourceForgetErr := fullForget(
		ctx,
		f.engine,
		"operator",
		f.scope,
		qualityPurpose,
		memy.ForgetRequest{
			OperationID:   "forget-source-procedure",
			Selector:      memy.Selector{Kind: memy.SelectSource, ID: f.source.ID},
			Reason:        "synthetic source revocation",
			PolicyVersion: "procedure-forget/v1",
			Limit:         procedurePurgeLimit,
			MaxBytes:      procedurePurgeByteCap,
			Expected:      nil},
	)
	if sourceForgetErr != nil {
		return sourceForgetErr
	}
	restoredOK, restoreErr := f.verifySourceRestoration(ctx)
	if restoreErr != nil {
		return restoreErr
	}
	foreignPreserved, foreignErr := f.foreignPreserved(ctx)
	if foreignErr != nil {
		return foreignErr
	}
	sourceRevokedOK := sourceReceipt.State == memy.PurgeComplete && restoredOK
	finalBody, err := f.project(ctx, staleSearch, nil)
	if err != nil {
		return err
	}
	r.Effective.Checks = append(
		r.Effective.Checks,
		procedureCheck(
			"effective-exact",
			pendingOK && retryOK && lateOK,
			"revoked-context-absent-pending-and-retry",
			len(finalBody.Projections),
		),
		procedureCheck("pending-purge-denial", pendingOK, "pending-revocation-denied", 0),
		procedureCheck("managed-purge-retry", retryOK, "same-operation-purge-complete", 0),
	)
	r.Rendered.Checks = append(
		r.Rendered.Checks,
		procedureCheck(
			"rendered-context",
			len(finalBody.Projections) == 0 && finalBody.Progress.CanonicalFiltered == 1,
			"forbidden-rendered-context-absent",
			0,
		),
	)
	// Source checks are errors, not snapshots misread as healthy empty. Current purge retains a tombstone.
	_, revokedErr := f.currentRecord(ctx, ref.RecordID)
	r.Canonical.Checks = append(
		r.Canonical.Checks,
		procedureCheck(
			"canonical-state",
			errors.Is(revokedErr, memy.ErrNotFound),
			"canonical-revoked-identity-denied",
			1,
		),
		procedureCheck(
			"source-revision-change",
			errors.Is(changedErr, memy.ErrSourceUnavailable),
			"current-source-revision-denied",
			1,
		),
		procedureCheck("source-removal", errors.Is(removedErr, memy.ErrSourceUnavailable), "removed-source-denied", 1),
		procedureCheck("late-derived-fence", lateOK, "late-write-stale-before-callback", 0),
		procedureCheck("durable-source-revocation", sourceRevokedOK, "registry-restoration-cannot-unrevoke-source", 2),
		procedureCheck("foreign-after-forget", foreignPreserved, "foreign-canonical-preserved", 1),
	)
	return nil
}
func (f *procedureFixture) failures(ctx context.Context, r *ScenarioReport) error {
	original := f.ports.Extractor
	f.ports.Extractor = reference.ExtractorFunc[ProcedureInput, ProcedureObservation, DocumentRef](
		func(context.Context, ProcedureInput) ([]memy.Suggestion[ProcedureObservation, DocumentRef], error) {
			return nil, memy.ErrUnavailable
		},
	)
	providerErr := f.commit(ctx, "provider-failure", procedurePayload("failure"), memy.Append, nil)
	f.ports.Extractor = original
	_, searchErr := f.recall(
		ctx,
		procedureSearch{"procedure-failing/v1", f.scope, nil, true},
		ProcedureQuery{Service: procedureService, MinimumSeverity: procedureMinimumSeverity},
	)
	f.authority.Fail(memy.ErrUnavailable)
	_, policyErr := f.engine.Get(
		ctx,
		"operator",
		f.scope,
		procedureRelevantA,
		procedureReadOptions(),
	)
	f.authority.Fail(nil)
	search := f.ports.Search(
		f.scope,
		[]memy.Candidate{{RecordID: procedureRelevantA, Revision: 1, Score: 1, Signals: nil}},
	)
	body, err := f.project(ctx, search, nil)
	if err != nil {
		return err
	}
	failingGrader := procedureControlledGrader{}
	_, graderErr := failingGrader.Grade(ctx, body.Projections)
	r.Execution.Checks = append(
		r.Execution.Checks,
		procedureCheck(
			"controlled-grader-executed",
			r.Versions.Grader == failingGrader.Version() && errors.Is(graderErr, memy.ErrUnavailable),
			"controlled-grader-unavailable",
			1,
		),
	)
	known := errors.Is(providerErr, memy.ErrUnavailable) && errors.Is(searchErr, memy.ErrUnavailable) &&
		errors.Is(policyErr, memy.ErrUnavailable) &&
		errors.Is(graderErr, memy.ErrUnavailable)
	r.Execution.Checks = append(
		r.Execution.Checks,
		procedureCheck(
			"known-controlled-port-errors",
			known,
			"four-controlled-unavailable-errors",
			procedureConflictCount,
		),
	)
	r.HostReview = StageResult{
		Version: procedureGuardedHostVersion,
		Checks: []Check{
			procedureCheck("error-not-rejection", known, "errors-are-unknown-not-rejection", procedureConflictCount),
		},
		Status: Unknown,
	}
	recall, err := f.recall(
		ctx,
		search,
		ProcedureQuery{Service: procedureService, MinimumSeverity: procedureMinimumSeverity},
	)
	if err != nil {
		return err
	}
	ok := len(recall.Records) == 1 && f.recordMatches(recall.Records[0].Record)
	r.Effective.Checks = append(
		r.Effective.Checks,
		procedureCheck("effective-exact", ok, "baseline-retained-after-errors", len(recall.Records)),
	)
	r.Rendered.Checks = append(
		r.Rendered.Checks,
		procedureCheck(
			"rendered-context",
			len(body.Projections) == 1 && f.projectionMatches(body.Projections[0]),
			"baseline-rendered-after-errors",
			len(body.Projections),
		),
	)
	return f.canonical(ctx, r, procedureOriginalCount)
}

func procedureVersions(p ProcedurePorts) PortVersions {
	return PortVersions{
		Provider:      p.ExtractorVersion,
		Model:         p.Mode,
		HostReview:    "procedure-host/v1",
		Retention:     procedureRetentionVersion,
		Resolver:      procedureResolverVersion,
		Consolidation: portNone,
		Search:        p.SearchVersion,
		Projector:     procedureProjectorVersion,
		Packing:       jsonPackingVersion,
		Grader:        p.GraderVersion,
	}
}
func procedureOptional(id string) []string {
	if id == procedureGuardedCase || id == procedurePermissiveCase {
		return nil
	}
	if id == procedureFailuresCase {
		return []string{stageCandidate}
	}
	return []string{stageCandidate, stageHostReview}
}

func procedureCaseVersions(id string, p ProcedurePorts) PortVersions {
	v := procedureVersions(p)
	switch id {
	case procedureGuardedCase:
		v.HostReview = procedureGuardedHostVersion
	case procedureFailuresCase:
		v.HostReview = procedureGuardedHostVersion
		v.Grader = (procedureControlledGrader{}).Version()
	case procedurePermissiveCase:
		v.HostReview = "permissive-host/v1"
	default:
		v.HostReview = portNone
	}
	return v
}
func procedureRequired(id string) []RequiredCheck {
	checks := []RequiredCheck{
		{Stage: stageEffective, ID: "effective-exact"},
		{Stage: stageCanonical, ID: "canonical-state"},
		{Stage: stageRendered, ID: "rendered-context"},
		{Stage: stageExecution, ID: "procedure-completed"},
		{Stage: stageExecution, ID: "configured-boundaries"},
	}
	if id != procedureForgetMemoryCase && id != procedureForgetSQLiteCase {
		checks = append(checks, RequiredCheck{Stage: stageCanonical, ID: "canonical-provenance"})
	}
	switch id {
	case procedureRetrievalCase:
		checks = append(checks, RequiredCheck{Stage: stageCanonical, ID: "current-retention"})
	case procedureGuardedCase, procedurePermissiveCase:
		checks = append(checks, RequiredCheck{Stage: stageHostReview, ID: "host-poisoning-decision"})
	case procedureForgetMemoryCase, procedureForgetSQLiteCase:
		checks = append(
			checks,
			RequiredCheck{Stage: stageEffective, ID: "pending-purge-denial"},
			RequiredCheck{Stage: stageEffective, ID: "managed-purge-retry"},
			RequiredCheck{Stage: stageCanonical, ID: "source-revision-change"},
			RequiredCheck{Stage: stageCanonical, ID: "source-removal"},
			RequiredCheck{Stage: stageCanonical, ID: "late-derived-fence"},
			RequiredCheck{Stage: stageCanonical, ID: "durable-source-revocation"},
			RequiredCheck{Stage: stageCanonical, ID: "foreign-after-forget"},
		)
	case procedureCrossScopeCase:
		checks = append(
			checks,
			RequiredCheck{Stage: stageCanonical, ID: "cross-scope-read-denied"},
			RequiredCheck{Stage: stageCanonical, ID: "foreign-state-preserved"},
		)
	case procedureAbstentionCase:
		checks = append(
			checks,
			RequiredCheck{Stage: stageRendered, ID: "projected-healthy-empty"},
			RequiredCheck{Stage: stageRendered, ID: "projected-conflict"},
			RequiredCheck{Stage: stageRendered, ID: "projected-unavailable"},
		)
	case procedureFailuresCase:
		checks = append(
			checks,
			RequiredCheck{Stage: stageHostReview, ID: "error-not-rejection"},
			RequiredCheck{Stage: stageExecution, ID: "known-controlled-port-errors"},
			RequiredCheck{Stage: stageExecution, ID: "controlled-grader-executed"},
		)
	}
	return checks
}

type procedureControlledGrader struct{}

func (procedureControlledGrader) Version() string { return "controlled-grader/v1" }

func (procedureControlledGrader) Grade(
	ctx context.Context,
	_ []memy.Projection[ProcedureObservation, DocumentRef],
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return false, memy.ErrUnavailable
}

func (f *procedureFixture) projectionSetMatches(
	actual []memy.Projection[ProcedureObservation, DocumentRef],
	ids []string,
) bool {
	if len(actual) != len(ids) {
		return false
	}
	expected := map[memy.RevisionRef]bool{}
	for _, id := range ids {
		receipt, ok := f.receipts[id]
		if !ok {
			return false
		}
		expected[memy.RevisionRef{RecordID: id, Revision: receipt.Revision}] = true
	}
	for _, projection := range actual {
		ref := memy.RevisionRef{RecordID: projection.RecordID, Revision: projection.Revision}
		if !expected[ref] || !f.projectionMatches(projection) {
			return false
		}
		delete(expected, ref)
	}
	return len(expected) == 0
}

func (f *procedureFixture) seedForeign(ctx context.Context) error {
	f.foreignScope = memy.Scope{Tenant: "synthetic-foreign", Namespace: f.scope.Namespace, Subject: f.scope.Subject}
	f.authority.Grant(
		"foreign-operator",
		f.foreignScope,
		"foreign-authority/v1",
		memy.ActionPropose,
		memy.ActionAccept,
		memy.ActionCommit,
		memy.ActionRead,
	)
	f.foreignSource = memy.Source[DocumentRef]{
		ID:        f.source.ID,
		Revision:  "foreign-r1",
		Reference: DocumentRef{Document: procedureForeignDocumentID, Section: "foreign-private"},
	}
	if err := f.sources.Put(f.foreignScope, f.foreignSource); err != nil {
		return err
	}
	f.foreignPayload = ProcedureObservation{
		Service:        procedureService,
		Procedure:      "synthetic-private-foreign-observation",
		ObservedResult: "synthetic-private-foreign-result",
		Preconditions:  []string{"foreign-access-only"},
	}
	proposal, err := f.engine.Remember(
		ctx,
		"foreign-operator",
		f.foreignScope,
		"foreign-proposal",
		qualityPurpose,
		memy.Suggestion[ProcedureObservation, DocumentRef]{
			Payload:    f.foreignPayload,
			Sources:    []memy.Source[DocumentRef]{f.foreignSource},
			Extractor:  "foreign-manual/v1",
			Evidence:   "synthetic foreign observation",
			ObservedAt: f.clock.Now(),
			Valid:      memy.Interval{Known: true, From: f.clock.Now(), To: time.Time{}},
			ExpiresAt:  time.Time{}, Lineage: nil, Losses: nil, Uncertainties: nil},
	)
	if err != nil {
		return err
	}
	accepted, err := f.engine.Accept(
		ctx,
		"foreign-operator",
		f.foreignScope,
		proposal.ID,
		proposal.Digest,
		proposal.Revision,
		qualityPurpose,
	)
	if err != nil {
		return err
	}
	f.foreignReceipt, err = f.engine.Commit(
		ctx,
		"foreign-operator",
		f.foreignScope,
		qualityPurpose,
		memy.CommitRequest{
			OperationID: "foreign-commit",
			ProposalID:  proposal.ID,
			Acceptance:  accepted,
			RecordID:    "foreign-observation",
			Reconcile: memy.Reconciliation{
				Mode:          memy.Append,
				PolicyVersion: "foreign-resolver/v1",
				Basis:         "synthetic foreign baseline",
				Related:       nil},
			Expected: 0},
	)
	return err
}
func (f *procedureFixture) foreignPreserved(ctx context.Context) (bool, error) {
	records, err := fullSnapshot(
		ctx,
		f.engine,
		"foreign-operator",
		f.foreignScope,
		procedureReadOptions(),
	)
	if err != nil {
		return false, err
	}
	if len(records) != 1 {
		return false, nil
	}
	record := records[0]
	return record.ID == f.foreignReceipt.RecordID && record.Revision == f.foreignReceipt.Revision &&
		record.Scope == f.foreignScope &&
		reflect.DeepEqual(record.Payload, f.foreignPayload) &&
		record.State == memy.Active &&
		len(record.Provenance.Sources) == 1 &&
		reflect.DeepEqual(record.Provenance.Sources[0], f.foreignSource) &&
		len(record.Provenance.Lineage) == 0 &&
		record.Provenance.Extractor == "foreign-manual/v1" &&
		record.Valid.Known &&
		record.Valid.From.Equal(f.clock.Now()) &&
		record.RecordedAt.Equal(f.clock.Now()) &&
		record.ExpiresAt.Equal(f.clock.Now().Add(procedureRetentionHours*time.Hour)), nil
}
func (f *procedureFixture) crossScope(ctx context.Context, r *ScenarioReport) error {
	_, foreignGetErr := f.engine.Get(
		ctx,
		"operator",
		f.foreignScope,
		f.foreignReceipt.RecordID,
		procedureReadOptions(),
	)
	_, foreignSnapshotErr := fullSnapshot(
		ctx,
		f.engine,
		"operator",
		f.foreignScope,
		procedureReadOptions(),
	)
	_, missingInAErr := f.engine.Get(
		ctx,
		"operator",
		f.scope,
		f.foreignReceipt.RecordID,
		procedureReadOptions(),
	)
	r.Canonical.Checks = append(
		r.Canonical.Checks,
		procedureCheck(
			"cross-scope-read-denied",
			errors.Is(foreignGetErr, memy.ErrUnauthorized) && errors.Is(foreignSnapshotErr, memy.ErrUnauthorized) &&
				errors.Is(missingInAErr, memy.ErrNotFound),
			"foreign-get-snapshot-denied",
			procedureOriginalCount,
		),
	)
	preserved, err := f.foreignPreserved(ctx)
	if err != nil {
		return err
	}
	r.Canonical.Checks = append(
		r.Canonical.Checks,
		procedureCheck("foreign-state-preserved", preserved, "foreign-exact-original-preserved", 1),
	)
	search := f.ports.Search(
		f.scope,
		[]memy.Candidate{
			{RecordID: "foreign-observation", Revision: 1, Score: procedureSecondaryScore, Signals: nil},
			{RecordID: procedureRelevantA, Revision: 1, Score: 1, Signals: nil},
		},
	)
	actual, err := f.recall(
		ctx,
		search,
		ProcedureQuery{Service: procedureService, MinimumSeverity: procedureMinimumSeverity},
	)
	if err != nil {
		return err
	}
	ok := len(actual.Records) == 1 && actual.Progress.CanonicalFiltered == 1
	if len(actual.Records) == 1 {
		ok = ok && actual.Records[0].Record.ID == procedureRelevantA && f.recordMatches(actual.Records[0].Record)
	}
	r.Effective.Checks = append(
		r.Effective.Checks,
		procedureCheck("effective-exact", ok, "foreign-candidate-filtered-one-authorized-ref", len(actual.Records)),
	)
	body, err := f.project(ctx, search, nil)
	if err != nil {
		return err
	}
	r.Rendered.Checks = append(
		r.Rendered.Checks,
		procedureCheck(
			"rendered-context",
			f.projectionSetMatches(body.Projections, []string{procedureRelevantA}) &&
				body.Progress.CanonicalFiltered == 1,
			"foreign-context-absent-exact-authorized-output",
			len(body.Projections),
		),
	)
	return f.canonical(ctx, r, procedureOriginalCount)
}

func (f *procedureFixture) recordSetMatches(
	actual []memy.Ranked[ProcedureObservation, DocumentRef],
	ids []string,
) bool {
	if len(actual) != len(ids) {
		return false
	}
	expected := map[memy.RevisionRef]bool{}
	for _, id := range ids {
		receipt, ok := f.receipts[id]
		if !ok {
			return false
		}
		expected[memy.RevisionRef{RecordID: id, Revision: receipt.Revision}] = true
	}
	for _, item := range actual {
		ref := memy.RevisionRef{RecordID: item.Record.ID, Revision: item.Record.Revision}
		if !expected[ref] || !f.recordMatches(item.Record) {
			return false
		}
		delete(expected, ref)
	}
	return len(expected) == 0
}

func (f *procedureFixture) gradeProjections(
	ctx context.Context,
	projections []memy.Projection[ProcedureObservation, DocumentRef],
) (bool, error) {
	if f.ports.Grade == nil {
		return true, nil
	}
	graderCodec := memy.JSONCodec[[]memy.Projection[ProcedureObservation, DocumentRef]]{}
	encoded, cloneErr := graderCodec.Encode(projections)
	if cloneErr != nil {
		return false, cloneErr
	}
	graderInput, cloneErr := graderCodec.Decode(encoded)
	if cloneErr != nil {
		return false, cloneErr
	}
	grade, gradeErr := f.ports.Grade(ctx, graderInput)
	if gradeErr != nil {
		return false, gradeErr
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	return grade, nil
}

func (f *procedureFixture) acceptPoison(
	ctx context.Context,
	proposal memy.Proposal[ProcedureObservation, DocumentRef],
	bad ProcedureObservation,
) error {
	p := proposal
	accepted, acceptErr := f.engine.Accept(ctx, "operator", f.scope, p.ID, p.Digest, p.Revision, qualityPurpose)
	if acceptErr != nil {
		return acceptErr
	}
	receipt, err := f.engine.Commit(
		ctx,
		"operator",
		f.scope,
		qualityPurpose,
		memy.CommitRequest{
			OperationID: "commit-poison",
			ProposalID:  p.ID,
			Acceptance:  accepted,
			RecordID:    "poison",
			Reconcile: memy.Reconciliation{
				Mode:          memy.Append,
				PolicyVersion: procedureResolverVersion,
				Basis:         "permissive synthetic host",
				Related:       nil},
			Expected: 0},
	)
	if err != nil {
		return err
	}
	f.receipts["poison"] = receipt
	f.committed++
	f.recordedAt["poison"] = f.clock.Now().Add(time.Duration(f.committed-1) * time.Nanosecond)
	f.payloads["poison"] = bad
	return nil
}

func (f *procedureFixture) packedMatches(
	packed, all memy.ProjectedRecallResult[ProcedureObservation, DocumentRef],
	bytes, limit uint64,
) bool {
	ok := len(packed.Projections) == 1 && len(packed.Omissions) == 1 && bytes == packed.Budget.Used && bytes <= limit &&
		packed.Budget.Exact
	if len(packed.Projections) == 1 && len(packed.Omissions) == 1 {
		ok = ok && packed.Projections[0].RecordID == all.Projections[0].RecordID &&
			packed.Projections[0].Revision == all.Projections[0].Revision &&
			packed.Omissions[0].Ref == (memy.RevisionRef{RecordID: all.Projections[1].RecordID, Revision: all.Projections[1].Revision}) &&
			packed.Omissions[0].Reason == memy.OmittedBudget
	}
	for _, p := range packed.Projections {
		ok = ok && f.projectionMatches(p)
	}
	return ok
}

func (f *procedureFixture) describePoisonCandidate(r *ScenarioReport) {
	r.Candidate = StageResult{
		Version: f.ports.ExtractorVersion,
		Checks: []Check{
			{
				ID:        "candidate-false-rule",
				Mandatory: false,
				Status:    Fail,
				Expected:  "truthful-data",
				Observed:  "false-rule",
				Evidence:  []Evidence{{Code: "synthetic-false-rule", Count: 1, Aliases: nil}},
			},
		},
		Status: ""}
}

func (f *procedureFixture) describeAbstention(
	r *ScenarioReport,
	renderedOK bool,
	emptyBody, conflictBody, unavailableBody memy.ProjectedRecallResult[ProcedureObservation, DocumentRef],
	emptyBodyErr, unavailableBodyErr error,
) {
	r.Rendered.Checks = append(
		r.Rendered.Checks,
		procedureCheck(
			"rendered-context",
			renderedOK,
			"projected-healthy-empty-conflict-unavailable-distinct",
			procedureOriginalCount,
		),
		procedureCheck(
			"projected-healthy-empty",
			len(emptyBody.Projections) == 0 && emptyBodyErr == nil,
			"healthy-empty-body",
			0,
		),
		procedureCheck(
			"projected-conflict",
			len(conflictBody.Projections) == 0 && conflictBody.Progress.CanonicalFiltered == 2,
			"conflict-suppressed-body",
			0,
		),
		procedureCheck(
			"projected-unavailable",
			errors.Is(unavailableBodyErr, memy.ErrUnavailable) && len(unavailableBody.Projections) == 0,
			"unavailable-fail-closed",
			0,
		),
	)
}

func procedureReadOptions() memy.ReadOptions {
	return memy.ReadOptions{
		Purpose:          qualityPurpose,
		ValidAsOf:        time.Time{},
		RecordedAsOf:     time.Time{},
		IncludeUnknown:   false,
		IncludeConflicts: false,
	}
}

func (f *procedureFixture) verifyPoisonRender(
	ctx context.Context,
	r *ScenarioReport,
	search memy.Search[ProcedureQuery],
	expectedIDs []string,
	guarded bool,
) error {
	body, err := f.project(ctx, search, nil)
	if err != nil {
		return err
	}
	renderedOK := f.projectionSetMatches(body.Projections, expectedIDs)
	for _, p := range body.Projections {
		renderedOK = renderedOK && f.projectionMatches(p)
	}
	// Attempted elevation cannot grant a different principal any rights. No dispatch port exists.
	_, unauthorizedErr := f.engine.Get(
		ctx,
		"intruder",
		f.scope,
		procedureRelevantA,
		procedureReadOptions(),
	)
	renderedOK = renderedOK && errors.Is(unauthorizedErr, memy.ErrUnauthorized)
	r.Rendered.Checks = append(
		r.Rendered.Checks,
		procedureCheck("rendered-context", renderedOK, "data-only-unchanged-authorization-zero-dispatch", 0),
	)
	if !guarded {
		r.Rendered.Checks = append(
			r.Rendered.Checks,
			Check{
				ID:        "semantic-protection-boundary",
				Mandatory: false,
				Status:    Fail,
				Expected:  "truthful-context",
				Observed:  "accepted-false-data",
				Evidence:  []Evidence{{Code: "host-accepted-false-data", Count: 1, Aliases: nil}},
			},
		)
	}
	return nil
}

func (f *procedureFixture) verifySourceRestoration(ctx context.Context) (bool, error) {
	if err := f.sources.Put(f.scope, f.source); err != nil {
		return false, err
	}
	_, restoredErr := f.engine.Get(
		ctx,
		"operator",
		f.scope,
		"relevant-b",
		procedureReadOptions(),
	)
	newSuggestion := memy.Suggestion[ProcedureObservation, DocumentRef]{
		Payload:    procedurePayload("restored"),
		Sources:    []memy.Source[DocumentRef]{f.source},
		Evidence:   "synthetic",
		Extractor:  f.ports.ExtractorVersion,
		ObservedAt: f.clock.Now(),
		Valid: memy.Interval{
			Known: false,
			From:  time.Time{},
			To:    time.Time{},
		},
		ExpiresAt:     time.Time{},
		Lineage:       nil,
		Losses:        nil,
		Uncertainties: nil,
	}
	_, restoredWriteErr := f.engine.Remember(
		ctx,
		"operator",
		f.scope,
		"restored-source-proposal",
		qualityPurpose,
		newSuggestion,
	)
	return errors.Is(restoredErr, memy.ErrNotFound) && errors.Is(restoredWriteErr, memy.ErrRevoked), nil
}

func (f *procedureFixture) currentRecord(
	ctx context.Context,
	id string,
) (memy.Record[ProcedureObservation, DocumentRef], error) {
	return f.engine.Get(ctx, "operator", f.scope, id, procedureReadOptions())
}
