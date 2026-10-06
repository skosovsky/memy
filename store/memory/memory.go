// Package memory supplies a nondurable reference store with atomic scoped
// transactions. Its state is lost when closed or the process exits.
package memory

import (
	"context"
	"crypto/rand"
	"sync"
	"sync/atomic"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/internal/kv"
)

type scopeState struct {
	gate       chan struct{}
	values     map[string]memy.Value
	index      kv.Index
	generation memy.Version
}

// Store isolates transactions by scope. Its mutex only protects the scope
// registry; host callbacks never execute while holding that global mutex.
type Store struct {
	mu     sync.Mutex
	once   sync.Once
	closed atomic.Bool
	scopes map[string]*scopeState
	secret [32]byte
}

// New returns an empty store. No goroutines are created.
func New() *Store {
	s := &Store{scopes: make(map[string]*scopeState)}
	_, _ = rand.Read(s.secret[:])
	return s
}

// Capabilities describes the in-process guarantees; Durable is false.
func (*Store) Capabilities() memy.StoreCapabilities {
	return memy.StoreCapabilities{Atomic: true, ConditionalWrite: true, FencedView: true, SchemaVersion: memy.SchemaVersion, Durable: false}
}

func (s *Store) acquire(ctx context.Context, scope memy.Scope) (*scopeState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.closed.Load() {
		s.mu.Unlock()
		return nil, memy.ErrClosed
	}
	state := s.scopes[scope.Key()]
	if state == nil {
		state = &scopeState{gate: make(chan struct{}, 1), values: make(map[string]memy.Value)}
		s.scopes[scope.Key()] = state
	}
	s.mu.Unlock()
	select {
	case state.gate <- struct{}{}:
		if s.closed.Load() {
			<-state.gate
			return nil, memy.ErrClosed
		}
		return state, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// View reads consistent addressed values without copying unrelated state.
func (s *Store) View(ctx context.Context, scope memy.Scope, fn func(memy.Bucket) error) error {
	return s.run(ctx, scope, false, fn)
}

// FencedView holds exact-scope exclusion through synchronous callback completion.
func (s *Store) FencedView(ctx context.Context, scope memy.Scope, fn func(memy.Bucket) error) error {
	return s.run(ctx, scope, false, fn)
}

// Update commits touched keys only after a successful callback.
func (s *Store) Update(ctx context.Context, scope memy.Scope, fn func(memy.Bucket) error) error {
	return s.run(ctx, scope, true, fn)
}

func (s *Store) run(ctx context.Context, scope memy.Scope, writable bool, fn func(memy.Bucket) error) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if fn == nil {
		return memy.ErrInvalid
	}
	state, err := s.acquire(ctx, scope)
	if err != nil {
		return err
	}
	defer func() { <-state.gate }()
	bucket := kv.New(ctx, state.values, &state.index, kv.Binding{Secret: s.secret[:], Scope: scope.Key(), Generation: state.generation}, writable)
	defer bucket.Seal()
	if err := fn(bucket); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if writable {
		bucket.Commit()
		state.generation = bucket.Binding.Generation
	}
	return nil
}

// Close prevents new operations, waits for started scoped callbacks and releases
// references. A callback must not recursively close or operate on this store.
func (s *Store) Close() error {
	s.once.Do(func() {
		s.mu.Lock()
		s.closed.Store(true)
		states := s.scopes
		s.scopes = nil
		s.mu.Unlock()
		for _, state := range states {
			state.gate <- struct{}{}
			state.values = nil
			<-state.gate
		}
	})
	return nil
}
