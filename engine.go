package memy

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"
)

const maximumLineageRecords = 10_000

// Config binds the consumer types to executable host ports. All ports are
// required; none silently substitutes permissive authority or missing evidence.
type Config[P, R, A any] struct {
	Store          Store
	Authority      Authority[A]
	Sources        Sources[R]
	Clock          Clock
	Retention      RetentionPolicy[P]
	PayloadCodec   Codec[P]
	ReferenceCodec Codec[R]
	Sinks          []Sink
	Reintroduction ReintroductionPolicy[A]
}

// Engine coordinates typed knowledge lifecycle over a transactional store.
// It creates no workers; callers own scheduling and may call it concurrently.
type Engine[P, R, A any] struct{ config Config[P, R, A] }

// New verifies the configured capabilities before exposing a lifecycle engine.
func New[P, R, A any](config Config[P, R, A]) (*Engine[P, R, A], error) {
	if slices.ContainsFunc(
		[]any{
			config.Store,
			config.Authority,
			config.Sources,
			config.Clock,
			config.Retention,
			config.PayloadCodec,
			config.ReferenceCodec,
		},
		nilPort,
	) {
		return nil, ErrInvalid
	}
	caps := config.Store.Capabilities()
	if !caps.Atomic || !caps.ConditionalWrite {
		return nil, ErrUnsupported
	}
	if caps.SchemaVersion != 1 || config.PayloadCodec.Version() == "" || config.ReferenceCodec.Version() == "" {
		return nil, ErrSchema
	}
	names := make(map[string]bool)
	for _, sink := range config.Sinks {
		if nilPort(sink) || sink.Name() == "" || names[sink.Name()] {
			return nil, ErrInvalid
		}
		names[sink.Name()] = true
	}
	config.Sinks = slices.Clone(config.Sinks)
	return &Engine[P, R, A]{config: config}, nil
}

func nilPort(port any) bool {
	if port == nil {
		return true
	}
	value := reflect.ValueOf(port)
	kind := value.Kind()
	if kind == reflect.Chan || kind == reflect.Func || kind == reflect.Interface ||
		kind == reflect.Map || kind == reflect.Pointer || kind == reflect.Slice {
		return value.IsNil()
	}
	return false
}

func (e *Engine[P, R, A]) authorize(
	ctx context.Context,
	authority A,
	scope Scope,
	action Action,
	purpose string,
) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	if err := scope.Validate(); err != nil {
		return Decision{}, err
	}
	decision, operationErr := e.config.Authority.Check(
		ctx,
		authority,
		Access{Scope: scope, Action: action, Purpose: purpose},
	)
	if operationErr != nil {
		return Decision{}, errors.Join(ErrUnavailable, operationErr)
	}
	if !decision.Allowed || decision.Actor == "" || decision.PolicyVersion == "" || decision.Scope != scope ||
		(!decision.ExpiresAt.IsZero() && !e.config.Clock.Now().Before(decision.ExpiresAt)) {
		return Decision{}, ErrUnauthorized
	}
	if decision.Fields != nil {
		return Decision{}, ErrUnsupported
	}
	return decision, nil
}

func (e *Engine[P, R, A]) reauthorize(
	ctx context.Context,
	authority A,
	scope Scope,
	action Action,
	purpose string,
	original Decision,
) error {
	current, operationErr := e.authorize(ctx, authority, scope, action, purpose)
	if operationErr != nil {
		return operationErr
	}
	if current.Actor != original.Actor || current.PolicyVersion != original.PolicyVersion {
		return ErrStaleAcceptance
	}
	return nil
}

func (e *Engine[P, R, A]) encodeSuggestion(s Suggestion[P, R]) (proposalDisk, error) {
	if err := s.Valid.Validate(); err != nil {
		return proposalDisk{}, err
	}
	if len(s.Sources) == 0 || s.Evidence == "" || s.Extractor == "" {
		return proposalDisk{}, ErrMissingEvidence
	}
	payload, operationErr := e.config.PayloadCodec.Encode(s.Payload)
	if operationErr != nil {
		return proposalDisk{}, operationErr
	}
	if len(payload) == 0 {
		return proposalDisk{}, ErrInvalid
	}
	sources := make([]sourceDisk, 0, len(s.Sources))
	for _, source := range s.Sources {
		if source.ID == "" || source.Revision == "" {
			return proposalDisk{}, ErrMissingEvidence
		}
		reference, err := e.config.ReferenceCodec.Encode(source.Reference)
		if err != nil {
			return proposalDisk{}, err
		}
		if len(reference) == 0 {
			return proposalDisk{}, ErrInvalid
		}
		sources = append(sources, sourceDisk{ID: source.ID, Revision: source.Revision, Reference: reference})
	}
	return proposalDisk{
		Payload:        payload,
		PayloadCodec:   e.config.PayloadCodec.Version(),
		ReferenceCodec: e.config.ReferenceCodec.Version(),
		Sources:        sources,
		Evidence:       s.Evidence,
		Extractor:      s.Extractor,
		ObservedAt:     s.ObservedAt,
		Valid:          s.Valid,
		ExpiresAt:      s.ExpiresAt,
		Lineage: slices.Clone(
			s.Lineage,
		),
		Losses: slices.Clone(s.Losses),
		Uncertainties: slices.Clone(
			s.Uncertainties,
		),
		ID:        "",
		Revision:  0,
		Digest:    "",
		Scope:     Scope{Tenant: "", Namespace: "", Subject: ""},
		State:     "",
		CreatedAt: time.Time{},
		Epoch:     0,
		Retention: Retention{PolicyVersion: "", ExpiresAt: time.Time{}},
	}, nil
}

func (e *Engine[P, R, A]) decodeProposal(p proposalDisk) (Proposal[P, R], error) {
	var result Proposal[P, R]
	if p.PayloadCodec != e.config.PayloadCodec.Version() || p.ReferenceCodec != e.config.ReferenceCodec.Version() {
		return result, ErrSchema
	}
	if err := p.Valid.Validate(); err != nil {
		return result, errors.Join(ErrSchema, err)
	}
	if p.ID == "" || p.Revision == 0 || p.Scope.Validate() != nil {
		return result, ErrSchema
	}
	wantDigest, operationErr := proposalContentDigest(p)
	if operationErr != nil || wantDigest != p.Digest {
		return result, errors.Join(ErrSchema, operationErr)
	}
	payload, operationErr := e.config.PayloadCodec.Decode(p.Payload)
	if operationErr != nil {
		return result, errors.Join(ErrSchema, operationErr)
	}
	sources := make([]Source[R], 0, len(p.Sources))
	for _, source := range p.Sources {
		reference, err := e.config.ReferenceCodec.Decode(source.Reference)
		if err != nil {
			return result, errors.Join(ErrSchema, err)
		}
		sources = append(sources, Source[R]{ID: source.ID, Revision: source.Revision, Reference: reference})
	}
	result = Proposal[P, R]{
		ID:        p.ID,
		Revision:  p.Revision,
		Digest:    p.Digest,
		Scope:     p.Scope,
		State:     p.State,
		CreatedAt: p.CreatedAt,
		Epoch:     p.Epoch,
		Retention: p.Retention,
		Suggestion: Suggestion[P, R]{
			Payload:    payload,
			Sources:    sources,
			Evidence:   p.Evidence,
			Extractor:  p.Extractor,
			ObservedAt: p.ObservedAt,
			Valid:      p.Valid,
			ExpiresAt:  p.ExpiresAt,
			Lineage: slices.Clone(
				p.Lineage,
			),
			Losses:        slices.Clone(p.Losses),
			Uncertainties: slices.Clone(p.Uncertainties),
		},
	}
	return result, nil
}

func (e *Engine[P, R, A]) validateInputs(ctx context.Context, b Bucket, scope Scope, p proposalDisk) error {
	epoch, _, operationErr := currentEpoch(b)
	if operationErr != nil {
		return operationErr
	}
	if p.Epoch != epoch.Value {
		return ErrStaleInput
	}
	if p.Scope != scope {
		return ErrScopeViolation
	}
	if revocationErr := validateRevocations(b, scope, "", p.Sources); revocationErr != nil {
		return revocationErr
	}
	if proposalExpired(p, e.config.Clock.Now()) {
		return ErrStaleInput
	}
	decoded, decodeErr := e.decodeProposal(p)
	if decodeErr != nil {
		return decodeErr
	}
	if sourceErr := e.validateSourceInputs(ctx, b, scope, decoded.Suggestion.Sources); sourceErr != nil {
		return sourceErr
	}
	for _, ref := range p.Lineage {
		if lineageErr := validateLineageRevision(b, scope, ref); lineageErr != nil {
			return lineageErr
		}
	}
	if retentionErr := e.validateRetention(ctx, scope, p); retentionErr != nil {
		return retentionErr
	}
	if err := e.validateReadLineage(ctx, b, p.Lineage, scope); err != nil {
		return err
	}
	return e.deadlineGate(b, scope, []time.Time{proposalDeadline(p)}, p.Lineage)
}

func (e *Engine[P, R, A]) validateSourceInputs(ctx context.Context, b Bucket, scope Scope, sources []Source[R]) error {
	for _, source := range sources {
		value, readErr := b.Get(objectKey("revocation", string(SelectSource)+"/"+source.ID))
		if readErr != nil {
			return readErr
		}
		if value.Data != nil {
			return ErrRevoked
		}
		if sourceErr := e.config.Sources.Validate(ctx, scope, source); sourceErr != nil {
			return errors.Join(ErrSourceUnavailable, sourceErr)
		}
	}
	return nil
}

func validateLineageRevision(b Bucket, scope Scope, ref RevisionRef) error {
	var record recordDisk
	if _, readErr := readDocument(b, revisionKey(ref.RecordID, ref.Revision), "record", &record); readErr != nil {
		return errors.Join(ErrStaleInput, readErr)
	}
	if record.Scope != scope || record.ID != ref.RecordID || record.State != Active || record.Revision != ref.Revision {
		return ErrStaleInput
	}
	return nil
}

// Remember persists a single proposal without accepting or committing it.
// Retrying the same operation identity never creates a sibling proposal.
func (e *Engine[P, R, A]) Remember(
	ctx context.Context,
	authority A,
	scope Scope,
	operationID, purpose string,
	suggestion Suggestion[P, R],
) (Proposal[P, R], error) {
	decision, operationErr := e.authorize(ctx, authority, scope, ActionPropose, purpose)
	if operationErr != nil {
		return Proposal[P, R]{}, operationErr
	}
	encoded, operationErr := e.encodeSuggestion(suggestion)
	if operationErr != nil {
		return Proposal[P, R]{}, operationErr
	}
	requestDigest, operationErr := operationDigest(scope, decision.Actor, purpose, struct {
		Scope      Scope
		Suggestion proposalDisk
	}{scope, encoded})
	if operationErr != nil {
		return Proposal[P, R]{}, operationErr
	}
	var result Proposal[P, R]
	operationErr = e.config.Store.Update(ctx, scope, func(b Bucket) error {
		op, opVersion, exists, transactionErr := operation(b, operationID, "remember", requestDigest)
		if transactionErr != nil {
			return transactionErr
		}
		if exists {
			result, transactionErr = e.singleProposalReplay(ctx, b, scope, op)
			if transactionErr != nil {
				return transactionErr
			}
			return e.reauthorize(ctx, authority, scope, ActionPropose, purpose, decision)
		}
		encoded.ID, _ = digest(struct {
			Scope     Scope
			Operation string
		}{scope, operationID})
		encoded.Revision = 1
		encoded, transactionErr = e.prepareProposal(ctx, b, scope, encoded)
		if transactionErr != nil {
			return transactionErr
		}
		if err := e.reauthorize(ctx, authority, scope, ActionPropose, purpose, decision); err != nil {
			return err
		}
		if saveErr := saveProposalOperation(
			b,
			operationID,
			"remember",
			requestDigest,
			opVersion,
			0,
			encoded,
		); saveErr != nil {
			return saveErr
		}
		result, transactionErr = e.decodeProposal(encoded)
		return transactionErr
	})
	if operationErr != nil {
		return Proposal[P, R]{}, operationErr
	}
	return result, nil
}

// Proposal returns one authorized detached proposal. Expired candidates are
// reported explicitly and never interpreted as accepted facts.
func (e *Engine[P, R, A]) Proposal(
	ctx context.Context,
	authority A,
	scope Scope,
	id, purpose string,
) (Proposal[P, R], error) {
	decision, operationErr := e.authorize(ctx, authority, scope, ActionRead, purpose)
	if operationErr != nil {
		return Proposal[P, R]{}, operationErr
	}
	var result Proposal[P, R]
	operationErr = e.config.Store.View(ctx, scope, func(b Bucket) error {
		var stored proposalDisk
		if _, err := readDocument(b, objectKey("proposal", id), "proposal", &stored); err != nil {
			return err
		}
		if stored.Scope != scope || stored.ID != id {
			return ErrSchema
		}
		if retentionErr := e.validateRetention(ctx, scope, stored); retentionErr != nil {
			return retentionErr
		}
		var transactionErr error
		result, transactionErr = e.decodeProposal(stored)
		if transactionErr != nil {
			return transactionErr
		}
		if (!stored.ExpiresAt.IsZero() && !e.config.Clock.Now().Before(stored.ExpiresAt)) ||
			(!stored.Retention.ExpiresAt.IsZero() && !e.config.Clock.Now().Before(stored.Retention.ExpiresAt)) {
			result.State = Expired
		}
		return e.reauthorize(ctx, authority, scope, ActionRead, purpose, decision)
	})
	if operationErr != nil {
		return Proposal[P, R]{}, operationErr
	}
	return result, nil
}

// Accept is a separate host-authorized review of exactly one digest/revision.
func (e *Engine[P, R, A]) Accept(
	ctx context.Context,
	authority A,
	scope Scope,
	id, proposalDigest string,
	revision Version,
	purpose string,
) (Acceptance, error) {
	decision, operationErr := e.authorize(ctx, authority, scope, ActionAccept, purpose)
	if operationErr != nil {
		return Acceptance{}, operationErr
	}
	var result Acceptance
	operationErr = e.config.Store.Update(ctx, scope, func(b Bucket) error {
		var p proposalDisk
		version, transactionErr := readDocument(b, objectKey("proposal", id), "proposal", &p)
		if transactionErr != nil {
			return transactionErr
		}
		if p.Scope != scope || p.ID != id {
			return ErrSchema
		}
		if p.Digest != proposalDigest || p.Revision != revision || (p.State != Proposed && p.State != Accepted) {
			return ErrStaleAcceptance
		}
		if err := e.validateInputs(ctx, b, scope, p); err != nil {
			return err
		}
		if err := e.reauthorize(ctx, authority, scope, ActionAccept, purpose, decision); err != nil {
			return err
		}
		if err := e.deadlineGate(
			b,
			scope,
			[]time.Time{proposalDeadline(p), decision.ExpiresAt},
			p.Lineage,
		); err != nil {
			return err
		}
		result = Acceptance{ProposalID: id, ProposalRevision: revision, Digest: proposalDigest, Actor: decision.Actor,
			Scope: scope, PolicyVersion: decision.PolicyVersion, ExpiresAt: decision.ExpiresAt}
		return persistAcceptance(b, p, version, result)
	})
	if operationErr != nil {
		return Acceptance{}, operationErr
	}
	return result, nil
}

// Reject closes a proposal without creating canonical memory.
func (e *Engine[P, R, A]) Reject(
	ctx context.Context,
	authority A,
	scope Scope,
	id, proposalDigest, purpose string,
) error {
	decision, operationErr := e.authorize(ctx, authority, scope, ActionAccept, purpose)
	if operationErr != nil {
		return operationErr
	}
	return e.config.Store.Update(ctx, scope, func(b Bucket) error {
		var p proposalDisk
		version, transactionErr := readDocument(b, objectKey("proposal", id), "proposal", &p)
		if transactionErr != nil {
			return transactionErr
		}
		if p.Digest != proposalDigest || p.Scope != scope || p.ID != id {
			return ErrStaleAcceptance
		}
		if err := e.reauthorize(ctx, authority, scope, ActionAccept, purpose, decision); err != nil {
			return err
		}
		if p.State == Rejected {
			return nil
		}
		p.State = Rejected
		transactionErr = writeDocument(b, objectKey("proposal", id), "proposal", version, p)
		return transactionErr
	})
}

// Commit conditionally appends a canonical revision and its durable receipt.
// The supplied acceptance must match a separately persisted host decision.
func (e *Engine[P, R, A]) Commit(
	ctx context.Context,
	authority A,
	scope Scope,
	purpose string,
	request CommitRequest,
) (CommitReceipt, error) {
	decision, operationErr := e.authorize(ctx, authority, scope, ActionCommit, purpose)
	if operationErr != nil {
		return CommitReceipt{}, operationErr
	}
	if request.RecordID == "" || request.ProposalID == "" || request.Expected >= MaxVersion {
		return CommitReceipt{}, ErrInvalid
	}
	requestDigest, operationErr := operationDigest(scope, decision.Actor, purpose, request)
	if operationErr != nil {
		return CommitReceipt{}, operationErr
	}
	var result CommitReceipt
	operationErr = e.config.Store.Update(ctx, scope, func(b Bucket) error {
		receipt, commitErr := e.commitOperation(ctx, b, authority, scope, purpose, decision, request, requestDigest)
		result = receipt
		return commitErr
	})
	if operationErr != nil {
		return CommitReceipt{}, operationErr
	}
	return result, nil
}

func (e *Engine[P, R, A]) typedRecord(stored recordDisk, state RecordState) (Record[P, R], error) {
	proposal, operationErr := e.decodeProposal(stored.Proposal)
	if operationErr != nil {
		return Record[P, R]{}, operationErr
	}
	s := proposal.Suggestion
	return Record[P, R]{
		ID:       stored.ID,
		Revision: stored.Revision,
		Scope:    stored.Scope,
		Payload:  s.Payload,
		State:    state,
		Provenance: Provenance[R]{
			Sources:   s.Sources,
			Extractor: s.Extractor,
			Evidence:  s.Evidence,
			Lineage: slices.Clone(
				stored.Lineage,
			),
			Losses:        s.Losses,
			Uncertainties: s.Uncertainties,
		},
		ObservedAt:    s.ObservedAt,
		RecordedAt:    stored.RecordedAt,
		Valid:         s.Valid,
		Retention:     stored.Proposal.Retention,
		ExpiresAt:     proposalDeadline(stored.Proposal),
		PolicyVersion: stored.PolicyVersion,
		Epoch:         stored.Proposal.Epoch,
		Related:       slices.Clone(stored.Related),
	}, nil
}

// Get returns a canonical revision selected using independent temporal
// predicates. Historical reads cannot bypass present revocation or authority.
func (e *Engine[P, R, A]) Get(
	ctx context.Context,
	authority A,
	scope Scope,
	id string,
	options ReadOptions,
) (Record[P, R], error) {
	decision, operationErr := e.authorize(ctx, authority, scope, ActionRead, options.Purpose)
	if operationErr != nil {
		return Record[P, R]{}, operationErr
	}
	var result Record[P, R]
	operationErr = e.config.Store.View(ctx, scope, func(b Bucket) error {
		selected, readErr := e.selectCanonical(ctx, b, scope, id, options)
		if readErr != nil {
			return readErr
		}
		result = selected
		if err := e.reauthorize(ctx, authority, scope, ActionRead, options.Purpose, decision); err != nil {
			return err
		}
		return e.deliveryDeadlineGate(b, scope, []Record[P, R]{selected})
	})
	if operationErr != nil {
		return Record[P, R]{}, operationErr
	}
	return result, nil
}

func (e *Engine[P, R, A]) selectCanonical(
	ctx context.Context,
	b Bucket,
	scope Scope,
	id string,
	options ReadOptions,
) (Record[P, R], error) {
	var head recordDisk
	if _, err := readDocument(b, objectKey("head", id), "record", &head); err != nil {
		return Record[P, R]{}, err
	}
	if head.Scope != scope || head.ID != id {
		return Record[P, R]{}, ErrSchema
	}
	if head.State == Revoked {
		return Record[P, R]{}, ErrNotFound
	}
	entries, operationErr := b.List(objectKey("record", id) + "/")
	if operationErr != nil {
		return Record[P, R]{}, operationErr
	}
	var selected *Record[P, R]
	for _, entry := range slices.Backward(entries) {
		candidate, eligible, candidateErr := e.canonicalCandidate(ctx, b, scope, id, entry, options)
		if candidateErr != nil {
			return Record[P, R]{}, candidateErr
		}
		if !eligible {
			continue
		}
		if selected != nil {
			return Record[P, R]{}, ErrUnresolvedConflict
		}
		selected = &candidate
	}
	if selected == nil {
		return Record[P, R]{}, ErrNotFound
	}
	return *selected, nil
}

func (e *Engine[P, R, A]) canonicalCandidate(
	ctx context.Context,
	b Bucket,
	scope Scope,
	id string,
	entry Entry,
	options ReadOptions,
) (Record[P, R], bool, error) {
	var candidate recordDisk
	if err := decodeDocument(entry.Value.Data, "record", &candidate); err != nil {
		return Record[P, R]{}, false, err
	}
	if candidate.Scope != scope || candidate.ID != id || entry.Key != revisionKey(candidate.ID, candidate.Revision) {
		return Record[P, R]{}, false, ErrSchema
	}
	if err := validateRevocations(b, scope, id, candidate.Proposal.Sources); err != nil {
		if errors.Is(err, ErrRevoked) {
			return Record[P, R]{}, false, ErrNotFound
		}
		return Record[P, R]{}, false, err
	}
	state, allowed := readable(candidate, options, e.config.Clock.Now())
	if !allowed {
		return Record[P, R]{}, false, nil
	}
	if err := e.validateStoredSources(ctx, b, scope, candidate.Proposal); err != nil {
		return Record[P, R]{}, false, err
	}
	if err := e.validateReadLineage(ctx, b, candidate.Lineage, scope); err != nil {
		return Record[P, R]{}, false, err
	}
	if retentionErr := e.validateRetention(ctx, scope, candidate.Proposal); retentionErr != nil {
		return Record[P, R]{}, false, retentionErr
	}
	typed, operationErr := e.typedRecord(candidate, state)
	return typed, operationErr == nil, operationErr
}

func readable(r recordDisk, options ReadOptions, now time.Time) (RecordState, bool) {
	if proposalExpired(r.Proposal, now) {
		return r.State, false
	}
	if !options.RecordedAsOf.IsZero() && r.RecordedAt.After(options.RecordedAsOf) {
		return r.State, false
	}
	state := r.State
	if !options.RecordedAsOf.IsZero() {
		state = r.InitialState
		for _, change := range r.Transitions {
			if !change.At.After(options.RecordedAsOf) {
				state = change.State
			}
		}
	}
	if state == Revoked || state == Superseded || (state == Conflicted && !options.IncludeConflicts) {
		return state, false
	}
	if !options.ValidAsOf.IsZero() && !r.Proposal.Valid.Contains(options.ValidAsOf) {
		if r.Proposal.Valid.Known || !options.IncludeUnknown {
			return state, false
		}
	}
	return state, true
}

func (e *Engine[P, R, A]) validateReadLineage(ctx context.Context, b Bucket, refs []RevisionRef, scope Scope) error {
	pending := slices.Clone(refs)
	seen := make(map[RevisionRef]bool)
	for len(pending) != 0 {
		ref := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[ref] {
			continue
		}
		seen[ref] = true
		if len(seen) > maximumLineageRecords {
			return ErrBudget
		}
		source, err := e.liveLineageInput(ctx, b, ref, scope)
		if err != nil {
			return err
		}
		pending = append(pending, source.Lineage...)
	}
	return e.deadlineGate(b, scope, nil, refs)
}

func proposalDeadline(p proposalDisk) time.Time {
	deadline := p.Retention.ExpiresAt
	if deadline.IsZero() || (!p.ExpiresAt.IsZero() && p.ExpiresAt.Before(deadline)) {
		return p.ExpiresAt
	}
	return deadline
}

func proposalExpired(p proposalDisk, now time.Time) bool {
	deadline := proposalDeadline(p)
	return !deadline.IsZero() && !now.Before(deadline)
}

func (e *Engine[P, R, A]) liveLineageInput(
	ctx context.Context,
	b Bucket,
	ref RevisionRef,
	scope Scope,
) (recordDisk, error) {
	var source recordDisk
	if _, err := readDocument(b, objectKey("head", ref.RecordID), "record", &source); err != nil {
		return recordDisk{}, errors.Join(ErrStaleInput, err)
	}
	if source.Scope != scope || source.ID != ref.RecordID || source.State != Active || source.Revision != ref.Revision {
		return recordDisk{}, ErrStaleInput
	}
	if proposalExpired(source.Proposal, e.config.Clock.Now()) {
		return recordDisk{}, ErrStaleInput
	}
	if err := validateRevocations(b, scope, source.ID, source.Proposal.Sources); err != nil {
		return recordDisk{}, errors.Join(ErrStaleInput, err)
	}
	if retentionErr := e.validateRetention(ctx, scope, source.Proposal); retentionErr != nil {
		return recordDisk{}, retentionErr
	}
	if err := e.validateStoredSources(ctx, b, scope, source.Proposal); err != nil {
		return recordDisk{}, err
	}
	if proposalExpired(source.Proposal, e.config.Clock.Now()) {
		return recordDisk{}, ErrStaleInput
	}
	return source, nil
}

// String describes the engine without exposing consumer payload or authority.
func (e *Engine[P, R, A]) String() string {
	return fmt.Sprintf(
		"memy(schema=%d,durable=%t)",
		e.config.Store.Capabilities().SchemaVersion,
		e.config.Store.Capabilities().Durable,
	)
}

func (e *Engine[P, R, A]) validateStoredSources(ctx context.Context, b Bucket, scope Scope, p proposalDisk) error {
	decoded, err := e.decodeProposal(p)
	if err != nil {
		return err
	}
	return e.validateSourceInputs(ctx, b, scope, decoded.Suggestion.Sources)
}
