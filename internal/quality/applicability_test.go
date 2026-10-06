package quality

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

func TestProcedureEveryDeliveredContextUsesConfiguredCap(t *testing.T) {
	for _, plan := range procedurePlans() {
		for _, mode := range []string{fixtureModeScripted, "integration"} {
			t.Run(plan.ID+"/"+mode, func(t *testing.T) {
				// Arrange: the cap cannot hold even a healthy empty body with metadata.
				c := DefaultCorpus()
				c.Budgets.ContextBytes = 1
				ports := DefaultProcedurePorts()
				ports.Mode = mode
				// Act: both ordinary plans and the typed integration entry point must enforce it.
				report, err := RunProcedureCase(t.Context(), plan.ID, c, CaseRun{Seed: c.Seed}, ports)
				FinalizeScenario(&report)
				// Assert: no scenario credits an unbounded context as a successful delivery.
				if !errors.Is(err, memy.ErrBudget) || report.Final == VerdictPass {
					t.Fatalf("report=%+v err=%v", report, err)
				}
			})
		}
	}
	// Arrange: the ordinary registry runner receives the same valid one-byte manifest.
	c := DefaultCorpus()
	c.Repeats, c.Budgets.ContextBytes = 1, 1
	// Act.
	report, _ := Run(t.Context(), c)
	// Assert.
	for _, scenario := range report.Scenarios {
		if scenario.Domain == procedureDomain && scenario.Final == VerdictPass {
			t.Fatalf("unbounded pass: %+v", scenario)
		}
	}
}

func TestProcedureReportsMeasuredCapAndActualGraderUse(t *testing.T) {
	for _, id := range []string{procedureCrossScopeCase, procedureRetrievalCase, procedureGuardedCase, procedurePermissiveCase, procedureAbstentionCase} {
		t.Run(id, func(t *testing.T) {
			procedureApplicabilityCase(t, id)
		})
	}
}

func procedureApplicabilityCase(t *testing.T, id string) {
	t.Helper()
	// Arrange: configure a real grader; only retrieval actually invokes this port.
	c := DefaultCorpus()
	ports := DefaultProcedurePorts()
	calls := 0
	ports.GraderVersion = "test-grader/v1"
	ports.Grade = func(context.Context, []memy.Projection[ProcedureObservation, DocumentRef]) (bool, error) {
		calls++
		return true, nil
	}
	// Act.
	report, err := RunProcedureCase(t.Context(), id, c, CaseRun{Seed: c.Seed}, ports)
	FinalizeScenario(&report)
	// Assert: the measured body and configured cap are explicit, uncalled grader is not credited.
	if err != nil || report.Final != VerdictPass || report.Metrics.ContextJSONBytes.Value == nil ||
		*report.Metrics.ContextJSONBytes.Value > c.Budgets.ContextBytes {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	budget, grader := false, false
	for _, check := range report.Execution.Checks {
		switch check.ID {
		case "context-budget":
			size, parseErr := strconv.ParseUint(check.Observed, 10, 64)
			budget = parseErr == nil && size == *report.Metrics.ContextJSONBytes.Value &&
				check.Expected == "full-body-json-bytes<="+strconv.FormatUint(c.Budgets.ContextBytes, 10) &&
				check.Evidence[0].Count > 0
		case "configured-grader":
			expected := "not_applicable"
			if calls > 0 {
				expected = "executed"
			}
			grader = check.Observed == expected && check.Evidence[0].Count == calls
		}
	}
	if !budget || !grader {
		t.Fatalf("missing applicability: %+v", report.Execution)
	}
}

func TestProcedureValidationPrecedesProviderInvocation(t *testing.T) {
	for _, mutation := range []string{"unknown-case", "budget", "corpus", "repeat", "seed", "nil-grader-version", "grader-none-version"} {
		t.Run(mutation, func(t *testing.T) {
			// Arrange.
			c, ports, id, run := DefaultCorpus(), DefaultProcedurePorts(), procedureRetrievalCase, CaseRun{Seed: 7}
			calls := 0
			original := ports.Extractor
			ports.Extractor = reference.ExtractorFunc[ProcedureInput, ProcedureObservation, DocumentRef](
				func(ctx context.Context, in ProcedureInput) ([]memy.Suggestion[ProcedureObservation, DocumentRef], error) {
					calls++
					return original.Extract(ctx, in)
				},
			)
			switch mutation {
			case "unknown-case":
				id = "unknown"
			case "budget":
				c.Budgets.InputBytes = 0
			case "corpus":
				c.Version = "unknown"
			case "repeat":
				run.Repeat = c.Repeats
			case "seed":
				run.Seed = 0
			case "nil-grader-version":
				ports.GraderVersion = "configured/v1"
			case "grader-none-version":
				ports.Grade = func(context.Context, []memy.Projection[ProcedureObservation, DocumentRef]) (bool, error) {
					return true, nil
				}
			}
			// Act.
			_, err := RunProcedureCase(t.Context(), id, c, run, ports)
			// Assert.
			if !errors.Is(err, memy.ErrInvalid) || calls != 0 {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestMalformedMeasuredValueIsUnknownWithSafeDiagnostic(t *testing.T) {
	// Arrange: a supplied known measurement has no value, unlike a missing optional metric.
	s := completeScenario()
	s.Metrics.PayloadBytes = Measurement{Status: measurementKnown, Unit: "bytes"}
	// Act: finalization is idempotent and preserves the safe diagnostic.
	FinalizeScenario(&s)
	FinalizeScenario(&s)
	// Assert.
	if s.Final != VerdictUnknown || s.Diagnostic != "invalid_measurement" ||
		s.Metrics.PayloadBytes.Reason != "invalid_measurement" {
		t.Fatal(s)
	}
}

func TestKnownExecutionFailureUsesFailedExitCode(t *testing.T) {
	// Arrange: a fully observed invariant fails, without incomplete execution.
	s := completeScenario()
	s.Execution.Checks[0].Status = Fail
	r := Report{Scenarios: []ScenarioReport{s}}
	// Act.
	FinalizeReport(&r)
	// Assert.
	if r.Final != VerdictFail || r.ExitCode() != 1 {
		t.Fatal(r)
	}
}
