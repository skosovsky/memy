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
	users      int // callbacks and gate waiters; protected by Store.mu
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
	s := &Store{
		scopes: make(map[string]*scopeState),
		mu:     sync.Mutex{},
		once:   sync.Once{},
		closed: atomic.Bool{},
		secret: [32]byte{},
	}
	_, _ = rand.Read(s.secret[:])
	return s
}

// Capabilities describes the in-process guarantees; Durable is false.
func (*Store) Capabilities() memy.StoreCapabilities {
	return memy.StoreCapabilities{
		Atomic:           true,
		ConditionalWrite: true,
		FencedView:       true,
		SchemaVersion:    memy.SchemaVersion,
		Durable:          false,
	}
}

func (s *Store) acquire(ctx context.Context, key string) (*scopeState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.closed.Load() {
		s.mu.Unlock()
		return nil, memy.ErrClosed
	}
	state := s.scopes[key]
	if state == nil {
		state = &scopeState{
			gate:       make(chan struct{}, 1),
			users:      0,
			values:     make(map[string]memy.Value),
			index:      kv.Index{},
			generation: 0,
		}
		s.scopes[key] = state
	}
	state.users++
	s.mu.Unlock()
	select {
	case state.gate <- struct{}{}:
		if s.closed.Load() {
			s.release(key, state, true)
			return nil, memy.ErrClosed
		}
		return state, nil
	case <-ctx.Done():
		s.release(key, state, false)
		return nil, ctx.Err()
	}
}

// release retains the registered gate until the last callback or waiter leaves.
// A successful write advances generation, including deletion of an absent key;
// such scopes retain their CAS tombstones and cursor generation until Close.
func (s *Store) release(key string, state *scopeState, held bool) {
	if held {
		<-state.gate
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state.users--
	if !s.closed.Load() && state.users == 0 && state.generation == 0 && len(state.values) == 0 {
		delete(s.scopes, key)
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
	key := scope.Key()
	state, err := s.acquire(ctx, key)
	if err != nil {
		return err
	}
	defer s.release(key, state, true)
	bucket := kv.New(
		ctx,
		state.values,
		&state.index,
		kv.Binding{Secret: s.secret[:], Scope: key, Generation: state.generation},
		writable,
	)
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
			state.index = kv.Index{}
			<-state.gate
		}
	})
	return nil
}
