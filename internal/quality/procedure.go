package quality

import (
	"context"
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
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
	contextLimit                uint64
	contextMaximum              uint64
	contextBodies               int
	graderCalls                 int
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
		contextLimit:   0, contextMaximum: 0, contextBodies: 0, graderCalls: 0,
		committed:    0,
		foreignScope: emptyScope,
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
	if budget == nil && f.contextLimit != 0 {
		budget = &memy.ProjectionBudget[ProcedureObservation, DocumentRef]{
			Max:    f.contextLimit,
			Codec:  memy.JSONCodec[ProcedureObservation]{},
			Policy: reference.JSONPacking[ProcedureObservation, DocumentRef]{RejectOversized: false},
		}
	}
	body, err := f.projectBody(ctx, search, budget)
	if err != nil {
		return body, err
	}
	if f.contextLimit != 0 {
		size, measureErr := procedureBodyBytes(body)
		if measureErr != nil {
			return body, measureErr
		}
		if body.Budget.Limit == 0 || size > f.contextLimit {
			return memy.ProjectedRecallResult[ProcedureObservation, DocumentRef]{}, memy.ErrBudget
		}
		f.contextMaximum = max(f.contextMaximum, size)
		f.contextBodies++
	}
	return body, nil
}

// projectBody is an oracle observation; nil budget is allowed only for the
// retrieval baseline before a separately bounded delivered body is packed.
func (f *procedureFixture) projectBody(
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
