package memy

import (
	"context"
	"errors"
	"math"
	"slices"
)

func recallCoverage[P, R any](found SearchResult, minimum *VisibilityToken) (RecallResult[P, R], error) {
	result := RecallResult[P, R]{
		Records: make([]Ranked[P, R], 0),
		Coverage: slices.Clone(
			found.Coverage,
		),
		Progress: RecallProgress{
			ReturnedCandidates:  0,
			CanonicalChecked:    0,
			CanonicalFiltered:   0,
			RankingOmitted:      0,
			CandidatesTruncated: false,
		},
	}
	available := false
	for _, coverage := range found.Coverage {
		if coverage.Status != coverageUnavailable {
			available = true
		}
		if !validIdentifier(coverage.Backend) {
			return RecallResult[P, R]{}, ErrInvalid
		}
		switch coverage.Status {
		case "ready", "degraded", "pending", coverageUnavailable, "eventual":
		default:
			return RecallResult[P, R]{}, ErrInvalid
		}
	}
	if !available {
		return result, ErrUnavailable
	}
	for _, coverage := range found.Coverage {
		if minimum != nil && !coverage.MinimumSatisfied {
			return RecallResult[P, R]{
				Coverage: result.Coverage,
				Records:  nil,
				Progress: RecallProgress{
					ReturnedCandidates:  0,
					CanonicalChecked:    0,
					CanonicalFiltered:   0,
					RankingOmitted:      0,
					CandidatesTruncated: false,
				},
			}, ErrVisibilityPending
		}
	}
	return result, nil
}

func (e *Engine[P, R, A]) recallCandidates(
	ctx context.Context, b Bucket, scope Scope, candidates []Candidate, options ReadOptions,
) ([]Ranked[P, R], error) {
	records := make([]Ranked[P, R], 0, len(candidates))
	for _, candidate := range candidates {
		if !validIdentifier(candidate.RecordID) || candidate.Revision == 0 || candidate.Revision > MaxVersion ||
			candidate.Score.Validate() != nil {
			return nil, ErrInvalid
		}
		record, eligible, candidateErr := e.recallCandidate(ctx, b, scope, candidate, options)
		if candidateErr != nil {
			return nil, candidateErr
		}
		if eligible {
			records = append(
				records,
				Ranked[P, R]{
					Record:      record,
					Score:       candidate.Score,
					Explanation: "",
					Signals:     slices.Clone(candidate.Signals),
				},
			)
		}
	}
	return records, nil
}

func (e *Engine[P, R, A]) recallCandidate(
	ctx context.Context, b Bucket, scope Scope, candidate Candidate, options ReadOptions,
) (Record[P, R], bool, error) {
	var head recordDisk
	if _, readErr := readDocument(b, objectKey("head", candidate.RecordID), "record", &head); readErr != nil {
		if errors.Is(readErr, ErrNotFound) {
			return Record[P, R]{}, false, nil
		}
		return Record[P, R]{}, false, readErr
	}
	if head.Scope != scope || head.ID != candidate.RecordID {
		return Record[P, R]{}, false, ErrSchema
	}
	if head.State == Revoked {
		return Record[P, R]{}, false, nil
	}
	key := revisionKey(candidate.RecordID, candidate.Revision)
	value, readErr := b.Get(key)
	if readErr != nil {
		return Record[P, R]{}, false, readErr
	}
	if value.Data == nil {
		return Record[P, R]{}, false, nil
	}
	return e.snapshotCandidate(ctx, b, scope, Entry{Key: key, Value: value}, options)
}

// Ranking runs outside canonical transactions and receives detached consumer data.
func (e *Engine[P, R, A]) rankRecall(
	ctx context.Context, candidates []Ranked[P, R], ranker Ranker[P, R], limit int,
) ([]Ranked[P, R], error) {
	input := make([]Ranked[P, R], 0, len(candidates))
	eligible := make(map[RevisionRef]Ranked[P, R], len(candidates))
	for _, original := range candidates {
		eligible[RevisionRef{original.Record.ID, original.Record.Revision}] = original
		cloned, cloneErr := e.cloneRecord(original.Record)
		if cloneErr != nil {
			return nil, cloneErr
		}
		original.Record = cloned
		original.Signals = slices.Clone(original.Signals)
		input = append(input, original)
	}
	ranked, rankingErr := ranker.Rank(ctx, input)
	if rankingErr != nil {
		return nil, rankingErr
	}
	if len(ranked) > len(candidates) {
		return nil, ErrInvalid
	}
	selected := make([]Ranked[P, R], 0, min(limit, len(ranked)))
	for _, item := range ranked {
		if item.Score.Validate() != nil {
			return nil, ErrInvalid
		}
		ref := RevisionRef{item.Record.ID, item.Record.Revision}
		original, ok := eligible[ref]
		if !ok {
			return nil, ErrInvalid
		}
		delete(eligible, ref)
		original.Score, original.Explanation = item.Score, item.Explanation
		if len(selected) < limit {
			selected = append(selected, original)
		}
	}
	return selected, nil
}

func finiteScore(score float64) bool { return !math.IsNaN(score) && !math.IsInf(score, 0) }

func (e *Engine[P, R, A]) projectionRecord(
	ctx context.Context, b Bucket, scope Scope, id string, revision Version, options ReadOptions,
) (recordDisk, Record[P, R], error) {
	var head recordDisk
	if _, readErr := readDocument(b, objectKey("head", id), "record", &head); readErr != nil {
		return recordDisk{}, Record[P, R]{}, readErr
	}
	if head.Scope != scope || head.ID != id {
		return recordDisk{}, Record[P, R]{}, ErrSchema
	}
	if head.State == Revoked {
		return recordDisk{}, Record[P, R]{}, ErrNotFound
	}
	if revokeErr := validateRevocations(b, scope, id, head.Proposal.Sources); revokeErr != nil {
		return recordDisk{}, Record[P, R]{}, errors.Join(ErrNotFound, revokeErr)
	}
	var current recordDisk
	if _, readErr := readDocument(b, revisionKey(id, revision), "record", &current); readErr != nil {
		return recordDisk{}, Record[P, R]{}, readErr
	}
	if current.Scope != scope || current.ID != id || current.Revision != revision {
		return recordDisk{}, Record[P, R]{}, ErrSchema
	}
	state, allowed := readable(current, options, e.config.Clock.Now())
	if !allowed {
		return recordDisk{}, Record[P, R]{}, ErrNotFound
	}
	if err := e.validateStoredSources(ctx, b, scope, current.Proposal); err != nil {
		return recordDisk{}, Record[P, R]{}, err
	}
	if lineageErr := e.validateReadLineage(ctx, b, current.Lineage, scope); lineageErr != nil {
		return recordDisk{}, Record[P, R]{}, lineageErr
	}
	if retentionErr := e.validateRetention(ctx, scope, current.Proposal); retentionErr != nil {
		return recordDisk{}, Record[P, R]{}, retentionErr
	}
	typed, decodeErr := e.typedRecord(current, state)
	return current, typed, decodeErr
}

func (e *Engine[P, R, A]) completeRecall(
	ctx context.Context,
	b Bucket,
	authority A,
	scope Scope,
	purpose string,
	decision Decision,
	selected []Ranked[P, R],
) error {
	if err := e.reauthorize(ctx, authority, scope, ActionRead, purpose, decision); err != nil {
		return err
	}
	records := make([]Record[P, R], 0, len(selected))
	for _, item := range selected {
		records = append(records, item.Record)
	}
	return e.deliveryDeadlineGate(b, scope, records)
}

func boundedCoverage(coverage []Coverage) []Coverage {
	if len(coverage) > MaxSearchBackends {
		return nil
	}
	return slices.Clone(coverage)
}

// Validate rejects malformed metadata before canonical reads, without fusion.
func validateSearchResult(found SearchResult, _ int) error {
	if len(found.Coverage) < 1 || len(found.Coverage) > MaxSearchBackends {
		return ErrInvalid
	}
	backends := make(map[string]string, len(found.Coverage))
	for _, c := range found.Coverage {
		if !validIdentifier(c.Backend) || backends[c.Backend] != "" {
			return ErrInvalid
		}
		switch c.Status {
		case "ready", "degraded", "pending", coverageUnavailable, "eventual":
		default:
			return ErrInvalid
		}
		backends[c.Backend] = c.Status
	}
	refs := make(map[RevisionRef]bool, len(found.Candidates))
	for _, c := range found.Candidates {
		ref := RevisionRef{c.RecordID, c.Revision}
		if !validRef(ref) || c.Score.Validate() != nil || refs[ref] || len(c.Signals) < 1 ||
			len(c.Signals) > MaxSearchBackends {
			return ErrInvalid
		}
		refs[ref] = true
		signals := make(map[string]bool, len(c.Signals))
		for _, signal := range c.Signals {
			if backends[signal.Backend] == "" || backends[signal.Backend] == coverageUnavailable ||
				signals[signal.Backend] ||
				signal.Rank < 1 ||
				signal.Score.Validate() != nil {
				return ErrInvalid
			}
			signals[signal.Backend] = true
		}
	}
	return nil
}

// Validate checks exact revision identity without loading canonical data.
func (ref RevisionRef) Validate() error {
	if !validRef(ref) {
		return ErrInvalid
	}
	return nil
}
