package quality

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/skosovsky/memy"
)

func preferenceTemporal(
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
	t0 := f.clock.Now()
	interval := memy.Interval{Known: true, From: t0.Add(-time.Hour), To: t0.Add(time.Hour)}
	ids := []string{preferencePastID, "future"}
	shufflePreference(seed, preferenceTemporalSalt, ids)
	out.Variation = "insertion/" + strings.Join(ids, "_")
	var past, future memy.Record[Preference, string]
	for _, id := range ids {
		if id == preferencePastID {
			past, err = f.commit(ctx, id, preferencePayload(preferencePastID, false), interval, 0, memy.Append, nil)
		} else {
			future, err = f.commit(
				ctx,
				id,
				preferencePayload("future", false),
				memy.Interval{Known: true, From: t0.Add(2 * time.Hour), To: t0.Add(preferenceFutureEnd)},
				0,
				memy.Append,
				nil,
			)
		}
		if err != nil {
			return out, err
		}
	}
	records, body, err := f.recall(
		ctx,
		ids,
		preferenceReadOptions(t0, time.Time{}, false, false),
		limit,
		maxCandidates,
		bytes,
	)
	if err != nil {
		return out, err
	}
	preferenceDelivered(&out, records, []memy.Record[Preference, string]{past}, body)
	later, _, err := f.recall(
		ctx,
		ids,
		preferenceReadOptions(t0.Add(preferenceFutureReadOffset), time.Time{}, false, false),
		limit,
		maxCandidates,
		bytes,
	)
	if err != nil {
		return out, err
	}
	out.check(
		stageEffective,
		"valid_time_selects_distinct_fact",
		preferenceSetSame(later, []memy.Record[Preference, string]{future}),
		len(later),
	)
	if correctionErr := f.preferenceTemporalCorrection(
		ctx,
		&out,
		past,
		t0,
		interval,
		limit,
		maxCandidates,
		bytes,
	); correctionErr != nil {
		return out, correctionErr
	}
	if revokeErr := f.preferenceTemporalRevoke(ctx, &out, t0, limit, maxCandidates, bytes); revokeErr != nil {
		return out, revokeErr
	}
	err = preferenceRetention(ctx, &out, t0, interval, limit, maxCandidates, bytes)
	return out, err
}

func preferenceRetention(
	ctx context.Context,
	out *preferenceRunResult,
	t0 time.Time,
	interval memy.Interval,
	limit, maxCandidates int,
	bytes uint64,
) error {
	expiry := t0.Add(time.Hour)
	expiring, err := newPreferenceFixture(ctx, false, expiry)
	if err != nil {
		return err
	}
	defer expiring.close()
	_, err = expiring.commit(ctx, "retained", preferencePayload("retained", false), interval, 0, memy.Append, nil)
	if err != nil {
		return err
	}
	expiring.clock.Set(t0.Add(2 * time.Hour))
	expired, _, err := expiring.recall(
		ctx,
		[]string{"retained"},
		preferenceReadOptions(t0, t0, false, false),
		limit,
		maxCandidates,
		bytes,
	)
	if err != nil {
		return err
	}
	_, expiryErr := expiring.engine.Get(
		ctx,
		"host",
		expiring.scope,
		"retained",
		preferenceReadOptions(t0, t0, false, false),
	)
	expiredCanonical, snapshotErr := fullSnapshot(
		ctx,
		expiring.engine,
		"host",
		expiring.scope,
		preferenceReadOptions(t0, t0, false, false),
	)
	if snapshotErr != nil {
		return snapshotErr
	}
	out.check(
		stageCanonical,
		"historical_read_obeys_current_retention",
		len(expired) == 0 && len(expiredCanonical) == 0 && errors.Is(expiryErr, memy.ErrNotFound),
		len(expired),
	)
	return nil
}

func (f *preferenceFixture) preferenceTemporalCorrection(
	ctx context.Context,
	out *preferenceRunResult,
	past memy.Record[Preference, string],
	t0 time.Time,
	interval memy.Interval,
	limit, maxCandidates int,
	bytes uint64,
) error {
	f.clock.Advance(preferenceCorrectionDelay)
	correction, err := f.commit(
		ctx,
		preferencePastID,
		preferencePayload("corrected", true),
		interval,
		1,
		memy.Supersede,
		[]memy.RevisionRef{{RecordID: past.ID, Revision: past.Revision}},
	)
	if err != nil {
		return err
	}
	historic, _, err := f.recall(
		ctx,
		[]string{preferencePastID},
		preferenceReadOptions(t0, t0, false, false),
		limit,
		maxCandidates,
		bytes,
	)
	if err != nil {
		return err
	}
	current, _, err := f.recall(
		ctx,
		[]string{preferencePastID},
		preferenceReadOptions(t0, time.Time{}, false, false),
		limit,
		maxCandidates,
		bytes,
	)
	if err != nil {
		return err
	}
	out.check(
		stageEffective,
		"recorded_time_selects_original_revision",
		preferenceSetSame(historic, []memy.Record[Preference, string]{past}) &&
			preferenceSetSame(current, []memy.Record[Preference, string]{correction}),
		len(historic),
	)
	return nil
}

func (f *preferenceFixture) preferenceTemporalRevoke(
	ctx context.Context,
	out *preferenceRunResult,
	t0 time.Time,
	limit, maxCandidates int,
	bytes uint64,
) error {
	_, err := fullForget(ctx, f.engine,
		"host",
		f.scope,
		preferencePurpose,
		memy.ForgetRequest{
			OperationID:   "temporal-forget",
			Selector:      memy.Selector{Kind: memy.SelectRecord, ID: preferencePastID},
			Reason:        "fixture privacy event",
			PolicyVersion: "forget/v1",
			Expected:      nil, Limit: 0, MaxBytes: 0},
	)
	if err != nil {
		return err
	}
	revoked, _, err := f.recall(
		ctx,
		[]string{preferencePastID},
		preferenceReadOptions(t0, t0, false, false),
		limit,
		maxCandidates,
		bytes,
	)
	if err != nil {
		return err
	}
	_, revokeErr := f.engine.Get(
		ctx,
		"host",
		f.scope,
		preferencePastID,
		preferenceReadOptions(t0, t0, false, false),
	)
	revokedCanonical, snapshotErr := fullSnapshot(
		ctx,
		f.engine,
		"host",
		f.scope,
		preferenceReadOptions(t0, t0, false, false),
	)
	if snapshotErr != nil {
		return snapshotErr
	}
	out.check(
		stageCanonical,
		"historical_read_obeys_current_revoke",
		len(revoked) == 0 && errors.Is(revokeErr, memy.ErrNotFound) &&
			!slices.ContainsFunc(
				revokedCanonical,
				func(r memy.Record[Preference, string]) bool { return r.ID == preferencePastID },
			),
		len(revoked),
	)
	return nil
}
