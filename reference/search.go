package reference

import (
	"context"
	"errors"
	"math"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/skosovsky/memy"
)

// Eventual exposes bounded retrieval without a minimum visibility guarantee.
type Eventual[Q any] struct{ Index *Index[Q] }

func (Eventual[Q]) Capabilities() memy.SearchCapabilities {
	return memy.SearchCapabilities{Scoped: true, Visibility: false, BoundedCandidates: true}
}

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
	result, err := e.Index.Search(ctx, scope, query, options)
	if err != nil {
		return result, err
	}
	for n := range result.Coverage {
		result.Coverage[n].Status = coverageEventual
		result.Coverage[n].MinimumSatisfied = false
	}
	return result, nil
}

// Backend binds a search adapter to its exact coverage identity.
type Backend[Q any] struct {
	ID     string
	Search memy.Search[Q]
}

// RRFConfig uses ranks, independent of raw backend score scales. Missing weights are 1.
type RRFConfig struct {
	K       float64
	Weights map[string]float64
}

// Composite executes synchronous calls in identity order and fuses distinct ranks.
// Each child must expose one coverage identity; nested composites are unsupported.
type Composite[Q any] struct {
	Backends      []Backend[Q]
	RRF           RRFConfig
	AllowDegraded bool
}

func validSearchIdentifier(value string) bool {
	return len(value) > 0 && len(value) <= 1024 && utf8.ValidString(value) && !strings.ContainsRune(value, 0) &&
		strings.TrimSpace(value) != ""
}
func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
func (c Composite[Q]) validate() error {
	if len(c.Backends) == 0 || len(c.Backends) > memy.MaxSearchBackends || !finite(c.RRF.K) || c.RRF.K <= 0 {
		return memy.ErrInvalid
	}
	names := make(map[string]bool, len(c.Backends))
	for _, b := range c.Backends {
		if !validSearchIdentifier(b.ID) || names[b.ID] || absentSearch(b.Search) {
			return memy.ErrInvalid
		}
		names[b.ID] = true
	}
	for name, weight := range c.RRF.Weights {
		if !names[name] || !finite(weight) || weight <= 0 {
			return memy.ErrInvalid
		}
	}
	return nil
}
func (c Composite[Q]) Capabilities() memy.SearchCapabilities {
	if c.validate() != nil {
		return memy.SearchCapabilities{}
	}
	caps := memy.SearchCapabilities{Scoped: true, Visibility: true, BoundedCandidates: true}
	for _, b := range c.Backends {
		p := b.Search.Capabilities()
		caps.Scoped = caps.Scoped && p.Scoped
		caps.Visibility = caps.Visibility && p.Visibility
		caps.BoundedCandidates = caps.BoundedCandidates && p.BoundedCandidates
	}
	return caps
}
func absentSearch[Q any](backend memy.Search[Q]) bool {
	if backend == nil {
		return true
	}
	value := reflect.ValueOf(backend)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	case reflect.Invalid,
		reflect.Bool,
		reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32,
		reflect.Int64,
		reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64,
		reflect.Uintptr,
		reflect.Float32,
		reflect.Float64,
		reflect.Complex64,
		reflect.Complex128,
		reflect.Array,
		reflect.String,
		reflect.Struct,
		reflect.UnsafePointer:
		return false
	}
	return false
}
func validSearchOptions(ctx context.Context, scope memy.Scope, options memy.SearchOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	if options.MaxCandidates < 1 || options.MaxCandidates > memy.MaxSearchCandidates {
		return memy.ErrInvalid
	}
	return validateMinimum(ctx, scope, options.Minimum)
}
func validateBackendResult(result memy.SearchResult, id string, maximum int) error {
	if len(result.Candidates) > maximum {
		return memy.ErrBudget
	}
	if len(result.Coverage) != 1 || result.Coverage[0].Backend != id {
		return memy.ErrInvalid
	}
	switch result.Coverage[0].Status {
	case coverageReady, coverageEventual, coveragePending, "degraded", coverageUnavailable:
	default:
		return memy.ErrInvalid
	}
	for _, candidate := range result.Candidates {
		if (memy.RevisionRef{RecordID: candidate.RecordID, Revision: candidate.Revision}).Validate() != nil ||
			!finite(candidate.Score) ||
			len(candidate.Signals) > 1 {
			return memy.ErrInvalid
		}
		for _, signal := range candidate.Signals {
			if signal.Backend != id || signal.Rank < 1 || !finite(signal.Score) {
				return memy.ErrInvalid
			}
		}
	}
	return nil
}
func candidateOrder(a, b memy.Candidate) int {
	if a.Score > b.Score {
		return -1
	}
	if a.Score < b.Score {
		return 1
	}
	if a.RecordID < b.RecordID {
		return -1
	}
	if a.RecordID > b.RecordID {
		return 1
	}
	if a.Revision < b.Revision {
		return -1
	}
	if a.Revision > b.Revision {
		return 1
	}
	return 0
}

func (c Composite[Q]) Search(
	ctx context.Context,
	scope memy.Scope,
	query Q,
	options memy.SearchOptions,
) (memy.SearchResult, error) {
	if err := c.validate(); err != nil {
		return memy.SearchResult{}, err
	}
	if err := validSearchOptions(ctx, scope, options); err != nil {
		return memy.SearchResult{}, err
	}
	caps := c.Capabilities()
	if !caps.Scoped || !caps.BoundedCandidates || (options.Minimum != nil && !caps.Visibility) {
		return memy.SearchResult{}, memy.ErrUnsupported
	}
	backends := slices.Clone(c.Backends)
	slices.SortFunc(backends, func(a, b Backend[Q]) int { return strings.Compare(a.ID, b.ID) })
	combined := memy.SearchResult{
		Coverage:   make([]memy.Coverage, 0, len(backends)),
		Candidates: make([]memy.Candidate, 0), CandidatesTruncated: false,
	}
	fused := make(map[memy.RevisionRef]memy.Candidate)
	successes := 0
	var failures error
	for _, backend := range backends {
		if err := ctx.Err(); err != nil {
			return combined, err
		}
		result, usable, skippedErr, backendErr := c.searchBackend(ctx, scope, query, options, backend, &combined)
		if backendErr != nil {
			return combined, backendErr
		}
		if !usable {
			failures = errors.Join(failures, skippedErr)
			continue
		}

		successes++
		combined.CandidatesTruncated = combined.CandidatesTruncated || result.CandidatesTruncated
		if fusionErr := c.fuseBackend(fused, backend.ID, result.Candidates); fusionErr != nil {
			return combined, fusionErr
		}
	}
	if successes == 0 {
		return combined, errors.Join(memy.ErrUnavailable, failures)
	}
	for _, candidate := range fused {
		combined.Candidates = append(combined.Candidates, candidate)
	}
	slices.SortFunc(combined.Candidates, candidateOrder)
	if len(combined.Candidates) > options.MaxCandidates {
		combined.Candidates = combined.Candidates[:options.MaxCandidates]
		combined.CandidatesTruncated = true
	}
	return combined, nil
}

func (c Composite[Q]) searchBackend(
	ctx context.Context,
	scope memy.Scope,
	query Q,
	options memy.SearchOptions,
	backend Backend[Q],
	combined *memy.SearchResult,
) (memy.SearchResult, bool, error, error) {
	result, err := backend.Search.Search(ctx, scope, query, options)
	// Cancellation may legitimately return no metadata at all. Preserve the
	// operation error before validating a successful/partial result envelope.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		if len(result.Coverage) == 1 && result.Coverage[0].Backend == backend.ID {
			combined.Coverage = append(combined.Coverage, result.Coverage...)
		}
		return result, false, nil, err
	}
	if cancellation := ctx.Err(); cancellation != nil {
		return result, false, nil, cancellation
	}
	if validation := validateBackendResult(result, backend.ID, options.MaxCandidates); validation != nil {
		return result, false, nil, validation
	}
	combined.Coverage = append(combined.Coverage, result.Coverage...)
	if cancellation := ctx.Err(); cancellation != nil && err == nil {
		return result, false, nil, cancellation
	}
	if err != nil {
		if !c.AllowDegraded || options.Minimum != nil || errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			return result, false, nil, err
		}
		return result, false, err, nil
	}
	coverage := result.Coverage[0]
	if coverage.Status == coverageUnavailable || coverage.Status == "degraded" {
		if !c.AllowDegraded || options.Minimum != nil {
			return result, false, nil, memy.ErrUnavailable
		}
		return result, false, memy.ErrUnavailable, nil
	}
	if options.Minimum != nil && !coverage.MinimumSatisfied {
		return result, false, nil, memy.ErrVisibilityPending
	}
	return result, true, nil, nil
}

func (c Composite[Q]) fuseBackend(
	fused map[memy.RevisionRef]memy.Candidate,
	backendID string,
	candidates []memy.Candidate,
) error {
	seen := make(map[memy.RevisionRef]bool, len(candidates))
	rank := 0
	weight := 1.0
	if configured, ok := c.RRF.Weights[backendID]; ok {
		weight = configured
	}
	for _, candidate := range candidates {
		ref := memy.RevisionRef{RecordID: candidate.RecordID, Revision: candidate.Revision}
		if seen[ref] {
			continue
		}
		seen[ref] = true
		rank++
		value := fused[ref]
		value.RecordID = ref.RecordID
		value.Revision = ref.Revision
		value.Score += weight / (c.RRF.K + float64(rank))
		if !finite(value.Score) {
			return memy.ErrInvalid
		}
		rawScore := candidate.Score
		if len(candidate.Signals) == 1 {
			rawScore = candidate.Signals[0].Score
		}
		value.Signals = append(value.Signals, memy.SearchSignal{Backend: backendID, Rank: rank, Score: rawScore})
		fused[ref] = value
	}
	return nil
}

const (
	coverageReady       = "ready"
	coverageEventual    = "eventual"
	coveragePending     = "pending"
	coverageUnavailable = "unavailable"
)
