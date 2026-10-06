package quality

import (
	"context"
	"slices"

	"github.com/skosovsky/memy"
)

func preferenceVersions() PortVersions {
	return PortVersions{
		Provider:      preferenceProviderVersion,
		Model:         fixtureModeScripted,
		HostReview:    "preference-review/v2",
		Retention:     "retention/v1",
		Resolver:      "resolver/v1",
		Consolidation: "consolidation/v2",
		Search:        "preference-index/v1",
		Projector:     "preference-projection/v2",
		Packing:       preferencePackingVersion,
		Grader:        "none",
	}
}

func preferenceVersionsFor(id string) PortVersions {
	versions := preferenceVersions()
	if id == preferenceSessionsID {
		versions.Search = "preference-index/v1+conflict-metadata/v2"
	}
	return versions
}

func preferencePlans() []CasePlan {
	base := []RequiredCheck{
		{Stage: stageEffective, ID: "exact_revisions_payload_validity_sources_lineage"},
		{Stage: stageRendered, ID: "exact_projection_body_and_budget"},
		{Stage: stageExecution, ID: preferenceCompleted},
	}
	add := func(extra ...RequiredCheck) []RequiredCheck { return append(slices.Clone(base), extra...) }
	result := preferenceBasePlans(add)
	for _, entry := range []struct {
		id   string
		mode memy.ConsolidationMode
	}{{preferenceBaselineID, ""}, {"exact", memy.ExactDedup}, {"domain", memy.DomainMerge}, {"semantic", memy.SemanticMerge}} {
		required := add(
			RequiredCheck{stageCanonical, "full_original_documents_preserved"},
			RequiredCheck{stageCanonical, "scope_sources_and_exact_lineage"},
		)
		optional := []string{}
		if entry.mode == "" {
			optional = []string{preferenceCandidateStage, stageHostReview}
		} else {
			required = append(required, RequiredCheck{stageExecution, "consolidation_proposal_created"})
			if entry.mode == memy.SemanticMerge {
				required = append(
					required,
					RequiredCheck{stageHostReview, "explicit_semantic_rejection"},
					RequiredCheck{preferenceCandidateStage, preferenceSourceIdentityCheck},
					RequiredCheck{preferenceCandidateStage, preferenceInputLineageCheck},
				)
			} else {
				required = append(
					required,
					RequiredCheck{preferenceCandidateStage, "facts_negation_validity_sources_lineage"},
					RequiredCheck{preferenceCandidateStage, "valid_interval_preserved"},
					RequiredCheck{preferenceCandidateStage, preferenceSourceIdentityCheck},
					RequiredCheck{preferenceCandidateStage, preferenceInputLineageCheck},
					RequiredCheck{preferenceCandidateStage, "negation_preserved"},
					RequiredCheck{stageHostReview, "explicit_apply_checked_receipt"},
				)
			}
		}
		result = append(
			result,
			CasePlan{
				ID:             "preference-consolidation-" + entry.id,
				Domain:         preferenceDomain,
				Version:        "preference-consolidation/v2",
				Groups:         []string{"consolidation"},
				Required:       required,
				OptionalStages: optional,
				Versions:       preferenceVersionsFor("preference-consolidation-" + entry.id), Run: nil},
		)
	}
	for i := range result {
		plan := &result[i]
		plan.Versions = preferenceVersionsFor(plan.ID)
		id := plan.ID
		plan.Run = func(ctx context.Context, c Corpus, run CaseRun) (ScenarioReport, error) {
			return runPreferencePlan(ctx, id, c, run)
		}
	}
	return result
}

func preferenceReport(id string, _ CaseRun, observed preferenceRunResult, err error) ScenarioReport {
	var emptyStage StageResult
	var metrics Metrics
	s := ScenarioReport{
		ID:         id,
		Domain:     preferenceDomain,
		Versions:   preferenceVersionsFor(id),
		Mode:       fixtureModeScripted,
		Variation:  observed.Variation,
		Candidate:  StageResult{Status: NotApplicable, Checks: nil, Version: ""},
		HostReview: StageResult{Status: NotApplicable, Checks: nil, Version: ""},
		Version:    "",
		Repeat:     0,
		Seed:       0,
		Effective:  emptyStage,
		Canonical:  emptyStage,
		Rendered:   emptyStage,
		Execution:  emptyStage,
		Metrics:    metrics,
		Final:      "",
		Diagnostic: "",
		Probes:     nil,
	}
	stage := func(name string) *StageResult {
		switch name {
		case preferenceCandidateStage:
			return &s.Candidate
		case stageHostReview:
			return &s.HostReview
		case stageEffective:
			return &s.Effective
		case stageCanonical:
			return &s.Canonical
		case stageRendered:
			return &s.Rendered
		default:
			return &s.Execution
		}
	}
	for _, o := range observed.Checks {
		p := stage(o.Stage)
		p.Status = Pass
		status := Pass
		if !o.OK {
			status = Fail
		}
		mandatory := o.Stage != preferenceCandidateStage || id != preferenceSemanticID ||
			o.ID == preferenceSourceIdentityCheck ||
			o.ID == preferenceInputLineageCheck
		p.Checks = append(
			p.Checks,
			Check{
				ID:        o.ID,
				Mandatory: mandatory,
				Status:    status,
				Expected:  o.Expected,
				Observed:  o.Observed,
				Evidence:  []Evidence{{Code: o.ID, Count: o.Count, Aliases: nil}},
			},
		)
	}
	if observed.HostDecision != "" {
		s.HostReview.Version = "preference-review/v2"
		for i := range s.HostReview.Checks {
			s.HostReview.Checks[i].Expected = observed.HostDecision
			if s.HostReview.Checks[i].Status == Pass {
				s.HostReview.Checks[i].Observed = observed.HostDecision
			} else {
				s.HostReview.Checks[i].Observed = preferenceUnknownID
			}
		}
	}
	if err == nil {
		s.Execution.Checks = append(
			s.Execution.Checks,
			Check{
				ID:        preferenceCompleted,
				Mandatory: true,
				Status:    Pass,
				Expected:  preferenceCompleted,
				Observed:  preferenceCompleted,
				Evidence:  []Evidence{{Code: "fixture_completed", Count: 1, Aliases: nil}},
			},
		)
	}
	s.Metrics = preferenceMetrics(id, observed, err)
	return s
}

func preferenceBasePlans(add func(...RequiredCheck) []RequiredCheck) []CasePlan {
	return []CasePlan{
		{
			ID:      preferenceSessionsID,
			Domain:  preferenceDomain,
			Version: "preference-sessions/v2",
			Groups:  []string{"sessions"},
			Required: add(
				RequiredCheck{stageEffective, "unknown_read_options"},
				RequiredCheck{stageEffective, "conflict_read_options"},
				RequiredCheck{stageEffective, "conflict_get_abstains"},
				RequiredCheck{stageCanonical, "historical_original_exact_revision"},
				RequiredCheck{stageCanonical, "scope_and_sources"},
			),
			OptionalStages: []string{preferenceCandidateStage, stageHostReview},
			Versions:       preferenceVersionsFor(preferenceSessionsID),
			Run:            nil,
		},
		{
			ID:      "preference-temporal",
			Domain:  preferenceDomain,
			Version: "preference-temporal/v2",
			Groups:  []string{"temporal"},
			Required: add(
				RequiredCheck{stageEffective, "valid_time_selects_distinct_fact"},
				RequiredCheck{stageEffective, "recorded_time_selects_original_revision"},
				RequiredCheck{stageCanonical, "historical_read_obeys_current_revoke"},
				RequiredCheck{stageCanonical, "historical_read_obeys_current_retention"},
			),
			OptionalStages: []string{preferenceCandidateStage, stageHostReview},
			Versions:       preferenceVersionsFor("preference-temporal"),
			Run:            nil,
		},
		{
			ID:      "preference-port-failures",
			Domain:  preferenceDomain,
			Version: "preference-port-failures/v2",
			Groups:  []string{preferenceAbstentionGroup},
			Required: add(
				RequiredCheck{stageExecution, "expected_provider_unavailable"},
				RequiredCheck{stageExecution, "provider_recovery_unaccepted_proposal"},
				RequiredCheck{stageExecution, "expected_policy_unavailable_fail_closed"},
				RequiredCheck{stageCanonical, "port_failures_do_not_apply_or_erase"},
			),
			OptionalStages: []string{preferenceCandidateStage, stageHostReview},
			Versions:       preferenceVersionsFor("preference-port-failures"),
			Run:            nil,
		},
	}
}

func runPreferencePlan(ctx context.Context, id string, c Corpus, run CaseRun) (ScenarioReport, error) {
	var observed preferenceRunResult
	var err error
	b := c.Budgets
	switch id {
	case preferenceSessionsID:
		observed, err = preferenceSessions(ctx, run.Seed, b.RecallLimit, b.MaxCandidates, b.ContextBytes)
	case "preference-temporal":
		observed, err = preferenceTemporal(ctx, run.Seed, b.RecallLimit, b.MaxCandidates, b.ContextBytes)
	case "preference-port-failures":
		observed, err = preferenceFailures(ctx, run.Seed, b.RecallLimit, b.MaxCandidates, b.ContextBytes)
	default:
		mode := memy.ConsolidationMode("")
		switch id {
		case "preference-consolidation-exact":
			mode = memy.ExactDedup
		case "preference-consolidation-domain":
			mode = memy.DomainMerge
		case preferenceSemanticID:
			mode = memy.SemanticMerge
		}
		observed, err = preferenceConsolidation(
			ctx,
			run.Seed,
			b.RecallLimit,
			b.MaxCandidates,
			b.ContextBytes,
			mode,
			memy.Budget{InputBytes: b.InputBytes, OutputBytes: b.OutputBytes, CostUnits: b.CostUnits},
		)
	}
	return preferenceReport(id, run, observed, err), err
}
