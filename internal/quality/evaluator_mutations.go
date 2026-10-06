package quality

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/skosovsky/memy"
)

func appendUnattemptedProbes(parentReport *ScenarioReport) {
	for _, id := range evaluatorProbeIDs() {
		parentReport.Execution.Checks = append(
			parentReport.Execution.Checks,
			Check{
				ID:        "detected_" + id,
				Mandatory: true,
				Status:    Unknown,
				Expected:  "negative_observed",
				Observed:  "not_attempted",
				Evidence:  []Evidence{{Code: "probe_not_attempted", Count: 0, Aliases: nil}},
			},
		)
	}
}

func mutateEvaluatorObservation(
	ctx context.Context,
	f *preferenceFixture,
	fresh *memy.Engine[Preference, string, string],
	c Corpus,
	id, recordID string,
	obs *evaluatorObservation,
	expected *evaluatorOracle,
) error {
	switch id {
	case "wrong_payload":
		obs.Records[0].Payload = preferencePayload("wrong", false)
		obs.Body.Projections[0].Output = preferencePayload("wrong", false)
	case "wrong_revision":
		obs.Records[0].Revision++
		obs.Body.Projections[0].Revision++
	case "missing_lineage":
		obs.Records[0].Provenance.Lineage = nil
		obs.Body.Projections[0].Provenance.Lineage = nil
	case probeFalseEmptyOutage:
		failure := evaluatorOutage(ctx, f, c, recordID)
		if !errors.Is(failure, memy.ErrUnavailable) {
			return memy.ErrSchema
		}
		// A faulty evaluator erases independent outage evidence into healthy empty.
		expected.Available = false
		expected.Records = nil
		obs.Available = true
		obs.Records = nil
		obs.Body.Projections = nil
	case "checker_error":
		obs.CheckerKnown = false
	case probeAdapterError:
		obs.AdapterError = evaluatorOutage(ctx, f, c, recordID)
		if !errors.Is(obs.AdapterError, memy.ErrUnavailable) {
			return memy.ErrSchema
		}
	case "late_privacy":
		_, err := fullForget(
			ctx,
			f.engine,
			"host",
			f.scope,
			qualityPurpose,
			memy.ForgetRequest{
				OperationID:   "evaluator-forget",
				Selector:      memy.Selector{Kind: memy.SelectRecord, ID: recordID},
				Reason:        "synthetic privacy event",
				PolicyVersion: "forget/v1",
				Expected:      nil, Limit: 0, MaxBytes: 0},
		)
		if err != nil {
			return err
		}
		post, err := fullSnapshot(
			ctx,
			fresh,
			"host",
			f.scope,
			memy.ReadOptions{
				Purpose:          qualityPurpose,
				ValidAsOf:        time.Time{},
				RecordedAsOf:     time.Time{},
				IncludeUnknown:   false,
				IncludeConflicts: false,
			},
		)
		if err != nil {
			return err
		}
		obs.Canonical = slices.DeleteFunc(
			post,
			func(r memy.Record[Preference, string]) bool { return r.ID != recordID },
		)
		expected.Records = nil
		expected.Canonical = nil
		// The leaked observations deliberately retain the previously accepted body.
	}
	return nil
}

func runEvaluatorMutations(
	ctx context.Context,
	f *preferenceFixture,
	fresh *memy.Engine[Preference, string, string],
	c Corpus,
	run CaseRun,
	recordID string,
	baseline evaluatorObservation,
	oracle evaluatorOracle,
	parentReport *ScenarioReport,
) error {
	for _, id := range evaluatorProbeIDs() {
		obs := cloneEvaluatorObservation(baseline)
		expected := oracle
		if err := mutateEvaluatorObservation(ctx, f, fresh, c, id, recordID, &obs, &expected); err != nil {
			return err
		}
		// Keep the exact byte receipt consistent with mutated rendered bytes: a
		// semantic mutation must be rejected by the content oracle, not merely by
		// an accidentally stale cost receipt left over from the baseline.
		mutatedJSON, marshalErr := json.Marshal(obs.Body)
		if marshalErr != nil {
			return marshalErr
		}
		obs.Body.Budget.Used = uint64(len(mutatedJSON))
		probe := evaluatorEvaluate(id, run, obs, expected)
		if id == probeFalseEmptyOutage || id == probeAdapterError {
			probe.Versions.Search = "evaluator-unavailable/v1"
		}
		probe.Candidate.Checks = slices.Clone(parentReport.Candidate.Checks)
		probe.HostReview.Checks = slices.Clone(parentReport.HostReview.Checks)
		FinalizeScenario(&probe)
		parentReport.Probes = append(parentReport.Probes, probe)
		negative := Report{
			Scenarios: []ScenarioReport{probe},
			Schema:    "",
			Manifest:  nil,
			Versions: PortVersions{
				Provider:      "",
				Model:         "",
				HostReview:    "",
				Retention:     "",
				Resolver:      "",
				Consolidation: "",
				Search:        "",
				Projector:     "",
				Packing:       "",
				Grader:        "",
			},
			Final:      "",
			Diagnostic: "",
		}
		FinalizeReport(&negative)
		parentReport.Execution.Checks = append(
			parentReport.Execution.Checks,
			evaluatorCheck("detected_"+id, probe.Final != VerdictPass && negative.ExitCode() != 0, 1),
		)
	}
	return nil
}
