package reference

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/internal/workcost"
)

// Index is a deterministic scoped index with explicit visibility lag. Writes
// must use Engine.WithDerivedWrite; direct calls are privileged adapter access.
type Index[Q any] struct {
	mu           sync.Mutex
	name         string
	match        func(Q, memy.Candidate) bool
	pending      map[memy.Scope]map[memy.RevisionRef]memy.Candidate
	visible      map[memy.Scope]map[memy.RevisionRef]memy.Candidate
	changed      chan struct{}
	failure      error
	purgeFailure error
}

// NewIndex creates an empty index, or returns nil for an invalid name.
// Match must be deterministic and pure.
func NewIndex[Q any](name string, match func(Q, memy.Candidate) bool) *Index[Q] {
	if !validSearchIdentifier(name) {
		return nil
	}
	return &Index[Q]{
		name:    name,
		match:   match,
		pending: make(map[memy.Scope]map[memy.RevisionRef]memy.Candidate),
		visible: make(map[memy.Scope]map[memy.RevisionRef]memy.Candidate),
		changed: make(chan struct{}), mu: sync.Mutex{}, failure: nil, purgeFailure: nil,
	}
}

// Name identifies this managed deletion participant.
func (i *Index[Q]) Name() string { return i.name }

// Capabilities declares scoped exact visibility support.
func (*Index[Q]) Capabilities() memy.SearchCapabilities {
	return memy.SearchCapabilities{Scoped: true, Visibility: true, BoundedCandidates: true}
}

// Stage queues candidate metadata without making it visible.
func (i *Index[Q]) Stage(ctx context.Context, scope memy.Scope, candidate memy.Candidate) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	if i == nil || (memy.RevisionRef{RecordID: candidate.RecordID, Revision: candidate.Revision}).Validate() != nil ||
		candidate.Score.Validate() != nil {
		return memy.ErrInvalid
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.failure != nil {
		return i.failure
	}
	candidate.Signals = nil
	if i.pending[scope] == nil {
		i.pending[scope] = make(map[memy.RevisionRef]memy.Candidate)
	}
	i.pending[scope][memy.RevisionRef{RecordID: candidate.RecordID, Revision: candidate.Revision}] = candidate
	return nil
}

// Acknowledge publishes one staged revision. Purge deletes staged entries too,
// so a delayed acknowledgement cannot republish a purged candidate.
func (i *Index[Q]) Acknowledge(ctx context.Context, token memy.VisibilityToken) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	key := memy.RevisionRef{RecordID: token.RecordID, Revision: token.Revision}
	if _, ok := i.visible[token.Scope][key]; ok {
		return nil
	}
	candidate, ok := i.pending[token.Scope][key]
	if !ok {
		return memy.ErrNotFound
	}
	if i.visible[token.Scope] == nil {
		i.visible[token.Scope] = make(map[memy.RevisionRef]memy.Candidate)
	}
	i.visible[token.Scope][key] = candidate
	delete(i.pending[token.Scope], key)
	if len(i.pending[token.Scope]) == 0 {
		delete(i.pending, token.Scope)
	}
	close(i.changed)
	i.changed = make(chan struct{})
	return nil
}

// Fail injects search/write failure; nil restores availability.
func (i *Index[Q]) Fail(err error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.failure = err
	close(i.changed)
	i.changed = make(chan struct{})
}

// FailPurge injects independent deletion failure; nil allows retry.
func (i *Index[Q]) FailPurge(err error) { i.mu.Lock(); defer i.mu.Unlock(); i.purgeFailure = err }

// Search waits for an exact token only with a deadline and reports pending
// when deadline expires; explicit cancellation remains [context.Canceled].
func (i *Index[Q]) Search(
	ctx context.Context,
	scope memy.Scope,
	query Q,
	options memy.SearchOptions,
) (memy.SearchResult, error) {
	if i == nil {
		return memy.SearchResult{}, memy.ErrInvalid
	}
	if err := validSearchOptions(ctx, scope, options); err != nil {
		return memy.SearchResult{}, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return i.pendingResult(err)
		}
		i.mu.Lock()
		if i.failure != nil {
			failure := i.failure
			i.mu.Unlock()
			return memy.SearchResult{
				Coverage: []memy.Coverage{
					{Backend: i.name, Status: coverageUnavailable, MinimumSatisfied: false},
				},
				Candidates: nil, CandidatesTruncated: false,
			}, errors.Join(
				memy.ErrUnavailable,
				failure,
			)
		}
		minimumSatisfied := true
		if options.Minimum != nil {
			_, minimumSatisfied = i.visible[scope][memy.RevisionRef{RecordID: options.Minimum.RecordID, Revision: options.Minimum.Revision}]
		}
		if minimumSatisfied {
			result := i.visibleResult(scope, query, options.MaxCandidates)
			if options.Minimum == nil && result.Coverage[0].Status == coverageReady {
				result.Coverage[0].Status = coverageEventual
			}
			i.mu.Unlock()
			if err := ctx.Err(); err != nil {
				return i.pendingResult(err)
			}
			return result, nil
		}
		changed := i.changed
		i.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return i.pendingResult(ctx.Err())
		}
	}
}

func validateMinimum(ctx context.Context, scope memy.Scope, token *memy.VisibilityToken) error {
	if token == nil {
		return nil
	}
	if err := token.Scope.Validate(); err != nil {
		return err
	}
	if err := (memy.RevisionRef{RecordID: token.RecordID, Revision: token.Revision}).Validate(); err != nil {
		return err
	}
	if token.Scope != scope {
		return memy.ErrScopeViolation
	}
	if _, ok := ctx.Deadline(); !ok {
		return memy.ErrInvalid
	}
	return nil
}

// visibleResult requires the caller to hold the index mutex.
func (i *Index[Q]) visibleResult(scope memy.Scope, query Q, maximum int) memy.SearchResult {
	result := memy.SearchResult{
		Candidates: make([]memy.Candidate, 0), CandidatesTruncated: false,
		Coverage: []memy.Coverage{{Backend: i.name, Status: coverageReady, MinimumSatisfied: true}},
	}
	for _, candidate := range i.pending[scope] {
		workcost.SearchCandidate()
		if i.match == nil || i.match(query, candidate) {
			result.Coverage[0].Status = coveragePending
			break
		}
	}
	// Use the selected scope only. The heap's root is the worst retained
	// candidate, so arbitrary pure matchers still see every scoped candidate.
	limit := min(maximum, len(i.visible[scope]))
	result.Candidates = make([]memy.Candidate, 0, limit)
	for _, candidate := range i.visible[scope] {
		workcost.SearchCandidate()
		if i.match != nil && !i.match(query, candidate) {
			continue
		}
		if len(result.Candidates) < limit {
			result.Candidates = append(result.Candidates, candidate)
			candidateUp(result.Candidates, len(result.Candidates)-1)
			continue
		}
		result.CandidatesTruncated = true
		if candidateOrder(candidate, result.Candidates[0]) < 0 {
			result.Candidates[0] = candidate
			candidateDown(result.Candidates)
		}
	}
	slices.SortFunc(result.Candidates, candidateOrder)
	for n := range result.Candidates {
		result.Candidates[n].Signals = []memy.SearchSignal{
			{Backend: i.name, Rank: n + 1, Score: result.Candidates[n].Score},
		}
	}
	return result
}

func (i *Index[Q]) pendingResult(err error) (memy.SearchResult, error) {
	result := memy.SearchResult{
		Coverage:   []memy.Coverage{{Backend: i.name, Status: coveragePending, MinimumSatisfied: false}},
		Candidates: nil, CandidatesTruncated: false,
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return result, errors.Join(memy.ErrVisibilityPending, err)
	}
	return result, err
}

// Purge removes visible and pending artifacts in the batch's exact scope.
func (i *Index[Q]) Purge(ctx context.Context, batch memy.PurgeBatch) (memy.PurgeAck, error) {
	if err := ctx.Err(); err != nil {
		return memy.PurgeAck{}, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.purgeFailure != nil {
		return memy.PurgeAck{}, i.purgeFailure
	}
	selected := make(map[string]bool, len(batch.Records))
	for _, id := range batch.Records {
		selected[id] = true
	}
	for _, scopes := range []map[memy.Scope]map[memy.RevisionRef]memy.Candidate{i.visible, i.pending} {
		values := scopes[batch.Scope]
		for ref := range values {
			if selected[ref.RecordID] || batch.Selector.Kind == memy.SelectScope ||
				batch.Selector.Kind == memy.SelectSubject {
				delete(values, ref)
			}
		}
		if len(values) == 0 {
			delete(scopes, batch.Scope)
		}
	}
	close(i.changed)
	i.changed = make(chan struct{})
	return memy.PurgeAck{Sink: i.name, OperationID: batch.OperationID, Epoch: batch.Epoch, Chunk: batch.Chunk}, nil
}

// candidateUp maintains a max heap under candidateOrder (worst first).
func candidateUp(values []memy.Candidate, child int) {
	for child > 0 {
		parent := (child - 1) / 2
		if candidateOrder(values[child], values[parent]) <= 0 {
			return
		}
		values[child], values[parent] = values[parent], values[child]
		child = parent
	}
}

func candidateDown(values []memy.Candidate) {
	parent := 0
	for {
		child := 2*parent + 1
		if child >= len(values) {
			return
		}
		if right := child + 1; right < len(values) && candidateOrder(values[right], values[child]) > 0 {
			child = right
		}
		if candidateOrder(values[parent], values[child]) >= 0 {
			return
		}
		values[parent], values[child] = values[child], values[parent]
		parent = child
	}
}
