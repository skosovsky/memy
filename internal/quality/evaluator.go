package quality

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"time"

	"github.com/skosovsky/memy"
)

// Privacy runs last so its real revocation cannot alter the other isolated
// observation probes, all of which retain the independent baseline snapshot.
const evaluatorValiditySpan = 12 * time.Hour

func evaluatorProbeIDs() []string {
	return []string{
		"wrong_payload",
		"wrong_revision",
		"missing_lineage",
		probeFalseEmptyOutage,
		"checker_error",
		probeAdapterError,
		"late_privacy",
	}
}

func evaluatorVersions() PortVersions {
	v := preferenceVersions()
	v.Provider, v.Model, v.Consolidation = "manual-remember/v1", portNone, portNone
	return v
}

func evaluatorPlans() []CasePlan {
	required := []RequiredCheck{
		{stageCandidate, "candidate_input"},
		{stageHostReview, "accepted_receipt"},
		{stageEffective, "effective_exact"},
		{stageCanonical, "canonical_exact"},
		{stageRendered, "rendered_exact"},
		{stageExecution, checkpointCompleted},
	}
	for _, id := range evaluatorProbeIDs() {
		required = append(required, RequiredCheck{stageExecution, "detected_" + id})
	}
	return []CasePlan{
		{
			ID:             "evaluator-self-check",
			Domain:         evaluatorDomain,
			Version:        "evaluator-fixtures/v2",
			Groups:         []string{evaluatorDomain},
			Versions:       evaluatorVersions(),
			Required:       required,
			OptionalStages: nil,
			Run:            runEvaluator,
		},
	}
}

// RunProbe exposes a normal negative report through the same aggregator and exit
// contract as a consumer run. Only the fixed synthetic mutation IDs are allowed.
func RunProbe(ctx context.Context, c Corpus, id string) (Report, error) {
	if err := validateCorpus(c); err != nil {
		return FailureReport(err), err
	}
	if !slices.Contains(evaluatorProbeIDs(), id) {
		return FailureReport(memy.ErrInvalid), memy.ErrInvalid
	}
	s, err := runEvaluator(ctx, c, CaseRun{Seed: DerivedSeed(c.Seed, "evaluator-self-check", 0), Repeat: 0})
	if err != nil {
		return FailureReport(err), err
	}
	for _, probe := range s.Probes {
		if probe.ID == id {
			r := Report{
				Schema:     reportSchema,
				Versions:   probe.Versions,
				Scenarios:  []ScenarioReport{probe},
				Manifest:   nil,
				Final:      "",
				Diagnostic: "",
			}
			FinalizeReport(&r)
			return r, nil
		}
	}
	if s.Final != VerdictPass && len(s.Probes) == 0 {
		// An insufficient baseline cannot support a mutation demonstration. Keep
		// its actual failed/unknown checkpoints instead of inventing a probe.
		r := Report{
			Schema:     reportSchema,
			Versions:   s.Versions,
			Scenarios:  []ScenarioReport{s},
			Manifest:   nil,
			Final:      "",
			Diagnostic: "",
		}
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

type evaluatorUnavailableSearch struct{}

func (evaluatorUnavailableSearch) Capabilities() memy.SearchCapabilities {
	return memy.SearchCapabilities{Scoped: true, Visibility: true, BoundedCandidates: true}
}

func (evaluatorUnavailableSearch) Search(
	context.Context,
	memy.Scope,
	preferenceQuery,
	memy.SearchOptions,
) (memy.SearchResult, error) {
	return memy.SearchResult{
		Coverage: []memy.Coverage{
			{Backend: "evaluator-unavailable/v1", Status: measurementUnavailable, MinimumSatisfied: false},
		},
		Candidates:          nil,
		CandidatesTruncated: false,
	}, memy.ErrUnavailable
}

func evaluatorOutage(ctx context.Context, f *preferenceFixture, c Corpus, id string) error {
	_, err := memy.Recall(
		ctx,
		f.engine,
		"host",
		f.scope,
		preferenceQuery{[]string{id}},
		evaluatorUnavailableSearch{},
		memy.ScoreRanker[Preference, string]{},
		memy.RecallOptions{
			Read: memy.ReadOptions{
				Purpose:          qualityPurpose,
				ValidAsOf:        time.Time{},
				RecordedAsOf:     time.Time{},
				IncludeUnknown:   false,
				IncludeConflicts: false,
			},
			Search: memy.SearchOptions{MaxCandidates: c.Budgets.MaxCandidates, Minimum: nil},
			Limit:  c.Budgets.RecallLimit,
		},
	)
	return err
}

func runEvaluator(ctx context.Context, c Corpus, run CaseRun) (ScenarioReport, error) {
	return runEvaluatorWithMarker(ctx, c, run, "")
}

func runEvaluatorWithMarker(ctx context.Context, c Corpus, run CaseRun, marker string) (ScenarioReport, error) {
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f, err := newPreferenceFixture(ctx, false, clock.Add(fixtureDay))
	if err != nil {
		return ScenarioReport{}, err
	}
	defer f.close()
	value := "variant-a"
	if run.Seed%2 != 0 {
		value = "variant-b-with-negation"
	}
	value += marker
	valid := memy.Interval{Known: true, From: clock.Add(-time.Hour), To: clock.Add(evaluatorValiditySpan)}
	parent, err := f.commit(
		ctx,
		"ancestor"+marker,
		preferencePayload("ancestor"+marker, false),
		valid,
		0,
		memy.Append,
		nil,
	)
	if err != nil {
		return ScenarioReport{}, err
	}
	lineage := []memy.RevisionRef{{RecordID: parent.ID, Revision: parent.Revision}}
	payload := preferencePayload(value, true)
	suggestion := f.suggestion(payload, valid)
	suggestion.Lineage = slices.Clone(lineage)
	proposal, err := f.engine.Remember(ctx, "host", f.scope, "evaluator-proposal", qualityPurpose, suggestion)
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
	want := memy.Record[Preference, string]{
		ID:       "evaluated" + marker,
		Revision: 1,
		Scope:    f.scope,
		Payload:  payload,
		Valid:    valid,
		Provenance: memy.Provenance[string]{
			Sources:   []memy.Source[string]{f.source},
			Extractor: suggestion.Extractor,
			Evidence:  suggestion.Evidence,
			Lineage:   slices.Clone(lineage), Losses: nil, Uncertainties: nil,
		},
		State:                  "",
		ObservedAt:             time.Time{},
		RecordedAt:             time.Time{},
		Retention:              memy.Retention{PolicyVersion: "", ExpiresAt: time.Time{}},
		ExpiresAt:              time.Time{},
		AuthorityPolicyVersion: "",
		Epoch:                  0,
		Reconciliation:         nil,
	}
	fresh, baseline, err := observeEvaluatorBaseline(ctx, f, c, want)
	if err != nil {
		return ScenarioReport{}, err
	}
	oracle := evaluatorOracle{
		Records:   []memy.Record[Preference, string]{want},
		Canonical: []memy.Record[Preference, string]{want},
		Available: true,
	}
	parentReport := evaluatorEvaluate("evaluator-self-check", run, baseline, oracle)
	parentReport.Candidate.Checks = []Check{evaluatorCheck("candidate_input", candidateOK, 1)}
	parentReport.HostReview.Checks = []Check{evaluatorCheck("accepted_receipt", hostOK, 1)}
	// Mutations require a verified real baseline. Valid small budgets may return
	// a metadata-only packed body, or even fail to fit that body. Neither result
	// supports indexing a projection or claiming the negative suite was run.
	ready := candidateOK && hostOK && baseline.AdapterError == nil && len(baseline.Records) == 1 &&
		len(baseline.Body.Projections) == 1 &&
		evaluatorExact(baseline.Records, oracle.Records) &&
		evaluatorExact(baseline.Canonical, oracle.Canonical) &&
		evaluatorRendered(baseline.Body, oracle.Records)
	if !ready {
		parentReport.Diagnostic = "baseline_not_ready"
		appendUnattemptedProbes(&parentReport)
		FinalizeScenario(&parentReport)
		return parentReport, nil
	}
	if err := runEvaluatorMutations(ctx, f, fresh, c, run, want.ID, baseline, oracle, &parentReport); err != nil {
		return ScenarioReport{}, err
	}
	FinalizeScenario(&parentReport)
	return parentReport, nil
}

func observeEvaluatorBaseline(
	ctx context.Context,
	f *preferenceFixture,
	c Corpus,
	want memy.Record[Preference, string],
) (*memy.Engine[Preference, string, string], evaluatorObservation, error) {
	records, body, err := f.recall(
		ctx,
		[]string{want.ID},
		memy.ReadOptions{
			Purpose:          "",
			ValidAsOf:        time.Time{},
			RecordedAsOf:     time.Time{},
			IncludeUnknown:   false,
			IncludeConflicts: false,
		},
		c.Budgets.RecallLimit,
		c.Budgets.MaxCandidates,
		c.Budgets.ContextBytes,
	)
	recallErr := err
	// Reopen the engine, then independently read canonical state via the public API.
	fresh, err := memy.New(f.config)
	if err != nil {
		return nil, evaluatorObservation{}, err
	}
	snapshot, err := fullSnapshot(
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
		return nil, evaluatorObservation{}, err
	}
	canonical := slices.DeleteFunc(snapshot, func(r memy.Record[Preference, string]) bool { return r.ID != want.ID })
	baseline := evaluatorObservation{
		Records:          records,
		Body:             body,
		Canonical:        canonical,
		Available:        !errors.Is(recallErr, memy.ErrUnavailable),
		CheckerKnown:     true,
		AdapterError:     recallErr,
		ContextDelivered: recallErr == nil,
	}
	return fresh, baseline, nil
}
