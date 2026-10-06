package quality

import (
	"encoding/json"
	"reflect"

	"github.com/skosovsky/memy"
)

func evaluatorCheck(id string, ok bool, count int) Check {
	status, observed := Pass, checkpointMatched
	if !ok {
		status, observed = Fail, "mismatched"
	}
	return Check{
		ID:        id,
		Mandatory: true,
		Status:    status,
		Expected:  checkpointMatched,
		Observed:  observed,
		Evidence:  []Evidence{{Code: id, Count: count, Aliases: nil}},
	}
}

func evaluatorExact(got, want []memy.Record[Preference, string]) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[memy.RevisionRef]bool)
	for _, g := range got {
		ref := memy.RevisionRef{RecordID: g.ID, Revision: g.Revision}
		if seen[ref] {
			return false
		}
		seen[ref] = true
		found := false
		for _, w := range want {
			if g.ID == w.ID && g.Revision == w.Revision && g.Scope == w.Scope &&
				reflect.DeepEqual(g.Payload, w.Payload) &&
				g.Valid == w.Valid &&
				reflect.DeepEqual(g.Provenance, w.Provenance) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func evaluatorRendered(
	body memy.ProjectedRecallResult[Preference, string],
	want []memy.Record[Preference, string],
) bool {
	// Check the real bytes a caller renders, including the projection envelope.
	raw, err := json.Marshal(body)
	if err != nil {
		return false
	}
	var rendered memy.ProjectedRecallResult[Preference, string]
	if json.Unmarshal(raw, &rendered) != nil || len(rendered.Projections) != len(want) ||
		len(rendered.Omissions) != 0 ||
		!body.Budget.Exact ||
		body.Budget.Used != uint64(len(raw)) ||
		body.Budget.Used > body.Budget.Limit {
		return false
	}
	seen := make(map[memy.RevisionRef]bool)
	for _, p := range rendered.Projections {
		ref := memy.RevisionRef{RecordID: p.RecordID, Revision: p.Revision}
		if seen[ref] {
			return false
		}
		seen[ref] = true
		found := false
		for _, w := range want {
			if p.RecordID == w.ID && p.Revision == w.Revision && p.Scope == w.Scope &&
				reflect.DeepEqual(p.Output, w.Payload) &&
				reflect.DeepEqual(p.Provenance, w.Provenance) &&
				p.Trust == "data" {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func evaluatorEvaluate(id string, run CaseRun, obs evaluatorObservation, oracle evaluatorOracle) ScenarioReport {
	var metrics Metrics
	s := ScenarioReport{
		ID:        id,
		Domain:    evaluatorDomain,
		Version:   "evaluator-fixtures/v2",
		Repeat:    run.Repeat,
		Seed:      run.Seed,
		Variation: "payload-parity/v1",
		Versions:  evaluatorVersions(),
		Mode:      fixtureModeScripted,
		Candidate: StageResult{
			Status:  "",
			Checks:  nil,
			Version: "",
		},
		HostReview: StageResult{Status: "", Checks: nil, Version: ""},
		Effective:  StageResult{Status: "", Checks: nil, Version: ""},
		Canonical:  StageResult{Status: "", Checks: nil, Version: ""},
		Rendered:   StageResult{Status: "", Checks: nil, Version: ""},
		Execution:  StageResult{Status: "", Checks: nil, Version: ""},
		Metrics:    metrics,
		Final:      "",
		Diagnostic: "",
		Probes:     nil,
	}
	// These observations preceded every mutation, including the late privacy leak.
	s.Candidate.Checks = []Check{evaluatorCheck("candidate_input", true, 1)}
	s.HostReview.Checks = []Check{evaluatorCheck("accepted_receipt", true, 1)}
	s.Effective.Checks = []Check{
		evaluatorCheck("effective_exact", evaluatorExact(obs.Records, oracle.Records), len(obs.Records)),
		evaluatorCheck("availability_exact", obs.Available == oracle.Available, 1),
	}
	s.Canonical.Checks = []Check{
		evaluatorCheck("canonical_exact", evaluatorExact(obs.Canonical, oracle.Canonical), len(obs.Canonical)),
	}
	s.Rendered.Checks = []Check{
		evaluatorCheck("rendered_exact", evaluatorRendered(obs.Body, oracle.Records), len(obs.Body.Projections)),
	}
	s.Execution.Checks = []Check{evaluatorCheck(checkpointCompleted, true, 1)}
	if !obs.CheckerKnown || obs.AdapterError != nil {
		code := "checker_unavailable"
		if obs.AdapterError != nil {
			code = "adapter_unavailable"
		}
		s.Execution.Checks = append(
			s.Execution.Checks,
			Check{
				ID:        code,
				Mandatory: true,
				Status:    Unknown,
				Expected:  checkpointCompleted,
				Observed:  string(Unknown),
				Evidence:  []Evidence{{Code: code, Count: 1, Aliases: nil}},
			},
		)
		s.Diagnostic = code
	}
	var payloadBytes uint64
	for _, r := range obs.Records {
		b, _ := json.Marshal(r.Payload)
		payloadBytes += uint64(len(b))
	}
	b, _ := json.Marshal(obs.Body)
	s.Metrics.PayloadBytes = KnownMeasurement("consumer-payload-json-bytes", payloadBytes)
	if obs.ContextDelivered {
		s.Metrics.ContextJSONBytes = KnownMeasurement("final-context-json-bytes", uint64(len(b)))
	} else {
		s.Metrics.ContextJSONBytes = UnavailableMeasurement("final-context-json-bytes", "execution_incomplete")
	}
	s.Metrics.CanonicalBytes = UnavailableMeasurement("serialized-canonical-bytes", "not_measured")
	s.Metrics.ProviderCost = UnavailableMeasurement("provider-cost-units", "manual_fixture")
	s.Metrics.RealProviderCost = UnavailableMeasurement("tokens-or-currency", fixtureModeScripted)
	FinalizeScenario(&s)
	return s
}

func cloneEvaluatorObservation(o evaluatorObservation) evaluatorObservation {
	// Mutation copies are detached from the normal output and the independent oracle.
	cloned := evaluatorObservation{
		Available:        o.Available,
		CheckerKnown:     o.CheckerKnown,
		AdapterError:     o.AdapterError,
		ContextDelivered: o.ContextDelivered,
		Records:          nil,
		Body: memy.ProjectedRecallResult[Preference, string]{
			Projections: nil,
			Coverage:    nil,
			Progress: memy.RecallProgress{
				ReturnedCandidates:  0,
				CanonicalChecked:    0,
				CanonicalFiltered:   0,
				RankingOmitted:      0,
				CandidatesTruncated: false,
			},
			Omissions: nil,
			Budget:    memy.BudgetUsage{Unit: "", Used: 0, Limit: 0, Exact: false},
		},
		Canonical: nil,
	}
	raw, _ := json.Marshal(o.Records)
	_ = json.Unmarshal(raw, &cloned.Records)
	raw, _ = json.Marshal(o.Canonical)
	_ = json.Unmarshal(raw, &cloned.Canonical)
	raw, _ = json.Marshal(o.Body)
	_ = json.Unmarshal(raw, &cloned.Body)
	cloned.Body.Budget = o.Body.Budget
	return cloned
}
