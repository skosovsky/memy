package quality

import (
	"context"
	"errors"
	"strconv"

	"github.com/skosovsky/memy"
)

func procedurePlans() []CasePlan {
	var plans []CasePlan
	for _, entry := range []struct{ id, group string }{{procedureRetrievalCase, "retrieval"}, {procedureCrossScopeCase, "retrieval"}, {procedureAbstentionCase, groupAbstention}, {procedureGuardedCase, "poisoning"}, {procedurePermissiveCase, "poisoning"}, {procedureForgetMemoryCase, groupForget}, {procedureForgetSQLiteCase, groupForget}, {procedureFailuresCase, groupAbstention}} {
		id := entry.id
		plans = append(
			plans,
			CasePlan{
				ID:             id,
				Domain:         procedureDomain,
				Version:        "procedure-fixtures/v2",
				Groups:         []string{entry.group},
				Versions:       procedureCaseVersions(id, DefaultProcedurePorts()),
				Required:       procedureRequired(id),
				OptionalStages: procedureOptional(id),
				Run: func(ctx context.Context, c Corpus, run CaseRun) (ScenarioReport, error) {
					return RunProcedureCase(ctx, id, c, run, DefaultProcedurePorts())
				},
			},
		)
	}
	return plans
}

func procedureCheck(id string, ok bool, expected string, count int) Check {
	status := Pass
	observed := expected
	if !ok {
		status = Fail
		observed = "mismatch"
	}
	return Check{
		ID:        id,
		Mandatory: true,
		Status:    status,
		Expected:  expected,
		Observed:  observed,
		Evidence:  []Evidence{{Code: id, Count: count, Aliases: nil}},
	}
}

func procedureReport(id string, run CaseRun, ports ProcedurePorts) ScenarioReport {
	return ScenarioReport{
		ID:         id,
		Domain:     procedureDomain,
		Version:    "procedure-fixtures/v2",
		Repeat:     run.Repeat,
		Seed:       run.Seed,
		Variation:  "seeded-insertion-order",
		Versions:   procedureCaseVersions(id, ports),
		Mode:       ports.Mode,
		Candidate:  StageResult{Status: NotApplicable, Checks: nil, Version: ""},
		HostReview: StageResult{Status: NotApplicable, Checks: nil, Version: ""},
		Effective:  StageResult{Version: ports.SearchVersion, Status: Unknown, Checks: nil},
		Canonical:  StageResult{Version: procedureRetentionVersion, Status: Unknown, Checks: nil},
		Rendered:   StageResult{Version: procedureProjectorVersion, Status: Unknown, Checks: nil},
		Execution:  StageResult{Version: ports.ExtractorVersion, Status: Unknown, Checks: nil},
		Metrics: Metrics{
			CanonicalBytes:   UnavailableMeasurement("serialized-canonical-bytes", "not_measured"),
			RealProviderCost: UnavailableMeasurement("provider-tokens-currency", fixtureModeScripted),
			PayloadBytes: Measurement{
				Status: "",
				Unit:   "",
				Value:  nil,
				Reason: "",
			},
			ContextJSONBytes: Measurement{Status: "", Unit: "", Value: nil, Reason: ""},
			ProviderCost:     Measurement{Status: "", Unit: "", Value: nil, Reason: ""},
		},
		Final: VerdictUnknown, Diagnostic: "", Probes: nil}
}

// RunProcedureCase executes the same typed oracles with consumer-supplied ports.
func RunProcedureCase(
	ctx context.Context,
	id string,
	c Corpus,
	run CaseRun,
	ports ProcedurePorts,
) (ScenarioReport, error) {
	report := procedureReport(id, run, ports)
	if err := validateProcedureRun(id, c, run, ports); err != nil {
		return report, err
	}

	storage := "memory"
	if id == procedureForgetSQLiteCase {
		storage = "sqlite"
	}
	f, err := newProcedureFixture(ctx, storage, ports)
	if err != nil {
		return report, err
	}
	report, runErr := f.runCase(ctx, id, c, run, report)
	f.reportApplicability(&report)
	return report, errors.Join(runErr, f.close())
}

func (f *procedureFixture) runCase(
	ctx context.Context,
	id string,
	c Corpus,
	run CaseRun,
	report ScenarioReport,
) (ScenarioReport, error) {
	var err error
	ports := f.ports
	if c.Budgets.MaxCandidates < 1 || c.Budgets.RecallLimit < 1 || c.Budgets.ContextBytes < 1 ||
		c.Budgets.CostUnits < 1 {
		return report, memy.ErrInvalid
	}
	f.candidateLimit = min(procedureRetrievalLimit, c.Budgets.MaxCandidates)
	f.recallLimit = min(procedureRetrievalLimit, c.Budgets.RecallLimit)
	f.costLimit = uint64(c.Budgets.CostUnits)
	f.contextLimit = c.Budgets.ContextBytes
	if err = f.seed(ctx, run.Seed); err != nil {
		return report, err
	}
	if id == procedureCrossScopeCase || id == procedureForgetMemoryCase || id == procedureForgetSQLiteCase {
		if err = f.seedForeign(ctx); err != nil {
			return report, err
		}
	}
	search := ports.Search(f.scope, f.candidates())
	if search == nil {
		return report, memy.ErrInvalid
	}
	switch id {
	case procedureCrossScopeCase:
		err = f.crossScope(ctx, &report)
	case procedureRetrievalCase:
		err = f.retrieval(ctx, c, &report, search)
	case procedureAbstentionCase:
		err = f.abstention(ctx, &report, search)
	case procedureGuardedCase, procedurePermissiveCase:
		err = f.poisoning(ctx, &report, id == procedureGuardedCase)
	case procedureForgetMemoryCase, procedureForgetSQLiteCase:
		err = f.forget(ctx, &report)
	case procedureFailuresCase:
		err = f.failures(ctx, &report)
	default:
		return report, memy.ErrInvalid
	}
	if err != nil {
		return report, err
	}
	report.Execution.Checks = append(
		report.Execution.Checks,
		procedureCheck("procedure-completed", true, "completed", 1),
		procedureCheck(
			"configured-boundaries",
			f.candidateLimit <= c.Budgets.MaxCandidates && f.recallLimit <= c.Budgets.RecallLimit &&
				f.providerCalls <= uint64(c.Budgets.CostUnits),
			"within-configured-candidate-recall-call-caps",
			f.candidateLimit,
		),
	)
	if ports.Mode == fixtureModeScripted {
		report.Metrics.ProviderCost = KnownMeasurement("scripted-call-unit", f.providerCalls)
	} else {
		report.Metrics.ProviderCost = UnavailableMeasurement("provider-cost-unit", "external-not-measured")
	}
	report.Metrics.RealProviderCost = UnavailableMeasurement("provider-tokens-currency", "provider-cost-not-measured")
	return report, nil
}

func procedureVersions(p ProcedurePorts) PortVersions {
	return PortVersions{
		Provider:      p.ExtractorVersion,
		Model:         p.Mode,
		HostReview:    "procedure-host/v1",
		Retention:     procedureRetentionVersion,
		Resolver:      procedureResolverVersion,
		Consolidation: portNone,
		Search:        p.SearchVersion,
		Projector:     procedureProjectorVersion,
		Packing:       jsonPackingVersion,
		Grader:        p.GraderVersion,
	}
}

func procedureOptional(id string) []string {
	if id == procedureGuardedCase || id == procedurePermissiveCase {
		return nil
	}
	if id == procedureFailuresCase {
		return []string{stageCandidate}
	}
	return []string{stageCandidate, stageHostReview}
}

func procedureCaseVersions(id string, p ProcedurePorts) PortVersions {
	v := procedureVersions(p)
	switch id {
	case procedureGuardedCase:
		v.HostReview = procedureGuardedHostVersion
	case procedureFailuresCase:
		v.HostReview = procedureGuardedHostVersion
		v.Grader = (procedureControlledGrader{}).Version()
	case procedurePermissiveCase:
		v.HostReview = "permissive-host/v1"
	default:
		v.HostReview = portNone
	}
	return v
}

func procedureRequired(id string) []RequiredCheck {
	checks := []RequiredCheck{
		{Stage: stageEffective, ID: "effective-exact"},
		{Stage: stageCanonical, ID: "canonical-state"},
		{Stage: stageRendered, ID: "rendered-context"},
		{Stage: stageExecution, ID: "procedure-completed"},
		{Stage: stageExecution, ID: "configured-boundaries"},
	}
	if id != procedureForgetMemoryCase && id != procedureForgetSQLiteCase {
		checks = append(checks, RequiredCheck{Stage: stageCanonical, ID: "canonical-provenance"})
	}
	switch id {
	case procedureRetrievalCase:
		checks = append(checks, RequiredCheck{Stage: stageCanonical, ID: "current-retention"})
	case procedureGuardedCase, procedurePermissiveCase:
		checks = append(checks, RequiredCheck{Stage: stageHostReview, ID: "host-poisoning-decision"})
	case procedureForgetMemoryCase, procedureForgetSQLiteCase:
		checks = append(
			checks,
			RequiredCheck{Stage: stageEffective, ID: "pending-purge-denial"},
			RequiredCheck{Stage: stageEffective, ID: "managed-purge-retry"},
			RequiredCheck{Stage: stageCanonical, ID: "source-revision-change"},
			RequiredCheck{Stage: stageCanonical, ID: "source-removal"},
			RequiredCheck{Stage: stageCanonical, ID: "late-derived-fence"},
			RequiredCheck{Stage: stageCanonical, ID: "durable-source-revocation"},
			RequiredCheck{Stage: stageCanonical, ID: "foreign-after-forget"},
		)
	case procedureCrossScopeCase:
		checks = append(
			checks,
			RequiredCheck{Stage: stageCanonical, ID: "cross-scope-read-denied"},
			RequiredCheck{Stage: stageCanonical, ID: "foreign-state-preserved"},
		)
	case procedureAbstentionCase:
		checks = append(
			checks,
			RequiredCheck{Stage: stageRendered, ID: "projected-healthy-empty"},
			RequiredCheck{Stage: stageRendered, ID: "projected-conflict"},
			RequiredCheck{Stage: stageRendered, ID: "projected-unavailable"},
		)
	case procedureFailuresCase:
		checks = append(
			checks,
			RequiredCheck{Stage: stageHostReview, ID: "error-not-rejection"},
			RequiredCheck{Stage: stageExecution, ID: "known-controlled-port-errors"},
			RequiredCheck{Stage: stageExecution, ID: "controlled-grader-executed"},
		)
	}
	return checks
}

func (f *procedureFixture) gradeProjections(
	ctx context.Context,
	projections []memy.Projection[ProcedureObservation, DocumentRef],
) (bool, error) {
	if f.ports.Grade == nil {
		return true, nil
	}
	graderCodec := memy.JSONCodec[[]memy.Projection[ProcedureObservation, DocumentRef]]{}
	encoded, cloneErr := graderCodec.Encode(projections)
	if cloneErr != nil {
		return false, cloneErr
	}
	graderInput, cloneErr := graderCodec.Decode(encoded)
	if cloneErr != nil {
		return false, cloneErr
	}
	f.graderCalls++
	grade, gradeErr := f.ports.Grade(ctx, graderInput)
	if gradeErr != nil {
		return false, gradeErr
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	return grade, nil
}

func validateProcedureRun(id string, c Corpus, run CaseRun, ports ProcedurePorts) error {
	if err := validateCorpus(c); err != nil {
		return err
	}
	if run.Seed == 0 || run.Repeat < 0 || run.Repeat >= c.Repeats || ports.Extractor == nil || ports.Search == nil ||
		ports.Mode == "" ||
		ports.ExtractorVersion == "" ||
		ports.SearchVersion == "" ||
		ports.GraderVersion == "" ||
		(ports.Grade == nil) != (ports.GraderVersion == portNone) {
		return memy.ErrInvalid
	}
	for _, plan := range procedurePlans() {
		if plan.ID == id {
			return nil
		}
	}
	return memy.ErrInvalid
}

func (f *procedureFixture) reportApplicability(r *ScenarioReport) {
	if f.contextBodies > 0 {
		r.Execution.Checks = append(
			r.Execution.Checks,
			Check{
				ID:        "context-budget",
				Mandatory: true,
				Status:    Pass,
				Expected:  "full-body-json-bytes<=" + strconv.FormatUint(f.contextLimit, 10),
				Observed:  strconv.FormatUint(f.contextMaximum, 10),
				Evidence:  []Evidence{{Code: "bounded-body-observations", Count: f.contextBodies, Aliases: nil}},
			},
		)
		r.Metrics.ContextJSONBytes = KnownMeasurement("maximum-observed-body-json-bytes", f.contextMaximum)
	}
	observed := "not_applicable"
	if f.graderCalls > 0 {
		observed = "executed"
	}
	r.Execution.Checks = append(
		r.Execution.Checks,
		Check{
			ID:        "configured-grader",
			Mandatory: true,
			Status:    Pass,
			Expected:  "actual-invocation-count",
			Observed:  observed,
			Evidence:  []Evidence{{Code: "configured-grader-calls", Count: f.graderCalls, Aliases: nil}},
		},
	)
}
