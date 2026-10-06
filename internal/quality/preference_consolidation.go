package quality

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

func preferenceConsolidation(
	ctx context.Context,
	seed uint64,
	limit, maxCandidates int,
	bytes uint64,
	mode memy.ConsolidationMode,
	budget memy.Budget,
) (preferenceRunResult, error) {
	var out preferenceRunResult
	f, err := newPreferenceFixture(ctx, false, time.Time{})
	if err != nil {
		return out, err
	}
	defer f.close()
	valid := memy.Interval{Known: true, From: f.clock.Now().Add(-time.Hour), To: f.clock.Now().Add(preferenceDay)}
	ids := []string{"positive-a", "positive-b", preferenceNegativeID}
	shufflePreference(seed, preferenceConsolidationSalt, ids)
	out.Variation = "insertion/" + strings.Join(ids, "_")
	originals := make([]memy.Record[Preference, string], 0, len(ids))
	for _, id := range ids {
		p := preferencePayload(preferenceMorning, false)
		if id == preferenceNegativeID {
			p = preferencePayload(preferenceEvening, true)
		}
		r, commitErr := f.commit(ctx, id, p, valid, 0, memy.Append, nil)
		if commitErr != nil {
			return out, commitErr
		}
		originals = append(originals, r)
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
	want := slices.Clone(originals)
	selected := slices.Clone(ids)
	if mode != "" {
		proposals, inputs, proposalErr := f.preferenceProposal(ctx, originals, valid, mode, budget)
		if proposalErr != nil {
			return out, proposalErr
		}
		out.check(stageExecution, "consolidation_proposal_created", len(proposals) == 1, len(proposals))
		if len(proposals) != 1 {
			return out, nil
		}
		good := preferenceCandidate(&out, proposals[0], originals, inputs, valid, f.source, mode)
		want, selected, err = f.reviewPreference(ctx, &out, proposals[0], originals, mode, good)
		if err != nil {
			return out, err
		}
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
	preferenceOriginalsPreserved(&out, before, after, f.scope, f.source, len(originals))
	records, body, err := f.recall(
		ctx,
		selected,
		preferenceReadOptions(time.Time{}, time.Time{}, false, false),
		limit,
		maxCandidates,
		bytes,
	)
	if err != nil {
		return out, err
	}
	preferenceDelivered(&out, records, want, body)
	return out, nil
}

func (f *preferenceFixture) preferenceMergeProvider(
	mode memy.ConsolidationMode,
	valid memy.Interval,
) memy.Consolidator[Preference, string] {
	if mode == memy.ExactDedup {
		return nil
	}
	return reference.MergeFunc[Preference, string](
		func(_ context.Context, _ []memy.Record[Preference, string], _ memy.Budget) (memy.MergeResult[Preference, string], error) {
			p := Preference{
				Facts: []PreferenceFact{
					{Key: preferenceDrink, Value: preferenceTea, Qualifier: preferenceMorning, Negated: false},
					{Key: preferenceDrink, Value: preferenceTea, Qualifier: preferenceEvening, Negated: true},
				},
			}
			v := valid
			if mode == memy.SemanticMerge {
				p = preferencePayload("always", false)
				v = memy.Interval{Known: false, From: time.Time{}, To: time.Time{}}
			}
			s := f.suggestion(p, v)
			return memy.MergeResult[Preference, string]{
				Suggestions: []memy.Suggestion[Preference, string]{s},
				CostUnits:   1,
				Utility:     1,
			}, nil
		},
	)
}

func (f *preferenceFixture) preferenceProposal(
	ctx context.Context,
	originals []memy.Record[Preference, string],
	valid memy.Interval,
	mode memy.ConsolidationMode,
	budget memy.Budget,
) ([]memy.Proposal[Preference, string], []memy.RevisionRef, error) {
	inputs := make([]memy.RevisionRef, len(originals))
	for i, r := range originals {
		inputs[i] = memy.RevisionRef{RecordID: r.ID, Revision: r.Revision}
	}
	provider := f.preferenceMergeProvider(mode, valid)
	proposals, err := memy.Consolidate(
		ctx,
		f.engine,
		"host",
		f.scope,
		memy.ConsolidationRequest{
			OperationID:    "consolidate",
			Purpose:        preferencePurpose,
			PolicyVersion:  "consolidation/v2",
			Mode:           mode,
			Inputs:         inputs,
			Budget:         budget,
			MinimumUtility: 0,
		},
		provider,
	)
	if err != nil {
		return nil, nil, err
	}
	return proposals, inputs, nil
}

func (f *preferenceFixture) reviewPreference(
	ctx context.Context,
	out *preferenceRunResult,
	proposal memy.Proposal[Preference, string],
	originals []memy.Record[Preference, string],
	mode memy.ConsolidationMode,
	good bool,
) ([]memy.Record[Preference, string], []string, error) {
	if mode == memy.SemanticMerge {
		out.HostDecision = "reject"
		err := f.engine.Reject(ctx, "host", f.scope, proposal.ID, proposal.Digest, preferencePurpose)
		out.check(stageHostReview, "explicit_semantic_rejection", err == nil && !good, 1)
		ids := make([]string, len(originals))
		for i, record := range originals {
			ids[i] = record.ID
		}
		return slices.Clone(originals), ids, err
	}
	out.HostDecision = "accept"
	applied, err := f.apply(ctx, preferenceDerivedID, proposal, 0, memy.Append, nil)
	if err != nil {
		return nil, nil, err
	}
	out.check(
		stageHostReview,
		"explicit_apply_checked_receipt",
		good && applied.ID == preferenceDerivedID && applied.Revision == 1,
		1,
	)
	want := []memy.Record[Preference, string]{applied}
	selected := []string{preferenceDerivedID}
	if mode == memy.ExactDedup {
		selected = append(selected, preferenceNegativeID)
		for _, record := range originals {
			if record.ID == preferenceNegativeID {
				want = append(want, record)
			}
		}
	}
	return want, selected, nil
}
