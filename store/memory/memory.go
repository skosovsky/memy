// Package memory supplies a nondurable reference store with atomic scoped
// transactions. Its state is lost when closed or the process exits.
package memory

import (
	"context"
	"sync"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/internal/kv"
)

// Store serializes updates using a context-aware process-local gate.
type Store struct {
	gate   chan struct{}
	once   sync.Once
	closed bool
	scopes map[string]map[string]memy.Value
}

// New returns an empty store. No goroutines are created.
func New() *Store {
	return &Store{
		gate:   make(chan struct{}, 1),
		scopes: make(map[string]map[string]memy.Value),
		once:   sync.Once{},
		closed: false,
	}
}

// Capabilities describes the in-process guarantees; Durable is false.
func (*Store) Capabilities() memy.StoreCapabilities {
	return memy.StoreCapabilities{Atomic: true, ConditionalWrite: true, SchemaVersion: memy.SchemaVersion, Durable: false}
}

func (s *Store) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case s.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// View reads a consistent detached snapshot.
func (s *Store) View(ctx context.Context, scope memy.Scope, fn func(memy.Bucket) error) error {
	return s.run(ctx, scope, false, fn)
}

// Update commits the whole callback only on success.
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
	if err := s.acquire(ctx); err != nil {
		return err
	}
	defer func() { <-s.gate }()
	if s.closed {
		return memy.ErrClosed
	}
	bucket := kv.New(ctx, kv.Clone(s.scopes[scope.Key()]), writable)
	defer bucket.Seal()
	if err := fn(bucket); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if writable {
		s.scopes[scope.Key()] = bucket.Values
	}
	return nil
}

// Close prevents further operations and releases references to all values.
func (s *Store) Close() error {
	s.once.Do(func() {
		s.gate <- struct{}{}
		defer func() { <-s.gate }()
		s.closed = true
		s.scopes = nil
	})
	return nil
}
