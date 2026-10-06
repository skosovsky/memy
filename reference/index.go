package reference

import (
	"context"
	"errors"
	"sync"

	"github.com/skosovsky/memy"
)

type indexedKey struct {
	scope memy.Scope
	ref   memy.RevisionRef
}

// Index is a deterministic scoped index with explicit visibility lag. Writes
// must use Engine.WithDerivedWrite; direct calls are privileged adapter access.
type Index[Q any] struct {
	mu           sync.Mutex
	name         string
	match        func(Q, memy.Candidate) bool
	pending      map[indexedKey]memy.Candidate
	visible      map[indexedKey]memy.Candidate
	changed      chan struct{}
	failure      error
	purgeFailure error
}

// NewIndex creates an empty index. Match must be deterministic and pure.
func NewIndex[Q any](name string, match func(Q, memy.Candidate) bool) *Index[Q] {
	return &Index[Q]{
		name:    name,
		match:   match,
		pending: make(map[indexedKey]memy.Candidate),
		visible: make(map[indexedKey]memy.Candidate),
		changed: make(chan struct{}), mu: sync.Mutex{}, failure: nil, purgeFailure: nil,
	}
}

// Name identifies this managed deletion participant.
func (i *Index[Q]) Name() string { return i.name }

// Capabilities declares scoped exact visibility support.
func (*Index[Q]) Capabilities() memy.SearchCapabilities {
	return memy.SearchCapabilities{Scoped: true, Visibility: true}
}

// Stage queues candidate metadata without making it visible.
func (i *Index[Q]) Stage(ctx context.Context, scope memy.Scope, candidate memy.Candidate) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	if candidate.RecordID == "" || candidate.Revision == 0 {
		return memy.ErrInvalid
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.failure != nil {
		return i.failure
	}
	i.pending[indexedKey{scope, memy.RevisionRef{RecordID: candidate.RecordID, Revision: candidate.Revision}}] = candidate
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
	key := indexedKey{token.Scope, memy.RevisionRef{RecordID: token.RecordID, Revision: token.Revision}}
	if _, ok := i.visible[key]; ok {
		return nil
	}
	candidate, ok := i.pending[key]
	if !ok {
		return memy.ErrNotFound
	}
	i.visible[key] = candidate
	delete(i.pending, key)
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
	if err := scope.Validate(); err != nil {
		return memy.SearchResult{}, err
	}
	if err := validateMinimum(ctx, scope, options.Minimum); err != nil {
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
					{Backend: i.name, Status: "unavailable", MinimumSatisfied: false},
				},
				Candidates: nil,
			}, errors.Join(
				memy.ErrUnavailable,
				failure,
			)
		}
		minimumSatisfied := true
		if options.Minimum != nil {
			_, minimumSatisfied = i.visible[indexedKey{scope, memy.RevisionRef{RecordID: options.Minimum.RecordID, Revision: options.Minimum.Revision}}]
		}
		if minimumSatisfied {
			result := i.visibleResult(scope, query)
			if options.Minimum == nil && result.Coverage[0].Status == "complete" {
				result.Coverage[0].Status = "eventual"
			}
			i.mu.Unlock()
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
	if token.Scope != scope {
		return memy.ErrScopeViolation
	}
	if _, ok := ctx.Deadline(); !ok {
		return memy.ErrInvalid
	}
	return nil
}

// visibleResult requires the caller to hold the index mutex.
func (i *Index[Q]) visibleResult(scope memy.Scope, query Q) memy.SearchResult {
	result := memy.SearchResult{
		Candidates: make([]memy.Candidate, 0),
		Coverage:   []memy.Coverage{{Backend: i.name, Status: "complete", MinimumSatisfied: true}},
	}
	for key, candidate := range i.pending {
		if key.scope == scope && (i.match == nil || i.match(query, candidate)) {
			result.Coverage[0].Status = "pending"
			break
		}
	}
	for key, candidate := range i.visible {
		if key.scope == scope && (i.match == nil || i.match(query, candidate)) {
			result.Candidates = append(result.Candidates, candidate)
		}
	}
	return result
}

func (i *Index[Q]) pendingResult(err error) (memy.SearchResult, error) {
	result := memy.SearchResult{
		Coverage:   []memy.Coverage{{Backend: i.name, Status: "pending", MinimumSatisfied: false}},
		Candidates: nil,
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
	for _, values := range []map[indexedKey]memy.Candidate{i.visible, i.pending} {
		for key := range values {
			if key.scope == batch.Scope &&
				(selected[key.ref.RecordID] || batch.Selector.Kind == memy.SelectScope || batch.Selector.Kind == memy.SelectSubject) {
				delete(values, key)
			}
		}
	}
	close(i.changed)
	i.changed = make(chan struct{})
	return memy.PurgeAck{Sink: i.name, OperationID: batch.OperationID, Epoch: batch.Epoch, Chunk: batch.Chunk}, nil
}
