package memy

import (
	"context"
	"slices"
	"time"
)

// SearchCapabilities separates scoped retrieval from visibility guarantees.
type SearchCapabilities struct {
	Scoped            bool
	Visibility        bool
	BoundedCandidates bool
}

// Candidate contains no payload: every result must be revalidated canonically.
type Candidate struct {
	RecordID string
	Revision Version
	Score    float64
	Signals  []SearchSignal
}

// SearchSignal retains the typed evidence used by a composition policy.
type SearchSignal struct {
	Backend string  `json:"backend"`
	Rank    int     `json:"rank"`
	Score   float64 `json:"score"`
}

// Coverage separates backend availability/index visibility from relevance.
type Coverage struct {
	Backend          string `json:"backend"`
	Status           string `json:"status"`
	MinimumSatisfied bool   `json:"minimum_satisfied"`
}

// SearchOptions requests an exact minimum revision, optionally waiting until
// the context deadline. A wait without a deadline is rejected as unbounded.
type SearchOptions struct {
	Minimum       *VisibilityToken
	MaxCandidates int
}

const MaxSearchCandidates = 10000
const MaxSearchBackends = 64

// SearchResult never conflates an unavailable/pending backend with absence.
type SearchResult struct {
	Candidates          []Candidate
	Coverage            []Coverage
	CandidatesTruncated bool
}

// Search is a consumer-query port separate from canonical storage.
type Search[Q any] interface {
	Capabilities() SearchCapabilities
	Search(context.Context, Scope, Q, SearchOptions) (SearchResult, error)
}

// Ranked holds a selected typed record and its ranking explanation.
type Ranked[P, R any] struct {
	Record      Record[P, R]   `json:"record"`
	Score       float64        `json:"score"`
	Explanation string         `json:"explanation"`
	Signals     []SearchSignal `json:"signals"`
}

// Ranker operates only on authorized/revalidated candidates.
type Ranker[P, R any] interface {
	Rank(context.Context, []Ranked[P, R]) ([]Ranked[P, R], error)
}

// RecallOptions selects canonical eligibility separately from search/ranking.
type RecallOptions struct {
	Read   ReadOptions
	Search SearchOptions
	Limit  int
}

// RecallProgress reports separate processing stages, never knowledge completeness.
type RecallProgress struct {
	ReturnedCandidates  int  `json:"returned_candidates"`
	CanonicalChecked    int  `json:"canonical_checked"`
	CanonicalFiltered   int  `json:"canonical_filtered"`
	RankingOmitted      int  `json:"ranking_omitted"`
	CandidatesTruncated bool `json:"candidates_truncated"`
}

// RecallResult is explicit about backend coverage and selected provenance.
type RecallResult[P, R any] struct {
	Records  []Ranked[P, R] `json:"records"`
	Coverage []Coverage     `json:"coverage"`
	Progress RecallProgress `json:"progress"`
}

// Recall searches an authorized exact scope, then validates every candidate
// revision inside a canonical transaction. A stale index cannot supply payload.
func Recall[P, R, Q, A any](
	ctx context.Context,
	e *Engine[P, R, A],
	authority A,
	scope Scope,
	query Q,
	search Search[Q],
	ranker Ranker[P, R],
	options RecallOptions,
) (RecallResult[P, R], error) {
	if e == nil || nilPort(search) || nilPort(ranker) || options.Limit <= 0 || options.Limit > MaxSearchCandidates ||
		options.Search.MaxCandidates < 1 ||
		options.Search.MaxCandidates > MaxSearchCandidates {
		return RecallResult[P, R]{}, ErrInvalid
	}
	decision, operationErr := e.authorize(ctx, authority, scope, ActionRead, options.Read.Purpose)
	if operationErr != nil {
		return RecallResult[P, R]{}, operationErr
	}
	if capabilityErr := validateRecallSearch(search, scope, options.Search); capabilityErr != nil {
		return RecallResult[P, R]{}, capabilityErr
	}
	found, operationErr := search.Search(ctx, scope, query, options.Search)
	if operationErr != nil {
		return RecallResult[P, R]{
			Coverage: boundedCoverage(found.Coverage),
			Records:  nil,
			Progress: RecallProgress{
				ReturnedCandidates:  0,
				CanonicalChecked:    0,
				CanonicalFiltered:   0,
				RankingOmitted:      0,
				CandidatesTruncated: false,
			},
		}, operationErr
	}
	if len(found.Candidates) > options.Search.MaxCandidates {
		return RecallResult[P, R]{}, ErrBudget
	}
	if err := validateSearchResult(found, options.Search.MaxCandidates); err != nil {
		return RecallResult[P, R]{}, err
	}
	result, coverageErr := recallCoverage[P, R](found, options.Search.Minimum)
	if coverageErr != nil {
		return result, coverageErr
	}
	result.Progress = RecallProgress{
		ReturnedCandidates: len(found.Candidates), CanonicalChecked: 0, CanonicalFiltered: 0, RankingOmitted: 0,
		CandidatesTruncated: found.CandidatesTruncated,
	}
	var candidates []Ranked[P, R]
	operationErr = e.config.Store.FencedView(ctx, scope, func(b Bucket) error {
		var err error
		candidates, err = e.recallCandidates(ctx, b, scope, found.Candidates, options.Read)
		if err != nil {
			return err
		}
		return e.completeRecall(ctx, b, authority, scope, options.Read.Purpose, decision, candidates)
	})
	if operationErr != nil {
		return RecallResult[P, R]{}, operationErr
	}
	result.Progress.CanonicalChecked = len(found.Candidates)
	result.Progress.CanonicalFiltered = len(found.Candidates) - len(candidates)
	selected, operationErr := e.rankRecall(ctx, candidates, ranker, options.Limit)
	if operationErr != nil {
		return RecallResult[P, R]{}, operationErr
	}
	result.Progress.RankingOmitted = len(candidates) - len(selected)
	operationErr = e.config.Store.FencedView(ctx, scope, func(b Bucket) error {
		if refreshErr := e.refreshRecalled(ctx, b, scope, selected, options.Read); refreshErr != nil {
			return refreshErr
		}
		if err := e.validateRankedRetention(ctx, b, scope, selected); err != nil {
			return err
		}
		result.Records = selected
		return e.completeRecall(ctx, b, authority, scope, options.Read.Purpose, decision, selected)
	})
	if operationErr != nil {
		return RecallResult[P, R]{}, operationErr
	}
	return result, nil
}

// ScoreRanker sorts descending score, breaking ties deterministically by ID and
// revision. It does not claim scores establish truth or alter authorization.
type ScoreRanker[P, R any] struct{}

// Rank returns a detached ordered selection with a stable explanation.
func (ScoreRanker[P, R]) Rank(ctx context.Context, records []Ranked[P, R]) ([]Ranked[P, R], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ordered := slices.Clone(records)
	slices.SortStableFunc(ordered, func(a, b Ranked[P, R]) int {
		if a.Score > b.Score {
			return -1
		}
		if a.Score < b.Score {
			return 1
		}
		if a.Record.ID < b.Record.ID {
			return -1
		}
		if a.Record.ID > b.Record.ID {
			return 1
		}
		if a.Record.Revision < b.Record.Revision {
			return -1
		}
		if a.Record.Revision > b.Record.Revision {
			return 1
		}
		return 0
	})
	for i := range ordered {
		ordered[i].Explanation = "descending search score; stable record/revision tie break"
	}
	return ordered, nil
}

// Projection carries consumer output plus access/provenance annotations as data.
type Projection[O, R any] struct {
	Output                 O               `json:"output"`
	Provenance             Provenance[R]   `json:"provenance"`
	Scope                  Scope           `json:"scope"`
	RecordID               string          `json:"record_id"`
	Revision               Version         `json:"revision"`
	State                  RecordState     `json:"state"`
	ExpiresAt              time.Time       `json:"expires_at"`
	Trust                  string          `json:"trust"`
	CacheKey               string          `json:"cache_key"`
	Reconciliation         *Reconciliation `json:"reconciliation"`
	AuthorityPolicyVersion string          `json:"authority_policy_version"`
}

// Projector turns an authorized typed record into a consumer context/export type.
type Projector[P, R, O any] interface {
	Version() string
	Project(context.Context, Record[P, R]) (O, error)
}

// Project rereads canonical state and projects only currently permitted data.
// It does not accept caller-supplied cached payload as authoritative input.
func Project[P, R, A, O any](
	ctx context.Context,
	e *Engine[P, R, A],
	authority A,
	scope Scope,
	id string,
	options ReadOptions,
	projector Projector[P, R, O],
) (Projection[O, R], error) {
	if e == nil || nilPort(projector) || !validIdentifier(projector.Version()) || !validIdentifier(id) {
		return Projection[O, R]{}, ErrInvalid
	}
	decision, operationErr := e.authorize(ctx, authority, scope, ActionRead, options.Purpose)
	if operationErr != nil {
		return Projection[O, R]{}, operationErr
	}
	initial, operationErr := e.Get(ctx, authority, scope, id, options)
	if operationErr != nil {
		return Projection[O, R]{}, operationErr
	}
	return finishProjection(ctx, e, authority, scope, decision, initial, options, projector)
}

func finishProjection[P, R, A, O any](
	ctx context.Context,
	e *Engine[P, R, A],
	authority A,
	scope Scope,
	decision Decision,
	initial Record[P, R],
	options ReadOptions,
	projector Projector[P, R, O],
) (Projection[O, R], error) {
	id := initial.ID
	snapshot, operationErr := e.projectionSnapshot(initial)
	if operationErr != nil {
		return Projection[O, R]{}, operationErr
	}
	projectorVersion := projector.Version()
	projectionInput, operationErr := e.cloneRecord(initial)
	if operationErr != nil {
		return Projection[O, R]{}, operationErr
	}
	output, operationErr := projector.Project(ctx, projectionInput)
	if operationErr != nil {
		return Projection[O, R]{}, operationErr
	}
	// Final fresh canonical revalidation defines the delivery boundary after the
	// trusted projector has already received its initially permitted input.
	var result Projection[O, R]
	operationErr = e.config.Store.FencedView(ctx, scope, func(b Bucket) error {
		current, record, transactionErr := e.projectionRecord(ctx, b, scope, id, initial.Revision, options)
		if transactionErr != nil {
			return transactionErr
		}
		if err := e.reauthorize(ctx, authority, scope, ActionRead, options.Purpose, decision); err != nil {
			return err
		}
		if err := e.validateCurrentRead(ctx, b, scope, current); err != nil {
			return err
		}
		finalSnapshot, transactionErr := e.projectionSnapshot(record)
		if transactionErr != nil {
			return transactionErr
		}
		if finalSnapshot != snapshot {
			return ErrStaleInput
		}
		cacheKey, transactionErr := digest(struct {
			Domain     string
			Snapshot   string
			Actor      string
			Scope      Scope
			Policy     string
			Purpose    string
			Projection string
			Ref        RevisionRef
			Epoch      Version
			Options    ReadOptions
		}{
			"projection/v2",
			snapshot,
			decision.Actor,
			scope,
			decision.PolicyVersion,
			options.Purpose,
			projectorVersion,
			RevisionRef{id, record.Revision},
			current.Proposal.Epoch,
			options,
		})
		if transactionErr != nil {
			return transactionErr
		}
		result = Projection[O, R]{
			Output:                 output,
			Provenance:             record.Provenance,
			Scope:                  scope,
			RecordID:               id,
			Revision:               record.Revision,
			State:                  record.State,
			ExpiresAt:              proposalDeadline(current.Proposal),
			Trust:                  "data",
			CacheKey:               cacheKey,
			Reconciliation:         cloneReconciliation(record.Reconciliation),
			AuthorityPolicyVersion: record.AuthorityPolicyVersion,
		}
		return nil
	})
	if operationErr != nil {
		return Projection[O, R]{}, operationErr
	}
	return result, nil
}

func validateRecallSearch[Q any](search Search[Q], scope Scope, options SearchOptions) error {
	caps := search.Capabilities()
	if !caps.Scoped || !caps.BoundedCandidates || (options.Minimum != nil && !caps.Visibility) {
		return ErrUnsupported
	}
	if options.Minimum != nil &&
		!validRef(RevisionRef{RecordID: options.Minimum.RecordID, Revision: options.Minimum.Revision}) {
		return ErrInvalid
	}
	if options.Minimum != nil && options.Minimum.Scope != scope {
		return ErrScopeViolation
	}
	return nil
}

func (e *Engine[P, R, A]) refreshRecalled(
	ctx context.Context,
	b Bucket,
	scope Scope,
	selected []Ranked[P, R],
	read ReadOptions,
) error {
	for i := range selected {
		ref := selected[i].Record
		record, eligible, err := e.recallCandidate(
			ctx,
			b,
			scope,
			Candidate{RecordID: ref.ID, Revision: ref.Revision, Score: 0, Signals: nil},
			read,
		)
		if err != nil {
			return err
		}
		if !eligible {
			return ErrStaleInput
		}
		selected[i].Record = record
	}
	return nil
}

const coverageUnavailable = "unavailable"
