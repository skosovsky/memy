package reference

import (
	"context"
	"errors"
	"reflect"

	"github.com/skosovsky/memy"
)

// Eventual adapts the reference index to an explicitly weaker visibility
// profile. It never upgrades a minimum-token request to a stronger guarantee.
type Eventual[Q any] struct{ Index *Index[Q] }

// Capabilities reports eventual consistency without minimum visibility support.
func (Eventual[Q]) Capabilities() memy.SearchCapabilities {
	return memy.SearchCapabilities{Scoped: true, Visibility: false}
}

// Search reports eventual coverage even when its current snapshot is empty.
func (e Eventual[Q]) Search(
	ctx context.Context,
	scope memy.Scope,
	query Q,
	options memy.SearchOptions,
) (memy.SearchResult, error) {
	if e.Index == nil {
		return memy.SearchResult{}, memy.ErrInvalid
	}
	if options.Minimum != nil {
		return memy.SearchResult{}, memy.ErrUnsupported
	}
	result, operationErr := e.Index.Search(ctx, scope, query, options)
	if operationErr != nil {
		return result, operationErr
	}
	for i := range result.Coverage {
		result.Coverage[i].Status = "eventual"
		result.Coverage[i].MinimumSatisfied = false
	}
	return result, nil
}

// Composite combines scoped backends, retaining coverage from every backend.
// It executes bounded synchronous calls; it creates no workers. With degraded
// mode enabled, partial backend failures remain explicit in coverage. Failure
// of every backend is unavailable, never an apparently successful empty result.
type Composite[Q any] struct {
	Backends      []memy.Search[Q]
	AllowDegraded bool
}

// Capabilities reports the intersection of every participating backend profile.
func (c Composite[Q]) Capabilities() memy.SearchCapabilities {
	caps := memy.SearchCapabilities{Scoped: len(c.Backends) != 0, Visibility: len(c.Backends) != 0}
	for _, backend := range c.Backends {
		if absentSearch(backend) {
			return memy.SearchCapabilities{}
		}
		profile := backend.Capabilities()
		caps.Scoped = caps.Scoped && profile.Scoped
		caps.Visibility = caps.Visibility && profile.Visibility
	}
	return caps
}

func absentSearch[Q any](backend memy.Search[Q]) bool {
	if backend == nil {
		return true
	}
	value := reflect.ValueOf(backend)
	kind := value.Kind()
	if kind == reflect.Chan || kind == reflect.Func || kind == reflect.Interface ||
		kind == reflect.Map || kind == reflect.Pointer || kind == reflect.Slice {
		return value.IsNil()
	}
	return false
}

// Search returns per-backend coverage without silently discarding failures.
func (c Composite[Q]) Search(
	ctx context.Context,
	scope memy.Scope,
	query Q,
	options memy.SearchOptions,
) (memy.SearchResult, error) {
	if len(c.Backends) == 0 {
		return memy.SearchResult{}, memy.ErrInvalid
	}
	caps := c.Capabilities()
	if !caps.Scoped || (options.Minimum != nil && !caps.Visibility) {
		return memy.SearchResult{}, memy.ErrUnsupported
	}
	combined := memy.SearchResult{Candidates: make([]memy.Candidate, 0), Coverage: make([]memy.Coverage, 0)}
	successes := 0
	var failures error
	for _, backend := range c.Backends {
		if err := ctx.Err(); err != nil {
			return combined, err
		}
		result, err := backend.Search(ctx, scope, query, options)
		combined.Coverage = append(combined.Coverage, result.Coverage...)
		if len(result.Coverage) == 0 {
			return combined, memy.ErrInvalid
		}
		if err != nil {
			failures = errors.Join(failures, err)
			if !c.AllowDegraded || options.Minimum != nil || errors.Is(err, context.Canceled) ||
				errors.Is(err, context.DeadlineExceeded) {
				return combined, err
			}
			continue
		}
		successes++
		combined.Candidates = append(combined.Candidates, result.Candidates...)
	}
	if successes == 0 {
		return combined, errors.Join(memy.ErrUnavailable, failures)
	}
	return combined, nil
}
