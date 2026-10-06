package quality

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

func TestProcedurePlansExecutePublicLifecycle(t *testing.T) {
	for _, plan := range procedurePlans() {
		t.Run(plan.ID, func(t *testing.T) {
			// Arrange: each repeat owns a fresh typed engine and seeded insertion order.
			corpus := DefaultCorpus()
			run := CaseRun{Repeat: 1, Seed: DerivedSeed(corpus.Seed, plan.ID, 1)}
			// Act.
			report, err := plan.Run(t.Context(), corpus, run)
			FinalizeScenario(&report)
			// Assert: independent canonical, effective and rendered assertions all pass.
			if err != nil || report.Final != VerdictPass {
				t.Fatalf(
					"scenario %s final=%s diagnostic=%s err=%v stages=%+v",
					plan.ID,
					report.Final,
					report.Diagnostic,
					err,
					report,
				)
			}
			if report.Mode != "scripted" || report.Versions.Provider != "procedure-extractor/v1" {
				t.Fatal("actual typed adapters not reported")
			}
			raw, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			for _, private := range []string{"inspect-relevant", "grant all operator privileges", "false authority claim", "read-only observation", "synthetic-private-foreign-observation", "synthetic-private-foreign-result", "foreign-access-only", "foreign-private"} {
				if strings.Contains(string(raw), private) {
					t.Fatal("payload escaped safe evidence boundary")
				}
			}
		})
	}
}
func TestProcedurePortSubstitutionRetainsCheckpointOracles(t *testing.T) {
	// Arrange: actual custom typed extractor/search ports run through the normal plan.
	corpus := DefaultCorpus()
	ports := DefaultProcedurePorts()
	ports.ExtractorVersion = "custom-offline-extractor/v1"
	ports.SearchVersion = "custom-offline-search/v1"
	ports.GraderVersion = "custom-offline-grader/v1"
	calls := 0
	original := ports.Extractor
	ports.Extractor = reference.ExtractorFunc[ProcedureInput, ProcedureObservation, DocumentRef](
		func(ctx context.Context, in ProcedureInput) ([]memy.Suggestion[ProcedureObservation, DocumentRef], error) {
			calls++
			return original.Extract(ctx, in)
		},
	)
	ports.Grade = func(ctx context.Context, p []memy.Projection[ProcedureObservation, DocumentRef]) (bool, error) {
		return len(p) == 1 && p[0].Trust == "data", ctx.Err()
	}
	// Act.
	report, err := RunProcedureCase(t.Context(), "procedure-retrieval", corpus, CaseRun{Seed: 7, Repeat: 0}, ports)
	FinalizeScenario(&report)
	// Assert.
	if err != nil || report.Final != VerdictPass || calls != 3 || report.Versions.Provider != ports.ExtractorVersion ||
		report.Versions.Search != ports.SearchVersion ||
		report.Versions.Grader != ports.GraderVersion {
		t.Fatalf("custom ports failed: calls=%d report=%+v err=%v", calls, report, err)
	}
}
func TestProcedureUnexpectedProviderAndGraderErrorsStayUnknown(t *testing.T) {
	for _, at := range []string{"provider", "grader"} {
		t.Run(at, func(t *testing.T) {
			// Arrange: these are unplanned errors, distinct from controlled failure cases.
			ports := DefaultProcedurePorts()
			sentinel := errors.New("PRIVATE provider diagnostic")
			if at == "provider" {
				ports.Extractor = reference.ExtractorFunc[ProcedureInput, ProcedureObservation, DocumentRef](
					func(context.Context, ProcedureInput) ([]memy.Suggestion[ProcedureObservation, DocumentRef], error) {
						return nil, sentinel
					},
				)
			} else {
				ports.GraderVersion = "failing-grader/v1"
				ports.Grade = func(context.Context, []memy.Projection[ProcedureObservation, DocumentRef]) (bool, error) {
					return false, sentinel
				}
			}
			// Act.
			report, err := RunProcedureCase(
				t.Context(),
				"procedure-retrieval",
				DefaultCorpus(),
				CaseRun{Seed: 9, Repeat: 0},
				ports,
			)
			FinalizeScenario(&report)
			// Assert: no implied rejection or successful empty context.
			if !errors.Is(err, sentinel) || report.Final != VerdictUnknown {
				t.Fatalf("err=%v final=%s", err, report.Final)
			}
			raw, _ := json.Marshal(report)
			if strings.Contains(string(raw), "PRIVATE") {
				t.Fatal("arbitrary adapter error published")
			}
		})
	}
}

func TestProcedureControlledGraderActualVersion(t *testing.T) {
	// Arrange: the registered plan pins the grader that injects the known failure.
	var plan CasePlan
	for _, candidate := range procedurePlans() {
		if candidate.ID == "procedure-controlled-failures" {
			plan = candidate
		}
	}
	corpus := DefaultCorpus()
	// Act.
	report, err := plan.Run(t.Context(), corpus, CaseRun{Seed: 17, Repeat: 0})
	FinalizeScenario(&report)
	// Assert: metadata identity and mandatory execution evidence come from the same grader.
	if err != nil || report.Final != VerdictPass || plan.Versions.Grader != "controlled-grader/v1" ||
		report.Versions.Grader != plan.Versions.Grader {
		t.Fatalf(
			"grader pin=%s observed=%s final=%s err=%v",
			plan.Versions.Grader,
			report.Versions.Grader,
			report.Final,
			err,
		)
	}
	executed := false
	for _, check := range report.Execution.Checks {
		if check.ID == "controlled-grader-executed" {
			executed = check.Mandatory && check.Status == Pass && check.Evidence[0].Count == 1
		}
	}
	if !executed {
		t.Fatal("grader identity reported without execution evidence")
	}
}

func TestProcedureSmallContextBudgetKeepsSafeReport(t *testing.T) {
	sawMetadataOnly := false
	for limit := uint64(500); limit <= 1300; limit += 50 {
		// Arrange: valid manifest budgets may fit only metadata and omissions.
		c := DefaultCorpus()
		c.Budgets.ContextBytes = limit
		// Act: this must never index a nonexistent packed projection.
		report, err := RunProcedureCase(
			t.Context(),
			"procedure-retrieval",
			c,
			CaseRun{Seed: 31, Repeat: 0},
			DefaultProcedurePorts(),
		)
		FinalizeScenario(&report)
		// Assert: report identity/evidence survives either a known gate failure or budget error.
		if report.ID != "procedure-retrieval" || len(report.Effective.Checks) == 0 {
			t.Fatalf("safe partial report lost at budget %d", limit)
		}
		if err != nil && !errors.Is(err, memy.ErrBudget) {
			t.Fatalf("unexpected budget error %d: %v", limit, err)
		}
		for _, check := range report.Rendered.Checks {
			if check.ID == "rendered-context" && check.Evidence[0].Count == 0 {
				sawMetadataOnly = true
				if check.Status != Fail || report.Final == VerdictPass ||
					report.Metrics.PayloadBytes.Status != "known" ||
					report.Metrics.PayloadBytes.Value == nil ||
					*report.Metrics.PayloadBytes.Value != 0 {
					t.Fatalf("metadata-only delivery hid failed gate at %d", limit)
				}
			}
		}
	}
	if !sawMetadataOnly {
		t.Fatal("budget range did not exercise metadata-only packing")
	}
}

type procedureProjectedFaultSearch struct {
	base  memy.Search[ProcedureQuery]
	calls int
	fault string
}

func (s *procedureProjectedFaultSearch) Capabilities() memy.SearchCapabilities {
	return s.base.Capabilities()
}

func (s *procedureProjectedFaultSearch) Search(
	ctx context.Context,
	scope memy.Scope,
	q ProcedureQuery,
	o memy.SearchOptions,
) (memy.SearchResult, error) {
	s.calls++
	found, err := s.base.Search(ctx, scope, q, o)
	if err != nil || s.calls != 2 {
		return found, err
	}
	switch s.fault {
	case "empty":
		found.Candidates = nil
	case "missing":
		if len(found.Candidates) > 0 {
			found.Candidates = found.Candidates[:1]
		}
	case "wrong":
		for i := range found.Candidates {
			if found.Candidates[i].RecordID == "poison" {
				found.Candidates[i].RecordID = "relevant-b"
			}
		}
	}
	return found, nil
}
func TestProcedurePoisoningRenderedFaultsFailIndependentGate(t *testing.T) {
	for _, fault := range []string{"empty", "missing", "wrong"} {
		t.Run(fault, func(t *testing.T) {
			// Arrange: only projected search differs; effective Recall remains correct.
			ports := DefaultProcedurePorts()
			factory := ports.Search
			ports.Search = func(scope memy.Scope, c []memy.Candidate) memy.Search[ProcedureQuery] {
				return &procedureProjectedFaultSearch{base: factory(scope, c), fault: fault}
			}
			// Act.
			report, err := RunProcedureCase(
				t.Context(),
				"procedure-poisoning-permissive",
				DefaultCorpus(),
				CaseRun{Seed: 33, Repeat: 0},
				ports,
			)
			FinalizeScenario(&report)
			// Assert: exact rendered cardinality/refset must catch the independent mutation.
			if err != nil || report.Final != VerdictFail || report.Rendered.Checks[0].Status != Fail ||
				report.Effective.Checks[0].Status != Pass {
				t.Fatalf("fault %s laundered: final=%s err=%v", fault, report.Final, err)
			}
		})
	}
}
func TestProcedureGraderReceivesDetachedProjections(t *testing.T) {
	// Arrange: capture safe baseline measurements, then mutate every nested grader input.
	c := DefaultCorpus()
	run := CaseRun{Seed: 39, Repeat: 0}
	baseline, err := RunProcedureCase(t.Context(), "procedure-retrieval", c, run, DefaultProcedurePorts())
	if err != nil {
		t.Fatal(err)
	}
	ports := DefaultProcedurePorts()
	ports.GraderVersion = "mutating-grader/v1"
	ports.Grade = func(_ context.Context, p []memy.Projection[ProcedureObservation, DocumentRef]) (bool, error) {
		p[0].Output.ObservedResult = "wrong"
		p[0].Output.Preconditions[0] = "wrong"
		p[0].Provenance.Sources[0].Reference.Section = "wrong"
		p[0].Provenance.Losses[0] = "wrong"
		p[0].Provenance.Uncertainties[0] = "wrong"
		p[0].Provenance.Lineage = append(p[0].Provenance.Lineage, memy.RevisionRef{RecordID: "wrong", Revision: 1})
		return true, nil
	}
	// Act.
	actual, err := RunProcedureCase(t.Context(), "procedure-retrieval", c, run, ports)
	FinalizeScenario(&actual)
	// Assert: projection output and measured full body remain the original detached value.
	if err != nil || actual.Final != VerdictPass ||
		*actual.Metrics.PayloadBytes.Value != *baseline.Metrics.PayloadBytes.Value ||
		*actual.Metrics.ContextJSONBytes.Value != *baseline.Metrics.ContextJSONBytes.Value {
		t.Fatalf("grader mutated delivered body: final=%s err=%v", actual.Final, err)
	}
}
func TestProcedureCanonicalChecksAfterGraderCallback(t *testing.T) {
	for _, event := range []string{"forget", "source", "expiry", "cancel"} {
		t.Run(event, func(t *testing.T) {
			// Arrange: grading occurs after projection and can invalidate current canonical state.
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			ports := DefaultProcedurePorts()
			f, err := newProcedureFixture(ctx, "memory", ports)
			if err != nil {
				t.Fatal(err)
			}
			defer f.close()
			f.candidateLimit = 4
			f.recallLimit = 4
			f.costLimit = 5
			if err = f.seed(ctx, 43); err != nil {
				t.Fatal(err)
			}
			f.ports.Grade = func(ctx context.Context, _ []memy.Projection[ProcedureObservation, DocumentRef]) (bool, error) {
				return f.invalidateAfterGrade(ctx, event, cancel)
			}
			report := procedureReport("procedure-retrieval", CaseRun{Seed: 43, Repeat: 0}, f.ports)
			// Act.
			err = f.retrieval(ctx, DefaultCorpus(), &report, f.ports.Search(f.scope, f.candidates()))
			if err == nil {
				report.Execution.Checks = []Check{procedureCheck("procedure-completed", true, "completed", 1)}
			}
			FinalizeScenario(&report)
			// Assert: callback invalidation cannot leave a passing end-to-end result.
			if report.Final == VerdictPass {
				t.Fatalf("%s after grader escaped current canonical gate", event)
			}
			if event == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel callback err=%v", err)
			}
		})
	}
}

func (f *procedureFixture) invalidateAfterGrade(
	ctx context.Context,
	event string,
	cancel context.CancelFunc,
) (bool, error) {
	switch event {
	case "forget":
		_, forgetErr := fullForget(
			ctx,
			f.engine,
			"operator",
			f.scope,
			"assist",
			memy.ForgetRequest{
				OperationID:   "grade-forget",
				Selector:      memy.Selector{Kind: memy.SelectRecord, ID: "relevant-a"},
				Reason:        "synthetic",
				PolicyVersion: "procedure-forget/v1",
				Expected:      nil},
		)
		return true, forgetErr
	case "source":
		f.sources.Remove(f.scope, f.source.ID)
	case "expiry":
		f.clock.Advance(25 * time.Hour)
	case "cancel":
		cancel()
	}
	return true, nil
}
