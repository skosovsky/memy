package quality

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"time"

	"github.com/skosovsky/memy"
)

// Privacy runs last so its real revocation cannot alter the other isolated
// observation probes, all of which retain the independent baseline snapshot.
var evaluatorProbeIDs = []string{"wrong_payload", "wrong_revision", "missing_lineage", "false_empty_outage", "checker_error", "adapter_error", "late_privacy"}

func evaluatorVersions() PortVersions {
	v := preferenceVersions()
	v.Provider, v.Model, v.Consolidation = "manual-remember/v1", "none", "none"
	return v
}

func evaluatorPlans() []CasePlan {
	required := []RequiredCheck{{"candidate", "candidate_input"}, {"host_review", "accepted_receipt"}, {"effective", "effective_exact"}, {"canonical", "canonical_exact"}, {"rendered", "rendered_exact"}, {"execution", "completed"}}
	for _, id := range evaluatorProbeIDs {
		required = append(required, RequiredCheck{"execution", "detected_" + id})
	}
	return []CasePlan{{ID: "evaluator-self-check", Domain: "evaluator", Version: "evaluator-fixtures/v2", Groups: []string{"evaluator"}, Versions: evaluatorVersions(), Required: required, Run: runEvaluator}}
}

// RunProbe exposes a normal negative report through the same aggregator and exit
// contract as a consumer run. Only the fixed synthetic mutation IDs are allowed.
func RunProbe(ctx context.Context, c Corpus, id string) (Report, error) {
	if err := validateCorpus(c); err != nil {
		return FailureReport(err), err
	}
	if !slices.Contains(evaluatorProbeIDs, id) {
		return FailureReport(memy.ErrInvalid), memy.ErrInvalid
	}
	s, err := runEvaluator(ctx, c, CaseRun{Seed: DerivedSeed(c.Seed, "evaluator-self-check", 0)})
	if err != nil {
		return FailureReport(err), err
	}
	for _, probe := range s.Probes {
		if probe.ID == id {
			r := Report{Schema: "memy-quality/v2", Versions: probe.Versions, Scenarios: []ScenarioReport{probe}}
			FinalizeReport(&r)
			return r, nil
		}
	}
	if s.Final != VerdictPass && len(s.Probes) == 0 {
		// An insufficient baseline cannot support a mutation demonstration. Keep
		// its actual failed/unknown checkpoints instead of inventing a probe.
		r := Report{Schema: "memy-quality/v2", Versions: s.Versions, Scenarios: []ScenarioReport{s}}
		FinalizeReport(&r)
		return r, nil
	}
	return FailureReport(memy.ErrInvalid), memy.ErrInvalid
}

type evaluatorObservation struct {
	Records          []memy.Record[Preference, string]
	Body             memy.ProjectedRecallResult[Preference, string]
	Canonical        []memy.Record[Preference, string]
	Available        bool
	CheckerKnown     bool
	AdapterError     error
	ContextDelivered bool
}

// The oracle is assembled from fixture inputs and receipt identities, never from
// the Recall observation being graded. No report serializes these private inputs.
type evaluatorOracle struct {
	Records   []memy.Record[Preference, string]
	Canonical []memy.Record[Preference, string]
	Available bool
}

func evaluatorCheck(id string, ok bool, count int) Check {
	status, observed := Pass, "matched"
	if !ok {
		status, observed = Fail, "mismatched"
	}
	return Check{ID: id, Mandatory: true, Status: status, Expected: "matched", Observed: observed, Evidence: []Evidence{{Code: id, Count: count}}}
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
			if g.ID == w.ID && g.Revision == w.Revision && g.Scope == w.Scope && reflect.DeepEqual(g.Payload, w.Payload) && g.Valid == w.Valid && reflect.DeepEqual(g.Provenance, w.Provenance) {
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

func evaluatorRendered(body memy.ProjectedRecallResult[Preference, string], want []memy.Record[Preference, string]) bool {
	// Check the real bytes a caller renders, including the projection envelope.
	raw, err := json.Marshal(body)
	if err != nil {
		return false
	}
	var rendered memy.ProjectedRecallResult[Preference, string]
	if json.Unmarshal(raw, &rendered) != nil || len(rendered.Projections) != len(want) || len(rendered.Omissions) != 0 || !body.Budget.Exact || body.Budget.Used != uint64(len(raw)) || body.Budget.Used > body.Budget.Limit {
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
			if p.RecordID == w.ID && p.Revision == w.Revision && p.Scope == w.Scope && reflect.DeepEqual(p.Output, w.Payload) && reflect.DeepEqual(p.Provenance, w.Provenance) && p.Trust == "data" {
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
	s := ScenarioReport{ID: id, Domain: "evaluator", Version: "evaluator-fixtures/v2", Repeat: run.Repeat, Seed: run.Seed, Variation: "payload-parity/v1", Versions: evaluatorVersions(), Mode: "scripted"}
	// These observations preceded every mutation, including the late privacy leak.
	s.Candidate.Checks = []Check{evaluatorCheck("candidate_input", true, 1)}
	s.HostReview.Checks = []Check{evaluatorCheck("accepted_receipt", true, 1)}
	s.Effective.Checks = []Check{evaluatorCheck("effective_exact", evaluatorExact(obs.Records, oracle.Records), len(obs.Records)), evaluatorCheck("availability_exact", obs.Available == oracle.Available, 1)}
	s.Canonical.Checks = []Check{evaluatorCheck("canonical_exact", evaluatorExact(obs.Canonical, oracle.Canonical), len(obs.Canonical))}
	s.Rendered.Checks = []Check{evaluatorCheck("rendered_exact", evaluatorRendered(obs.Body, oracle.Records), len(obs.Body.Projections))}
	s.Execution.Checks = []Check{evaluatorCheck("completed", true, 1)}
	if !obs.CheckerKnown || obs.AdapterError != nil {
		code := "checker_unavailable"
		if obs.AdapterError != nil {
			code = "adapter_unavailable"
		}
		s.Execution.Checks = append(s.Execution.Checks, Check{ID: code, Mandatory: true, Status: Unknown, Expected: "completed", Observed: "unknown", Evidence: []Evidence{{Code: code, Count: 1}}})
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
	s.Metrics.RealProviderCost = UnavailableMeasurement("tokens-or-currency", "scripted")
	FinalizeScenario(&s)
	return s
}

func cloneEvaluatorObservation(o evaluatorObservation) evaluatorObservation {
	// Mutation copies are detached from the normal output and the independent oracle.
	cloned := evaluatorObservation{Available: o.Available, CheckerKnown: o.CheckerKnown, AdapterError: o.AdapterError, ContextDelivered: o.ContextDelivered}
	raw, _ := json.Marshal(o.Records)
	_ = json.Unmarshal(raw, &cloned.Records)
	raw, _ = json.Marshal(o.Canonical)
	_ = json.Unmarshal(raw, &cloned.Canonical)
	raw, _ = json.Marshal(o.Body)
	_ = json.Unmarshal(raw, &cloned.Body)
	cloned.Body.Budget = o.Body.Budget
	return cloned
}

type evaluatorUnavailableSearch struct{}

func (evaluatorUnavailableSearch) Capabilities() memy.SearchCapabilities {
	return memy.SearchCapabilities{Scoped: true, Visibility: true, BoundedCandidates: true}
}
func (evaluatorUnavailableSearch) Search(context.Context, memy.Scope, preferenceQuery, memy.SearchOptions) (memy.SearchResult, error) {
	return memy.SearchResult{Coverage: []memy.Coverage{{Backend: "evaluator-unavailable/v1", Status: "unavailable"}}}, memy.ErrUnavailable
}

func evaluatorOutage(ctx context.Context, f *preferenceFixture, c Corpus, id string) error {
	_, err := memy.Recall(ctx, f.engine, "host", f.scope, preferenceQuery{[]string{id}}, evaluatorUnavailableSearch{}, memy.ScoreRanker[Preference, string]{}, memy.RecallOptions{Read: memy.ReadOptions{Purpose: "quality"}, Search: memy.SearchOptions{MaxCandidates: c.Budgets.MaxCandidates}, Limit: c.Budgets.RecallLimit})
	return err
}

func runEvaluator(ctx context.Context, c Corpus, run CaseRun) (ScenarioReport, error) {
	return runEvaluatorWithMarker(ctx, c, run, "")
}

func runEvaluatorWithMarker(ctx context.Context, c Corpus, run CaseRun, marker string) (ScenarioReport, error) {
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f, err := newPreferenceFixture(ctx, false, clock.Add(24*time.Hour))
	if err != nil {
		return ScenarioReport{}, err
	}
	defer f.close()
	value := "variant-a"
	if run.Seed%2 != 0 {
		value = "variant-b-with-negation"
	}
	value += marker
	valid := memy.Interval{Known: true, From: clock.Add(-time.Hour), To: clock.Add(12 * time.Hour)}
	parent, err := f.commit(ctx, "ancestor"+marker, preferencePayload("ancestor"+marker, false), valid, 0, memy.Append, nil)
	if err != nil {
		return ScenarioReport{}, err
	}
	lineage := []memy.RevisionRef{{RecordID: parent.ID, Revision: parent.Revision}}
	payload := preferencePayload(value, true)
	suggestion := f.suggestion(payload, valid)
	suggestion.Lineage = slices.Clone(lineage)
	proposal, err := f.engine.Remember(ctx, "host", f.scope, "evaluator-proposal", "quality", suggestion)
	if err != nil {
		return ScenarioReport{}, err
	}
	// Candidate checkpoint grades the real proposal against known inputs.
	candidateOK := reflect.DeepEqual(proposal.Suggestion, suggestion)
	applied, err := f.apply(ctx, "evaluated"+marker, proposal, 0, memy.Append, nil)
	if err != nil {
		return ScenarioReport{}, err
	}
	hostOK := applied.ID == "evaluated"+marker && applied.Revision == 1
	want := memy.Record[Preference, string]{ID: "evaluated" + marker, Revision: 1, Scope: f.scope, Payload: payload, Valid: valid, Provenance: memy.Provenance[string]{Sources: []memy.Source[string]{f.source}, Extractor: suggestion.Extractor, Evidence: suggestion.Evidence, Lineage: slices.Clone(lineage)}}
	records, body, err := f.recall(ctx, []string{want.ID}, memy.ReadOptions{}, c.Budgets.RecallLimit, c.Budgets.MaxCandidates, c.Budgets.ContextBytes)
	recallErr := err
	// Reopen the engine, then independently read canonical state via the public API.
	fresh, err := memy.New(f.config)
	if err != nil {
		return ScenarioReport{}, err
	}
	snapshot, err := fullSnapshot(fresh, ctx, "host", f.scope, memy.ReadOptions{Purpose: "quality"})
	if err != nil {
		return ScenarioReport{}, err
	}
	canonical := slices.DeleteFunc(snapshot, func(r memy.Record[Preference, string]) bool { return r.ID != want.ID })
	baseline := evaluatorObservation{Records: records, Body: body, Canonical: canonical, Available: !errors.Is(recallErr, memy.ErrUnavailable), CheckerKnown: true, AdapterError: recallErr, ContextDelivered: recallErr == nil}
	oracle := evaluatorOracle{Records: []memy.Record[Preference, string]{want}, Canonical: []memy.Record[Preference, string]{want}, Available: true}
	parentReport := evaluatorEvaluate("evaluator-self-check", run, baseline, oracle)
	parentReport.Candidate.Checks = []Check{evaluatorCheck("candidate_input", candidateOK, 1)}
	parentReport.HostReview.Checks = []Check{evaluatorCheck("accepted_receipt", hostOK, 1)}
	// Mutations require a verified real baseline. Valid small budgets may return
	// a metadata-only packed body, or even fail to fit that body. Neither result
	// supports indexing a projection or claiming the negative suite was run.
	ready := candidateOK && hostOK && recallErr == nil && len(baseline.Records) == 1 && len(baseline.Body.Projections) == 1 && evaluatorExact(baseline.Records, oracle.Records) && evaluatorExact(baseline.Canonical, oracle.Canonical) && evaluatorRendered(baseline.Body, oracle.Records)
	if !ready {
		parentReport.Diagnostic = "baseline_not_ready"
		for _, id := range evaluatorProbeIDs {
			parentReport.Execution.Checks = append(parentReport.Execution.Checks, Check{ID: "detected_" + id, Mandatory: true, Status: Unknown, Expected: "negative_observed", Observed: "not_attempted", Evidence: []Evidence{{Code: "probe_not_attempted", Count: 0}}})
		}
		FinalizeScenario(&parentReport)
		return parentReport, nil
	}
	for _, id := range evaluatorProbeIDs {
		obs := cloneEvaluatorObservation(baseline)
		expected := oracle
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
		case "false_empty_outage":
			failure := evaluatorOutage(ctx, f, c, want.ID)
			if !errors.Is(failure, memy.ErrUnavailable) {
				return ScenarioReport{}, memy.ErrSchema
			}
			// A faulty evaluator erases independent outage evidence into healthy empty.
			expected.Available = false
			expected.Records = nil
			obs.Available = true
			obs.Records = nil
			obs.Body.Projections = nil
		case "checker_error":
			obs.CheckerKnown = false
		case "adapter_error":
			obs.AdapterError = evaluatorOutage(ctx, f, c, want.ID)
			if !errors.Is(obs.AdapterError, memy.ErrUnavailable) {
				return ScenarioReport{}, memy.ErrSchema
			}
		case "late_privacy":
			_, err := fullForget(f.engine, ctx, "host", f.scope, "quality", memy.ForgetRequest{OperationID: "evaluator-forget", Selector: memy.Selector{Kind: memy.SelectRecord, ID: want.ID}, Reason: "synthetic privacy event", PolicyVersion: "forget/v1"})
			if err != nil {
				return ScenarioReport{}, err
			}
			post, err := fullSnapshot(fresh, ctx, "host", f.scope, memy.ReadOptions{Purpose: "quality"})
			if err != nil {
				return ScenarioReport{}, err
			}
			obs.Canonical = slices.DeleteFunc(post, func(r memy.Record[Preference, string]) bool { return r.ID != want.ID })
			expected.Records = nil
			expected.Canonical = nil
			// The leaked observations deliberately retain the previously accepted body.
		}
		// Keep the exact byte receipt consistent with mutated rendered bytes: a
		// semantic mutation must be rejected by the content oracle, not merely by
		// an accidentally stale cost receipt left over from the baseline.
		mutatedJSON, marshalErr := json.Marshal(obs.Body)
		if marshalErr != nil {
			return ScenarioReport{}, marshalErr
		}
		obs.Body.Budget.Used = uint64(len(mutatedJSON))
		probe := evaluatorEvaluate(id, run, obs, expected)
		if id == "false_empty_outage" || id == "adapter_error" {
			probe.Versions.Search = "evaluator-unavailable/v1"
		}
		probe.Candidate.Checks = slices.Clone(parentReport.Candidate.Checks)
		probe.HostReview.Checks = slices.Clone(parentReport.HostReview.Checks)
		FinalizeScenario(&probe)
		parentReport.Probes = append(parentReport.Probes, probe)
		negative := Report{Scenarios: []ScenarioReport{probe}}
		FinalizeReport(&negative)
		parentReport.Execution.Checks = append(parentReport.Execution.Checks, evaluatorCheck("detected_"+id, probe.Final != VerdictPass && negative.ExitCode() != 0, 1))
	}
	FinalizeScenario(&parentReport)
	return parentReport, nil
}
