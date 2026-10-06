package quality

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/skosovsky/memy"
)

func observed(id string, status Status, mandatory bool) Check {
	return Check{
		ID:        id,
		Status:    status,
		Mandatory: mandatory,
		Expected:  "completed",
		Observed:  "completed",
		Evidence:  []Evidence{{Code: "observed", Count: 1}},
	}
}
func completeScenario() ScenarioReport {
	s := ScenarioReport{}
	for _, stage := range stages(&s) {
		stage.Checks = []Check{observed("checkpoint", Pass, true)}
	}
	return s
}
func TestFinalVerdictUsesEveryStage(t *testing.T) {
	for _, name := range []string{"candidate", "host_review", "effective", "canonical", "rendered", "execution"} {
		t.Run(name, func(t *testing.T) {
			// Arrange.
			s := completeScenario()
			names := []string{"candidate", "host_review", "effective", "canonical", "rendered", "execution"}
			for i, n := range names {
				if n == name {
					stages(&s)[i].Checks[0].Status = Fail
				}
			}
			// Act.
			FinalizeScenario(&s)
			// Assert.
			want := VerdictFail
			if name == "execution" {
				want = VerdictUnknown
			}
			if s.Final != want {
				t.Fatalf("got %s want %s", s.Final, want)
			}
		})
	}
	// Arrange: known failure followed by evaluator uncertainty.
	s := completeScenario()
	s.Effective.Checks[0].Status = Fail
	s.Rendered.Checks[0].Status = Unknown
	// Act.
	FinalizeScenario(&s)
	// Assert.
	if s.Final != VerdictUnknown {
		t.Fatal(s.Final)
	}
}
func TestExpectedCandidateRejectionCanPassProtocol(t *testing.T) {
	// Arrange.
	s := completeScenario()
	s.Candidate.Checks[0] = observed("candidate_quality", Fail, false)
	// Act.
	FinalizeScenario(&s)
	// Assert.
	if s.Final != VerdictPass || s.Candidate.Status != Fail {
		t.Fatal(s)
	}
}
func TestMissingEvidenceAndDeclaredCheckpointAreUnknown(t *testing.T) {
	// Arrange.
	s := completeScenario()
	s.Canonical.Checks[0].Evidence = nil
	p := CasePlan{Required: []RequiredCheck{{Stage: "rendered", ID: "exact_revision"}}}
	// Act.
	enforcePlan(&s, p)
	FinalizeScenario(&s)
	// Assert.
	if s.Final != VerdictUnknown || s.Canonical.Checks[0].Status != Unknown {
		t.Fatal(s)
	}
}
func TestUnknownPrecedenceAndSafeErrors(t *testing.T) {
	// Arrange.
	secret := "PRIVATE-SOURCE-SECRET"
	err := errors.New(secret)
	r := FailureReport(err)
	// Act.
	encoded, marshalErr := json.Marshal(r)
	// Assert.
	if marshalErr != nil || strings.Contains(string(encoded), secret) || r.ExitCode() != 2 ||
		r.Diagnostic != "execution_error" {
		t.Fatal(string(encoded), marshalErr)
	}
	for _, status := range []Verdict{VerdictPass, VerdictFail, VerdictUnknown} {
		want := map[Verdict]int{VerdictPass: 0, VerdictFail: 1, VerdictUnknown: 2}[status]
		if (Report{Final: status}).ExitCode() != want {
			t.Fatal(status)
		}
	}
}
func TestMalformedManifestFailsBeforeDispatch(t *testing.T) {
	for _, mutation := range []string{"version", "seed", "repeats", "provider", "budget", "unknown_plan", "duplicate_plan", "plan_version", "plan_ports", "missing_groups"} {
		t.Run(mutation, func(t *testing.T) {
			// Arrange.
			c := DefaultCorpus()
			switch mutation {
			case "version":
				c.Version = "PRIVATE"
			case "seed":
				c.Seed = 0
			case "repeats":
				c.Repeats = 21
			case "provider":
				c.Versions.Provider = "PRIVATE"
			case "budget":
				c.Budgets.MaxCandidates = 0
			case "unknown_plan":
				c.Scenarios[0].ID = "PRIVATE"
			case "duplicate_plan":
				c.Scenarios = append(c.Scenarios, c.Scenarios[0])
			case "plan_version":
				c.Scenarios[0].Version = "PRIVATE"
			case "plan_ports":
				c.Scenarios[0].Versions.Model = "PRIVATE"
			case "missing_groups":
				c.Scenarios = c.Scenarios[:1]
			}
			// Act.
			r, err := Run(context.Background(), c)
			raw, _ := json.Marshal(r)
			// Assert.
			if !errors.Is(err, memy.ErrInvalid) || r.ExitCode() != 2 || len(r.Scenarios) != 0 ||
				strings.Contains(string(raw), "PRIVATE") {
				t.Fatalf("%s %v", raw, err)
			}
		})
	}
}
func TestSeedAndRepeatsActuallyDispatch(t *testing.T) {
	// Arrange.
	c := DefaultCorpus()
	c.Repeats = 1
	// Act.
	a, errA := Run(context.Background(), c)
	b, errB := Run(context.Background(), c)
	// Assert: fixtures record deterministic variation and safe observations.
	if errA != nil || errB != nil {
		t.Fatal(errA, errB)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("same pinned manifest produced nondeterministic evidence")
	}
	if len(a.Scenarios) != len(c.Scenarios) {
		t.Fatal("repeat not executed")
	}
	for _, s := range a.Scenarios {
		if s.Variation == "" || s.Seed != DerivedSeed(c.Seed, s.ID, 0) {
			t.Fatalf("missing variation %s", s.ID)
		}
	}
	// Arrange.
	c.Seed++
	// Act.
	changed, err := Run(context.Background(), c)
	// Assert.
	if err != nil {
		t.Fatal(err)
	}
	if changed.Scenarios[0].Seed == a.Scenarios[0].Seed {
		t.Fatal("seed decorative")
	}
	variationChanged := false
	for i := range a.Scenarios {
		if a.Scenarios[i].Variation != changed.Scenarios[i].Variation {
			variationChanged = true
		}
	}
	if !variationChanged {
		t.Fatal("seed changed metadata but no actual recorded fixture variation")
	}
	c.Repeats = 2
	repeated, repeatErr := Run(context.Background(), c)
	if repeatErr != nil {
		t.Fatal(repeatErr)
	}
	if len(repeated.Scenarios) != 2*len(c.Scenarios) {
		t.Fatal("repeat count not dispatched")
	}
	for i := 0; i < len(repeated.Scenarios); i += 2 {
		if repeated.Scenarios[i].Repeat != 0 || repeated.Scenarios[i+1].Repeat != 1 ||
			repeated.Scenarios[i].Seed == repeated.Scenarios[i+1].Seed {
			t.Fatal("repeat identity or variation seed lost")
		}
	}
}
func validateSchema(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	if err = compiler.AddResource("urn:memy:quality", doc); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("urn:memy:quality")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err = json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if err = schema.Validate(decoded); err != nil {
		t.Fatal(err)
	}
}
func TestQualitySchemasExecuteAgainstActualReports(t *testing.T) {
	// Arrange.
	c := DefaultCorpus()
	c.Repeats = 1
	// Act.
	report, err := Run(context.Background(), c)
	// Assert.
	if err != nil {
		t.Fatal(err)
	}
	validateSchema(t, "../../schemas/quality-corpus-v2.schema.json", c)
	validateSchema(t, "../../schemas/quality-report-v2.schema.json", report)
	validateSchema(t, "../../schemas/quality-report-v2.schema.json", FailureReport(errors.New("SECRET")))
}
func TestStrictManifestDecode(t *testing.T) {
	// Arrange.
	raw, _ := json.Marshal(DefaultCorpus())
	raw = append(raw[:len(raw)-1], []byte(",\"private_unknown\":true}")...)
	path := t.TempDir() + "/manifest.json"
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	// Act.
	_, err := Load(path)
	// Assert.
	if !errors.Is(err, memy.ErrInvalid) {
		t.Fatal(err)
	}
}

func TestFailureReportMatchesSchemaAndCannotLeakLoadData(t *testing.T) {
	// Arrange.
	err := errors.New("private-source secret query and rendered output")
	// Act.
	report := FailureReport(err)
	// Assert.
	validateSchema(t, "../../schemas/quality-report-v2.schema.json", report)
	if report.Manifest != nil || report.Diagnostic != "execution_error" || report.ExitCode() != 2 {
		t.Fatal("unsafe failure report")
	}
}

func TestSavedManifestPinsRegisteredPlans(t *testing.T) {
	// Arrange.
	expected := DefaultCorpus()
	// Act.
	saved, err := Load("../../testdata/quality-v2.json")
	// Assert.
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved, expected) {
		t.Fatal("saved corpus no longer matches registered fixture versions/configuration")
	}
}
