package quality

import (
	"context"
	"errors"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/skosovsky/memy"
)

func preferenceSessions(
	ctx context.Context,
	seed uint64,
	limit, maxCandidates int,
	bytes uint64,
) (preferenceRunResult, error) {
	var out preferenceRunResult
	f, err := newPreferenceFixture(ctx, true, time.Time{})
	if err != nil {
		return out, err
	}
	defer f.close()
	known := memy.Interval{Known: true, From: f.clock.Now().Add(-preferenceDay), To: f.clock.Now().Add(preferenceDay)}
	old, err := f.commit(ctx, "corrected", preferencePayload(preferenceMorning, false), known, 0, memy.Append, nil)
	if err != nil {
		return out, err
	}
	f.clock.Advance(time.Hour)
	corrected, err := f.commit(
		ctx,
		"corrected",
		preferencePayload(preferenceEvening, true),
		known,
		1,
		memy.Supersede,
		[]memy.RevisionRef{{RecordID: old.ID, Revision: old.Revision}},
	)
	if err != nil {
		return out, err
	}
	// A fresh host engine carries no session transcript or prior Recall result.
	fresh, err := memy.New(f.config)
	if err != nil {
		return out, err
	}
	f.engine = fresh
	items := []string{preferenceUnknownID, preferenceConflictID}
	shufflePreference(seed, preferenceSessionsSalt, items)
	out.Variation = "insertion/" + strings.Join(items, "_")
	unknown, conflict, err := f.insertSessionAlternatives(ctx, items, known)
	if err != nil {
		return out, err
	}
	records, body, err := f.recall(
		ctx,
		[]string{"corrected"},
		preferenceReadOptions(time.Time{}, time.Time{}, false, false),
		limit,
		maxCandidates,
		bytes,
	)
	if err != nil {
		return out, err
	}
	preferenceDelivered(&out, records, []memy.Record[Preference, string]{corrected}, body)
	if readErr := f.preferenceSessionReads(ctx, &out, unknown, conflict, limit, maxCandidates, bytes); readErr != nil {
		return out, readErr
	}
	history, err := fullSnapshot(ctx, f.engine,
		"host",
		f.scope,
		preferenceReadOptions(time.Time{}, old.RecordedAt, true, true),
	)
	if err != nil {
		return out, err
	}
	out.check(
		stageCanonical,
		"historical_original_exact_revision",
		slices.ContainsFunc(history, func(r memy.Record[Preference, string]) bool { return preferenceSame(r, old) }),
		len(history),
	)
	out.check(
		stageCanonical,
		"scope_and_sources",
		slices.Equal(corrected.Provenance.Sources, []memy.Source[string]{f.source}) && corrected.Scope == f.scope,
		1,
	)
	return out, nil
}

// shufflePreference varies fixture event order reproducibly; it creates no security token.
func shufflePreference(seed, salt uint64, values []string) {
	//nolint:gosec // Deterministic fixture scheduling requires a reproducible PRNG, not cryptographic randomness.
	rng := rand.New(rand.NewPCG(seed, salt^seed))
	rng.Shuffle(len(values), func(i, j int) { values[i], values[j] = values[j], values[i] })
}

func (f *preferenceFixture) insertSessionAlternatives(
	ctx context.Context,
	items []string,
	known memy.Interval,
) (memy.Record[Preference, string], memy.Record[Preference, string], error) {
	var unknown, conflict memy.Record[Preference, string]
	var err error
	for _, id := range items {
		switch id {
		case preferenceUnknownID:
			unknown, err = f.commit(
				ctx,
				id,
				preferencePayload("unobserved", false),
				memy.Interval{Known: false, From: time.Time{}, To: time.Time{}},
				0,
				memy.Append,
				nil,
			)
		case preferenceConflictID:
			_, err = f.commit(ctx, id, preferencePayload(preferenceMorning, false), known, 0, memy.Append, nil)
			if err == nil {
				f.clock.Advance(time.Hour)
				conflict, err = f.commit(
					ctx,
					id,
					preferencePayload(preferenceEvening, false),
					known,
					1,
					memy.Conflict,
					[]memy.RevisionRef{{RecordID: id, Revision: 1}},
				)
			}
		}
		if err != nil {
			return unknown, conflict, err
		}
	}
	return unknown, conflict, nil
}

func (f *preferenceFixture) preferenceSessionReads(
	ctx context.Context,
	out *preferenceRunResult,
	unknown, conflict memy.Record[Preference, string],
	limit, maxCandidates int,
	bytes uint64,
) error {
	unknownHidden, _, err := f.recall(
		ctx,
		[]string{preferenceUnknownID},
		preferenceReadOptions(f.clock.Now(), time.Time{}, false, false),
		limit,
		maxCandidates,
		bytes,
	)
	if err != nil {
		return err
	}
	unknownShown, _, err := f.recall(
		ctx,
		[]string{preferenceUnknownID},
		preferenceReadOptions(f.clock.Now(), time.Time{}, true, false),
		limit,
		maxCandidates,
		bytes,
	)
	if err != nil {
		return err
	}
	out.check(
		stageEffective,
		"unknown_read_options",
		len(unknownHidden) == 0 && preferenceSetSame(unknownShown, []memy.Record[Preference, string]{unknown}),
		len(unknownShown),
	)
	hidden, _, err := f.recallUsing(
		ctx,
		[]string{preferenceConflictID},
		preferenceReadOptions(time.Time{}, time.Time{}, false, false),
		limit,
		maxCandidates,
		bytes,
		preferenceConflictSearch{Scope: f.scope},
	)
	if err != nil {
		return err
	}
	shown, _, err := f.recallUsing(
		ctx,
		[]string{preferenceConflictID},
		preferenceReadOptions(time.Time{}, time.Time{}, false, true),
		limit,
		maxCandidates,
		bytes,
		preferenceConflictSearch{Scope: f.scope},
	)
	if err != nil {
		return err
	}
	out.check(
		stageEffective,
		"conflict_read_options",
		len(hidden) == 0 && len(shown) == 2 && slices.ContainsFunc(shown, func(r memy.Record[Preference, string]) bool {
			return preferenceSame(r, conflict) && r.State == memy.Conflicted
		}),
		len(shown),
	)
	_, conflictErr := f.engine.Get(
		ctx,
		"host",
		f.scope,
		preferenceConflictID,
		preferenceReadOptions(time.Time{}, time.Time{}, false, true),
	)
	out.check(stageEffective, "conflict_get_abstains", errors.Is(conflictErr, memy.ErrUnresolvedConflict), 1)
	return nil
}
