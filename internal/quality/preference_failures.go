package quality

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

func preferenceFailures(
	ctx context.Context,
	seed uint64,
	limit, maxCandidates int,
	bytes uint64,
) (preferenceRunResult, error) {
	var out preferenceRunResult
	f, err := newPreferenceFixture(ctx, false, time.Time{})
	if err != nil {
		return out, err
	}
	defer f.close()
	order := []string{preferenceBaselineID, "irrelevant"}
	shufflePreference(seed, preferenceFailuresSalt, order)
	out.Variation = "insertion/" + strings.Join(order, "_")
	var baseline memy.Record[Preference, string]
	for _, id := range order {
		record, operationErr := f.commit(
			ctx,
			id,
			preferencePayload(id, false),
			memy.Interval{Known: false, From: time.Time{}, To: time.Time{}},
			0,
			memy.Append,
			nil,
		)
		if operationErr != nil {
			return out, operationErr
		}
		if id == preferenceBaselineID {
			baseline = record
		}
	}
	before, err := fullSnapshot(
		ctx,
		f.engine,
		"host",
		f.scope,
		preferenceReadOptions(time.Time{}, time.Time{}, false, false),
	)
	if err != nil {
		return out, err
	}
	recovered, providerErr := f.preferenceProviderFailure(ctx, &out)
	if providerErr != nil || !recovered {
		return out, providerErr
	}
	f.policy.Fail(memy.ErrUnavailable)
	unknown, err := f.engine.Get(
		ctx,
		"host",
		f.scope,
		preferenceBaselineID,
		preferenceReadOptions(time.Time{}, time.Time{}, false, false),
	)
	out.check(
		stageExecution,
		"expected_policy_unavailable_fail_closed",
		errors.Is(err, memy.ErrUnavailable) && unknown.ID == "",
		0,
	)
	out.Checks[len(out.Checks)-1].Expected = preferenceUnavailable
	out.Checks[len(out.Checks)-1].Observed = ErrorClass(err)
	f.policy.Fail(nil)
	if err == nil {
		return out, nil
	}
	if !errors.Is(err, memy.ErrUnavailable) {
		return out, err
	}
	after, err := fullSnapshot(
		ctx,
		f.engine,
		"host",
		f.scope,
		preferenceReadOptions(time.Time{}, time.Time{}, false, false),
	)
	if err != nil {
		return out, err
	}
	out.check(stageCanonical, "port_failures_do_not_apply_or_erase", reflect.DeepEqual(before, after), len(after))
	records, body, err := f.recall(
		ctx,
		[]string{preferenceBaselineID},
		preferenceReadOptions(time.Time{}, time.Time{}, false, false),
		limit,
		maxCandidates,
		bytes,
	)
	if err != nil {
		return out, err
	}
	preferenceDelivered(&out, records, []memy.Record[Preference, string]{baseline}, body)
	return out, nil
}

// Conflict candidates are scripted metadata, not managed derived artifacts.
// Engine.WithDerivedWrite rejects conflicted lineage; Recall must nevertheless
// honor IncludeConflicts when a search port supplies those exact revisions.
type preferenceConflictSearch struct{ Scope memy.Scope }

func (preferenceConflictSearch) Capabilities() memy.SearchCapabilities {
	return memy.SearchCapabilities{Scoped: true, BoundedCandidates: true, Visibility: false}
}

func (search preferenceConflictSearch) Search(
	ctx context.Context,
	scope memy.Scope,
	query preferenceQuery,
	opts memy.SearchOptions,
) (memy.SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return memy.SearchResult{Candidates: nil, Coverage: nil, CandidatesTruncated: false}, err
	}
	if opts.MaxCandidates < 1 {
		return memy.SearchResult{Candidates: nil, Coverage: nil, CandidatesTruncated: false}, memy.ErrInvalid
	}
	result := memy.SearchResult{
		Coverage: []memy.Coverage{
			{Backend: "preference-conflict-metadata/v2", Status: "ready", MinimumSatisfied: false},
		},
		Candidates:          nil,
		CandidatesTruncated: false,
	}
	if scope != search.Scope {
		return memy.SearchResult{Candidates: nil, Coverage: nil, CandidatesTruncated: false}, memy.ErrScopeViolation
	}
	if !slices.Contains(query.IDs, preferenceConflictID) {
		return result, nil
	}
	for revision := memy.Version(1); revision <= 2; revision++ {
		result.Candidates = append(
			result.Candidates,
			memy.Candidate{
				RecordID: preferenceConflictID,
				Revision: revision,
				Score:    memy.ScoreOf(1),
				Signals: []memy.SearchSignal{
					{Backend: "preference-conflict-metadata/v2", Rank: int(revision), Score: memy.ScoreOf(1)},
				},
			},
		)
	}
	if len(result.Candidates) > opts.MaxCandidates {
		result.Candidates = result.Candidates[:opts.MaxCandidates]
		result.CandidatesTruncated = true
	}
	return result, nil
}

func (f *preferenceFixture) preferenceProviderFailure(ctx context.Context, out *preferenceRunResult) (bool, error) {
	// Mutable fixture-owned failures are classified by sentinel, never message.
	failed := true
	provider := reference.ExtractorFunc[string, Preference, string](
		func(_ context.Context, _ string) ([]memy.Suggestion[Preference, string], error) {
			if failed {
				return nil, memy.ErrUnavailable
			}
			return []memy.Suggestion[Preference, string]{
				f.suggestion(
					preferencePayload(preferenceEvening, true),
					memy.Interval{Known: false, From: time.Time{}, To: time.Time{}},
				),
			}, nil
		},
	)
	_, err := memy.Extract(
		ctx,
		f.engine,
		"host",
		f.scope,
		memy.ExtractionJob{
			OperationID:     "failed-provider",
			ProviderVersion: preferenceProviderVersion,
			Purpose:         preferencePurpose,
		},
		"synthetic input",
		memy.JSONCodec[string]{},
		provider,
	)
	out.check(stageExecution, "expected_provider_unavailable", errors.Is(err, memy.ErrUnavailable), 0)
	out.Checks[len(out.Checks)-1].Expected = preferenceUnavailable
	out.Checks[len(out.Checks)-1].Observed = ErrorClass(err)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, memy.ErrUnavailable) {
		return false, err
	}
	failed = false
	recovery, err := memy.Extract(
		ctx,
		f.engine,
		"host",
		f.scope,
		memy.ExtractionJob{
			OperationID:     "recovered-provider",
			ProviderVersion: preferenceProviderVersion,
			Purpose:         preferencePurpose,
		},
		"synthetic input",
		memy.JSONCodec[string]{},
		provider,
	)
	if err != nil {
		return false, err
	}
	out.check(stageExecution, "provider_recovery_unaccepted_proposal", len(recovery) == 1, len(recovery))
	return true, nil
}
