package quality

import (
	"context"
	"encoding/json"
	"github.com/skosovsky/memy"
	"strings"
	"testing"
	"time"
)

func TestPreferencePlansPublicLifecycle(t *testing.T) {
	for _, plan := range preferencePlans() {
		t.Run(plan.ID, func(t *testing.T) {
			// Arrange: independent engine/store per plan and real seed-dependent ordering.
			corpus := Corpus{Budgets: Budgets{MaxCandidates: 100, RecallLimit: 100, ContextBytes: 20000, InputBytes: 10000, OutputBytes: 10000, CostUnits: 5}}
			// Act.
			report, err := plan.Run(context.Background(), corpus, CaseRun{Repeat: 0, Seed: 23})
			enforcePlan(&report, plan)
			FinalizeScenario(&report)
			// Assert: acceptance is not used as the final protocol verdict.
			if err != nil || report.Final != VerdictPass {
				t.Fatalf("run error=%v report=%+v", err, report)
			}
			if plan.ID == "preference-consolidation-semantic" && report.Candidate.Status != Fail {
				t.Fatal("bad semantic candidate was hidden")
			}
			if report.Metrics.ContextJSONBytes.Value == nil || report.Metrics.PayloadBytes.Value == nil || *report.Metrics.ContextJSONBytes.Value <= *report.Metrics.PayloadBytes.Value {
				t.Fatal("payload and final context measurements conflated")
			}
			if report.Metrics.CanonicalBytes.Status != "unavailable" || report.Metrics.RealProviderCost.Status != "unavailable" {
				t.Fatal("unmeasured storage/provider cost claimed")
			}
		})
	}
}
func TestPreferenceReportsDeterministicAndSafe(t *testing.T) {
	for _, plan := range preferencePlans() {
		t.Run(plan.ID, func(t *testing.T) {
			// Arrange.
			corpus := Corpus{Budgets: Budgets{MaxCandidates: 100, RecallLimit: 100, ContextBytes: 20000, InputBytes: 10000, OutputBytes: 10000, CostUnits: 5}}
			run := CaseRun{Seed: 42}
			// Act.
			a, err := plan.Run(t.Context(), corpus, run)
			if err != nil {
				t.Fatal(err)
			}
			b, err := plan.Run(t.Context(), corpus, run)
			if err != nil {
				t.Fatal(err)
			}
			ra, _ := json.Marshal(a)
			rb, _ := json.Marshal(b)
			// Assert: report evidence has no raw domain fact/reference, even on semantic rejection.
			if string(ra) != string(rb) {
				t.Fatal("same seed did not reproduce report")
			}
			for _, forbidden := range []string{"morning", "evening", "synthetic-reference", "fixture-source"} {
				if strings.Contains(string(ra), forbidden) {
					t.Fatalf("report disclosed fixture data %q", forbidden)
				}
			}
		})
	}
}
func TestPreferenceErrorDoesNotCertifyZeroMeasurements(t *testing.T) {
	// Arrange.
	plan := preferencePlans()[0]
	corpus := Corpus{Budgets: Budgets{MaxCandidates: 100, RecallLimit: 100, ContextBytes: 1, InputBytes: 10000, OutputBytes: 10000, CostUnits: 5}}
	// Act.
	report, err := plan.Run(t.Context(), corpus, CaseRun{Seed: 1})
	// Assert.
	if err == nil || report.Metrics.ContextJSONBytes.Status != "unavailable" || report.Metrics.ContextJSONBytes.Value != nil {
		t.Fatal("failed packing presented as measured zero")
	}
}

func TestPreferenceOracleRejectsSameCountWrongObservation(t *testing.T) {
	// Arrange: exact expected facts are fixture-owned, independent of count.
	expected := memy.Record[Preference, string]{ID: "expected", Revision: 2, Payload: preferencePayload("evening", true), Scope: memy.Scope{Tenant: "fixture", Namespace: "preference", Subject: "subject"}, Valid: memy.Interval{Known: true, From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}, Provenance: memy.Provenance[string]{Sources: []memy.Source[string]{{ID: "source", Revision: "r1", Reference: "synthetic"}}, Lineage: []memy.RevisionRef{{RecordID: "input", Revision: 1}}}}
	for _, fault := range []string{"identity", "revision", "payload", "negation", "validity", "source", "lineage", "scope"} {
		t.Run(fault, func(t *testing.T) {
			// Arrange.
			got := expected
			switch fault {
			case "identity":
				got.ID = "other"
			case "revision":
				got.Revision = 1
			case "payload":
				got.Payload = preferencePayload("other", true)
			case "negation":
				got.Payload = preferencePayload("evening", false)
			case "validity":
				got.Valid = memy.Interval{}
			case "source":
				got.Provenance.Sources = nil
			case "lineage":
				got.Provenance.Lineage = nil
			case "scope":
				got.Scope.Subject = "foreign"
			}
			// Act.
			matched := preferenceSetSame([]memy.Record[Preference, string]{got}, []memy.Record[Preference, string]{expected})
			// Assert.
			if matched {
				t.Fatal("same record count masked incorrect fact")
			}
		})
	}
}
func TestPreferenceSeedChangesActualInsertionOrder(t *testing.T) {
	// Arrange.
	plan := preferencePlans()[3]
	corpus := Corpus{Budgets: Budgets{MaxCandidates: 100, RecallLimit: 100, ContextBytes: 20000, InputBytes: 10000, OutputBytes: 10000, CostUnits: 5}}
	variations := map[string]bool{}
	// Act: changes originate in event ordering recorded by the executed fixture.
	for seed := uint64(1); seed <= 6; seed++ {
		report, err := plan.Run(t.Context(), corpus, CaseRun{Seed: seed})
		if err != nil {
			t.Fatal(err)
		}
		variations[report.Variation] = true
	}
	// Assert.
	if len(variations) < 2 {
		t.Fatal("seed was decorative")
	}
}
