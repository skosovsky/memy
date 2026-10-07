package checkpoint

import (
	"bytes"
	"context"
	"errors"
	"slices"

	"github.com/skosovsky/memy"
)

// Reader is the host-owned checkpoint read/invalidation boundary.
type Reader interface {
	Load(context.Context, memy.Scope, string) (Artifact, error)
	Invalidate(context.Context, memy.Scope, string) error
}

// ReadValidated returns checkpoint bytes only under the canonical lineage gate.
// Scope and authority come from authentication, never the checkpoint payload.
// All checkpoint I/O finishes before the canonical gate. The final callback
// performs no external I/O and does not recursively enter the engine.
func ReadValidated[P, R, A any](
	ctx context.Context,
	s Reader,
	e *memy.Engine[P, R, A],
	authority A,
	scope memy.Scope,
	purpose, key string,
) ([]byte, error) {
	a, err := s.Load(ctx, scope, key)
	if err != nil {
		return nil, err
	}
	a.Lineage = slices.Clone(a.Lineage)
	data := bytes.Clone(a.Data)
	expected, validation := Key(a)
	if validation != nil || expected != key || a.Scope != scope || len(data) == 0 {
		return nil, memy.ErrSchema
	}
	fence, err := e.Fence(ctx, authority, scope, purpose)
	if err != nil {
		return nil, err
	}
	err = e.WithDerivedWrite(ctx, authority, fence, purpose, a.Lineage, func(ctx context.Context) error {
		return ctx.Err()
	})
	if err != nil {
		return nil, errors.Join(err, s.Invalidate(ctx, scope, key))
	}
	return data, nil
}
