package quality

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestEvaluatorInsufficientBaselineDoesNotRunMutations(t *testing.T) {
	for _, budget := range []uint64{1, 200, 500, 600, 700, 800, 900, 1000, 1100, 1200, 1300} {
		t.Run(strconv.FormatUint(budget, 10), func(t *testing.T) {
			// Arrange: a valid candidate budget and a valid context budget too small
			// for this fixture's full real projection envelope.
			c := DefaultCorpus()
			c.Budgets.ContextBytes = budget
			c.Budgets.MaxCandidates = 1
			c.Budgets.RecallLimit = 1
			if err := validateCorpus(c); err != nil {
				t.Fatal(err)
			}
			// Act: both direct evaluator and the CLI seam preserve safe reports.
			s, err := runEvaluator(t.Context(), c, CaseRun{Seed: 2})
			if err != nil {
				t.Fatal(err)
			}
			r, err := RunProbe(t.Context(), c, "wrong_payload")
			// Assert: no fabricated successful probe or lost baseline evidence.
			if err != nil {
				t.Fatal(err)
			}
			assertEvaluatorBaseline(t, budget, s, r)
		})
	}
}

func TestEvaluatorMutationsRejectWrongObservations(t *testing.T) {
	// Arrange: seeded input variation, a real engine, and the independent oracle.
	c := DefaultCorpus()
	// Act: each probe mutates a detached observation after successful host review.
	s, err := runEvaluator(t.Context(), c, CaseRun{Seed: 19})
	// Assert: no count-only or accepted-candidate shortcut can accept any probe.
	if err != nil {
		t.Fatal(err)
	}
	if s.Final != VerdictPass || len(s.Probes) != len(evaluatorProbeIDs()) {
		t.Fatalf("self-check: %s, probes %d", s.Final, len(s.Probes))
	}
	for _, id := range evaluatorProbeIDs() {
		t.Run(id, func(t *testing.T) {
			probe := findEvaluatorProbe(t, s.Probes, id)
			assertEvaluatorProbe(t, id, probe)
		})
	}
}

func TestEvaluatorDiagnosticsDoNotSerializePrivateMarkers(t *testing.T) {
	// Arrange: a marker that appears in both private payload and canonical IDs.
	const marker = "private-secret-marker-9f371"
	c := DefaultCorpus()
	// Act: serialize the complete parent and all nested failures.
	s, err := runEvaluatorWithMarker(t.Context(), c, CaseRun{Seed: 2}, marker)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	// Assert: reports expose fixed codes and measured counts, never secret input.
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), marker) || strings.Contains(string(raw), "variant-a") ||
		strings.Contains(string(raw), "synthetic-reference") {
		t.Fatal("private fixture data leaked into report")
	}
	if s.Final != VerdictPass {
		t.Fatalf("marker altered self-check: %s", s.Final)
	}
}

func TestRunProbeUsesNormalNegativeAggregation(t *testing.T) {
	// Arrange.
	c := DefaultCorpus()
	for _, tc := range []struct {
		id   string
		exit int
	}{{"wrong_payload", 1}, {"checker_error", 2}, {"not-registered", 2}} {
		t.Run(tc.id, func(t *testing.T) {
			// Act.
			r, err := RunProbe(t.Context(), c, tc.id)
			// Assert.
			if r.ExitCode() != tc.exit {
				t.Fatalf("exit %d", r.ExitCode())
			}
			if tc.id == "not-registered" && err == nil {
				t.Fatal("unknown mutation accepted")
			}
			if tc.id != "not-registered" && (err != nil || len(r.Scenarios) != 1 || r.Scenarios[0].ID != tc.id) {
				t.Fatal("negative scenario not preserved")
			}
		})
	}
}

func TestEvaluatorSeedChangesRealInputAndMutationIsDetached(t *testing.T) {
	// Arrange: distinct input variants under one stable report variation version.
	c := DefaultCorpus()
	// Act.
	a, err := runEvaluator(t.Context(), c, CaseRun{Seed: 2})
	if err != nil {
		t.Fatal(err)
	}
	b, err := runEvaluator(t.Context(), c, CaseRun{Seed: 3})
	if err != nil {
		t.Fatal(err)
	}
	// Assert: both real variants pass all baseline exact-content oracles; detached
	// probes cannot alter baseline gates even after mutation and privacy events.
	if a.Final != VerdictPass || b.Final != VerdictPass || a.Variation != "payload-parity/v1" ||
		b.Variation != a.Variation {
		t.Fatal("seeded runs inconsistent")
	}
	if !reflect.DeepEqual(a.Candidate, b.Candidate) || a.Effective.Status != Pass || b.Effective.Status != Pass {
		t.Fatal("mutation contaminated baseline")
	}
	if *a.Metrics.PayloadBytes.Value == *b.Metrics.PayloadBytes.Value {
		t.Fatal("seed did not alter the actual measured fixture payload")
	}
}

func assertEvaluatorBaseline(t *testing.T, budget uint64, s ScenarioReport, r Report) {
	t.Helper()
	if s.Final == VerdictPass {
		// At the upper end this real envelope fits. The self-check may pass,
		// but the requested negative probe still has its normal failed exit.
		if budget <= 1000 || len(s.Probes) != len(evaluatorProbeIDs()) || r.ExitCode() != 1 {
			t.Fatal("small budget bypassed readiness")
		}
		return
	}
	if s.Final == VerdictPass || len(s.Probes) != 0 || s.Diagnostic != "baseline_not_ready" ||
		r.ExitCode() == 0 ||
		len(r.Scenarios) != 1 {
		t.Fatal("insufficient baseline accepted")
	}
	if s.Candidate.Status != Pass || s.HostReview.Status != Pass {
		t.Fatal("completed earlier checkpoints lost")
	}
	if s.Rendered.Status != Fail {
		t.Fatal("real missing projection not reported")
	}
	assertEvaluatorBaselineMeasurement(t, budget, s.Metrics.ContextJSONBytes)
	missing := 0
	for _, check := range s.Execution.Checks {
		if strings.HasPrefix(check.ID, "detected_") {
			if check.Status != Unknown || check.Observed != "not_attempted" {
				t.Fatal("unattempted probe fabricated")
			}
			missing++
		}
	}
	if missing != len(evaluatorProbeIDs()) {
		t.Fatal("missing mandatory probe evidence")
	}
	if _, err := json.Marshal(r); err != nil {
		t.Fatal(err)
	}
}

func findEvaluatorProbe(t *testing.T, probes []ScenarioReport, id string) *ScenarioReport {
	t.Helper()
	var probe *ScenarioReport
	for i := range probes {
		if probes[i].ID == id {
			probe = &probes[i]
			break
		}
	}
	if probe == nil {
		t.Fatal("probe missing")
	}
	return probe
}

func assertEvaluatorProbe(t *testing.T, id string, probe *ScenarioReport) {
	t.Helper()

	r := Report{Scenarios: []ScenarioReport{*probe}}
	FinalizeReport(&r)
	wantExit := 1
	if id == "checker_error" || id == "adapter_error" {
		wantExit = 2
	}
	if r.ExitCode() != wantExit || probe.Final == VerdictPass {
		t.Fatalf("negative classified %s exit %d", probe.Final, r.ExitCode())
	}
	if probe.Candidate.Status != Pass || probe.HostReview.Status != Pass {
		t.Fatal("prior successful checkpoints overwritten")
	}
	assertEvaluatorMutationContent(t, id, probe)
}

func assertEvaluatorMutationContent(t *testing.T, id string, probe *ScenarioReport) {
	t.Helper()

	if id == "wrong_payload" || id == "wrong_revision" || id == "missing_lineage" || id == "late_privacy" {
		if probe.Effective.Status != Fail || probe.Rendered.Status != Fail {
			t.Fatal("common exact-record/rendered checker missed mutation")
		}
	}
	if probe.Canonical.Status != Pass {
		t.Fatal("mutation changed original canonical oracle")
	}
	if id == "wrong_payload" || id == "wrong_revision" || id == "missing_lineage" {
		if probe.Effective.Checks[0].Evidence[0].Count != 1 {
			t.Fatal("mutation changed count instead of content")
		}
	}
	if id == "false_empty_outage" && probe.Effective.Checks[1].Status != Fail {
		t.Fatal("independent unavailable evidence was hidden by healthy-empty observation")
	}
	if id == "adapter_error" &&
		(probe.Metrics.ContextJSONBytes.Status != "known" || probe.Metrics.ContextJSONBytes.Value == nil) {
		t.Fatal("later adapter error erased genuine prior context measurement")
	}
}

func assertEvaluatorBaselineMeasurement(t *testing.T, budget uint64, measurement Measurement) {
	t.Helper()
	if budget == 1 || budget == 200 {
		if measurement.Status != "unavailable" ||
			measurement.Reason != "execution_incomplete" ||
			measurement.Value != nil {
			t.Fatal("undelivered context measured as marshaled zero body")
		}
	} else if measurement.Status != "known" {
		t.Fatal("real delivered metadata-only context measurement lost")
	}
}
