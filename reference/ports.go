// Package reference supplies deterministic offline host/provider adapters.
// They are runnable reference implementations, not vendor integrations.
package reference

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/skosovsky/memy"
)

type policyKey struct {
	actor string
	scope memy.Scope
}
type grant struct {
	version string
	actions map[memy.Action]bool
	fields  []string
}

// Policy applies explicit grants to a consumer-owned authenticated identity.
// Identity must derive from host authentication rather than provider content.
type Policy[A any] struct {
	mu       sync.RWMutex
	identity func(A) string
	grants   map[policyKey]grant
	failure  error
}

// NewPolicy starts with no grants.
func NewPolicy[A any](identity func(A) string) *Policy[A] {
	return &Policy[A]{identity: identity, grants: make(map[policyKey]grant), mu: sync.RWMutex{}, failure: nil}
}

// Grant replaces the exact actor/scope policy. Every change should use a new
// version so outstanding acceptances are invalidated.
func (p *Policy[A]) Grant(actor string, scope memy.Scope, version string, actions ...memy.Action) {
	p.mu.Lock()
	defer p.mu.Unlock()
	allowed := make(map[memy.Action]bool, len(actions))
	for _, action := range actions {
		allowed[action] = true
	}
	p.grants[policyKey{actor, scope}] = grant{version: version, actions: allowed, fields: nil}
}

// RestrictFields declares a profile unsupported by the v1 engine. Empty nonnil
// fields also represents a restricted profile rather than full access.
func (p *Policy[A]) RestrictFields(actor string, scope memy.Scope, fields []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := policyKey{actor, scope}
	g := p.grants[key]
	if fields == nil {
		g.fields = nil
	} else {
		g.fields = append([]string{}, fields...)
	}
	p.grants[key] = g
}

// Fail injects an authority outage; nil restores service.
func (p *Policy[A]) Fail(err error) { p.mu.Lock(); defer p.mu.Unlock(); p.failure = err }

// Check evaluates an exact actor/scope/action grant. No existence is disclosed.
func (p *Policy[A]) Check(ctx context.Context, authority A, access memy.Access) (memy.Decision, error) {
	if err := ctx.Err(); err != nil {
		return memy.Decision{}, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.failure != nil {
		return memy.Decision{}, p.failure
	}
	if p.identity == nil {
		return memy.Decision{}, memy.ErrInvalid
	}
	actor := p.identity(authority)
	g := p.grants[policyKey{actor, access.Scope}]
	var fields []string
	if g.fields != nil {
		fields = append([]string{}, g.fields...)
	}
	return memy.Decision{
		Allowed:       g.actions[access.Action],
		Actor:         actor,
		Scope:         access.Scope,
		PolicyVersion: g.version,
		Fields:        fields, ExpiresAt: time.Time{},
	}, nil
}

type sourceKey struct {
	scope memy.Scope
	id    string
}
type registeredSource struct {
	revision  string
	reference []byte
}

// Registry validates exact source identity, revision and typed reference.
type Registry[R any] struct {
	mu      sync.RWMutex
	codec   memy.Codec[R]
	values  map[sourceKey]registeredSource
	failure error
}

// NewRegistry starts empty; missing sources fail explicitly.
func NewRegistry[R any](codec memy.Codec[R]) *Registry[R] {
	return &Registry[R]{
		codec:   codec,
		values:  make(map[sourceKey]registeredSource),
		mu:      sync.RWMutex{},
		failure: nil,
	}
}

// Put establishes the host's current source revision.
func (r *Registry[R]) Put(scope memy.Scope, source memy.Source[R]) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if source.ID == "" || source.Revision == "" || r.codec == nil {
		return memy.ErrInvalid
	}
	encoded, operationErr := r.codec.Encode(source.Reference)
	if operationErr != nil {
		return operationErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values[sourceKey{scope, source.ID}] = registeredSource{source.Revision, bytes.Clone(encoded)}
	return nil
}

// Remove makes a source unavailable to subsequent proposals/acceptances.
func (r *Registry[R]) Remove(scope memy.Scope, id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.values, sourceKey{scope, id})
}

// Fail injects a source service outage; nil restores service.
func (r *Registry[R]) Fail(err error) { r.mu.Lock(); defer r.mu.Unlock(); r.failure = err }

// Validate checks source reference and current revision without network calls.
func (r *Registry[R]) Validate(ctx context.Context, scope memy.Scope, source memy.Source[R]) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	encoded, operationErr := r.codec.Encode(source.Reference)
	if operationErr != nil {
		return operationErr
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.failure != nil {
		return r.failure
	}
	registered, ok := r.values[sourceKey{scope, source.ID}]
	if !ok {
		return memy.ErrSourceUnavailable
	}
	if registered.revision != source.Revision || !bytes.Equal(registered.reference, encoded) {
		return memy.ErrStaleInput
	}
	return nil
}

// Clock is a thread-safe deterministic clock for fixtures and offline examples.
type Clock struct {
	mu  sync.RWMutex
	now time.Time
}

// NewClock starts at an explicit timestamp.
func NewClock(at time.Time) *Clock {
	return &Clock{now: at.UTC(), mu: sync.RWMutex{}}
}

// Now returns a detached timestamp.
func (c *Clock) Now() time.Time { c.mu.RLock(); defer c.mu.RUnlock(); return c.now }

// Set sets a fixture timestamp, including deliberate clock regressions.
func (c *Clock) Set(at time.Time) { c.mu.Lock(); defer c.mu.Unlock(); c.now = at.UTC() }

// Advance moves the fixture clock by a duration.
func (c *Clock) Advance(by time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.now = c.now.Add(by) }

// SystemClock uses the host clock for examples outside deterministic fixtures.
type SystemClock struct{}

// Now returns current UTC time.
func (SystemClock) Now() time.Time { return time.Now().UTC() }

// Retain provides an explicit versioned fixed expiry policy for caller types.
type Retain[P any] struct {
	Version   string
	ExpiresAt time.Time
}

// Evaluate returns the configured policy without inspecting or mutating payload.
func (r Retain[P]) Evaluate(ctx context.Context, _ memy.Scope, _ P) (memy.Retention, error) {
	if err := ctx.Err(); err != nil {
		return memy.Retention{}, err
	}
	if r.Version == "" {
		return memy.Retention{}, memy.ErrPolicyDenied
	}
	return memy.Retention{PolicyVersion: r.Version, ExpiresAt: r.ExpiresAt}, nil
}

// ExtractorFunc adapts a scripted typed function to the extraction port.
type ExtractorFunc[I, P, R any] func(context.Context, I) ([]memy.Suggestion[P, R], error)

// Extract executes the script synchronously with the caller's context.
func (f ExtractorFunc[I, P, R]) Extract(ctx context.Context, input I) ([]memy.Suggestion[P, R], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f == nil {
		return nil, memy.ErrInvalid
	}
	return f(ctx, input)
}

// MergeFunc adapts a typed deterministic domain or scripted semantic provider.
type MergeFunc[P, R any] func(context.Context, []memy.Record[P, R], memy.Budget) (memy.MergeResult[P, R], error)

// Merge executes an injected script; its outputs remain unaccepted proposals.
func (f MergeFunc[P, R]) Merge(
	ctx context.Context,
	inputs []memy.Record[P, R],
	budget memy.Budget,
) (memy.MergeResult[P, R], error) {
	if err := ctx.Err(); err != nil {
		return memy.MergeResult[P, R]{}, err
	}
	if f == nil {
		return memy.MergeResult[P, R]{}, memy.ErrInvalid
	}
	return f(ctx, inputs, budget)
}

// ReintroductionFunc adapts the explicit host tombstone-retirement policy.
type ReintroductionFunc[A any] func(context.Context, A, memy.Scope, memy.ReintroductionRequest) error

// Allow evaluates the host's versioned reintroduction decision.
func (f ReintroductionFunc[A]) Allow(
	ctx context.Context,
	authority A,
	scope memy.Scope,
	request memy.ReintroductionRequest,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if f == nil {
		return memy.ErrPolicyDenied
	}
	return f(ctx, authority, scope, request)
}

// ResolverFunc adapts typed host claim mapping and reconciliation policy.
// The host resolves once and retains its CommitRequest for operation replay.
type ResolverFunc[P, R, K any] func(context.Context, K, memy.Proposal[P, R], []memy.Record[P, R]) (memy.CommitTarget, error)

// Resolve executes the host's explicit policy without interpreting embeddings.
func (f ResolverFunc[P, R, K]) Resolve(
	ctx context.Context,
	key K,
	proposal memy.Proposal[P, R],
	records []memy.Record[P, R],
) (memy.CommitTarget, error) {
	if err := ctx.Err(); err != nil {
		return memy.CommitTarget{}, err
	}
	if f == nil {
		return memy.CommitTarget{}, memy.ErrInvalid
	}
	return f(ctx, key, proposal, records)
}
