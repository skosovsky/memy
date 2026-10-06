package quality

import (
	"encoding/json"
	"reflect"
	"slices"
	"time"

	"github.com/skosovsky/memy"
)

func preferenceSame(got, want memy.Record[Preference, string]) bool {
	return got.ID == want.ID && got.Revision == want.Revision && reflect.DeepEqual(got.Payload, want.Payload) &&
		reflect.DeepEqual(got.Valid, want.Valid) &&
		reflect.DeepEqual(got.Provenance.Sources, want.Provenance.Sources) &&
		reflect.DeepEqual(got.Provenance.Lineage, want.Provenance.Lineage) &&
		got.Scope == want.Scope
}

func preferenceSetSame(got, want []memy.Record[Preference, string]) bool {
	if len(got) != len(want) {
		return false
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			if preferenceSame(g, w) {
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

func preferenceDelivered(
	r *preferenceRunResult,
	records, want []memy.Record[Preference, string],
	body memy.ProjectedRecallResult[Preference, string],
) {
	r.check(
		stageEffective,
		"exact_revisions_payload_validity_sources_lineage",
		preferenceSetSame(records, want),
		len(records),
	)
	ok := len(body.Projections) == len(want) && len(body.Omissions) == 0
	for _, w := range want {
		found := false
		for _, p := range body.Projections {
			if p.RecordID == w.ID && p.Revision == w.Revision && reflect.DeepEqual(p.Output, w.Payload) &&
				reflect.DeepEqual(p.Provenance.Sources, w.Provenance.Sources) &&
				reflect.DeepEqual(p.Provenance.Lineage, w.Provenance.Lineage) &&
				p.Scope == w.Scope {
				found = true
				break
			}
		}
		ok = ok && found
	}
	encoded, err := json.Marshal(body)
	r.check(
		stageRendered,
		"exact_projection_body_and_budget",
		ok && err == nil && body.Budget.Exact && body.Budget.Used == uint64(len(encoded)) &&
			body.Budget.Used <= body.Budget.Limit,
		len(body.Projections),
	)
	r.ContextBytes = uint64(len(encoded))
	for _, record := range records {
		b, _ := json.Marshal(record.Payload)
		r.PayloadBytes += uint64(len(b))
	}
}

func preferenceRefsEqual(a, b []memy.RevisionRef) bool {
	if len(a) != len(b) {
		return false
	}
	for _, r := range a {
		if !slices.Contains(b, r) {
			return false
		}
	}
	return true
}

func preferenceCandidate(
	out *preferenceRunResult,
	proposal memy.Proposal[Preference, string],
	originals []memy.Record[Preference, string],
	inputs []memy.RevisionRef,
	valid memy.Interval,
	source memy.Source[string],
	mode memy.ConsolidationMode,
) bool {
	expectedLineage := slices.Clone(inputs)
	if mode == memy.ExactDedup {
		expectedLineage = nil
		for _, r := range originals {
			if r.ID != preferenceNegativeID {
				expectedLineage = append(expectedLineage, memy.RevisionRef{RecordID: r.ID, Revision: r.Revision})
			}
		}
	}
	good := proposal.Suggestion.Valid == valid &&
		slices.Equal(proposal.Suggestion.Sources, []memy.Source[string]{source}) &&
		preferenceRefsEqual(proposal.Suggestion.Lineage, expectedLineage)
	if mode == memy.ExactDedup {
		good = good && reflect.DeepEqual(proposal.Suggestion.Payload, preferencePayload(preferenceMorning, false))
	} else {
		good = good &&
			reflect.DeepEqual(
				proposal.Suggestion.Payload,
				Preference{
					Facts: []PreferenceFact{
						{Key: preferenceDrink, Value: preferenceTea, Qualifier: preferenceMorning, Negated: false},
						{Key: preferenceDrink, Value: preferenceTea, Qualifier: preferenceEvening, Negated: true},
					},
				},
			)
	}
	out.CandidateBad = !good
	out.check(preferenceCandidateStage, "facts_negation_validity_sources_lineage", good, 1)
	out.check(preferenceCandidateStage, "valid_interval_preserved", proposal.Suggestion.Valid == valid, 1)
	out.check(
		preferenceCandidateStage,
		preferenceSourceIdentityCheck,
		slices.Equal(proposal.Suggestion.Sources, []memy.Source[string]{source}),
		len(proposal.Suggestion.Sources),
	)
	out.check(
		preferenceCandidateStage,
		preferenceInputLineageCheck,
		preferenceRefsEqual(proposal.Suggestion.Lineage, expectedLineage),
		len(proposal.Suggestion.Lineage),
	)
	negationPresent := slices.ContainsFunc(
		proposal.Suggestion.Payload.Facts,
		func(f PreferenceFact) bool {
			return f.Value == preferenceTea && f.Qualifier == preferenceEvening && f.Negated
		},
	)
	out.check(
		preferenceCandidateStage,
		"negation_preserved",
		mode == memy.ExactDedup || negationPresent,
		len(proposal.Suggestion.Payload.Facts),
	)
	return good
}

func preferenceReadOptions(validAsOf, recordedAsOf time.Time, includeUnknown, includeConflicts bool) memy.ReadOptions {
	return memy.ReadOptions{
		Purpose:          preferencePurpose,
		ValidAsOf:        validAsOf,
		RecordedAsOf:     recordedAsOf,
		IncludeUnknown:   includeUnknown,
		IncludeConflicts: includeConflicts,
	}
}

func preferenceMetrics(id string, observed preferenceRunResult, err error) Metrics {
	var metrics Metrics
	if err == nil {
		metrics.PayloadBytes = KnownMeasurement("bytes/json-payload", observed.PayloadBytes)
		metrics.ContextJSONBytes = KnownMeasurement("bytes/json-context", observed.ContextBytes)
	} else {
		metrics.PayloadBytes = UnavailableMeasurement("bytes/json-payload", "execution_incomplete")
		metrics.ContextJSONBytes = UnavailableMeasurement("bytes/json-context", "execution_incomplete")
	}
	metrics.CanonicalBytes = UnavailableMeasurement(
		"bytes/serialized-canonical",
		"not_measured_adapter_storage_excluded",
	)
	cost := uint64(0)
	if id == "preference-consolidation-domain" || id == preferenceSemanticID {
		cost = 1
	}
	if err == nil {
		metrics.ProviderCost = KnownMeasurement("scripted-cost-units", cost)
	} else {
		metrics.ProviderCost = UnavailableMeasurement("scripted-cost-units", "execution_incomplete")
	}
	metrics.RealProviderCost = UnavailableMeasurement("tokens-or-currency", "scripted_provider")
	return metrics
}

func preferenceOriginalsPreserved(
	out *preferenceRunResult,
	before, after []memy.Record[Preference, string],
	scope memy.Scope,
	source memy.Source[string],
	originalCount int,
) {
	originalsEqual := true
	for _, record := range before {
		originalsEqual = originalsEqual &&
			slices.ContainsFunc(
				after,
				func(r memy.Record[Preference, string]) bool { return reflect.DeepEqual(r, record) },
			)
	}
	out.check(
		stageCanonical,
		"full_original_documents_preserved",
		originalsEqual && len(before) == originalCount,
		len(before),
	)
	out.check(
		stageCanonical,
		"scope_sources_and_exact_lineage",
		!slices.ContainsFunc(after, func(r memy.Record[Preference, string]) bool {
			return r.Scope != scope || !slices.Equal(r.Provenance.Sources, []memy.Source[string]{source})
		}),
		len(after),
	)
}
