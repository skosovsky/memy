package memy

import (
	"context"
	"errors"
	"math"
	"slices"
	"time"
)

// ConsolidationMode is an opt-in proposal policy; none auto-applies knowledge.
type ConsolidationMode string

const (
	ExactDedup    ConsolidationMode = "exact_dedup"
	DomainMerge   ConsolidationMode = "domain_merge"
	SemanticMerge ConsolidationMode = "semantic_merge"
)

// Budget limits complete encoded record/suggestion content and provider cost units.
type Budget struct {
	InputBytes  int
	OutputBytes int
	CostUnits   int64
}

// ConsolidationRequest binds exact scoped revisions to a versioned policy.
type ConsolidationRequest struct {
	OperationID    string
	Purpose        string
	PolicyVersion  string
	Mode           ConsolidationMode
	Inputs         []RevisionRef
	Budget         Budget
	MinimumUtility float64
}

// MergeResult makes provider cost, usefulness and potential losses reviewable.
type MergeResult[P, R any] struct {
	Suggestions []Suggestion[P, R]
	CostUnits   int64
	Utility     float64
}

// Consolidator supplies domain or semantic merge in consumer-owned types.
type Consolidator[P, R any] interface {
	Merge(context.Context, []Record[P, R], Budget) (MergeResult[P, R], error)
}

// Snapshot returns typed eligible canonical state under the host read profile.
// It never includes revoked payload, and unsupported field masks fail closed.
func (e *Engine[P, R, A]) Snapshot(
	ctx context.Context,
	authority A,
	scope Scope,
	options ReadOptions,
) ([]Record[P, R], error) {
	decision, operationErr := e.authorize(ctx, authority, scope, ActionRead, options.Purpose)
	if operationErr != nil {
		return nil, operationErr
	}
	result := make([]Record[P, R], 0)
	operationErr = e.config.Store.View(ctx, scope, func(b Bucket) error {
		entries, transactionErr := b.List("record/")
		if transactionErr != nil {
			return transactionErr
		}
		for _, entry := range entries {
			record, eligible, candidateErr := e.snapshotCandidate(ctx, b, scope, entry, options)
			if candidateErr != nil {
				return candidateErr
			}
			if eligible {
				result = append(result, record)
			}
		}
		if err := e.reauthorize(ctx, authority, scope, ActionRead, options.Purpose, decision); err != nil {
			return err
		}
		return e.deliveryDeadlineGate(b, scope, result)
	})
	if operationErr != nil {
		return nil, operationErr
	}
	return result, nil
}

func (e *Engine[P, R, A]) snapshotCandidate(
	ctx context.Context,
	b Bucket,
	scope Scope,
	entry Entry,
	options ReadOptions,
) (Record[P, R], bool, error) {
	var stored recordDisk
	if decodeErr := decodeDocument(entry.Value.Data, "record", &stored); decodeErr != nil {
		return Record[P, R]{}, false, decodeErr
	}
	if stored.Scope != scope || entry.Key != revisionKey(stored.ID, stored.Revision) {
		return Record[P, R]{}, false, ErrSchema
	}
	if revocationErr := validateRevocations(b, scope, stored.ID, stored.Proposal.Sources); revocationErr != nil {
		if errors.Is(revocationErr, ErrRevoked) {
			return Record[P, R]{}, false, nil
		}
		return Record[P, R]{}, false, revocationErr
	}
	state, allowed := readable(stored, options, e.config.Clock.Now())
	if !allowed {
		return Record[P, R]{}, false, nil
	}
	if err := e.validateStoredSources(ctx, b, scope, stored.Proposal); err != nil {
		return Record[P, R]{}, false, err
	}
	if lineageErr := e.validateReadLineage(ctx, b, stored.Lineage, scope); lineageErr != nil {
		if ineligibleLineage(lineageErr) {
			return Record[P, R]{}, false, nil
		}
		return Record[P, R]{}, false, lineageErr
	}
	if retentionErr := e.validateRetention(ctx, scope, stored.Proposal); retentionErr != nil {
		return Record[P, R]{}, false, retentionErr
	}
	typed, decodeErr := e.typedRecord(stored, state)
	return typed, decodeErr == nil, decodeErr
}

// Consolidate prepares proposals from exact scoped canonical inputs. Exact
// dedup uses no provider. Domain/semantic merge runs an injected typed provider.
// Outputs always need separate host acceptance; originals are never removed.
func Consolidate[P, R, A any](
	ctx context.Context,
	e *Engine[P, R, A],
	authority A,
	scope Scope,
	request ConsolidationRequest,
	provider Consolidator[P, R],
) ([]Proposal[P, R], error) {
	if e == nil || request.PolicyVersion == "" || len(request.Inputs) < 2 || len(request.Inputs) > 256 ||
		request.Budget.InputBytes <= 0 || request.Budget.OutputBytes <= 0 || request.Budget.CostUnits < 0 ||
		math.IsNaN(request.MinimumUtility) || math.IsInf(request.MinimumUtility, 0) {
		return nil, ErrInvalid
	}
	decision, operationErr := e.authorize(ctx, authority, scope, ActionConsolidate, request.Purpose)
	if operationErr != nil {
		return nil, operationErr
	}
	if modeErr := consolidationMode(request.Mode, nilPort(provider)); modeErr != nil {
		return nil, modeErr
	}
	// Recover completed proposals before re-evaluating live inputs. Idempotent
	// replay returns the original content revision until its purge boundary.
	requestDigest, operationErr := consolidationDigest(scope, decision.Actor, request)
	if operationErr != nil {
		return nil, operationErr
	}
	var replay []Proposal[P, R]
	var completed bool
	operationErr = e.config.Store.View(ctx, scope, func(b Bucket) error {
		op, _, exists, transactionErr := operation(b, request.OperationID, "extract", requestDigest)
		if transactionErr != nil {
			return transactionErr
		}
		if !exists {
			return nil
		}
		if _, err := e.authorize(ctx, authority, scope, ActionRead, request.Purpose); err != nil {
			return err
		}
		if _, err := e.authorize(ctx, authority, scope, ActionPropose, request.Purpose); err != nil {
			return err
		}
		replay, transactionErr = e.loadProposals(ctx, b, scope, op.Proposals)
		if transactionErr != nil {
			return transactionErr
		}
		completed = true
		return e.reauthorize(ctx, authority, scope, ActionConsolidate, request.Purpose, decision)
	})
	if operationErr != nil {
		return nil, operationErr
	}
	if completed {
		return replay, nil
	}
	state, operationErr := e.Snapshot(
		ctx,
		authority,
		scope,
		ReadOptions{
			Purpose:          request.Purpose,
			ValidAsOf:        time.Time{},
			RecordedAsOf:     time.Time{},
			IncludeUnknown:   false,
			IncludeConflicts: false,
		},
	)
	if operationErr != nil {
		return nil, operationErr
	}
	inputs, inputErr := e.consolidationInputs(state, request)
	if inputErr != nil {
		return nil, inputErr
	}
	script := extractionMerge[P, R, A]{
		engine:    e,
		authority: authority,
		scope:     scope,
		request:   request,
		decision:  decision,
		inputs:    inputs,
		provider:  provider,
	}
	return Extract(
		ctx,
		e,
		authority,
		scope,
		ExtractionJob{
			OperationID:     request.OperationID,
			ProviderVersion: request.PolicyVersion,
			Purpose:         request.Purpose,
		},
		request,
		JSONCodec[ConsolidationRequest]{},
		script,
	)
}

func (e *Engine[P, R, A]) consolidationInputs(
	state []Record[P, R], request ConsolidationRequest,
) ([]Record[P, R], error) {
	available := make(map[RevisionRef]Record[P, R], len(state))
	for _, record := range state {
		available[RevisionRef{record.ID, record.Revision}] = record
	}
	inputs := make([]Record[P, R], 0, len(request.Inputs))
	seen := make(map[RevisionRef]bool)
	inputBytes := 0
	for _, ref := range request.Inputs {
		if seen[ref] {
			return nil, ErrInvalid
		}
		seen[ref] = true
		record, ok := available[ref]
		if !ok || record.State != Active {
			return nil, ErrStaleInput
		}
		payload, err := e.budgetRecord(record)
		if err != nil {
			return nil, err
		}
		if len(payload) > request.Budget.InputBytes-inputBytes {
			return nil, ErrBudget
		}
		inputBytes += len(payload)
		inputs = append(inputs, record)
	}
	return inputs, nil
}

type extractionMerge[P, R, A any] struct {
	engine    *Engine[P, R, A]
	authority A
	scope     Scope
	request   ConsolidationRequest
	decision  Decision
	inputs    []Record[P, R]
	provider  Consolidator[P, R]
}

func (s extractionMerge[P, R, A]) Extract(ctx context.Context, _ ConsolidationRequest) ([]Suggestion[P, R], error) {
	result, operationErr := s.merge(ctx)
	if operationErr != nil {
		return nil, operationErr
	}
	if result.CostUnits < 0 || result.CostUnits > s.request.Budget.CostUnits {
		return nil, ErrBudget
	}
	if math.IsNaN(result.Utility) || math.IsInf(result.Utility, 0) || result.Utility < s.request.MinimumUtility {
		return nil, ErrPolicyDenied
	}
	if len(result.Suggestions) == 0 {
		return nil, ErrMissingEvidence
	}
	if preparationErr := s.prepareSuggestions(&result); preparationErr != nil {
		return nil, preparationErr
	}
	if err := s.engine.reauthorize(
		ctx,
		s.authority,
		s.scope,
		ActionConsolidate,
		s.request.Purpose,
		s.decision,
	); err != nil {
		return nil, err
	}
	return result.Suggestions, nil
}

func (s extractionMerge[P, R, A]) merge(ctx context.Context) (MergeResult[P, R], error) {
	var result MergeResult[P, R]
	var operationErr error
	if s.request.Mode == ExactDedup {
		result, operationErr = exactDedup(s.engine, s.inputs)
	} else {
		providerInput := make([]Record[P, R], 0, len(s.inputs))
		for _, original := range s.inputs {
			cloned, cloneErr := s.engine.cloneRecord(original)
			if cloneErr != nil {
				return MergeResult[P, R]{}, cloneErr
			}
			providerInput = append(providerInput, cloned)
		}
		result, operationErr = s.provider.Merge(ctx, providerInput, s.request.Budget)
	}
	return result, operationErr
}

func (s extractionMerge[P, R, A]) prepareSuggestions(result *MergeResult[P, R]) error {
	outputBytes := 0
	for i := range result.Suggestions {
		p := &result.Suggestions[i]
		s.stampSuggestion(p)
		encoded, err := s.engine.encodeSuggestion(*p)
		if err != nil {
			return err
		}
		raw, err := encodeDocument("suggestion", encoded)
		if err != nil {
			return err
		}
		if len(raw) > s.request.Budget.OutputBytes-outputBytes {
			return ErrBudget
		}
		outputBytes += len(raw)
	}
	return nil
}

func exactDedup[P, R, A any](e *Engine[P, R, A], inputs []Record[P, R]) (MergeResult[P, R], error) {
	groups := make(map[string][]Record[P, R])
	for _, record := range inputs {
		payload, err := e.config.PayloadCodec.Encode(record.Payload)
		if err != nil {
			return MergeResult[P, R]{}, err
		}
		key, err := digest(struct {
			Payload []byte
			Valid   Interval
			Scope   Scope
		}{payload, record.Valid, record.Scope})
		if err != nil {
			return MergeResult[P, R]{}, err
		}
		groups[key] = append(groups[key], record)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := MergeResult[P, R]{Utility: 1, Suggestions: nil, CostUnits: 0}
	for _, key := range keys {
		group := groups[key]
		if len(group) < 2 {
			continue
		}
		p := Suggestion[P, R]{
			Payload:       group[0].Payload,
			Valid:         group[0].Valid,
			ObservedAt:    group[0].ObservedAt,
			Evidence:      "exact canonical payload and valid interval equality; originals retained",
			Extractor:     "exact-dedup/v1",
			Sources:       nil,
			ExpiresAt:     time.Time{},
			Lineage:       nil,
			Losses:        nil,
			Uncertainties: nil,
		}
		seenSources := make(map[string]bool)
		for _, record := range group {
			p.Lineage = append(p.Lineage, RevisionRef{RecordID: record.ID, Revision: record.Revision})
			p.Losses = append(p.Losses, record.Provenance.Losses...)
			p.Uncertainties = append(p.Uncertainties, record.Provenance.Uncertainties...)
			if sourceErr := e.dedupSources(&p, seenSources, record.Provenance.Sources); sourceErr != nil {
				return result, sourceErr
			}
		}
		result.Suggestions = append(result.Suggestions, p)
	}
	return result, nil
}

func (e *Engine[P, R, A]) dedupSources(
	proposal *Suggestion[P, R], seenSources map[string]bool, sources []Source[R],
) error {
	for _, source := range sources {
		reference, err := e.config.ReferenceCodec.Encode(source.Reference)
		if err != nil {
			return err
		}
		sourceKey, err := digest(struct {
			ID        string
			Revision  string
			Reference []byte
		}{source.ID, source.Revision, reference})
		if err != nil {
			return err
		}
		if !seenSources[sourceKey] {
			proposal.Sources = append(proposal.Sources, source)
			seenSources[sourceKey] = true
		}
	}
	return nil
}

func consolidationMode(mode ConsolidationMode, missingProvider bool) error {
	switch mode {
	case ExactDedup:
	case DomainMerge, SemanticMerge:
		if missingProvider {
			return ErrUnsupported
		}
	default:
		return ErrUnsupported
	}
	return nil
}

func consolidationDigest(scope Scope, actor string, request ConsolidationRequest) (string, error) {
	encodedRequest, operationErr := (JSONCodec[ConsolidationRequest]{}).Encode(request)
	if operationErr != nil {
		return "", operationErr
	}
	return operationDigest(scope, actor, request.Purpose, struct {
		Input    []byte
		Codec    string
		Provider string
		Scope    Scope
	}{encodedRequest, "json/v1", request.PolicyVersion, scope})
}

func (e *Engine[P, R, A]) budgetRecord(r Record[P, R]) ([]byte, error) {
	p, err := e.encodeSuggestion(Suggestion[P, R]{
		Payload: r.Payload, Sources: r.Provenance.Sources, Evidence: r.Provenance.Evidence,
		Extractor: r.Provenance.Extractor, ObservedAt: r.ObservedAt, Valid: r.Valid,
		ExpiresAt: r.ExpiresAt, Lineage: r.Provenance.Lineage, Losses: r.Provenance.Losses,
		Uncertainties: r.Provenance.Uncertainties,
	})
	if err != nil {
		return nil, err
	}
	p.Scope, p.Epoch, p.Retention = r.Scope, r.Epoch, r.Retention
	return encodeDocument("record", recordDisk{
		ID: r.ID, Revision: r.Revision, Scope: r.Scope, Proposal: p, State: r.State,
		InitialState: r.State, RecordedAt: r.RecordedAt, PolicyVersion: r.PolicyVersion,
		Related: r.Related, Lineage: r.Provenance.Lineage, Transitions: nil,
	})
}

func (s extractionMerge[P, R, A]) stampSuggestion(p *Suggestion[P, R]) {
	// Mandatory canonical lineage is stamped by core, never entrusted to a
	// semantic provider. Exact dedup keeps the narrower group lineage.
	if s.request.Mode != ExactDedup {
		p.Lineage = slices.Clone(s.request.Inputs)
	}
	if len(p.Sources) == 0 {
		for _, record := range s.inputs {
			p.Sources = append(p.Sources, record.Provenance.Sources...)
		}
	}
	for _, record := range s.inputs {
		if !record.ExpiresAt.IsZero() &&
			(p.ExpiresAt.IsZero() || record.ExpiresAt.Before(p.ExpiresAt)) {
			p.ExpiresAt = record.ExpiresAt
		}
	}
	p.Extractor = s.request.PolicyVersion
}
