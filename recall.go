package memy

import (
	"context"
	"slices"
	"time"
)

// SearchCapabilities separates scoped retrieval from visibility guarantees.
type SearchCapabilities struct {
	Scoped     bool
	Visibility bool
}

// Candidate contains no payload: every result must be revalidated canonically.
type Candidate struct {
	RecordID string
	Revision Version
	Score    float64
}

// Coverage reports completeness independently for each search backend.
type Coverage struct {
	Backend          string `json:"backend"`
	Status           string `json:"status"`
	MinimumSatisfied bool   `json:"minimum_satisfied"`
}

// SearchOptions requests an exact minimum revision, optionally waiting until
// the context deadline. A wait without a deadline is rejected as unbounded.
type SearchOptions struct{ Minimum *VisibilityToken }

// SearchResult never conflates an unavailable/pending backend with absence.
type SearchResult struct {
	Candidates []Candidate
	Coverage   []Coverage
}

// Search is a consumer-query port separate from canonical storage.
type Search[Q any] interface {
	Capabilities() SearchCapabilities
	Search(context.Context, Scope, Q, SearchOptions) (SearchResult, error)
}

// Ranked holds a selected typed record and its ranking explanation.
type Ranked[P, R any] struct {
	Record      Record[P, R] `json:"record"`
	Score       float64      `json:"score"`
	Explanation string       `json:"explanation"`
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

// RecallResult is explicit about backend coverage and selected provenance.
type RecallResult[P, R any] struct {
	Records  []Ranked[P, R] `json:"records"`
	Coverage []Coverage     `json:"coverage"`
	Complete bool           `json:"complete"`
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
	if e == nil || nilPort(search) || nilPort(ranker) || options.Limit <= 0 || options.Limit > 10000 {
		return RecallResult[P, R]{}, ErrInvalid
	}
	decision, operationErr := e.authorize(ctx, authority, scope, ActionRead, options.Read.Purpose)
	if operationErr != nil {
		return RecallResult[P, R]{}, operationErr
	}
	caps := search.Capabilities()
	if !caps.Scoped || (options.Search.Minimum != nil && !caps.Visibility) {
		return RecallResult[P, R]{}, ErrUnsupported
	}
	if options.Search.Minimum != nil && !validRef(RevisionRef{RecordID: options.Search.Minimum.RecordID, Revision: options.Search.Minimum.Revision}) {
		return RecallResult[P, R]{}, ErrInvalid
	}
	if options.Search.Minimum != nil && options.Search.Minimum.Scope != scope {
		return RecallResult[P, R]{}, ErrScopeViolation
	}
	found, operationErr := search.Search(ctx, scope, query, options.Search)
	if operationErr != nil {
		return RecallResult[P, R]{Coverage: found.Coverage, Records: nil, Complete: false}, operationErr
	}
	if len(found.Coverage) == 0 || len(found.Candidates) > 10000 {
		return RecallResult[P, R]{}, ErrInvalid
	}
	result, coverageErr := recallCoverage[P, R](found, options.Search.Minimum)
	if coverageErr != nil {
		return result, coverageErr
	}
	operationErr = e.config.Store.View(ctx, scope, func(b Bucket) error {
		candidates, candidateErr := e.recallCandidates(ctx, b, scope, found.Candidates, options.Read)
		if candidateErr != nil {
			return candidateErr
		}
		selected, rankingErr := e.rankRecall(ctx, candidates, ranker, options.Limit)
		if rankingErr != nil {
			return rankingErr
		}
		if retentionErr := e.validateRankedRetention(ctx, b, scope, selected); retentionErr != nil {
			return retentionErr
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
	// Reread under a transaction so projection cannot race a canonical revoke.
	var result Projection[O, R]
	operationErr = e.config.Store.View(ctx, scope, func(b Bucket) error {
		current, record, transactionErr := e.projectionRecord(ctx, b, scope, id, initial.Revision, options)
		if transactionErr != nil {
			return transactionErr
		}
		projectionInput, transactionErr := e.cloneRecord(record)
		if transactionErr != nil {
			return transactionErr
		}
		output, transactionErr := projector.Project(ctx, projectionInput)
		if transactionErr != nil {
			return transactionErr
		}
		if err := e.reauthorize(ctx, authority, scope, ActionRead, options.Purpose, decision); err != nil {
			return err
		}
		if err := e.validateCurrentRead(ctx, b, scope, current); err != nil {
			return err
		}
		cacheKey, transactionErr := digest(struct {
			Actor      string
			Scope      Scope
			Policy     string
			Purpose    string
			Projection string
			Ref        RevisionRef
			Epoch      Version
			Options    ReadOptions
		}{
			decision.Actor,
			scope,
			decision.PolicyVersion,
			options.Purpose,
			projector.Version(),
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
